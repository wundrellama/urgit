package launcher

// The cleanup obligation runs from its trigger (RIDER-CI-P4-04 line 14;
// runner/launcher/INTEGRATION.md §7.2): within one allowance after an
// owner's destroy, a job's deadline or the launcher's stop, every owned
// resource is stopped and cleaned or explicitly quarantined — including
// the time spent waiting for a start in progress, and whatever the stop
// drains first. Each test measures from the trigger to the settled
// outcome, end to end, not a helper's own duration.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// startGate is the model host with the bounding seam, whose VMM start for
// one id waits: it finishes when the test lets it, or ends when its
// context does — a jailer cut short, whose VMM's fate is unknown. Its
// cgroup removal for another id may wait for its context too (a removal
// that does not end by itself); every cgroup removal's deadline is kept.
type startGate struct {
	*modelHost
	id       string
	stuckRm  string
	entered  chan struct{}
	finish   chan struct{}
	once     sync.Once
	finished sync.Once
	rmMu     sync.Mutex
	rmBy     map[string]time.Time
}

func newStartGate(t *testing.T) *startGate {
	return &startGate{modelHost: newModelHost(t, &tracer{}), entered: make(chan struct{}), finish: make(chan struct{})}
}

func (h *startGate) release() { h.finished.Do(func() { close(h.finish) }) }

func (h *startGate) Bound(ctx context.Context) Host { return &startGateView{startGate: h, ctx: ctx} }

type startGateView struct {
	*startGate
	ctx context.Context
}

func (v *startGateView) StartVM(id string, spec VMSpec) (int, error) {
	if id != v.id {
		return v.modelHost.StartVM(id, spec)
	}
	v.once.Do(func() { close(v.entered) })
	select {
	case <-v.finish:
		return v.modelHost.StartVM(id, spec)
	case <-v.ctx.Done():
		return 0, fmt.Errorf("jailer cut short: %w", v.ctx.Err())
	}
}

func (v *startGateView) RemoveCgroup(id string) error {
	if d, ok := v.ctx.Deadline(); ok {
		v.rmMu.Lock()
		if v.rmBy == nil {
			v.rmBy = map[string]time.Time{}
		}
		v.rmBy[id] = d
		v.rmMu.Unlock()
	}
	if id == v.stuckRm {
		<-v.ctx.Done()
		return fmt.Errorf("cgroup removal cut short: %w", v.ctx.Err())
	}
	return v.modelHost.RemoveCgroup(id)
}

// removalDeadline is the deadline id's last cgroup removal carried.
func (h *startGate) removalDeadline(id string) time.Time {
	h.rmMu.Lock()
	defer h.rmMu.Unlock()
	return h.rmBy[id]
}

const triggerBound = 2 * time.Second

// settledSlack is what the tests allow past the obligation for the
// outcome's own publication (and a loaded test machine).
const settledSlack = triggerBound / 4

