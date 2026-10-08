package launcher

// Late-accounting ruling 01 (INTEGRATION.md §11.8), one test per case the
// ruling's verification names:
//   - timely cleanup plus late confirmed accounting: its capacity returns
//     only after the confirmation, and the late bookkeeping is reported
//     apart and kept in the evidence — an on-time one is not;
//   - pending, unconfirmed accounting — and a failure after an effect —
//     withholds the capacity, and nothing is withdrawn before its evidence;
//   - intermediate durable images across a restart, each answered as its
//     evidence says;
//   - a still-failing barrier against a repaired one;
//   - a lost reply, answered from the evidence, against a purported
//     withdrawal;
//   - stale and incarnation-confused calls;
//   - already-quarantined incidents, never released by evidence.
// Truly late or uncertain cleanup stays under A (completion_test.go,
// quarantine_test.go); here, that it leaves no evidence. The files are
// private, the host the nonexecuting model, a restart a fresh service over
// the same files: not a power loss, not a real process.

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

// The wire's protocol is 3 since a destroy of an incarnation not held is
// done only on its evidence, and released asks for it (§11.8): a launcher
// that answers hello with protocol 2 — whose absence a runner would take
// for a release — is refused at hello, before any request. The launcher is
// a scripted reply on a fresh private unix socket.
func TestAProtocolTwoLauncherIsRefused(t *testing.T) {
	t.Chdir(t.TempDir()) // a short, private socket path
	l, err := net.Listen("unix", "old.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	served := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			served <- err
			return
		}
		defer c.Close()
		if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
			served <- err
			return
		}
		_, err = fmt.Fprintln(c, `{"ok":true,"launcher":"old","protocol":2}`)
		served <- err
	}()
	cl, err := Dial(context.Background(), "old.sock", "0va")
	if err == nil {
		cl.Close()
		t.Fatal("A LAUNCHER OF PROTOCOL 2 WAS WORKED WITH")
	}
	if !strings.Contains(err.Error(), "protocol 2") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// keptEvidence is the disposition kept for exactly r's incarnation.
func keptEvidence(t *testing.T, cfg Config, r ReserveReply) (Release, bool) {
	t.Helper()
	ev, found, err := ReadEvidence(cfg.StateDir, r.Ref())
	if err != nil {
		t.Fatalf("its evidence: %v", err)
	}
	if !found || ev.Release == nil {
		return Release{}, false
	}
	return *ev.Release, true
}

// unlinkedRecord says whether the trace holds an unlink of id's record.
func unlinkedRecord(tr *tracer, id string) bool {
	for _, e := range tr.all() {
		if e.kind == "fs" && e.op == "unlink" && e.id == id {
			return true
		}
	}
	return false
}

// reports collects what a service reports.
type reports struct {
	mu  sync.Mutex
	got []Outcome
}

func (r *reports) add(o Outcome) {
	r.mu.Lock()
	r.got = append(r.got, o)
	r.mu.Unlock()
}

func (r *reports) all() []Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Outcome(nil), r.got...)
}

