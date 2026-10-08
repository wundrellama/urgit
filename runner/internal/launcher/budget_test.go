package launcher

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// boundedHost is the model host with the bounding seam: every Kill and
// removal records when it ran and the deadline it was given (none when the
// core called the host directly).
type boundedHost struct {
	*modelHost
	cmu   sync.Mutex
	calls []boundedCall
}

type boundedCall struct {
	op       string
	at       time.Time
	deadline time.Time // zero: no deadline
}

func (b *boundedHost) note(op string, ctx context.Context) {
	c := boundedCall{op: op, at: time.Now()}
	if ctx != nil {
		c.deadline, _ = ctx.Deadline()
	}
	b.cmu.Lock()
	b.calls = append(b.calls, c)
	b.cmu.Unlock()
}

func (b *boundedHost) recorded() []boundedCall {
	b.cmu.Lock()
	defer b.cmu.Unlock()
	return append([]boundedCall(nil), b.calls...)
}

func (b *boundedHost) Kill(id string, pid int, sig string) error {
	b.note("kill-"+sig, nil)
	return b.modelHost.Kill(id, pid, sig)
}
func (b *boundedHost) RemoveNetwork(id string, index int) error {
	b.note("rmnet", nil)
	return b.modelHost.RemoveNetwork(id, index)
}
func (b *boundedHost) RemoveCgroup(id string) error {
	b.note("rmcgroup", nil)
	return b.modelHost.RemoveCgroup(id)
}
func (b *boundedHost) RemoveDisk(id string) error {
	b.note("rmdisk", nil)
	return b.modelHost.RemoveDisk(id)
}
func (b *boundedHost) RemoveJail(id string) error {
	b.note("rmjail", nil)
	return b.modelHost.RemoveJail(id)
}

// Bound is the seam: the view it returns carries ctx into every call.
func (b *boundedHost) Bound(ctx context.Context) Host { return &boundView{boundedHost: b, ctx: ctx} }

type boundView struct {
	*boundedHost
	ctx context.Context
}

func (v *boundView) Kill(id string, pid int, sig string) error {
	v.note("kill-"+sig, v.ctx)
	return v.modelHost.Kill(id, pid, sig)
}
func (v *boundView) RemoveNetwork(id string, index int) error {
	v.note("rmnet", v.ctx)
	return v.modelHost.RemoveNetwork(id, index)
}
func (v *boundView) RemoveCgroup(id string) error {
	v.note("rmcgroup", v.ctx)
	return v.modelHost.RemoveCgroup(id)
}
func (v *boundView) RemoveDisk(id string) error {
	v.note("rmdisk", v.ctx)
	return v.modelHost.RemoveDisk(id)
}
func (v *boundView) RemoveJail(id string) error {
	v.note("rmjail", v.ctx)
	return v.modelHost.RemoveJail(id)
}

