package launcher

// Settled admission at the launcher (INTEGRATION.md §11.10; settled-
// admission ruling 01, QUESTIONS-SOURCE-01 §9): a reserve request is
// admitted at most once, ever, and settled on demand — its reservation
// held (admitted), released on its evidence (released), or closed for good,
// durably, before the answer (closed). The files are private, the host the
// nonexecuting model, each fault injected before or after its syscall, a
// restart a fresh service over the same files, and a lost reply a request
// asked again: not a power loss, not a real process.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// token is a fresh request token.
func token(t *testing.T) string {
	t.Helper()
	r, err := NewRequest()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// requested is lockedReq naming request r.
func requested(attempt, r string) ReserveRequest {
	req := lockedReq(attempt, 1, 128)
	req.Request = r
	return req
}

// ledger is request r's ledger entry as its file holds it.
func ledger(t *testing.T, cfg Config, r string) (requestNote, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.StateDir, "requests", r+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return requestNote{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	n, err := decodeNote(data)
	if err != nil {
		t.Fatalf("ledger entry of %s: %v", r, err)
	}
	return n, true
}

// A request the launcher has not admitted when a settle decides is closed,
// durably, before the answer, and never admitted afterwards: a delayed
// reserve that crosses the settle (after an empty list) is refused, in this
// life and after a restart, and a replay of it too. The control: the same
// reserve with a request never settled is admitted.
func TestADelayedAdmissionIsRefusedOnceItsRequestIsSettled(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := token(t)
	// the owner lost its answer and looked: nothing is held
	if vms, err := s.List(owner1); err != nil || len(vms) != 0 {
		t.Fatalf("fixture: an empty list: %+v %v", vms, err)
	}
	st, err := s.Settle(owner1, "0vdelayed", r)
	if err != nil || st.Outcome != SettledClosed {
		t.Fatalf("an unseen request's settlement: %+v %v", st, err)
	}
	if n, ok := ledger(t, cfg, r); !ok || n.Outcome != SettledClosed || n.Owner != owner1 || n.Attempt != "0vdelayed" {
		t.Fatalf("THE CLOSURE WAS NOT DURABLE BEFORE ITS ANSWER: %+v %v", n, ok)
	}
	// the delayed reserve arrives now
	if rr, err := s.Reserve(owner1, requested("0vdelayed", r)); !errors.Is(err, ErrSettled) || rr.ID != "" || used(s)[2] != 0 {
		t.Fatalf("A LATE ADMISSION FOLLOWED A CERTIFIED CLOSURE: %+v %v, used %v", rr, err, used(s))
	}
	s2 := reopen(t, s, h)
	if rr, err := s2.Reserve(owner1, requested("0vdelayed", r)); !errors.Is(err, ErrSettled) || rr.ID != "" || used(s2)[2] != 0 {
		t.Fatalf("A LATE ADMISSION FOLLOWED A CERTIFIED CLOSURE after a restart: %+v %v, used %v", rr, err, used(s2))
	}
	if st, err := s2.Settle(owner1, "0vdelayed", r); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("its settlement asked again after a restart: %+v %v", st, err)
	}
	// control: a request never settled is admitted
	if _, err := s2.Reserve(owner1, requested("0vdelayed", token(t))); err != nil {
		t.Fatalf("the control's admission: %v", err)
	}
}

// A reserve admitted whose reply was lost is settled as admitted, with its
// exact identity, as often as it is asked, across a restart: its
// obligations are its reservation's, the charge is taken once, and a replay
// of the request is refused — no duplicate reservation, no early uncharge.
func TestAnAdmittedRequestWhoseReplyWasLostIsFoundExactly(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := token(t)
	got := mustReserve(t, s, owner1, requested("0vlost", r)) // its answer never reaches its owner
	for i := 0; i < 2; i++ {
		st, err := s.Settle(owner1, "0vlost", r)
		if err != nil || st.Outcome != SettledAdmitted || st.Record == nil || !got.Ref().Names(*st.Record) || st.Record.Request != r {
			t.Fatalf("AN ADMITTED REQUEST WAS NOT FOUND EXACTLY (ask %d): %+v %v", i+1, st, err)
		}
	}
	if _, err := s.Reserve(owner1, requested("0vlost", r)); !errors.Is(err, ErrSettled) || used(s)[2] != 1 {
		t.Fatalf("A REPLAYED REQUEST WAS ADMITTED TWICE: %v, used %v", err, used(s))
	}
	s2 := reopen(t, s, h)
	if st, err := s2.Settle(owner1, "0vlost", r); err != nil || st.Outcome != SettledAdmitted || !got.Ref().Names(*st.Record) || used(s2)[2] != 1 {
		t.Fatalf("an admitted request after a restart: %+v %v, used %v", st, err, used(s2))
	}
	// its owner resolves it by its identity; then it is released, on its
	// evidence: a replay is refused — before any settlement asks, by the
	// ledger alone — and its settlement says released
	if err := s2.DestroyOf(owner1, got.Ref()); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Reserve(owner1, requested("0vlost", r)); !errors.Is(err, ErrSettled) || used(s2)[2] != 0 {
		t.Fatalf("A REPLAYED REQUEST WAS ADMITTED AFTER ITS RELEASE: %v, used %v", err, used(s2))
	}
	st, err := s2.Settle(owner1, "0vlost", r)
	if err != nil || st.Outcome != SettledReleased || st.Release == nil || st.Release.By != ByTimelyCleanup {
		t.Fatalf("its settlement once released: %+v %v", st, err)
	}
}

// A request is admitted once even while its first admission is still
// being published: the same token, for another attempt (a confused or
// replaying caller), is refused then — not only once the ledger shows it.
func TestARequestIsAdmittedOnceWhileItsAdmissionPublishes(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, newModelHost(t, tr), fsys)
	r := token(t)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	fsys.inject(&fsFault{op: "rename", path: r + ".json", times: 1, block: func() {
		once.Do(func() { close(reached) })
		<-release
	}})
	first := make(chan error, 1)
	go func() {
		_, err := s.Reserve(owner1, requested("0vonce", r))
		first <- err
	}()
	<-reached
	_, err := s.Reserve(owner1, requested("0vtwin", r))
	close(release)
	ferr := <-first
	// the named observation first: a twin admitted would also publish the
	// request's ledger entry through the same path, and so fail the first
	// admission's own publication (trial-20)
	if !errors.Is(err, ErrSettled) {
		t.Fatalf("A REQUEST WAS ADMITTED TWICE WHILE ITS FIRST ADMISSION PUBLISHED: %v, used %v; the first: %v", err, used(s), ferr)
	}
	if ferr != nil || used(s)[2] != 1 {
		t.Fatalf("the first admission: %v, used %v", ferr, used(s))
	}
}