// (a) and (d): the owner's destroy while a start is pending. A start that
// finishes within a quarter of the allowance is no success to publish: its
// VMM, known now, is rolled back and released. One that has not finished
// by then is cut short, and its VMM — whose fate is unknown — keeps the
// record quarantined and charged, never reported stopped. Either way the
// outcome is settled within one allowance of the destroy's request, the
// wait for the start included.
func TestCancelledStartIsBoundedByItsTrigger(t *testing.T) {
	for _, c := range []struct {
		name        string
		finishAfter time.Duration // 0: the start never finishes by itself
	}{{"the start finishes within its quarter", triggerBound / 8}, {"the start does not finish", 0}} {
		t.Run(c.name, func(t *testing.T) {
			h := newStartGate(t)
			t.Cleanup(h.release)
			cfg := testConfig(t.TempDir())
			cfg.CleanupBound = triggerBound
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
			h.id = r.ID
			created := make(chan error, 1)
			go func() { _, err := s.Create(owner1, r.ID); created <- err }()
			<-h.entered // the start is pending
			trigger := time.Now()
			if c.finishAfter > 0 {
				time.AfterFunc(c.finishAfter, h.release)
			}
			derr := s.Destroy(owner1, r.ID)
			settled := time.Since(trigger)
			cerr := <-created
			rec, stillHeld := held(s, r.ID)
			if settled > triggerBound+settledSlack {
				t.Fatalf("THE CLEANUP OUTLIVED ITS TRIGGER'S DEADLINE: settled %v after the destroy was asked for (allowance %v)", settled, triggerBound)
			}
			if cerr == nil {
				t.Fatal("A START THAT ENDED AFTER ITS ROLLBACK WAS ASKED FOR WAS PUBLISHED AS A SUCCESS")
			}
			if c.finishAfter > 0 {
				if derr != nil || stillHeld || len(h.leaks(r.ID)) != 0 {
					t.Fatalf("A START WAS CUT BEFORE ITS QUARTER, OR ITS VMM LEFT: destroy %v; still held %v %+v; leaks %v", derr, stillHeld, rec, h.leaks(r.ID))
				}
				return
			}
			if !errors.Is(derr, ErrQuarantined) || !stillHeld || rec.State != StateQuarantined || !rec.HasVMM || rec.PID != 0 || !strings.Contains(rec.Reason, "outcome unknown") || strings.Contains(rec.Reason, "stopped") || used(s)[2] != 1 {
				t.Fatalf("A START CUT AT ITS QUARTER WAS NOT LEFT VISIBLE AND CHARGED: destroy %v; record %+v; used %v", derr, rec, used(s))
			}
			if settled < triggerBound/4-triggerBound/16 {
				t.Fatalf("the start was cut %v after the trigger, before its quarter (%v)", settled, triggerBound/4)
			}
		})
	}
}

// A create's rollback runs from its trigger, not from the moment the
// create noticed it: asked to roll back while its start is pending, the
// start finishing an eighth of the allowance later, its rollback's host
// calls carry the destroy's deadline — one allowance after the request.
func TestRollbackRunsFromItsTrigger(t *testing.T) {
	h := newStartGate(t)
	t.Cleanup(h.release)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	h.id = r.ID
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-h.entered
	asked := time.Now()
	time.AfterFunc(triggerBound/8, h.release)
	if err := s.Destroy(owner1, r.ID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if err := <-created; err == nil {
		t.Fatal("fixture: the create rolls back")
	}
	got := h.removalDeadline(r.ID)
	if want := asked.Add(triggerBound); got.IsZero() || got.Sub(want).Abs() > triggerBound/16 {
		t.Fatalf("THE ROLLBACK DID NOT RUN FROM ITS TRIGGER: its host calls' deadline %v after the destroy was asked for, want %v", got.Sub(asked), triggerBound)
	}
}

// (b): the job's deadline reached while the start is pending. The
// deadline is a trigger by itself: whether or not a reaper pass comes
// right after it, the start is cut a quarter of the allowance after the
// deadline, and the outcome — its VMM unknown: quarantined, charged — is
// settled within one allowance of the deadline. (d): a start that
// finishes after the deadline is no success to publish; its VMM, known
// now, is rolled back.
func TestDeadlineDuringAStartIsBoundedByTheDeadline(t *testing.T) {
	for _, c := range []struct {
		name        string
		reap        bool          // a reaper pass right after the deadline
		finishAfter time.Duration // after the deadline; 0: never, by itself
	}{
		{"a reaper pass comes right after it", true, 0},
		{"no pass comes: the deadline cuts it by itself", false, 0},
		{"it finishes after the deadline", false, triggerBound / 8},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newStartGate(t)
			t.Cleanup(h.release)
			cfg := testConfig(t.TempDir())
			cfg.CleanupBound = triggerBound
			s := open(t, cfg, h, nil)
			req, deadline := shortDeadline("0v1")
			r := mustReserve(t, s, owner1, req)
			h.id = r.ID
			created := make(chan error, 1)
			go func() { _, err := s.Create(owner1, r.ID); created <- err }()
			<-h.entered // the start began before the deadline
			if c.finishAfter > 0 {
				time.AfterFunc(time.Until(deadline)+c.finishAfter, h.release)
			}
			if c.reap {
				time.Sleep(time.Until(deadline) + 50*time.Millisecond)
				s.ReapOnce()
			}
			cerr := <-created
			settled := time.Since(deadline)
			rec, stillHeld := held(s, r.ID)
			if settled > triggerBound+settledSlack {
				t.Fatalf("THE CLEANUP OUTLIVED THE JOB'S DEADLINE: settled %v after it (allowance %v); the create %v; record %+v %v", settled, triggerBound, cerr, rec, stillHeld)
			}
			if cerr == nil {
				t.Fatal("A START THAT ENDED AFTER THE JOB'S DEADLINE WAS PUBLISHED AS A SUCCESS")
			}
			if c.finishAfter > 0 {
				if stillHeld || len(h.leaks(r.ID)) != 0 {
					t.Fatalf("the VMM of a start that finished after the deadline was not rolled back: %+v %v; leaks %v", rec, stillHeld, h.leaks(r.ID))
				}
				return
			}
			if !stillHeld || rec.State != StateQuarantined || !strings.Contains(rec.Reason, "outcome unknown") || used(s)[2] != 1 {
				t.Fatalf("a start cut after the deadline was not left visible and charged: %+v %v; used %v", rec, stillHeld, used(s))
			}
		})
	}
}