func TestOnTimeCleanupWithLateConfirmedAccounting(t *testing.T) {
	for _, c := range []struct {
		name string
		late bool // the confirmation comes after the obligation's deadline
	}{{"confirmed after the deadline", true}, {"confirmed in time (control)", false}} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			clock := freeze(&cfg)
			s := open(t, cfg, h, fsys)
			rs := &reports{}
			s.SetReport(rs.add)
			r := mustReserve(t, s, owner1, lockedReq("0vacct", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			// the withdrawal's unlink succeeds; its directory's fsync fails
			x := &fsFault{op: "syncdir", path: "attempts", mode: "before", err: errInjected, times: 1}
			fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", block: func() { fsys.inject(x) }, times: 1})
			due := clock.now().Add(cfg.CleanupBound)
			err := s.DestroyOf(owner1, r.Ref())
			if rec, ok := held(s, r.ID); !ok || rec.State != StateStopping || used(s)[2] != 1 {
				t.Fatalf("CAPACITY RETURNED BEFORE ITS ACCOUNTING WAS CONFIRMED: %v; %+v %v, used %v", err, rec, ok, used(s))
			}
			if !errors.Is(err, ErrNotDurable) || x.hits != 1 {
				t.Fatalf("fixture: an unconfirmed withdrawal: %v (%d)", err, x.hits)
			}
			// the query retries nothing (its owner's destroy, the reaper's
			// pass and the stop do: TestAPendingAccountingWaitsForItsBarrier)
			if _, err := s.Released(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) {
				t.Fatalf("A PENDING ACCOUNTING WAS ANSWERED RELEASED: %v", err)
			}
			rel, ok := keptEvidence(t, cfg, r)
			if !ok || rel.By != ByTimelyCleanup || rel.DueUnixNano != due.UnixNano() || rel.CleanedUnixNano >= rel.DueUnixNano || rel.ConfirmedUnixNano != 0 {
				t.Fatalf("THE RELEASE WAS NOT AUTHORIZED BY ITS EVIDENCE FIRST: %+v %v", rel, ok)
			}
			at := due.Add(-time.Millisecond)
			if c.late {
				at = due.Add(time.Second)
			}
			clock.set(at)
			s.ReapOnce() // its retry confirms it
			if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
				t.Fatalf("a confirmed accounting did not return the capacity: used %v", used(s))
			}
			rel, _ = keptEvidence(t, cfg, r)
			got, err := s.Released(owner1, r.Ref())
			out := rs.all()
			if c.late {
				if !rel.LateAccounting || rel.ConfirmedUnixNano != at.UnixNano() || err != nil || !got.Late() || len(out) != 1 || out[0].Kind != "released" || !out[0].LateAccounting {
					t.Fatalf("A LATE ACCOUNTING WAS CLAIMED ON TIME: evidence %+v; released %+v %v; reports %+v", rel, got, err, out)
				}
				return
			}
			if rel.LateAccounting || rel.ConfirmedUnixNano != at.UnixNano() || err != nil || got.Late() || len(out) != 0 {
				t.Fatalf("an accounting confirmed in time: evidence %+v; released %+v %v; reports %+v", rel, got, err, out)
			}
		})
	}
}

// An owner's destroy whose accounting is confirmed after the deadline —
// its withdrawal published late — is answered done with the late
// bookkeeping, and reported apart, as the launcher's own teardowns are.
func TestAnOwnersLateAccountingIsReportedApart(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := freeze(&cfg)
	s := open(t, cfg, h, fsys)
	rs := &reports{}
	s.SetReport(rs.add)
	r := mustReserve(t, s, owner1, lockedReq("0vown", 1, 128))
	if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
		t.Fatal(err)
	}
	due := clock.now().Add(cfg.CleanupBound)
	// the unlink runs past the deadline
	fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", block: func() { clock.set(due.Add(time.Second)) }, times: 1})
	rel, err := s.DestroyOfRelease(owner1, r.Ref())
	out := rs.all()
	if err != nil || !rel.Late() || len(out) != 1 || out[0].By != "destroy" || !out[0].LateAccounting {
		t.Fatalf("AN OWNER'S LATE ACCOUNTING WAS NOT REPORTED APART: %+v %v; reports %+v", rel, err, out)
	}
}

// Nothing is withdrawn before its release's evidence is durable: evidence
// not written, written but not certified, or renamed with an error after it
// acted — each leaves the release pending (charged, not an incident, the
// record still there), and a retry finishes it.
func TestNoWithdrawalBeforeItsEvidenceIsDurable(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault func(r ReserveReply) *fsFault
	}{
		{"its evidence not written", func(r ReserveReply) *fsFault {
			return &fsFault{op: "open", path: r.Incarnation + ".json.tmp", mode: "before", err: errInjected, times: 1}
		}},
		{"its evidence's directory fsync failed", func(r ReserveReply) *fsFault {
			return &fsFault{op: "syncdir", path: "released", mode: "before", err: errInjected, times: 1}
		}},
		{"its evidence's rename acted, then failed", func(r ReserveReply) *fsFault {
			return &fsFault{op: "rename", path: r.Incarnation + ".json", mode: "after", err: errInjected, times: 1}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0vev", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			x := fsys.inject(c.fault(r))
			err := s.DestroyOf(owner1, r.Ref())
			if x.hits != 1 {
				t.Fatalf("fixture: the fault was not reached (%d)", x.hits)
			}
			if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateStopping || unlinkedRecord(tr, r.ID) {
				t.Fatalf("A RECORD WAS WITHDRAWN BEFORE ITS EVIDENCE WAS DURABLE: %+v %v", d, ok)
			}
			rec, ok := held(s, r.ID)
			if !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) || !ok || rec.State == StateQuarantined || used(s)[2] != 1 {
				t.Fatalf("a pending accounting: %v; %+v %v used %v", err, rec, ok, used(s))
			}
			fsys.clear()
			s.ReapOnce()
			if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
				t.Fatalf("the retry did not release it: used %v", used(s))
			}
			if rel, ok := keptEvidence(t, cfg, r); !ok || rel.ConfirmedUnixNano == 0 {
				t.Fatalf("its evidence after the retry: %+v %v", rel, ok)
			}
		})
	}
}