// One teardown has one allowance, rider 04's 120 s (INTEGRATION.md §7.2),
// never a fresh one per step: a VMM that ignores TERM gets KILL a quarter
// of the way in, not half, so the removals keep at least half; and every
// host call of the teardown carries that one deadline.
func TestTeardownHasOneAllowance(t *testing.T) {
	h := &boundedHost{modelHost: newModelHost(t, &tracer{})}
	cfg := testConfig(t.TempDir())
	cfg.CleanupBound = 2 * time.Second
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	// the VMM survives TERM and dies on KILL
	h.mu.Lock()
	h.hang[r.ID] = true
	h.mu.Unlock()
	var kills atomic.Int32
	h.at("kill/before", func(id string) {
		if kills.Add(1) == 2 {
			h.mu.Lock()
			h.hang[id] = false
			h.mu.Unlock()
		}
	})
	start := time.Now()
	if err := s.Destroy(owner1, r.ID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	var kill time.Time
	calls := h.recorded()
	for _, c := range calls {
		if c.op == "kill-KILL" {
			kill = c.at
		}
	}
	quarter := cfg.CleanupBound / 4
	if kill.IsZero() || kill.Sub(start) > quarter+cfg.CleanupBound/8 {
		t.Fatalf("THE VMM'S STOP TOOK MORE THAN ITS SHARE: KILL %v after the teardown began, want within %v (the quarter) of the %v allowance; calls %+v", kill.Sub(start), quarter, cfg.CleanupBound, calls)
	}
	want := start.Add(cfg.CleanupBound)
	for _, c := range calls {
		if c.deadline.IsZero() || c.deadline.Before(want.Add(-cfg.CleanupBound/8)) || c.deadline.After(want.Add(cfg.CleanupBound/8)) || !c.deadline.Equal(calls[0].deadline) {
			t.Fatalf("HOST CALLS DID NOT SHARE THE TEARDOWN'S ONE DEADLINE (%v from its start): %+v", cfg.CleanupBound, calls)
		}
	}
}

// createdPair reserves and creates two locked VMs that pass their deadline
// when clock advances by expire.
func createdPair(t *testing.T, s *Service) (a, b ReserveReply) {
	t.Helper()
	a = mustReserve(t, s, owner1, expiring("0v1", 1, 128))
	b = mustReserve(t, s, owner1, expiring("0v2", 1, 128))
	for _, r := range []ReserveReply{a, b} {
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}

// holdFirstRemoval holds the first teardown to reach its cgroup removal
// there; slow is its id once g.reached is closed.
func holdFirstRemoval(h *modelHost, g *gate) *string {
	var once sync.Once
	var slow string
	h.at("rmcgroup/before", func(id string) {
		first := false
		once.Do(func() { slow, first = id, true })
		if first {
			g.wait(id)
		}
	})
	return &slow
}

// The reaper tears each due record down on its own (INTEGRATION.md §7.2):
// one whose removal waits on the host never holds another's cleanup back.
func TestReaperTeardownsDoNotQueueBehindEachOther(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, nil)
	a, b := createdPair(t, s)
	g := newGate()
	t.Cleanup(g.open)
	slow := holdFirstRemoval(h, g)
	clock.pass(t, s, a.ID, b.ID)
	done := make(chan struct{})
	go func() { s.ReapOnce(); close(done) }()
	<-g.reached
	other := a.ID
	if *slow == a.ID {
		other = b.ID
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && h.exists("jail", other) {
		time.Sleep(5 * time.Millisecond)
	}
	released := !h.exists("jail", other)
	g.open()
	<-done
	if !released {
		t.Fatalf("THE REAPER QUEUED ONE TEARDOWN BEHIND ANOTHER: %s's cleanup waited for %s's removal", other, *slow)
	}
	if used(s)[2] != 0 {
		t.Fatalf("after the pass: %v", used(s))
	}
}

// Shutdown's drain waits for operations, not for teardowns in progress: a
// teardown — an owner's destroy, a create's rollback — has its own
// allowance already, and every other owned VM's teardown starts beside it.
func TestShutdownDoesNotWaitOnATeardownInProgress(t *testing.T) {
	for _, busy := range []string{"an owner's destroy", "a create's rollback"} {
		t.Run(busy, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			s := open(t, testConfig(t.TempDir()), h, nil)
			idle := mustReserve(t, s, owner1, lockedReq("0v2", 1, 128))
			if _, err := s.Create(owner1, idle.ID); err != nil {
				t.Fatal(err)
			}
			slow := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
			g := newGate()
			t.Cleanup(g.open)
			h.at("rmcgroup/before", func(id string) {
				if id == slow.ID {
					g.wait(id)
				}
			})
			done := make(chan error, 1)
			if busy == "an owner's destroy" {
				if _, err := s.Create(owner1, slow.ID); err != nil {
					t.Fatal(err)
				}
				go func() { done <- s.Destroy(owner1, slow.ID) }()
			} else {
				h.fail("start", "noeffect", errInjected) // its rollback removes what the other steps made
				go func() { _, err := s.Create(owner1, slow.ID); done <- err }()
			}
			<-g.reached // slow's teardown holds its token, at its cgroup removal
			shut := make(chan error, 1)
			go func() { shut <- s.Shutdown(context.Background()) }()
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && h.exists("jail", idle.ID) {
				time.Sleep(5 * time.Millisecond)
			}
			released := !h.exists("jail", idle.ID)
			g.open()
			<-done
			err := <-shut
			// the observation first: a teardown made to wait begins after its
			// deadline, a late recovery whose error would otherwise hide it
			if !released {
				t.Fatalf("SHUTDOWN WAITED ON A TEARDOWN IN PROGRESS (%s) before tearing the idle VM down (shutdown: %v)", busy, err)
			}
			if err != nil {
				t.Fatalf("shutdown: %v", err)
			}
		})
	}
}

// Shutdown's teardowns run side by side for the same reason: the stop is
// bounded by one allowance, not by one per owned VM.
func TestShutdownTeardownsDoNotQueueBehindEachOther(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	a, b := createdPair(t, s)
	g := newGate()
	t.Cleanup(g.open)
	slow := holdFirstRemoval(h, g)
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(context.Background()) }()
	<-g.reached
	other := a.ID
	if *slow == a.ID {
		other = b.ID
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && h.exists("jail", other) {
		time.Sleep(5 * time.Millisecond)
	}
	released := !h.exists("jail", other)
	g.open()
	err := <-done
	// the observation first: a queued teardown begins after its deadline,
	// a late recovery whose error would otherwise hide the queue
	if !released {
		t.Fatalf("SHUTDOWN QUEUED ONE TEARDOWN BEHIND ANOTHER: %s's cleanup waited for %s's removal (shutdown: %v)", other, *slow, err)
	}
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