// (c): the launcher's stop while one VM runs and another's start is
// pending. The stop request is every teardown's trigger: the drain that
// waits for the pending start (cut at a quarter) spends the same
// allowance, so the running VM's teardown — whose removal here does not
// end by itself — is cut at the stop's deadline, not one allowance after
// the drain; the whole stop is settled within one allowance of its
// request.
func TestStopDrainingAStartKeepsEveryTeardownInTheStopsAllowance(t *testing.T) {
	h := newStartGate(t)
	t.Cleanup(h.release)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	running := mustReserve(t, s, owner1, lockedReq("0v2", 1, 128))
	if _, err := s.Create(owner1, running.ID); err != nil {
		t.Fatal(err)
	}
	h.stuckRm = running.ID
	pending := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	h.id = pending.ID
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, pending.ID); created <- err }()
	<-h.entered
	stop := time.Now()
	s.BeginStop("launcher stopping")
	err := s.Shutdown(context.Background())
	took := time.Since(stop)
	<-created
	if took > triggerBound+settledSlack {
		t.Fatalf("THE STOP OUTLIVED ITS TRIGGER'S ALLOWANCE: %v after the stop was asked for (allowance %v): %v", took, triggerBound, err)
	}
	if took < triggerBound-settledSlack {
		t.Fatalf("the running VM's stuck removal was cut %v after the stop, before the stop's deadline (%v): %v", took, triggerBound, err)
	}
	if err == nil || !strings.Contains(err.Error(), running.ID) {
		t.Fatalf("the running VM's cut-short removal must be a reported quarantine: %v", err)
	}
	next, oerr := NewService(s.cfg, h)
	if oerr != nil {
		t.Fatal(oerr)
	}
	defer next.Close()
	for _, id := range []string{running.ID, pending.ID} {
		if rec, ok := held(next, id); !ok || rec.State != StateQuarantined {
			t.Fatalf("%s after the stop: %+v %v — every resource not cleaned is explicitly quarantined", id, rec, ok)
		}
	}
}