// Settlement waits for a reserve that is still publishing the reservation
// it admitted: acknowledged, it is admitted; not acknowledged (its record's
// publication failed), it is closed — never answered from the instant in
// between.
func TestASettlementWaitsForItsRequestsPublication(t *testing.T) {
	for _, c := range []struct {
		name    string
		fails   bool // the record's publication fails
		outcome string
	}{{"acknowledged", false, SettledAdmitted}, {"not acknowledged", true, SettledClosed}} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, newModelHost(t, tr), fsys)
			r := token(t)
			reached, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			// one fault both holds the record's rename and, when the case
			// says so, fails it (the first matching fault is the one that acts)
			x := &fsFault{op: "rename", path: IDFor(cfg.IDPrefix, "0vpub") + ".json", times: 1, block: func() {
				once.Do(func() { close(reached) })
				<-release
			}}
			if c.fails {
				x.mode, x.err = "before", errInjected
			}
			fsys.inject(x)
			reserved := make(chan error, 1)
			go func() {
				_, err := s.Reserve(owner1, requested("0vpub", r))
				reserved <- err
			}()
			<-reached
			settled := make(chan Settlement, 1)
			serr := make(chan error, 1)
			go func() {
				st, err := s.Settle(owner1, "0vpub", r)
				settled <- st
				serr <- err
			}()
			select {
			case st := <-settled:
				t.Fatalf("A SETTLEMENT WAS ANSWERED WHILE ITS ADMISSION WAS PUBLISHING: %+v %v", st, <-serr)
			case <-time.After(200 * time.Millisecond):
			}
			close(release)
			err := <-reserved
			st, e := <-settled, <-serr
			if e != nil || st.Outcome != c.outcome || (c.fails != (err != nil)) {
				t.Fatalf("settled %+v %v after the reserve's %v", st, e, err)
			}
			if c.fails && used(s)[2] != 0 {
				t.Fatalf("an unacknowledged admission stayed charged: %v", used(s))
			}
		})
	}
}

