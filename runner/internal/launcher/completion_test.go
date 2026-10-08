package launcher

// The obligation at completion (runner/launcher/INTEGRATION.md §11.3;
// independent review 01, R3): a releasing teardown's outcome is decided
// after its last host effect has returned, and a cleanup that returns at
// or after its obligation's deadline missed it, however it began: it ends
// quarantined and charged, its incident marked missed, its caller answered
// quarantined. The clocks here are the service's own (source-clock
// semantics), not a measured VM overrun.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// frozenClock is a service clock that moves only when the test sets it.
type frozenClock struct {
	mu sync.Mutex
	t  time.Time
}

func freeze(cfg *Config) *frozenClock {
	c := &frozenClock{t: time.Now()}
	cfg.now = c.now
	return c
}

func (c *frozenClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *frozenClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// missed says whether rec is a charged quarantine whose incident records
// the missed obligation.
func missed(s *Service, rec Record, ok bool) bool {
	return ok && rec.State == StateQuarantined && rec.Incident != nil && rec.Incident.Missed && used(s)[2] == 1
}

// A destroy asked in time whose last host effect returns after — or
// exactly at — its obligation's deadline missed it: quarantined, charged,
// missed, answered quarantined; its cleanup still ran (the model's
// resources are gone). Returning just before the deadline releases.
func TestCleanupFinishingLateIsAMissedObligation(t *testing.T) {
	for _, c := range []struct {
		name    string
		finish  time.Duration // after the deadline (negative: before it)
		release bool
	}{
		{"it finishes after the deadline", time.Second, false},
		{"it finishes exactly at the deadline", 0, false},
		{"it finishes just before the deadline (control)", -time.Nanosecond, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			clock := freeze(&cfg)
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0vlate", 1, 128))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			asked := clock.now()
			deadline := asked.Add(cfg.CleanupBound)
			h.at("rmjail/after", func(string) { clock.set(deadline.Add(c.finish)) })
			err := s.Destroy(owner1, r.ID)
			rec, ok := held(s, r.ID)
			if c.release {
				if err != nil || ok || used(s)[2] != 0 {
					t.Fatalf("a cleanup finished in time: %v; held %v %+v", err, ok, rec)
				}
				return
			}
			if !errors.Is(err, ErrQuarantined) || !missed(s, rec, ok) {
				t.Fatalf("A CLEANUP THAT FINISHED LATE RELEASED: %v; held %v %+v; used %v", err, ok, rec, used(s))
			}
			if len(h.leaks(r.ID)) != 0 || rec.holds() {
				t.Fatalf("the late cleanup did not clean up: leaks %v, record %+v", h.leaks(r.ID), rec)
			}
			last := rec.Incident.Attempts[len(rec.Incident.Attempts)-1]
			if last.Result != "resolved" || !strings.Contains(last.Detail+rec.Reason, "its cleanup finished at "+instant(deadline.Add(c.finish))+", not before its obligation's deadline "+instant(deadline)) {
				t.Fatalf("the incident does not say the cleanup finished late: %+v; reason %q", last, rec.Reason)
			}
		})
	}
}

// lateReturnHost is a boundable model whose jail removal returns only after
// the teardown's own deadline has cut it — and then claims success.
type lateReturnHost struct {
	*modelHost
	ctx context.Context
}

func (h *lateReturnHost) Bound(ctx context.Context) Host { return &lateReturnHost{h.modelHost, ctx} }

func (h *lateReturnHost) RemoveJail(id string) error {
	if h.ctx != nil {
		<-h.ctx.Done()
	}
	return h.modelHost.RemoveJail(id)
}

// A host effect that returns after its context was cut at the deadline —
// claiming success — is a cleanup that finished late: whatever bounded it,
// the check follows its return.
func TestALateReturnAfterCancellationIsAMissedObligation(t *testing.T) {
	h := &lateReturnHost{modelHost: newModelHost(t, &tracer{})}
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0vcut", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	err := s.Destroy(owner1, r.ID)
	rec, ok := held(s, r.ID)
	if !errors.Is(err, ErrQuarantined) || !missed(s, rec, ok) {
		t.Fatalf("A CLEANUP THAT FINISHED LATE RELEASED: %v; held %v %+v; used %v", err, ok, rec, used(s))
	}
}

// The reaper's teardown that finishes late is reported as the incident it
// is: quarantined, LATE.
func TestReapedCleanupFinishingLateIsReportedLate(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	clock := freeze(&cfg)
	s := open(t, cfg, h, nil)
	var mu sync.Mutex
	var got []Outcome
	s.SetReport(func(o Outcome) {
		mu.Lock()
		got = append(got, o)
		mu.Unlock()
	})
	req := lockedReq("0vreap", 1, 128)
	req.DeadlineUnix = clock.now().Add(time.Hour).Unix()
	r := mustReserve(t, s, owner1, req)
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	due := time.Unix(req.DeadlineUnix, 0)
	clock.set(due.Add(time.Millisecond)) // the reaper comes just after the deadline: in time
	h.at("rmjail/after", func(string) { clock.set(due.Add(cfg.CleanupBound)) })
	s.ReapOnce()
	rec, ok := held(s, r.ID)
	mu.Lock()
	reports := append([]Outcome(nil), got...)
	mu.Unlock()
	if !missed(s, rec, ok) || len(reports) != 1 || reports[0].Kind != "quarantined" || !reports[0].Late {
		t.Fatalf("A REAPED CLEANUP THAT FINISHED LATE RELEASED OR WAS NOT REPORTED LATE: held %v %+v; reports %+v", ok, rec, reports)
	}
}