// A reaped record's teardown ends by the job's deadline plus one
// allowance, however late in that allowance the reaper's pass reaches it:
// here the pass comes half an allowance after the deadline, and the VM's
// removal, which does not end by itself, is cut at the deadline's end —
// not half an allowance later.
func TestReapedTeardownEndsByTheDeadlinesAllowance(t *testing.T) {
	h := newStartGate(t)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	req, deadline := shortDeadline("0v1")
	r := mustReserve(t, s, owner1, req)
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.stuckRm = r.ID
	time.Sleep(time.Until(deadline) + triggerBound/2)
	s.ReapOnce()
	settled := time.Since(deadline)
	rec, stillHeld := held(s, r.ID)
	if settled > triggerBound+settledSlack || !stillHeld || rec.State != StateQuarantined {
		t.Fatalf("THE REAPED TEARDOWN OUTLIVED ITS DEADLINE'S ALLOWANCE: settled %v after the deadline (allowance %v); record %+v %v", settled, triggerBound, rec, stillHeld)
	}
}

// The reaper's passes never wait for one another's teardowns: while one
// record's teardown waits on a removal that does not end by itself, the
// next record to pass its deadline is reaped a tick after it, not after
// that teardown.
func TestReaperPassesDoNotWaitForEachOther(t *testing.T) {
	h := newStartGate(t)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	first, firstAt := shortDeadline("0v1")
	a := mustReserve(t, s, owner1, first)
	second := lockedReq("0v2", 1, 128)
	second.DeadlineUnix = firstAt.Add(time.Second).Unix()
	b := mustReserve(t, s, owner1, second)
	for _, id := range []string{a.ID, b.ID} {
		if _, err := s.Create(owner1, id); err != nil {
			t.Fatal(err)
		}
	}
	h.stuckRm = a.ID
	ctx, cancel := context.WithCancel(context.Background())
	reaping := make(chan struct{})
	go func() { s.Reap(ctx, 50*time.Millisecond); close(reaping) }()
	defer func() {
		cancel()
		<-reaping
	}()
	secondAt := time.Unix(second.DeadlineUnix, 0)
	var released time.Time
	for limit := secondAt.Add(3 * triggerBound); time.Now().Before(limit); time.Sleep(10 * time.Millisecond) {
		if _, ok := held(s, b.ID); !ok {
			released = time.Now()
			break
		}
	}
	if released.IsZero() || released.Sub(secondAt) > triggerBound/4 {
		t.Fatalf("A REAPER PASS WAITED FOR ANOTHER'S TEARDOWN: the second record was released %v after its deadline (zero: never)", released.Sub(secondAt))
	}
}

// A destroy that waits for an operation in progress (a connect, here) keeps
// its request as the trigger: the teardown after the wait ends by the
// request plus one allowance, not one allowance after the wait.
func TestDestroyThatWaitsKeepsItsRequestsDeadline(t *testing.T) {
	h := newStartGate(t)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.stuckRm = r.ID
	g := newGate()
	t.Cleanup(g.open)
	h.at("connect/before", g.wait)
	connected := make(chan struct{})
	go func() {
		if f, err := s.Connect(owner1, r.ID, 5000); err == nil {
			f.Close()
		}
		close(connected)
	}()
	<-g.reached // the connect holds the record
	asked := time.Now()
	time.AfterFunc(triggerBound/2, g.open)
	err := s.Destroy(owner1, r.ID)
	settled := time.Since(asked)
	<-connected
	if settled > triggerBound+settledSlack || !errors.Is(err, ErrQuarantined) {
		t.Fatalf("THE DESTROY'S CLEANUP OUTLIVED ITS REQUEST'S DEADLINE: settled %v after the request (allowance %v): %v", settled, triggerBound, err)
	}
}

