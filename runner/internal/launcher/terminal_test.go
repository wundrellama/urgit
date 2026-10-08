package launcher

// Review 02's class D (runner/launcher/LIFECYCLE.md §4.3): a Shutdown or
// a Close owns the service's end from its drain until the state lock is
// released, and nothing else ends the service meanwhile. The reviewer held
// Shutdown right after its drain through a review-only source overlay;
// Config.at is that same point in the production code (nil there), so the
// interleaving is exercised on every run, by scheduling, not by sleeping.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// holdAt makes the first terminal call to reach point stop there until
// release is closed; entered is closed when it gets there. A later one
// passes (it may only get there if it did not wait for the first).
func holdAt(cfg *Config, point string) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	cfg.at = func(p string) {
		if p != point {
			return
		}
		first := false
		once.Do(func() {
			first = true
			close(entered)
		})
		if first {
			<-release
		}
	}
	return entered, release
}

// Review 02's TestIndependentR2ShutdownCannotOutliveStateOwnership, ported
// to Config.at: Shutdown held right after its drain and before its
// teardowns — where Close used to release the lock to a new opener while
// the old Shutdown still had a record to remove.
func TestShutdownCannotOutliveStateOwnershipPort(t *testing.T) {
	for _, concurrentClose := range []bool{false, true} {
		name := "held-lock-control"
		if concurrentClose {
			name = "close-during-shutdown-gap"
		}
		t.Run(name, func(t *testing.T) {
			host := newFakeHost()
			cfg := Config{}
			entered, release := holdAt(&cfg, "shutdown: drained")
			s := newService(t, host, cfg)
			reply := reserve(t, s, ownerA, "review-shutdown-owner", 1, 128, time.Now().Add(time.Hour))
			cfg = s.cfg
			cfg.at = nil
			shutdown := make(chan error, 1)
			go func() { shutdown <- s.Shutdown(context.Background()) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("shutdown unlocked interval not reached")
			}
			closeDone := make(chan error, 1)
			closeReturned := false
			if concurrentClose {
				go func() { closeDone <- s.Close() }()
				select {
				case err := <-closeDone:
					closeReturned = true
					if err != nil {
						t.Error(err)
					}
				case <-time.After(100 * time.Millisecond): // A correct Close waits for the held shutdown.
				}
			}
			replacement, openErr := NewService(cfg, host)
			close(release)
			select {
			case err := <-shutdown:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(time.Second):
				t.Fatal("shutdown fixture did not join")
			}
			if concurrentClose && !closeReturned {
				select {
				case err := <-closeDone:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Fatal("Close fixture did not join")
				}
			}
			if replacement != nil {
				defer replacement.Close()
			}
			if !errors.Is(openErr, ErrStateBusy) {
				t.Fatalf("STATE OWNERSHIP ESCAPED: new opener=%v oldCloseReturned=%v oldShutdownRemovedJail=%v id=%s", openErr, closeReturned, host.has("rmjail "+reply.ID), reply.ID)
			}
		})
	}
}

// While a Shutdown owns the service's end — held after its drain — a Close
// or another Shutdown waits and no new opener gets the state; when either
// returns, the Shutdown's teardown has already happened, and then the
// state is free.
func TestTerminalCallsWaitForTheShutdownInProgress(t *testing.T) {
	for _, other := range []string{"a close", "another shutdown"} {
		t.Run(other, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			entered, release := holdAt(&cfg, "shutdown: drained")
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			free := cfg
			free.at = nil
			first := make(chan error, 1)
			go func() { first <- s.Shutdown(context.Background()) }()
			<-entered
			type result struct {
				err     error
				removed int // jail removals when it returned
			}
			second := make(chan result, 1)
			go func() {
				var err error
				if other == "a close" {
					err = s.Close()
				} else {
					err = s.Shutdown(context.Background())
				}
				second <- result{err, h.count("rmjail")}
			}()
			select {
			case res := <-second:
				t.Fatalf("%s returned while the shutdown held the service: %v", other, res.err)
			case <-time.After(100 * time.Millisecond):
			}
			if s2, err := openService(free, h, osFS{}); !errors.Is(err, ErrStateBusy) {
				if s2 != nil {
					s2.Close()
				}
				t.Fatalf("STATE OWNERSHIP ESCAPED: a new opener while the shutdown held the service: %v", err)
			}
			close(release)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			res := <-second
			if res.err != nil || res.removed != 1 {
				t.Fatalf("%s: %v, returning after %d jail removals, want the shutdown's 1", other, res.err, res.removed)
			}
			s3, err := openService(free, h, osFS{})
			if err != nil {
				t.Fatalf("the state after the shutdown: %v", err)
			}
			closeAtEnd(t, s3)
			if len(s3.All()) != 0 || len(h.leaks(r.ID)) != 0 {
				t.Fatalf("after the shutdown: records %+v leaks %v", s3.All(), h.leaks(r.ID))
			}
		})
	}
}

// A Shutdown whose context ends before its drain does acts no more: it
// keeps the lock (nothing was torn down, an operation still runs), and a
// later Close ends the service once that operation has finished.
func TestShutdownThatGivesUpLeavesTheEndToALaterClose(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	g := newGate()
	t.Cleanup(g.open)
	h.at("prepare/before", g.wait)
	created := make(chan error, 1)
	go func() { _, err := s.Create(owner1, r.ID); created <- err }()
	<-g.reached
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a shutdown whose drain timed out: %v", err)
	}
	if s2, err := openService(cfg, h, osFS{}); !errors.Is(err, ErrStateBusy) {
		if s2 != nil {
			s2.Close()
		}
		t.Fatalf("the lock left with a shutdown that gave up: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	g.open()
	if err := <-created; err != nil {
		t.Fatalf("the create in progress: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if h.count("rmjail") != 0 {
		t.Fatal("a shutdown that gave up tore something down")
	}
	s3 := open(t, cfg, h, nil)
	if rec, ok := held(s3, r.ID); !ok || rec.State != StateRunning {
		t.Fatalf("after the close: %+v %v", rec, ok)
	}
}
