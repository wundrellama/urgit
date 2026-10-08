package launcher

// A settlement ends before the state is handed on (INTEGRATION.md §11.11;
// independent review 05, R5-1): the closure or the certification of a
// request no record carries is counted from its beginning to its end, and a
// terminal call — Close, Shutdown, serve's stop — gives the state lock up
// only once it has ended. It delays no teardown, begins nothing once the
// service stops, and gives its count back on every path; the records
// directory's recertification is counted the same way. Private files, the
// nonexecuting model host and a fault that holds one syscall; a "second
// owner" is another Service over the same state directory, not a process.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// holdPoint holds a syscall (a fault's block) until it is freed.
type holdPoint struct {
	reached, release chan struct{}
	once, freed      sync.Once
}

func newHold() *holdPoint {
	return &holdPoint{reached: make(chan struct{}), release: make(chan struct{})}
}

func (h *holdPoint) block() {
	h.once.Do(func() { close(h.reached) })
	<-h.release
}

func (h *holdPoint) free() { h.freed.Do(func() { close(h.release) }) }

func (h *holdPoint) wait(t *testing.T, what string) {
	t.Helper()
	select {
	case <-h.reached:
	case <-time.After(5 * time.Second):
		t.Fatalf("fixture: %s was not reached", what)
	}
}

// ownable says whether another service can take the state now; one that
// could gives it back at once.
func ownable(t *testing.T, cfg Config, h Host) bool {
	t.Helper()
	next, err := NewService(cfg, h)
	if err != nil {
		if !errors.Is(err, ErrStateBusy) {
			t.Fatalf("fixture: a second open failed otherwise: %v", err)
		}
		return false
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	return true
}

// returned says whether ch delivers within d; what it delivered is put back.
func returned(ch chan error, d time.Duration) bool {
	select {
	case err := <-ch:
		ch <- err
		return true
	case <-time.After(d):
		return false
	}
}

// joined is what ch delivers, or a failure of the test after d.
func joined(t *testing.T, ch chan error, what string, d time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(d):
		t.Fatalf("%s did not end", what)
		return nil
	}
}

// closedFor settles r in the background and reports whether it answered
// closed.
func closedFor(s *Service, attempt, r string) chan error {
	out := make(chan error, 1)
	go func() {
		st, err := s.Settle(owner1, attempt, r)
		if err == nil && st.Outcome != SettledClosed {
			err = fmt.Errorf("outcome %q", st.Outcome)
		}
		out <- err
	}()
	return out
}

// refusedLater is the next owner's proof that r's closure is in the state
// it loaded.
func refusedLater(t *testing.T, cfg Config, h Host, attempt, r string) {
	t.Helper()
	next := open(t, cfg, h, nil)
	if _, err := next.Reserve(owner1, requested(attempt, r)); !errors.Is(err, ErrSettled) {
		t.Fatalf("the closure is not in the state the next owner loaded: %v", err)
	}
}