// Truly late or failed cleanup stays under A: quarantined, charged, and no
// release evidence is written for it.
func TestALateOrFailedCleanupLeavesNoReleaseEvidence(t *testing.T) {
	for _, c := range []struct {
		name string
		late bool // the cleanup finishes after the deadline; else a removal fails
	}{{"its cleanup finished late", true}, {"its jail removal failed", false}} {
		t.Run(c.name, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			clock := freeze(&cfg)
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0vlate", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			if c.late {
				due := clock.now().Add(cfg.CleanupBound)
				h.at("rmjail/after", func(string) { clock.set(due.Add(time.Second)) })
			} else {
				h.fail("rmjail", "before", errInjected)
			}
			if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("fixture: quarantined: %v", err)
			}
			if _, ok := keptEvidence(t, cfg, r); ok {
				t.Fatal("A LATE OR FAILED CLEANUP LEFT RELEASE EVIDENCE")
			}
			if _, err := s.Released(owner1, r.Ref()); !errors.Is(err, ErrState) || used(s)[2] != 1 {
				t.Fatalf("an incident answered released: %v, used %v", err, used(s))
			}
		})
	}
}

// A restart finds each durable image of a release and answers as its
// evidence says: stopping alone — its evidence never written — is an
// incident (the cleanup's timing was only in memory); stopping with its
// evidence is a release whose accounting is pending, confirmed by the
// first reaper pass, late; the evidence with the record gone is released,
// its confirmation instant unrecorded — late; a confirmation the evidence
// could not record counts as late too.
func TestARestartAnswersEachDurableImageOfARelease(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault func(fsys *faultFS, r ReserveReply) // the first life's failure
		image string                              // incident | pending | released
	}{
		{"stopping only: its evidence never written", func(fsys *faultFS, r ReserveReply) {
			fsys.inject(&fsFault{op: "open", path: r.Incarnation + ".json.tmp", mode: "before", err: errInjected})
		}, "incident"},
		{"stopping and its evidence: its unlink failed", func(fsys *faultFS, r ReserveReply) {
			fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", mode: "before", err: errInjected})
		}, "pending"},
		{"its evidence, the record gone: its confirmation lost", func(fsys *faultFS, r ReserveReply) {
			fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", block: func() {
				fsys.inject(&fsFault{op: "syncdir", path: "attempts", mode: "before", err: errInjected})
			}, times: 1})
		}, "released"},
		{"confirmed in time, its evidence's update failed", func(fsys *faultFS, r ReserveReply) {
			fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", block: func() {
				fsys.inject(&fsFault{op: "rename", path: r.Incarnation + ".json", mode: "before", err: errInjected})
			}, times: 1})
		}, "released"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			clock := freeze(&cfg)
			s, err := openService(cfg, h, fsys)
			if err != nil {
				t.Fatal(err)
			}
			r := mustReserve(t, s, owner1, lockedReq("0vimg", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			due := clock.now().Add(cfg.CleanupBound)
			c.fault(fsys, r)
			_ = s.DestroyOf(owner1, r.Ref())
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			clock.set(due.Add(time.Second)) // the restart comes after the deadline
			s2 := open(t, cfg, h, nil)
			rec, ok := held(s2, r.ID)
			switch c.image {
			case "incident":
				if !ok || rec.State != StateQuarantined || used(s2)[2] != 1 {
					t.Fatalf("A RESTART RELEASED WITHOUT EVIDENCE: %+v %v, used %v", rec, ok, used(s2))
				}
				if _, err := s2.Released(owner1, r.Ref()); !errors.Is(err, ErrState) {
					t.Fatalf("A RESTART RELEASED WITHOUT EVIDENCE: released %v", err)
				}
				return
			case "pending":
				if !ok || rec.State == StateQuarantined || used(s2)[2] != 1 {
					t.Fatalf("A RESTART TURNED AN AUTHORIZED RELEASE INTO AN INCIDENT: %+v %v, used %v", rec, ok, used(s2))
				}
				if _, err := s2.Released(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) {
					t.Fatalf("A PENDING ACCOUNTING WAS ANSWERED RELEASED after a restart: %v", err)
				}
				s2.ReapOnce() // the first pass confirms it
			}
			if _, ok := held(s2, r.ID); ok || used(s2)[2] != 0 {
				t.Fatalf("the release after the restart: used %v", used(s2))
			}
			got, err := s2.Released(owner1, r.Ref())
			if err != nil || !got.Late() {
				t.Fatalf("A LATE OR UNRECORDED ACCOUNTING WAS CLAIMED ON TIME: %+v %v", got, err)
			}
			if rel, err := s2.DestroyOfRelease(owner1, r.Ref()); err != nil || !rel.Late() {
				t.Fatalf("A LATE OR UNRECORDED ACCOUNTING WAS CLAIMED ON TIME: its owner's replay %+v %v", rel, err)
			}
		})
	}
}