// Every failure of a settlement's durable closure — before its syscall,
// or after it acted — refuses the answer (not durable), keeps the request
// refused in this life, and is settled by a retry once repaired; a restart
// meanwhile finds a request that may still be admitted, whose admission a
// later settlement finds. The admission's own ledger entry failing leaves
// nothing acknowledged or charged, and its settlement closed.
func TestASettlementIsDurableBeforeItsAnswer(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault func(r string) *fsFault
	}{
		{"the closure's rename fails before", func(r string) *fsFault {
			return &fsFault{op: "rename", path: r + ".json", mode: "before", err: errInjected}
		}},
		{"the closure's rename acts, then fails", func(r string) *fsFault {
			return &fsFault{op: "rename", path: r + ".json", mode: "after", err: errInjected}
		}},
		{"the ledger's fsync fails", func(string) *fsFault {
			return &fsFault{op: "syncdir", path: "requests", mode: "before", err: errInjected}
		}},
		{"the state directory's fsync fails", func(string) *fsFault { return nil }}, // set below: cfg's path
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := token(t)
			x := c.fault(r)
			if x == nil {
				x = &fsFault{op: "syncdir", path: cfg.StateDir, mode: "before", err: errInjected}
			}
			fsys.inject(x)
			st, err := s.Settle(owner1, "0vdur", r)
			if !errors.Is(err, ErrNotDurable) || st.Outcome != "" || x.hits == 0 {
				t.Fatalf("A CLOSURE WAS ANSWERED BEFORE IT WAS DURABLE: %+v %v (%d hits)", st, err, x.hits)
			}
			if _, err := s.Reserve(owner1, requested("0vdur", r)); !errors.Is(err, ErrSettled) {
				t.Fatalf("A REQUEST WHOSE SETTLEMENT BEGAN WAS ADMITTED IN THE SAME LIFE: %v", err)
			}
			fsys.clear() // repaired
			if st, err := s.Settle(owner1, "0vdur", r); err != nil || st.Outcome != SettledClosed {
				t.Fatalf("the settlement retried once repaired: %+v %v", st, err)
			}
			if n, ok := ledger(t, cfg, r); !ok || n.Outcome != SettledClosed {
				t.Fatalf("its closure: %+v %v", n, ok)
			}
		})
	}
	t.Run("an entry found is certified before it answers", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			fault func(r string) *fsFault
		}{
			{"requests/'s fsync failing", func(string) *fsFault {
				return &fsFault{op: "syncdir", path: "requests", mode: "before", err: errInjected}
			}},
			{"its own fsync failing", func(r string) *fsFault {
				return &fsFault{op: "sync", path: r + ".json", mode: "before", err: errInjected}
			}},
		} {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, newModelHost(t, tr), fsys)
			r := token(t)
			// the closure renamed into place, then its answer refused: the
			// entry is visible, its name not certified
			fsys.inject(&fsFault{op: "syncdir", path: "requests", mode: "before", err: errInjected, times: 1})
			if _, err := s.Settle(owner1, "0vcert", r); !errors.Is(err, ErrNotDurable) {
				t.Fatalf("fixture (%s): %v", c.name, err)
			}
			if _, ok := ledger(t, cfg, r); !ok {
				t.Fatalf("fixture (%s): the entry is visible", c.name)
			}
			x := fsys.inject(c.fault(r))
			for i := 0; i < 2; i++ {
				if st, err := s.Settle(owner1, "0vcert", r); !errors.Is(err, ErrNotDurable) || st.Outcome != "" {
					t.Fatalf("AN ENTRY FOUND WAS ANSWERED BEFORE IT WAS CERTIFIED (%s, ask %d): %+v %v", c.name, i+1, st, err)
				}
			}
			if x.hits == 0 {
				t.Fatalf("fixture (%s): the fault was not reached", c.name)
			}
			fsys.clear()
			if st, err := s.Settle(owner1, "0vcert", r); err != nil || st.Outcome != SettledClosed {
				t.Fatalf("certified once repaired (%s): %+v %v", c.name, st, err)
			}
		}
	})
	t.Run("a restart before the retry", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		r := token(t)
		fsys.inject(&fsFault{op: "rename", path: r + ".json", mode: "before", err: errInjected})
		if _, err := s.Settle(owner1, "0vre", r); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("fixture: %v", err)
		}
		fsys.clear()
		s2 := reopen(t, s, h)
		// the request was never durably closed: its admission, if it comes,
		// is what a later settlement finds — the owner never returned its
		// capacity meanwhile
		got := mustReserve(t, s2, owner1, requested("0vre", r))
		if st, err := s2.Settle(owner1, "0vre", r); err != nil || st.Outcome != SettledAdmitted || !got.Ref().Names(*st.Record) {
			t.Fatalf("A LATER ADMISSION WAS NOT FOUND BY ITS SETTLEMENT: %+v %v", st, err)
		}
	})
	t.Run("the admission's ledger entry fails", func(t *testing.T) {
		for _, mode := range []string{"before", "after"} {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := token(t)
			fsys.inject(&fsFault{op: "rename", path: r + ".json", mode: mode, err: errInjected, times: 1})
			rr, err := s.Reserve(owner1, requested("0vled", r))
			if !errors.Is(err, ErrNotDurable) || rr.ID != "" || used(s)[2] != 0 {
				t.Fatalf("AN ADMISSION WAS ACKNOWLEDGED WITHOUT ITS LEDGER ENTRY (%s): %+v %v, used %v", mode, rr, err, used(s))
			}
			if _, ok := durable(t, cfg.StateDir, IDFor(cfg.IDPrefix, "0vled")); ok {
				t.Fatalf("its record was published without its ledger entry (%s)", mode)
			}
			if st, err := s.Settle(owner1, "0vled", r); err != nil || st.Outcome != SettledClosed {
				t.Fatalf("its settlement (%s): %+v %v", mode, st, err)
			}
			if _, err := s.Reserve(owner1, requested("0vled", r)); !errors.Is(err, ErrSettled) {
				t.Fatalf("its replay (%s): %v", mode, err)
			}
		}
	})
	t.Run("the admission's record fails after its ledger entry", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		r := token(t)
		fsys.inject(&fsFault{op: "rename", path: IDFor(cfg.IDPrefix, "0vrec") + ".json", mode: "before", err: errInjected, times: 1})
		if _, err := s.Reserve(owner1, requested("0vrec", r)); !errors.Is(err, ErrNotDurable) || used(s)[2] != 0 {
			t.Fatalf("fixture: %v, used %v", err, used(s))
		}
		if n, ok := ledger(t, cfg, r); !ok || n.Outcome != SettledAdmitted {
			t.Fatalf("fixture: its ledger entry: %+v %v", n, ok)
		}
		s2 := reopen(t, s, h)
		st, err := s2.Settle(owner1, "0vrec", r)
		if err != nil || st.Outcome != SettledClosed || !strings.Contains(st.Why, "never acknowledged") {
			t.Fatalf("A REQUEST NEVER ACKNOWLEDGED WAS NOT CLOSED: %+v %v", st, err)
		}
		if _, err := s2.Reserve(owner1, requested("0vrec", r)); !errors.Is(err, ErrSettled) {
			t.Fatalf("its replay: %v", err)
		}
	})
}