// Every terminal call, at every stage of a settlement's publication: the
// closure's rename, requests/'s link certified by the state directory's
// fsync, an entry found certified by its own fsync. The terminal call does
// not return, and no second owner can open the state, until the settlement
// has ended; it then answers closed, the call returns, and the next owner
// finds the closure.
func TestASettlementEndsBeforeTheStateIsHandedOn(t *testing.T) {
	stages := []struct {
		name  string
		found bool // the request's entry is there already: its certification
		fault func(cfg Config, r string, h *holdPoint) *fsFault
	}{
		{"the closure's rename", false, func(_ Config, r string, h *holdPoint) *fsFault {
			return &fsFault{op: "rename", path: r + ".json", times: 1, block: h.block}
		}},
		{"requests' link certified", false, func(cfg Config, _ string, h *holdPoint) *fsFault {
			return &fsFault{op: "syncdir", path: cfg.StateDir, times: 1, block: h.block}
		}},
		{"an entry found certified", true, func(_ Config, r string, h *holdPoint) *fsFault {
			return &fsFault{op: "sync", path: r + ".json", times: 1, block: h.block}
		}},
	}
	terminals := []struct {
		name string
		end  func(s *Service) error
	}{
		{"Close", func(s *Service) error { return s.Close() }},
		{"Shutdown", func(s *Service) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return s.Shutdown(ctx)
		}},
		{"BeginStop then Shutdown", func(s *Service) error {
			s.BeginStop("launcher stopping")
			return s.Shutdown(context.Background())
		}},
	}
	for _, st := range stages {
		for _, tc := range terminals {
			t.Run(st.name+", "+tc.name, func(t *testing.T) {
				tr := &tracer{}
				fsys := newFaultFS(tr)
				h := newModelHost(t, tr)
				cfg := testConfig(t.TempDir())
				s := open(t, cfg, h, fsys)
				r := token(t)
				if st.found {
					if err := <-closedFor(s, "0vend", r); err != nil {
						t.Fatalf("fixture: %v", err)
					}
				}
				hold := newHold()
				t.Cleanup(hold.free)
				fsys.inject(st.fault(cfg, r, hold))
				settled := closedFor(s, "0vend", r)
				hold.wait(t, "the settlement's "+st.name)
				ended := make(chan error, 1)
				go func() { ended <- tc.end(s) }()
				early := returned(ended, 200*time.Millisecond)
				handedOn := ownable(t, cfg, h)
				hold.free()
				if early || handedOn {
					t.Fatalf("A TERMINAL CALL GAVE THE STATE UP WHILE A SETTLEMENT RAN (%s, %s): returned %v, a second owner opened %v", st.name, tc.name, early, handedOn)
				}
				if err := joined(t, settled, "the settlement", 5*time.Second); err != nil {
					t.Fatalf("the settlement: %v", err)
				}
				if err := joined(t, ended, "the terminal call", 5*time.Second); err != nil {
					t.Fatalf("the terminal call: %v", err)
				}
				refusedLater(t, cfg, h, "0vend", r)
			})
		}
	}
}