// When the quarantine of a cleanup that finished late cannot be published,
// the durable stopping record still holds it: a restart loads it
// quarantined, charged.
func TestALateFinishWhosePublicationFailsStaysHeld(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := freeze(&cfg)
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, lockedReq("0vpub", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	deadline := clock.now().Add(cfg.CleanupBound)
	h.at("rmjail/after", func(string) {
		clock.set(deadline.Add(time.Second))
		fsys.inject(&fsFault{op: "rename", path: r.ID + ".json", mode: "before", err: errInjected, times: 1})
	})
	err := s.Destroy(owner1, r.ID)
	if !errors.Is(err, ErrQuarantined) {
		t.Fatalf("A CLEANUP THAT FINISHED LATE RELEASED: %v", err)
	}
	if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateStopping {
		t.Fatalf("the durable record: %+v %v", d, ok)
	}
	s2 := reopen(t, s, h)
	if rec, ok := held(s2, r.ID); !ok || rec.State != StateQuarantined || used(s2)[2] != 1 {
		t.Fatalf("A LATE FINISH WHOSE PUBLICATION FAILED WAS NOT HELD: %+v %v", rec, ok)
	}
}

// A release decided in time whose own withdrawal is published after the
// deadline stands — nothing was left running or held past it — but it is
// not hidden: the released record's reason says so, and a teardown the
// launcher started itself is reported (released, LateAccounting).
// Policy-dependent (late-accounting ruling 01; INTEGRATION.md §11.8): this
// was §8's candidate, held; the ruling makes it late bookkeeping — its
// report's flag LateAccounting (Late before it), its reason's words the
// ruling's.
func TestAReleasePublishedLateIsSaid(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := freeze(&cfg)
	s := open(t, cfg, h, fsys)
	var mu sync.Mutex
	var got []Outcome
	s.SetReport(func(o Outcome) {
		mu.Lock()
		got = append(got, o)
		mu.Unlock()
	})
	req := lockedReq("0vpub", 1, 128)
	req.DeadlineUnix = clock.now().Add(time.Hour).Unix()
	r := mustReserve(t, s, owner1, req)
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	due := time.Unix(req.DeadlineUnix, 0)
	clock.set(due.Add(time.Millisecond))
	// the withdrawal's unlink runs past the deadline
	fsys.inject(&fsFault{op: "unlink", path: r.ID + ".json", block: func() { clock.set(due.Add(cfg.CleanupBound + time.Second)) }, times: 1})
	s.ReapOnce()
	if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
		t.Fatalf("a release decided in time did not stand: used %v", used(s))
	}
	hist := s.History()
	mu.Lock()
	reports := append([]Outcome(nil), got...)
	mu.Unlock()
	if len(hist) != 1 || !strings.Contains(hist[0].Reason, "LATE BOOKKEEPING: its accounting (its record's withdrawal) was confirmed at") || !strings.Contains(hist[0].Reason, "its cleanup had finished in time, at") || len(reports) != 1 || reports[0].Kind != "released" || !reports[0].LateAccounting {
		t.Fatalf("A RELEASE PUBLISHED LATE WAS HIDDEN: history %+v; reports %+v", hist, reports)
	}
}

// A release decided in time whose withdrawal's directory fsync failed stays
// charged until a retry confirms it; when the reaper's pass confirms it
// after the obligation's deadline, that is said too: the released record's
// reason, and the report (released, LateAccounting). Policy-dependent
// (late-accounting ruling 01; §11.8): as the test above.
func TestAReleaseConfirmedLateIsSaid(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := freeze(&cfg)
	s := open(t, cfg, h, fsys)
	var mu sync.Mutex
	var got []Outcome
	s.SetReport(func(o Outcome) {
		mu.Lock()
		got = append(got, o)
		mu.Unlock()
	})
	r := mustReserve(t, s, owner1, lockedReq("0vconf", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	// the withdrawal's unlink succeeds, its directory fsync fails
	x := &fsFault{op: "syncdir", mode: "before", err: errInjected, times: 1}
	fsys.clear()
	fsys.inject(&fsFault{op: "unlink", path: ".json", block: func() { fsys.inject(x) }, times: 1})
	deadline := clock.now().Add(cfg.CleanupBound)
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrNotDurable) || x.hits != 1 {
		t.Fatalf("fixture: an unconfirmed withdrawal: %v (fault hits %d)", err, x.hits)
	}
	clock.set(deadline.Add(time.Second)) // the confirmation comes after the deadline
	s.ReapOnce()
	if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
		t.Fatalf("fixture: the reaper did not confirm the release: used %v", used(s))
	}
	hist := s.History()
	mu.Lock()
	reports := append([]Outcome(nil), got...)
	mu.Unlock()
	if len(hist) != 1 || !strings.Contains(hist[0].Reason, "its cleanup finished at") || !strings.Contains(hist[0].Reason, "its accounting (its record's withdrawal) was confirmed at "+instant(deadline.Add(time.Second))) || len(reports) != 1 || reports[0].Kind != "released" || !reports[0].LateAccounting {
		t.Fatalf("A RELEASE CONFIRMED LATE WAS HIDDEN: history %+v; reports %+v", hist, reports)
	}
}