// Settlement is bound to its request, its attempt and its owner: another
// owner, or the wrong attempt (a stale or confused caller), is refused and
// changes nothing — the owner's own settlement still finds its
// reservation, or closes its request; a competing request of the same
// attempt is neither settled by, nor settles, another; two settlements of
// one request at once agree, with one entry.
func TestSettlementIsBoundToItsRequestAttemptAndOwner(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r1 := token(t)
	got := mustReserve(t, s, owner1, requested("0vown", r1))
	if _, err := s.Settle(owner2, "0vown", r1); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("ANOTHER OWNER SETTLED A REQUEST: %v", err)
	}
	if _, err := s.Settle(owner1, "0vother", r1); !errors.Is(err, ErrStale) {
		t.Fatalf("A REQUEST WAS SETTLED FOR ANOTHER ATTEMPT: %v", err)
	}
	if st, err := s.Settle(owner1, "0vown", r1); err != nil || st.Outcome != SettledAdmitted || !got.Ref().Names(*st.Record) {
		t.Fatalf("its owner's settlement after the refusals: %+v %v", st, err)
	}
	// a request not held: another owner's settlement closes nothing of its
	r2 := token(t)
	if st, err := s.Settle(owner1, "0vown2", r2); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("fixture: %+v %v", st, err)
	}
	if _, err := s.Settle(owner2, "0vown2", r2); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("ANOTHER OWNER WAS ANSWERED FROM A REQUEST'S LEDGER: %v", err)
	}
	if _, err := s.Settle(owner1, "0vwrong", r2); !errors.Is(err, ErrStale) {
		t.Fatalf("A CLOSED REQUEST WAS ANSWERED FOR ANOTHER ATTEMPT: %v", err)
	}
	// competing requests of one attempt: closing one leaves the other
	c1, c2 := token(t), token(t)
	if st, err := s.Settle(owner1, "0vcomp", c1); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("fixture: %+v %v", st, err)
	}
	other := mustReserve(t, s, owner1, requested("0vcomp", c2))
	if st, err := s.Settle(owner1, "0vcomp", c1); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("A COMPETING REQUEST'S RESERVATION ANSWERED ANOTHER'S SETTLEMENT: %+v %v", st, err)
	}
	if st, err := s.Settle(owner1, "0vcomp", c2); err != nil || st.Outcome != SettledAdmitted || !other.Ref().Names(*st.Record) {
		t.Fatalf("the competing request's own settlement: %+v %v", st, err)
	}
	// two settlements of one unseen request at once
	r3 := token(t)
	var wg sync.WaitGroup
	outs := make([]string, 2)
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := s.Settle(owner1, "0vtwice", r3)
			outs[i] = fmt.Sprint(st.Outcome, err)
		}()
	}
	wg.Wait()
	if outs[0] != SettledClosed+"<nil>" || outs[1] != outs[0] {
		t.Fatalf("two settlements of one request disagree: %v", outs)
	}
}