// A teardown asked for after the job's deadline has passed — by the
// owner's destroy or the launcher's stop, before any reaper pass — runs
// from the deadline, the earlier trigger: asked half an allowance after
// the deadline, the VM's removal, which does not end by itself, is cut at
// the deadline's end, not half an allowance later.
func TestTeardownsAfterTheDeadlineEndByItsAllowance(t *testing.T) {
	for _, by := range []string{"the owner's destroy", "the launcher's stop"} {
		t.Run(by, func(t *testing.T) {
			h := newStartGate(t)
			cfg := testConfig(t.TempDir())
			cfg.CleanupBound = triggerBound
			s := open(t, cfg, h, nil)
			req, deadline := shortDeadline("0v1")
			r := mustReserve(t, s, owner1, req)
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			h.stuckRm = r.ID
			time.Sleep(time.Until(deadline) + triggerBound/2)
			var err error
			if by == "the owner's destroy" {
				err = s.Destroy(owner1, r.ID)
			} else {
				s.BeginStop("launcher stopping")
				err = s.Shutdown(context.Background())
			}
			settled := time.Since(deadline)
			if settled > triggerBound+settledSlack || err == nil {
				t.Fatalf("A TEARDOWN ASKED FOR AFTER THE DEADLINE OUTLIVED THE DEADLINE'S ALLOWANCE: settled %v after it (allowance %v): %v", settled, triggerBound, err)
			}
		})
	}
}

// Shutdown alone — no BeginStop — cuts no create short: the start in
// progress finishes first (here half an allowance after the call), as it
// always did. The stop's allowance runs from the call all the same: the
// running VM's teardown, whose removal does not end by itself, is cut at
// the stop's deadline, not one allowance after the drain.
func TestShutdownAloneKeepsTheStopsAllowance(t *testing.T) {
	h := newStartGate(t)
	t.Cleanup(h.release)
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, nil)
	running := mustReserve(t, s, owner1, lockedReq("0v2", 1, 128))
	if _, err := s.Create(owner1, running.ID); err != nil {
		t.Fatal(err)
	}
	h.stuckRm = running.ID
	pending := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	h.id = pending.ID
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, pending.ID); created <- err }()
	<-h.entered
	stop := time.Now()
	time.AfterFunc(triggerBound/2, h.release)
	err := s.Shutdown(context.Background())
	took := time.Since(stop)
	if cerr := <-created; cerr != nil {
		t.Fatalf("Shutdown alone cut the create short: %v", cerr)
	}
	if took > triggerBound+settledSlack || err == nil || !strings.Contains(err.Error(), running.ID) {
		t.Fatalf("THE STOP OUTLIVED ITS TRIGGER'S ALLOWANCE: %v after the stop was asked for (allowance %v): %v", took, triggerBound, err)
	}
	if _, ok := held(s, pending.ID); ok || len(h.leaks(pending.ID)) != 0 {
		t.Fatalf("the create that finished was not torn down: %v", h.leaks(pending.ID))
	}
}

// A teardown that halts — its stopping record not durable — keeps the
// obligation it began: the owner's retry, half an allowance later, ends by
// the first request's deadline (its removal here does not end by itself),
// not one allowance after the retry.
func TestRetryAfterAHaltKeepsTheObligation(t *testing.T) {
	h := newStartGate(t)
	fsys := newFaultFS(&tracer{})
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = triggerBound
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.stuckRm = r.ID
	fsys.inject(&fsFault{op: "open", path: r.ID + ".json.tmp", mode: "before", err: errInjected, times: 1}) // its stopping record
	asked := time.Now()
	if err := s.Destroy(owner1, r.ID); err == nil || !strings.Contains(err.Error(), "halted") {
		t.Fatalf("fixture: the first teardown halts: %v", err)
	}
	time.Sleep(triggerBound/2 - time.Since(asked))
	err := s.Destroy(owner1, r.ID)
	settled := time.Since(asked)
	if settled > triggerBound+settledSlack || !errors.Is(err, ErrQuarantined) {
		t.Fatalf("A RETRY AFTER A HALT RESTARTED THE OBLIGATION: settled %v after the first request (allowance %v): %v", settled, triggerBound, err)
	}
}