// A pending accounting waits for its barrier: while the records directory's
// fsync still fails, each retry keeps it charged and reported unconfirmed —
// at a reopen whose own certification fails too — and once it is repaired
// the retry releases it.
func TestAPendingAccountingWaitsForItsBarrier(t *testing.T) {
	for _, c := range []struct {
		name   string
		reopen bool // the first life stops; a reopen finds it
	}{{"in the same life", false}, {"across a reopen whose barrier fails", true}} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0vbar", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			failing := &fsFault{op: "syncdir", path: "attempts", mode: "before", err: errInjected}
			if c.reopen {
				// its unlink fails in the first life: it stays, pending
				fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", mode: "before", err: errInjected})
			} else {
				fsys.inject(&fsFault{op: "unlink", path: "/" + r.ID + ".json", block: func() { fsys.inject(failing) }, times: 1})
			}
			if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) {
				t.Fatalf("fixture: pending: %v", err)
			}
			if c.reopen {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				fsys.clear()
				fsys.inject(failing)
				var err error
				if s, err = openService(cfg, h, fsys); err != nil {
					t.Fatal(err)
				}
				closeAtEnd(t, s)
			}
			rs := &reports{}
			s.SetReport(rs.add)
			s.ReapOnce() // the barrier still fails
			if _, ok := held(s, r.ID); !ok || used(s)[2] != 1 {
				t.Fatalf("A PENDING ACCOUNTING WAS RELEASED ON A FAILING BARRIER: used %v", used(s))
			}
			if out := rs.all(); len(out) != 1 || out[0].Kind != "unconfirmed" {
				t.Fatalf("A PENDING ACCOUNTING WAS NOT RETRIED BY THE REAPER: reports %+v", out)
			}
			fsys.clear() // repaired
			s.ReapOnce()
			if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
				t.Fatalf("A REPAIRED BARRIER'S RETRY RELEASED NOTHING: used %v", used(s))
			}
		})
	}
}

// A lost reply is answered from the evidence: an authorized, committed
// release replayed — in the same life or after a restart — is done; a
// record gone without evidence (a purported withdrawal) is refused, and so
// is released; a quarantined destroy replayed is still the incident's
// refusal.
func TestALostReplyIsAnsweredFromTheEvidence(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0vlost", 1, 128))
	if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
		t.Fatal(err)
	}
	if err := s.DestroyOf(owner1, r.Ref()); err != nil {
		t.Fatal(err)
	}
	// its reply lost: the owner asks again
	if err := s.DestroyOf(owner1, r.Ref()); err != nil {
		t.Fatalf("AN AUTHORIZED RELEASE WITH A LOST REPLY WAS REFUSED: %v", err)
	}
	q := mustReserve(t, s, owner1, lockedReq("0vpurported", 1, 128))
	x := mustReserve(t, s, owner1, lockedReq("0vquar", 1, 128))
	if _, err := s.CreateOf(owner1, x.Ref()); err != nil {
		t.Fatal(err)
	}
	h.fail("rmjail", "before", errInjected)
	if err := s.DestroyOf(owner1, x.Ref()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: quarantined: %v", err)
	}
	h.heal("rmjail")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// q's record goes without a release's evidence (the fixture's own file)
	if err := os.Remove(filepath.Join(cfg.StateDir, "attempts", q.ID+".json")); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, cfg, h, nil)
	if err := s2.DestroyOf(owner1, r.Ref()); err != nil {
		t.Fatalf("AN AUTHORIZED RELEASE WITH A LOST REPLY WAS REFUSED after a restart: %v", err)
	}
	if err := s2.DestroyOf(owner1, q.Ref()); !errors.Is(err, ErrUnproven) {
		t.Fatalf("A PURPORTED WITHDRAWAL WAS ANSWERED RELEASED: %v", err)
	}
	if _, err := s2.Released(owner1, q.Ref()); !errors.Is(err, ErrUnproven) {
		t.Fatalf("A PURPORTED WITHDRAWAL WAS ANSWERED RELEASED: released %v", err)
	}
	if err := s2.DestroyOf(owner1, x.Ref()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("a quarantined destroy replayed: %v", err)
	}
}