// Settlement answers presence, never absence, while the inventory is not
// authoritative (§§11.6, 11.7): a request not held is refused, not closed;
// one held is still found. And the ledger is kept and read only in a real
// directory: a symlink at requests/ refuses the admission of a request and
// its settlement, and nothing is written through it.
func TestSettlementNeverRestsOnAnUnprovenAbsence(t *testing.T) {
	t.Run("an entry the loader cannot account for", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		r1, r2 := token(t), token(t)
		got := mustReserve(t, s, owner1, requested("0vheld", r1))
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.StateDir, "attempts", IDFor(cfg.IDPrefix, "0vx")+".json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		s2 := open(t, cfg, h, nil)
		if st, err := s2.Settle(owner1, "0vnot", r2); !errors.Is(err, ErrUnsafeState) || st.Outcome != "" {
			t.Fatalf("A REQUEST WAS CLOSED ON A PARTIAL INVENTORY: %+v %v", st, err)
		}
		if _, ok := ledger(t, cfg, r2); ok {
			t.Fatal("a refusal closed the request")
		}
		if st, err := s2.Settle(owner1, "0vheld", r1); err != nil || st.Outcome != SettledAdmitted || !got.Ref().Names(*st.Record) {
			t.Fatalf("a held request on a partial inventory: %+v %v", st, err)
		}
	})
	t.Run("requests/ is a symlink", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, filepath.Join(cfg.StateDir, "requests")); err != nil {
			t.Fatal(err)
		}
		// what lies at its target: a closure of r, which no barrier of the
		// launcher's certified
		r, fresh := token(t), token(t)
		planted, err := encodeNote(requestNote{Request: r, Attempt: "0vsym", Owner: owner1, Outcome: SettledClosed, At: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(elsewhere, r+".json"), planted, 0o600); err != nil {
			t.Fatal(err)
		}
		// a request nothing there names: nothing is written through it, and
		// it is neither admitted nor settled
		_, err = s.Reserve(owner1, requested("0vsym2", fresh))
		if through, _ := os.ReadDir(elsewhere); len(through) != 1 {
			t.Fatalf("SOMETHING WAS WRITTEN THROUGH A SYMLINK: %d entries", len(through))
		}
		if used(s)[2] != 0 || !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("A REQUEST WAS ADMITTED WITHOUT A LEDGER IT COULD READ: %v, used %v", err, used(s))
		}
		if st, err := s.Settle(owner1, "0vsym2", fresh); err == nil || st.Outcome != "" {
			t.Fatalf("A REQUEST WAS SETTLED THROUGH A SYMLINK: %+v %v", st, err)
		}
		// the request it names: refused as unreadable, never as settled — its
		// entry was not read
		_, err = s.Reserve(owner1, requested("0vsym", r))
		if used(s)[2] != 0 || !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("A LEDGER THAT IS NOT A REAL DIRECTORY WAS READ: %v, used %v", err, used(s))
		}
		if through, _ := os.ReadDir(elsewhere); len(through) != 1 {
			t.Fatalf("SOMETHING WAS WRITTEN THROUGH A SYMLINK: %d entries", len(through))
		}
	})
}