// Several settlements in progress: the state is given up only once every
// one has ended, not the first.
func TestEverySettlementInProgressIsWaitedFor(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r1, r2 := token(t), token(t)
	h1, h2 := newHold(), newHold()
	t.Cleanup(h1.free)
	t.Cleanup(h2.free)
	fsys.inject(&fsFault{op: "rename", path: r1 + ".json", times: 1, block: h1.block})
	fsys.inject(&fsFault{op: "rename", path: r2 + ".json", times: 1, block: h2.block})
	s1, s2 := closedFor(s, "0vone", r1), closedFor(s, "0vtwo", r2)
	h1.wait(t, "the first settlement's rename")
	h2.wait(t, "the second settlement's rename")
	ended := make(chan error, 1)
	go func() { ended <- s.Close() }()
	h1.free()
	if err := joined(t, s1, "the first settlement", 5*time.Second); err != nil {
		t.Fatalf("the first settlement: %v", err)
	}
	early := returned(ended, 200*time.Millisecond)
	handedOn := ownable(t, cfg, h)
	h2.free()
	if early || handedOn {
		t.Fatalf("A TERMINAL CALL GAVE THE STATE UP WHILE ANOTHER SETTLEMENT RAN: returned %v, a second owner opened %v", early, handedOn)
	}
	if err := joined(t, s2, "the second settlement", 5*time.Second); err != nil {
		t.Fatalf("the second settlement: %v", err)
	}
	if err := joined(t, ended, "Close", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	refusedLater(t, cfg, h, "0vone", r1)
}

// Shutdown's teardowns never wait for a settlement — it touches no record —
// while the state is still given up only once the settlement has ended.
func TestShutdownsTeardownsNeverWaitForASettlement(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	running := mustReserve(t, s, owner1, requested("0vrun", token(t)))
	if _, err := s.CreateOf(owner1, running.Ref()); err != nil {
		t.Fatal(err)
	}
	r := token(t)
	hold := newHold()
	t.Cleanup(hold.free)
	fsys.inject(&fsFault{op: "rename", path: r + ".json", times: 1, block: hold.block})
	settled := closedFor(s, "0vwait", r)
	hold.wait(t, "the settlement's rename")
	ended := make(chan error, 1)
	go func() { ended <- s.Shutdown(context.Background()) }()
	torn := false
	for limit := time.Now().Add(5 * time.Second); !torn && time.Now().Before(limit); time.Sleep(10 * time.Millisecond) {
		_, still := held(s, running.ID)
		torn = !still
	}
	handedOn := ownable(t, cfg, h)
	hold.free()
	if !torn {
		t.Fatalf("A TEARDOWN WAITED FOR A SETTLEMENT: %s was still held, the settlement's publication in progress", running.ID)
	}
	if handedOn {
		t.Fatal("A TERMINAL CALL GAVE THE STATE UP WHILE A SETTLEMENT RAN: shutdown, after its teardowns")
	}
	if err := joined(t, settled, "the settlement", 5*time.Second); err != nil {
		t.Fatalf("the settlement: %v", err)
	}
	if err := joined(t, ended, "Shutdown", 5*time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	refusedLater(t, cfg, h, "0vwait", r)
}

// Shutdown's deadline bounds its wait for a settlement as it bounds its
// drain: past it, Shutdown returns the work still in progress as its error
// and keeps the state; a later Close gives it up once the settlement ends.
func TestShutdownKeepsTheStatePastItsDeadlineWhileASettlementRuns(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r := token(t)
	hold := newHold()
	t.Cleanup(hold.free)
	fsys.inject(&fsFault{op: "rename", path: r + ".json", times: 1, block: hold.block})
	settled := closedFor(s, "0vdue", r)
	hold.wait(t, "the settlement's rename")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- s.Shutdown(ctx) }()
	if !returned(ended, 5*time.Second) {
		hold.free()
		t.Fatal("SHUTDOWN'S DEADLINE WAS NOT KEPT: it did not return, a settlement still running")
	}
	err := <-ended
	handedOn := ownable(t, cfg, h)
	if err == nil || handedOn {
		hold.free()
		t.Fatalf("SHUTDOWN GAVE THE STATE UP PAST ITS DEADLINE WITH A SETTLEMENT RUNNING: %v, a second owner opened %v", err, handedOn)
	}
	hold.free()
	if err := joined(t, settled, "the settlement", 5*time.Second); err != nil {
		t.Fatalf("the settlement: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("the later Close: %v", err)
	}
	refusedLater(t, cfg, h, "0vdue", r)
}

// Once the service stops, no settlement begins: a new one is refused, and
// one waiting — behind another settlement of its request, or behind a
// reserve still publishing its record — is refused as the stop wakes it,
// while what was already in progress is waited for.
func TestNoSettlementBeginsOnceTheServiceStops(t *testing.T) {
	t.Run("a new settlement", func(t *testing.T) {
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, newModelHost(t, &tracer{}), nil)
		s.BeginStop("launcher stopping")
		r := token(t)
		if st, err := s.Settle(owner1, "0vnew", r); !errors.Is(err, ErrClosed) || st.Outcome != "" {
			t.Fatalf("A SETTLEMENT BEGAN AFTER THE SERVICE STOPPED: %+v %v", st, err)
		}
		if _, ok := ledger(t, cfg, r); ok {
			t.Fatal("A SETTLEMENT BEGAN AFTER THE SERVICE STOPPED: its closure was written")
		}
	})
	t.Run("one waiting for another of its request", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		r := token(t)
		hold := newHold()
		t.Cleanup(hold.free)
		fsys.inject(&fsFault{op: "rename", path: r + ".json", times: 1, block: hold.block})
		first := closedFor(s, "0vtwin", r)
		hold.wait(t, "the first settlement's rename")
		waiting := make(chan error, 1)
		go func() { _, err := s.Settle(owner1, "0vtwin", r); waiting <- err }()
		time.Sleep(50 * time.Millisecond) // it waits for the first
		ended := make(chan error, 1)
		go func() { ended <- s.Close() }()
		refused := returned(waiting, 2*time.Second)
		hold.free()
		if !refused {
			t.Fatal("A WAITING SETTLEMENT WAS NOT REFUSED ONCE THE SERVICE STOPPED: behind another of its request")
		}
		if err := <-waiting; !errors.Is(err, ErrClosed) {
			t.Fatalf("A WAITING SETTLEMENT WAS NOT REFUSED ONCE THE SERVICE STOPPED: %v", err)
		}
		if err := joined(t, first, "the first settlement", 5*time.Second); err != nil {
			t.Fatalf("the first settlement: %v", err)
		}
		if err := joined(t, ended, "Close", 5*time.Second); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("one waiting for a reserve still publishing", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		r := token(t)
		hold := newHold()
		t.Cleanup(hold.free)
		fsys.inject(&fsFault{op: "rename", path: IDFor(cfg.IDPrefix, "0vpub") + ".json", times: 1, block: hold.block})
		reserved := make(chan error, 1)
		go func() { _, err := s.Reserve(owner1, requested("0vpub", r)); reserved <- err }()
		hold.wait(t, "the reserve's record rename")
		waiting := make(chan error, 1)
		go func() { _, err := s.Settle(owner1, "0vpub", r); waiting <- err }()
		time.Sleep(50 * time.Millisecond) // it waits for the reserve
		ended := make(chan error, 1)
		go func() { ended <- s.Close() }()
		refused := returned(waiting, 2*time.Second)
		hold.free()
		if !refused {
			t.Fatal("A WAITING SETTLEMENT WAS NOT REFUSED ONCE THE SERVICE STOPPED: behind a reserve")
		}
		if err := <-waiting; !errors.Is(err, ErrClosed) {
			t.Fatalf("A WAITING SETTLEMENT WAS NOT REFUSED ONCE THE SERVICE STOPPED: %v", err)
		}
		if err := joined(t, reserved, "the reserve", 5*time.Second); err != nil {
			t.Fatalf("the reserve, already in progress: %v", err)
		}
		if err := joined(t, ended, "Close", 5*time.Second); err != nil {
			t.Fatal(err)
		}
	})
}

// A settlement that fails gives the state back like one that answers: its
// count is released on the error path, and the terminal call ends.
func TestAFailedSettlementGivesTheStateBack(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r := token(t)
	hold := newHold()
	t.Cleanup(hold.free)
	fsys.inject(&fsFault{op: "rename", path: r + ".json", mode: "before", err: errInjected, times: 1, block: hold.block})
	failed := make(chan error, 1)
	go func() { _, err := s.Settle(owner1, "0vfail", r); failed <- err }()
	hold.wait(t, "the closure's rename")
	ended := make(chan error, 1)
	go func() { ended <- s.Close() }()
	hold.free()
	if err := joined(t, failed, "the settlement", 5*time.Second); !errors.Is(err, ErrNotDurable) {
		t.Fatalf("fixture: the settlement fails: %v", err)
	}
	if !returned(ended, 2*time.Second) {
		t.Fatal("A FAILED SETTLEMENT KEPT THE STATE: Close did not end")
	}
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
	// never closed, never admitted: the next owner settles it
	next := open(t, cfg, h, nil)
	if st, err := next.Settle(owner1, "0vfail", r); err != nil || st.Outcome != SettledClosed {
		t.Fatalf("the next owner's settlement: %+v %v", st, err)
	}
}

// The records directory's recertification (§11.6) is counted like a
// settlement: a terminal call waits for one in progress, and none begins
// once the service stops.
func TestARecertificationEndsBeforeTheStateIsHandedOn(t *testing.T) {
	fenced := func(t *testing.T) (*Service, *faultFS, Config, Host) {
		t.Helper()
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		fsys.inject(&fsFault{op: "syncdir", path: "attempts", mode: "before", err: errInjected, times: 1})
		s := open(t, cfg, h, fsys)
		if len(s.Problems()) == 0 {
			t.Fatal("fixture: the open is fenced, its records directory uncertified")
		}
		return s, fsys, cfg, h
	}
	t.Run("one in progress", func(t *testing.T) {
		s, fsys, cfg, h := fenced(t)
		hold := newHold()
		t.Cleanup(hold.free)
		fsys.inject(&fsFault{op: "syncdir", path: "attempts", times: 1, block: hold.block})
		passed := make(chan error, 1)
		go func() { s.ReapOnce(); passed <- nil }()
		hold.wait(t, "the recertification's fsync")
		ended := make(chan error, 1)
		go func() { ended <- s.Close() }()
		early := returned(ended, 200*time.Millisecond)
		handedOn := ownable(t, cfg, h)
		hold.free()
		if early || handedOn {
			t.Fatalf("A TERMINAL CALL GAVE THE STATE UP DURING A RECERTIFICATION: returned %v, a second owner opened %v", early, handedOn)
		}
		joined(t, passed, "the reaper pass", 5*time.Second)
		if err := joined(t, ended, "Close", 5*time.Second); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("none once the service stops", func(t *testing.T) {
		s, fsys, _, _ := fenced(t)
		s.BeginStop("launcher stopping")
		x := fsys.inject(&fsFault{op: "syncdir", path: "attempts"})
		s.ReapOnce()
		if x.hits != 0 {
			t.Fatalf("A RECERTIFICATION BEGAN AFTER THE SERVICE STOPPED: %d fsync(s) of the records directory", x.hits)
		}
	})
}

// serve's stop, in its order and on the wire: a client's settlement in
// progress when the listener closes (Serve returns without joining its
// handlers), BeginStop, then Shutdown. Shutdown waits for the settlement
// before the state goes; a call on a connection the stop found open is
// refused; the client's answer is a closure the next owner finds.
func TestServeStopWaitsForASettlementOnTheWire(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "s.sock")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	served := make(chan error, 1)
	go func() { served <- sv.Serve(l) }()
	cl, err := Dial(context.Background(), "s.sock", owner1.Daemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	late, err := Dial(context.Background(), "s.sock", owner1.Daemon) // open when the stop comes
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	r := token(t)
	hold := newHold()
	t.Cleanup(hold.free)
	fsys.inject(&fsFault{op: "rename", path: r + ".json", times: 1, block: hold.block})
	type answer struct {
		st  Settlement
		err error
	}
	answered := make(chan answer, 1)
	go func() { st, err := cl.Settle("0vwire", r); answered <- answer{st, err} }()
	hold.wait(t, "the settlement's rename, on the wire")
	l.Close()
	if err := joined(t, served, "Serve", 5*time.Second); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	s.BeginStop("launcher stopping")
	ended := make(chan error, 1)
	go func() { ended <- s.Shutdown(context.Background()) }()
	early := returned(ended, 200*time.Millisecond)
	handedOn := ownable(t, cfg, h)
	lst, lerr := late.Settle("0vlate", token(t))
	hold.free()
	if early || handedOn {
		t.Fatalf("SERVE'S STOP GAVE THE STATE UP WHILE A SETTLEMENT ON THE WIRE RAN: shutdown returned %v, a second owner opened %v", early, handedOn)
	}
	if lerr == nil || !strings.Contains(lerr.Error(), ErrClosed.Error()) {
		t.Fatalf("A SETTLEMENT BEGAN ON THE WIRE AFTER THE STOP: %+v %v", lst, lerr)
	}
	var got answer
	select {
	case got = <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("the client's settlement did not end")
	}
	if got.err != nil || got.st.Outcome != SettledClosed {
		t.Fatalf("the client's settlement: %+v %v", got.st, got.err)
	}
	if err := joined(t, ended, "Shutdown", 5*time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	refusedLater(t, cfg, h, "0vwire", r)
}
