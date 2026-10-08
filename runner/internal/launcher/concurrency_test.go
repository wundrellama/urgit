package launcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Reservations racing for the last capacity: exactly what fits is
// admitted, and a restart holds exactly the acknowledged ones.
func TestConcurrentReservationsNeverOvercommit(t *testing.T) {
	cfg := testConfig(t.TempDir()) // 8 cpus, 18432 MiB, 2 guests
	s := open(t, cfg, nil, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	acked := map[string]bool{}
	over, other := 0, 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := s.Reserve(owner1, lockedReq(fmt.Sprintf("0v%d", i), 3, 4096))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				acked[r.ID] = true
			case errors.Is(err, ErrOverBudget):
				over++
			default:
				other++
				t.Errorf("unexpected refusal: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if len(acked) != 2 || over != 14 || other != 0 {
		t.Fatalf("acked %d, over budget %d, other %d", len(acked), over, other)
	}
	if u := used(s); u != [3]int{6, 2 * (4096 + OverheadMiB), 2} {
		t.Fatalf("charge %v", u)
	}
	s2 := reopen(t, s, nil)
	for _, r := range s2.All() {
		if !acked[r.ID] {
			t.Fatalf("a restart holds an unacknowledged reservation %s", r.ID)
		}
	}
	if len(s2.All()) != 2 {
		t.Fatalf("a restart holds %d reservations", len(s2.All()))
	}
}

// The same attempt reserved from many connections at once is one
// reservation with one record.
func TestSameAttemptReservedConcurrentlyOnce(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Reserve(owner1, lockedReq("0vsame", 1, 128))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, exists := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrExists):
			exists++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 || exists != 7 || used(s)[2] != 1 || len(stateEntries(t, cfg.StateDir)) != 1 {
		t.Fatalf("ok %d exists %d used %v state %v", ok, exists, used(s), stateEntries(t, cfg.StateDir))
	}
}

// A destroy that arrives while a create holds the record waits — it does
// not run a second cleanup beside the create — and asks the create to
// roll back at its next step: nothing boots, one cleanup removes
// everything, one release.
func TestDestroyDuringCreateWaitsAndRollsBack(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("cgroup/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached // the create is inside CreateCgroup, its disk prepared
	destroyed := make(chan error, 1)
	go func() { destroyed <- s.Destroy(owner1, r.ID) }()
	waitFor(t, 2*time.Second, "the destroy's cancel request", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.vms[r.ID]
		return e != nil && e.cancel != ""
	})
	select {
	case err := <-destroyed:
		t.Fatalf("destroy returned while the create held the record: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if h.count("rmjail")+h.count("rmcgroup")+h.count("kill") != 0 {
		t.Fatal("a cleanup ran beside the create")
	}
	g.open()
	cerr := <-created
	if cerr == nil || !strings.Contains(cerr.Error(), "cancelled: destroy requested by its owner") || !strings.Contains(cerr.Error(), "rolled back") {
		t.Fatalf("create: %v", cerr)
	}
	if err := <-destroyed; err != nil {
		t.Fatalf("destroy: %v", err)
	}
	tr.dump(t)
	if h.count("start") != 0 {
		t.Fatal("the VM booted after its destroy was requested")
	}
	if h.count("rmcgroup") != 1 || h.count("rmjail") != 1 || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("cleanups: rmcgroup %d rmjail %d leaks %v", h.count("rmcgroup"), h.count("rmjail"), h.leaks(r.ID))
	}
	if _, ok := held(s, r.ID); ok || len(s.History()) != 1 {
		t.Fatalf("held %v history %+v", ok, s.History())
	}
}