// Settlement changes no disposition: a request whose reservation became
// an incident (A) is answered admitted — quarantined, charged — and its
// settlement neither releases nor reclassifies it; one whose reservation
// was released with late bookkeeping (Q8) is answered released, the late
// bookkeeping kept.
func TestSettlementLeavesEveryDispositionAsItIs(t *testing.T) {
	t.Run("an incident", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		r := token(t)
		got := mustReserve(t, s, owner1, requested("0vinc", r))
		if _, err := s.CreateOf(owner1, got.Ref()); err != nil {
			t.Fatal(err)
		}
		h.fail("rmjail", "before", errInjected)
		if err := s.DestroyOf(owner1, got.Ref()); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("fixture: quarantined: %v", err)
		}
		for i := 0; i < 2; i++ {
			st, err := s.Settle(owner1, "0vinc", r)
			if err != nil || st.Outcome != SettledAdmitted || st.Record.State != StateQuarantined || used(s)[2] != 1 {
				t.Fatalf("AN INCIDENT WAS SETTLED AWAY: %+v %v, used %v", st, err, used(s))
			}
		}
		if rec, ok := held(s, got.ID); !ok || rec.State != StateQuarantined {
			t.Fatalf("its incident after the settlements: %+v %v", rec, ok)
		}
	})
	t.Run("late bookkeeping", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		cfg := testConfig(t.TempDir())
		clock := freeze(&cfg)
		s := open(t, cfg, newModelHost(t, tr), fsys)
		r := token(t)
		got := mustReserve(t, s, owner1, requested("0vlate", r))
		due := clock.now().Add(cfg.CleanupBound)
		// its cleanup finishes in time; its record's withdrawal — its
		// accounting — runs after the deadline
		fsys.inject(&fsFault{op: "unlink", path: "/" + got.ID + ".json", block: func() { clock.set(due.Add(time.Second)) }, times: 1})
		if err := s.DestroyOf(owner1, got.Ref()); err != nil {
			t.Fatal(err)
		}
		st, err := s.Settle(owner1, "0vlate", r)
		if err != nil || st.Outcome != SettledReleased || st.Release == nil || !st.Release.Late() || !st.Release.LateAccounting {
			t.Fatalf("A LATE BOOKKEEPING WAS NOT KEPT BY ITS SETTLEMENT: %+v %v", st, err)
		}
	})
}