// A stale or incarnation-confused call is answered only from its own
// incarnation's evidence: an earlier incarnation's release is done and the
// one held now untouched; a made-up token proves nothing; another owner's
// evidence proves nothing to this one.
func TestAStaleReferenceIsAnsweredOnlyFromItsOwnEvidence(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	first := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
	if err := s.DestroyOf(owner1, first.Ref()); err != nil {
		t.Fatal(err)
	}
	second := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
	if _, err := s.CreateOf(owner1, second.Ref()); err != nil {
		t.Fatal(err)
	}
	intact := func(what string) {
		t.Helper()
		if rec, ok := held(s, second.ID); !ok || rec.Incarnation != second.Incarnation || rec.State != StateRunning || !h.exists("vmm", second.ID) {
			t.Fatalf("A STALE REFERENCE REACHED THE NEW INCARNATION (%s): %+v %v", what, rec, ok)
		}
	}
	if err := s.DestroyOf(owner1, first.Ref()); err != nil {
		t.Fatalf("an earlier incarnation's release, proven: %v", err)
	}
	intact("its evidence")
	madeUp := second.Ref()
	madeUp.Incarnation = strings.Repeat("f", 32)
	if err := s.DestroyOf(owner1, madeUp); !errors.Is(err, ErrUnproven) {
		t.Fatalf("AN INCARNATION NEVER RELEASED WAS ANSWERED DONE: %v", err)
	}
	intact("a made-up token")
	if err := s.DestroyOf(owner2, first.Ref()); !errors.Is(err, ErrUnproven) {
		t.Fatalf("ANOTHER OWNER'S EVIDENCE PROVED A RELEASE: %v", err)
	}
	if _, err := s.Released(owner2, first.Ref()); !errors.Is(err, ErrUnproven) {
		t.Fatalf("ANOTHER OWNER'S EVIDENCE PROVED A RELEASE: released %v", err)
	}
	intact("another owner")
}

// An already-quarantined incident is never released by evidence: with a
// timely cleanup's evidence planted for its exact incarnation, a restart
// still loads it quarantined, charged; released and its owner's destroy
// refuse; the reaper leaves it. Only the operator's release returns it,
// and its evidence then says so.
func TestAnIncidentIsNeverReleasedByEvidence(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0vinc", 1, 128))
	if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
		t.Fatal(err)
	}
	h.fail("rmjail", "before", errInjected)
	if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: quarantined: %v", err)
	}
	h.heal("rmjail")
	rec, _ := held(s, r.ID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// a timely cleanup's evidence for its exact incarnation, planted
	now := time.Now()
	ev := rec
	ev.State, ev.Incident = StateReleased, nil
	ev.Release = &Release{By: ByTimelyCleanup, Final: StateDestroyed, DueUnixNano: now.Add(time.Minute).UnixNano(), CleanedUnixNano: now.UnixNano(), DecidedUnixNano: now.UnixNano(), ConfirmedUnixNano: now.UnixNano()}
	st := &store{dir: filepath.Join(cfg.StateDir, "attempts"), prefix: cfg.IDPrefix, fs: osFS{}}
	if err := st.archive(ev); err != nil {
		t.Fatal(err)
	}
	s2 := open(t, cfg, h, nil)
	s2.ReapOnce()
	if got, ok := held(s2, r.ID); !ok || got.State != StateQuarantined || used(s2)[2] != 1 {
		t.Fatalf("AN INCIDENT WAS RELEASED WITHOUT THE OPERATOR: %+v %v, used %v", got, ok, used(s2))
	}
	if _, err := s2.Released(owner1, r.Ref()); !errors.Is(err, ErrState) {
		t.Fatalf("AN INCIDENT WAS RELEASED WITHOUT THE OPERATOR: released %v", err)
	}
	if err := s2.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("AN INCIDENT WAS RELEASED WITHOUT THE OPERATOR: its owner's destroy %v", err)
	}
	in, err := s2.RetryCleanup(rec.Selection())
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Release(in.Selection); err != nil {
		t.Fatalf("the operator's release: %v", err)
	}
	if got, err := s2.Released(owner1, r.Ref()); err != nil || got.By != ByOperator {
		t.Fatalf("the operator's release's evidence: %+v %v", got, err)
	}
}