// The deadline reaper does not block on a create in progress: it asks it
// to roll back and moves on.
func TestDeadlineDuringCreateRollsBack(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("cgroup/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	clock.pass(t, s, r.ID) // the deadline passes while the create runs
	done := make(chan struct{})
	go func() { s.ReapOnce(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the reaper blocked on a create in progress")
	}
	g.open()
	if err := <-created; err == nil || !strings.Contains(err.Error(), "cancelled: deadline") {
		t.Fatalf("create past its deadline: %v", err)
	}
	if h.count("start") != 0 || len(h.leaks(r.ID)) != 0 || used(s)[2] != 0 {
		t.Fatalf("start %d leaks %v used %v", h.count("start"), h.leaks(r.ID), used(s))
	}
}

// Two creates of one record: the second is refused, one VM boots.
func TestConcurrentCreateIsRefused(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("prepare/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	if _, err := s.Create(owner1, r.ID); !errors.Is(err, ErrState) || !strings.Contains(err.Error(), "create in progress") {
		t.Fatalf("second create: %v", err)
	}
	g.open()
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	if h.count("prepare") != 1 || h.count("start") != 1 {
		t.Fatalf("prepare %d start %d", h.count("prepare"), h.count("start"))
	}
}

// Stop and connect during a create are refused (the record is busy); they
// neither wait for nor disturb it, and once it runs they reach its VMM.
func TestStopAndConnectDuringCreateAreRefused(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("start/after", g.wait) // the VMM exists, StartVM has not returned
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	if err := s.Stop(owner1, r.ID); !errors.Is(err, ErrState) {
		t.Fatalf("stop during a create: %v", err)
	}
	if _, err := s.Connect(owner1, r.ID, 5000); !errors.Is(err, ErrState) {
		t.Fatalf("connect during a create: %v", err)
	}
	if h.count("kill")+h.count("connect") != 0 {
		t.Fatal("a refused stop or connect reached the host")
	}
	g.open()
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	f, err := s.Connect(owner1, r.ID, 5000)
	if err != nil {
		t.Fatalf("connect to the running VM: %v", err)
	}
	f.Close()
	if err := s.Stop(owner1, r.ID); err != nil || h.exists("vmm", r.ID) {
		t.Fatalf("stop of the running VM: %v (vmm still there: %v)", err, h.exists("vmm", r.ID))
	}
}

// An operator's clear of a record being created is refused at once: it
// neither waits on the create nor asks it to roll back.
func TestRefusedClearDoesNotCancelACreate(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("prepare/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	cleared := make(chan error, 1)
	go func() { cleared <- s.ClearQuarantine(r.ID) }()
	select {
	case err := <-cleared:
		if !errors.Is(err, ErrState) {
			t.Fatalf("clear of a record being created: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clear waited on a record being created")
	}
	s.mu.Lock()
	cancel := s.vms[r.ID].cancel
	s.mu.Unlock()
	if cancel != "" {
		t.Fatalf("a refused clear cancelled the create: %q", cancel)
	}
	g.open()
	if err := <-created; err != nil {
		t.Fatalf("the create: %v", err)
	}
}

// Destroys, a clear and the reaper converging on one record run one
// cleanup: every removal happens once and the record is released once.
// The clear, which never waits, is refused while the teardown runs.
func TestOverlappingTeardownsRunOnce(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	clock.pass(t, s, r.ID) // past its deadline: the reaper wants it too
	g := newGate()
	t.Cleanup(g.open)
	h.at("rmcgroup/before", g.wait)
	first := make(chan error, 1)
	go func() { first <- s.Destroy(owner1, r.ID) }()
	<-g.reached
	second := make(chan error, 1)
	go func() { second <- s.Destroy(owner1, r.ID) }()
	cleared := make(chan error, 1)
	go func() { cleared <- s.ClearQuarantine(r.ID) }()
	select {
	case err := <-cleared:
		if !errors.Is(err, ErrState) {
			t.Fatalf("clear during a teardown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the clear waited on a teardown in progress")
	}
	s.ReapOnce() // busy: left alone, returns at once
	if _, err := s.Connect(owner1, r.ID, 5000); !errors.Is(err, ErrState) {
		t.Fatalf("connect during a teardown: %v", err)
	}
	if err := s.Stop(owner1, r.ID); !errors.Is(err, ErrState) {
		t.Fatalf("stop during a teardown: %v", err)
	}
	if rec, err := s.Inspect(owner1, r.ID); err != nil || rec.State != StateStopping {
		t.Fatalf("inspect during a teardown: %+v %v", rec, err)
	}
	g.open()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second destroy: %v", err)
	}
	if err := s.ClearQuarantine(r.ID); !errors.Is(err, ErrUnknown) {
		t.Fatalf("clear of a released record: %v", err)
	}
	tr.dump(t)
	for op, want := range map[string]int{"rmcgroup": 1, "rmdisk": 1, "rmjail": 1, "start": 1} {
		if n := h.count(op); n != want {
			t.Fatalf("%s ran %d times, want %d", op, n, want)
		}
	}
	if len(s.History()) != 1 || used(s)[2] != 0 {
		t.Fatalf("history %+v used %v", s.History(), used(s))
	}
}

// The reaper's candidates are pointers taken before it starts. A
// candidate released while the reaper worked on another is skipped, even
// when its attempt has been reserved again under the same id meanwhile:
// the new incarnation sees no host operation and keeps its record.
func TestStaleReaperCannotTouchANewIncarnation(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	// stage 01 fixture adaptation: the reaper no longer pauses between
	// candidates, so the stale-candidate window is held at the pass's own
	// point (candidates taken, none looked at) instead of inside w's
	// teardown
	entered, release := holdAt(&cfg, "reaper: candidates taken")
	s := open(t, cfg, h, nil)
	w := mustReserve(t, s, owner1, expiring("0vw", 2, 1024))
	x := mustReserve(t, s, owner1, expiring("0vx", 2, 1024))
	for _, id := range []string{w.ID, x.ID} {
		if _, err := s.Create(owner1, id); err != nil {
			t.Fatal(err)
		}
	}
	clock.pass(t, s, w.ID, x.ID) // both past their deadlines
	reaped := make(chan struct{})
	go func() { s.ReapOnce(); close(reaped) }()
	<-entered // the reaper holds its candidates [w, x] and has acted on neither
	if err := s.Destroy(owner1, x.ID); err != nil {
		t.Fatal(err)
	}
	fresh := lockedReq("0vx", 1, 128) // the same attempt, the same id, a new incarnation
	x2 := mustReserve(t, s, owner1, fresh)
	if x2.ID != x.ID {
		t.Fatalf("ids differ: %s %s", x2.ID, x.ID)
	}
	mark := len(tr.all())
	close(release)
	<-reaped
	for _, e := range tr.all()[mark:] {
		if e.id == x.ID {
			tr.dump(t)
			t.Fatalf("the reaper's stale candidate acted on the new incarnation: %s", e)
		}
	}
	rec, ok := held(s, x.ID)
	d, dok := durable(t, cfg.StateDir, x.ID)
	if !ok || rec.State != StatePreparing || rec.CPUs != 1 || !dok || d.State != StatePreparing || d.CPUs != 1 {
		t.Fatalf("new incarnation: memory %+v %v durable %+v %v", rec, ok, d, dok)
	}
	if _, ok := held(s, w.ID); ok {
		t.Fatal("w was not reaped")
	}
	if _, err := s.Create(owner1, x.ID); err != nil {
		t.Fatalf("the new incarnation cannot boot: %v", err)
	}
}

// The reaper's other kind of stale candidate: a reservation whose
// publication failed after the reaper took its snapshot. It is dropped
// still `preparing` — never acknowledged, it has no terminal state — so
// only its released flag keeps the reaper away from it and from the new
// incarnation of its attempt.
func TestStaleReaperSkipsAReservationDroppedMeanwhile(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	// stage 01 fixture adaptation: the reaper no longer pauses between
	// candidates, so the window — x released after the snapshot, before
	// the reaper looks at it — is held at the pass's own point
	entered, release := holdAt(&cfg, "reaper: candidates taken")
	s := open(t, cfg, h, fsys)
	w := mustReserve(t, s, owner1, expiring("0vw", 2, 1024))
	if _, err := s.Create(owner1, w.ID); err != nil {
		t.Fatal(err)
	}
	xid := IDFor(cfg.IDPrefix, "0vx")
	// x's first publication stops at its fsync, then fails
	pub := newGate()
	t.Cleanup(pub.open)
	x := fsys.inject(&fsFault{op: "sync", path: xid + ".json.tmp", mode: "before", err: errInjected, times: 1, block: func() { pub.wait("") }})
	reserved := make(chan error, 1)
	go func() {
		_, err := s.Reserve(owner1, expiring("0vx", 2, 1024))
		reserved <- err
	}()
	<-pub.reached               // x is held and charged, its publication in flight
	clock.pass(t, s, w.ID, xid) // w and x past their deadlines
	reaped := make(chan struct{})
	go func() { s.ReapOnce(); close(reaped) }()
	<-entered // the reaper's candidates are [w, x]; it has looked at neither
	pub.open()
	if err := <-reserved; !errors.Is(err, errInjected) {
		t.Fatalf("x's failed publication: %v", err)
	}
	mustReserve(t, s, owner1, lockedReq("0vx", 1, 128)) // the same id, a new incarnation
	mark := len(tr.all())
	close(release)
	<-reaped
	for _, e := range tr.all()[mark:] {
		if e.id == xid {
			tr.dump(t)
			t.Fatalf("the reaper acted on a reservation dropped meanwhile: %s", e)
		}
	}
	rec, ok := held(s, xid)
	d, dok := durable(t, cfg.StateDir, xid)
	if x.hits != 1 || !ok || rec.CPUs != 1 || !dok || d.CPUs != 1 || d.State != StatePreparing {
		t.Fatalf("new incarnation: memory %+v %v durable %+v %v (fault hits %d)", rec, ok, d, dok, x.hits)
	}
}

// A destroy waiting for a record's token holds that record, not its id:
// if the record is released and its attempt reserved again meanwhile, the
// waiter returns (idempotent) without touching the new incarnation.
func TestStaleDestroyWaiterCannotTouchANewIncarnation(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	x := mustReserve(t, s, owner1, lockedReq("0vx", 2, 1024))
	s.mu.Lock()
	old := s.vms[x.ID]
	s.take(old, "create") // an operation in progress on the old incarnation
	s.mu.Unlock()
	destroyed := make(chan error, 1)
	go func() { destroyed <- s.Destroy(owner1, x.ID) }()
	waitFor(t, 2*time.Second, "the destroy waiting on the old record", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return old.cancel != ""
	})
	// from here no host operation on this id is legitimate: the old
	// incarnation is released without one, the new one is only reserved
	mark := len(tr.all())
	// the old incarnation is released meanwhile, as a teardown releases it
	// — fixture adaptation (late-accounting ruling 01; §11.8): a release
	// drops its entry only with its disposition, confirmed; before it, the
	// fixture dropped the entry alone
	if _, err := s.st.remove(x.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	old.released, old.disposition = true, Release{By: ByTimelyCleanup, Final: StateDestroyed}
	s.drop(old)
	s.mu.Unlock()
	x2 := mustReserve(t, s, owner1, lockedReq("0vx", 1, 128))
	s.mu.Lock()
	s.put(old) // the operation ends; the waiter wakes
	s.mu.Unlock()
	if err := <-destroyed; err != nil {
		t.Fatalf("stale destroy: %v", err)
	}
	for _, e := range tr.all()[mark:] {
		if e.id == x.ID {
			t.Fatalf("the stale destroy acted on the new incarnation: %s", e)
		}
	}
	if rec, ok := held(s, x2.ID); !ok || rec.CPUs != 1 || rec.State != StatePreparing {
		t.Fatalf("new incarnation %+v %v", rec, ok)
	}
	if d, ok := durable(t, cfg.StateDir, x2.ID); !ok || d.CPUs != 1 {
		t.Fatalf("new incarnation's record %+v %v", d, ok)
	}
}

// Shutdown refuses new work, lets the operation in progress finish, then
// tears down what it owns and gives the state directory back.
func TestShutdownWaitsForAnOperationThenTearsDown(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	idle := mustReserve(t, s, owner2, lockedReq("0v2", 1, 128))
	g := newGate()
	t.Cleanup(g.open)
	h.at("start/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	shut := make(chan error, 1)
	go func() { shut <- s.Shutdown(context.Background()) }()
	waitFor(t, 2*time.Second, "closing", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closing })
	if _, err := s.Reserve(owner1, lockedReq("0v3", 1, 128)); !errors.Is(err, ErrClosed) {
		t.Fatalf("reserve during shutdown: %v", err)
	}
	if err := s.Destroy(owner2, idle.ID); !errors.Is(err, ErrClosed) {
		t.Fatalf("destroy during shutdown: %v", err)
	}
	select {
	case err := <-shut:
		t.Fatalf("shutdown returned while an operation held a record: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	g.open()
	if err := <-created; err != nil {
		t.Fatalf("the create in progress: %v", err)
	}
	if err := <-shut; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if len(s.All()) != 0 || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("left after shutdown: %+v %v", s.All(), h.leaks(r.ID))
	}
	s2, err := NewService(cfg, h)
	if err != nil {
		t.Fatalf("the state directory was not given back: %v", err)
	}
	s2.Close()
}

// Close is a process stop: new work refused, the operation in progress
// finishes, nothing is torn down, the lock is given back.
func TestCloseWaitsForTheCurrentOperation(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("start/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("close returned while an operation held a record: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	g.open()
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	s2 := open(t, cfg, h, nil)
	if rec, ok := held(s2, r.ID); !ok || rec.State != StateRunning || !h.exists("vmm", r.ID) {
		t.Fatalf("after a stop the VM is still owned and running: %+v %v", rec, ok)
	}
}