// The wire speaks protocol 4: a reserve that names no request is refused;
// the client names a fresh one when its caller names none, and echoes it;
// settle answers on the wire as in process; a launcher of protocol 3 is
// refused at hello, before any request.
func TestTheWireSettlesRequests(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, newModelHost(t, &tracer{}), nil)
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "s.sock")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	go sv.Serve(l)
	t.Cleanup(func() { l.Close() })
	cl, err := Dial(context.Background(), "s.sock", owner1.Daemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if reply, _, err := cl.call(Request{Op: "reserve", Attempt: "0vbare", Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 10, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}, false); err == nil || reply.OK || used(s)[2] != 0 {
		t.Fatalf("A RESERVE WITHOUT A REQUEST WAS ADMITTED ON THE WIRE: %+v %v", reply, err)
	}
	filled, err := cl.Reserve(lockedReq("0vfill", 1, 128))
	if err != nil || !ValidRequest(filled.Request) {
		t.Fatalf("the client names a request for its caller: %+v %v", filled, err)
	}
	r := token(t)
	got, err := cl.Reserve(requested("0vwire", r))
	if err != nil || got.Request != r {
		t.Fatalf("a named request: %+v %v", got, err)
	}
	if st, err := cl.Settle("0vwire", r); err != nil || st.Outcome != SettledAdmitted || !got.Ref().Names(*st.Record) {
		t.Fatalf("settle on the wire, admitted: %+v %v", st, err)
	}
	unseen := token(t)
	if st, err := cl.Settle("0vnone", unseen); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("settle on the wire, closed: %+v %v", st, err)
	}
	if _, err := cl.Reserve(requested("0vnone", unseen)); err == nil || !strings.Contains(err.Error(), ErrSettled.Error()) {
		t.Fatalf("A SETTLED REQUEST WAS ADMITTED ON THE WIRE: %v", err)
	}
	if _, err := cl.DestroyOf(got.Ref()); err != nil {
		t.Fatal(err)
	}
	if st, err := cl.Settle("0vwire", r); err != nil || st.Outcome != SettledReleased || st.Release == nil {
		t.Fatalf("settle on the wire, released: %+v %v", st, err)
	}
	// a launcher of protocol 3, a scripted reply on a fresh private socket
	old, err := net.Listen("unix", "old3.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	go func() {
		c, err := old.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
			fmt.Fprintln(c, `{"ok":true,"launcher":"old","protocol":3}`)
		}
	}()
	if c3, err := Dial(context.Background(), "old3.sock", "0va"); err == nil || !strings.Contains(err.Error(), "protocol 3") {
		if c3 != nil {
			c3.Close()
		}
		t.Fatalf("A LAUNCHER OF PROTOCOL 3 WAS WORKED WITH: %v", err)
	}
}
