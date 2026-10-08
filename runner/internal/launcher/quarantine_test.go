package launcher

// specs/ci-execution-contract.md §9: "Quarantine is cleared only by the
// root CLI `urgit-vm-launcher clear <id>` after an operator inspected
// it." These tests hold every path to a quarantined record — the owner,
// a waiter, the reaper, a restart, a failed or missing publication — and
// keep proof of absence apart from the operator's clearance.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The independent review's regression (kept byte-identical in
// .scratch/repair/reference/reviews/opus-lifecycle-review01/ and replayed
// there through a Go overlay), ported with its scenario, fixture and
// assertions unchanged: a real persisted quarantine after a failed disk
// removal, the failure healed, then the operator's clear (control) or the
// owner's retry, with and without a reopen. Added after its own checks:
// the refused retry reached no host operation.
func TestIndependentQuarantineRequiresOperatorPort(t *testing.T) {
	for _, mode := range []string{"operator-control", "owner-retry", "owner-after-reopen"} {
		t.Run(mode, func(t *testing.T) {
			host := newFakeHost()
			service := newService(t, host, Config{})
			reply := reserve(t, service, ownerA, "review-operator-boundary", 2, 1024, time.Now().Add(time.Hour))
			if _, err := service.Create(ownerA, reply.ID); err != nil {
				t.Fatal(err)
			}
			failedRemoval := errors.New("review removal failure")
			host.mu.Lock()
			host.fail["rmdisk"] = failedRemoval
			host.mu.Unlock()
			err := service.Destroy(ownerA, reply.ID)
			rec, inspectErr := service.Inspect(ownerA, reply.ID)
			if !errors.Is(err, ErrQuarantined) || inspectErr != nil || rec.State != StateQuarantined || !rec.HasDisk || !host.has("rmdisk "+reply.ID) {
				t.Fatalf("quarantine precondition not reached: destroy=%v inspect=%v record=%+v", err, inspectErr, rec)
			}
			host.mu.Lock()
			delete(host.fail, "rmdisk")
			host.mu.Unlock()
			if mode == "owner-after-reopen" {
				cfg := service.cfg
				service.Close()
				service = newService(t, host, cfg)
				rec, err = service.Inspect(ownerA, reply.ID)
				if err != nil || rec.State != StateQuarantined {
					t.Fatalf("restart fixture did not retain quarantine: %v %+v", err, rec)
				}
			}
			if mode == "operator-control" {
				if err = service.ClearQuarantine(reply.ID); err != nil {
					t.Fatal(err)
				}
				_, _, _, _, _, used := service.Budget()
				if used != 0 {
					t.Fatalf("operator control still charged %d guests", used)
				}
				return
			}
			host.mu.Lock()
			opsBefore := len(host.ops)
			host.mu.Unlock()
			err = service.Destroy(ownerA, reply.ID)
			rec, inspectErr = service.Inspect(ownerA, reply.ID)
			_, _, _, _, _, used := service.Budget()
			if !errors.Is(err, ErrQuarantined) || inspectErr != nil || rec.State != StateQuarantined || used != 1 {
				t.Fatalf("OPERATOR CLEARANCE BYPASSED: owner retry err=%v inspect=%v record=%+v used=%d", err, inspectErr, rec, used)
			}
			host.mu.Lock()
			opsAfter := host.ops[opsBefore:]
			host.mu.Unlock()
			if len(opsAfter) != 0 {
				t.Fatalf("the refused retry reached the host: %v", opsAfter)
			}
		})
	}
}

// A quarantine with a known pid (the VMM survived TERM and KILL) is refused
// to its owner even after the process has gone and a cleanup would
// succeed — before and after a reopen — and the reaper leaves it; the
// operator's clear releases it.
func TestOwnerCannotClearAKnownPIDQuarantine(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	s := open(t, testConfig(t.TempDir()), h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.hang[r.ID] = true
	h.mu.Unlock()
	err := s.Destroy(owner1, r.ID)
	rec, _ := held(s, r.ID)
	if !errors.Is(err, ErrQuarantined) || rec.State != StateQuarantined || !rec.HasVMM || rec.PID == 0 || !strings.Contains(rec.Reason, "still alive") {
		t.Fatalf("a surviving VMM: %v %+v", err, rec)
	}
	// the process ends by itself: a cleanup would now succeed
	h.mu.Lock()
	h.hang[r.ID] = false
	h.mu.Unlock()
	if err := os.Remove(h.marker("vmm", r.ID)); err != nil {
		t.Fatal(err)
	}
	ops := len(tr.all())
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || len(tr.all()) != ops {
		t.Fatalf("the owner cleared a known-pid quarantine: %v", err)
	}
	s = reopen(t, s, h)
	s.ReapOnce()
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || len(tr.all()) != ops {
		t.Fatalf("after a reopen the owner or the reaper cleared it: %v", err)
	}
	if err := s.ClearQuarantine(r.ID); err != nil {
		t.Fatalf("operator clear: %v", err)
	}
	if len(h.leaks(r.ID)) != 0 || used(s)[2] != 0 {
		t.Fatalf("after the clear: leaks %v used %v", h.leaks(r.ID), used(s))
	}
}

// waiting reports whether an operation is blocked for a record's token.
func waiting(s *Service) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting > 0
}

// A destroy that waited for another operation takes the record as that
// operation left it: quarantined, it is refused untouched — whichever
// operation quarantined it. The waiter is proven to be waiting before the
// quarantine happens.
func TestQuarantineReachedWhileAWaiterWaited(t *testing.T) {
	for _, by := range []string{"a create's rollback", "the deadline reaper", "another owner destroy"} {
		t.Run(by, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			clock := withClock(&cfg)
			s := open(t, cfg, h, nil)
			g := newGate()
			t.Cleanup(g.open)
			var r ReserveReply
			first := make(chan error, 1)
			switch by {
			case "a create's rollback":
				r = mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
				h.at("cgroup/before", g.wait)
				h.fail("rmcgroup", "before", errInjected)
				go func() { _, err := s.Create(owner1, r.ID); first <- err }()
			case "the deadline reaper":
				r = mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
				if _, err := s.Create(owner1, r.ID); err != nil {
					t.Fatal(err)
				}
				clock.pass(t, s, r.ID)
				h.at("rmcgroup/before", g.wait)
				h.fail("rmcgroup", "before", errInjected)
				go func() { s.ReapOnce(); first <- nil }()
			case "another owner destroy":
				r = mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
				if _, err := s.Create(owner1, r.ID); err != nil {
					t.Fatal(err)
				}
				h.at("rmcgroup/before", g.wait)
				h.fail("rmcgroup", "before", errInjected)
				go func() { first <- s.Destroy(owner1, r.ID) }()
			}
			<-g.reached
			if rec, _ := held(s, r.ID); rec.State == StateQuarantined {
				t.Fatalf("fixture: quarantined before the waiter arrived: %+v", rec)
			}
			waiter := make(chan error, 1)
			go func() { waiter <- s.Destroy(owner1, r.ID) }()
			waitFor(t, 2*time.Second, "the destroy waiting for the record", func() bool { return waiting(s) })
			g.open()
			if err := <-first; by != "the deadline reaper" && !errors.Is(err, ErrQuarantined) {
				t.Fatalf("the first operation did not quarantine: %v", err)
			}
			removals := h.count("rmcgroup")
			err := <-waiter
			tr.dump(t)
			rec, ok := held(s, r.ID)
			if !errors.Is(err, ErrQuarantined) || !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
				t.Fatalf("a waiter cleared the quarantine it waited into: %v; %+v %v", err, rec, ok)
			}
			if removals != 1 || h.count("rmcgroup") != 1 {
				t.Fatalf("the waiter ran a second teardown: rmcgroup %d then %d", removals, h.count("rmcgroup"))
			}
			h.heal("rmcgroup")
			if err := s.ClearQuarantine(r.ID); err != nil || used(s)[2] != 0 {
				t.Fatalf("operator clear: %v used %v", err, used(s))
			}
		})
	}
}

// The reaper never clears a quarantine: not one past its deadline, and
// not a candidate that became quarantined after the reaper took its
// candidates.
func TestReaperNeverClearsAQuarantine(t *testing.T) {
	t.Run("past its deadline", func(t *testing.T) {
		tr := &tracer{}
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		s := open(t, cfg, h, nil)
		r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		h.fail("rmcgroup", "before", errInjected)
		if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("destroy: %v", err)
		}
		h.heal("rmcgroup")
		clock.pass(t, s, r.ID)
		ops := len(tr.all())
		for i := 0; i < 3; i++ {
			s.ReapOnce()
		}
		if rec, _ := held(s, r.ID); rec.State != StateQuarantined || len(tr.all()) != ops {
			t.Fatalf("the reaper acted on a quarantine: %+v, ops %v", rec, tr.all()[ops:])
		}
	})
	t.Run("quarantined after the reaper's snapshot", func(t *testing.T) {
		tr := &tracer{}
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		// stage 01 fixture adaptation: the reaper no longer pauses between
		// candidates (each teardown runs on its own), so the window this
		// test needs — a candidate changing after the snapshot, before the
		// reaper looks at it — is held at the pass's own point instead of
		// inside w's teardown
		entered, release := holdAt(&cfg, "reaper: candidates taken")
		s := open(t, cfg, h, nil)
		w := mustReserve(t, s, owner1, expiring("0vw", 2, 1024))
		x := mustReserve(t, s, owner1, expiring("0vx", 2, 1024))
		for _, id := range []string{w.ID, x.ID} {
			if _, err := s.Create(owner1, id); err != nil {
				t.Fatal(err)
			}
		}
		clock.pass(t, s, w.ID, x.ID)
		reaped := make(chan struct{})
		go func() { s.ReapOnce(); close(reaped) }()
		<-entered // the reaper holds [w, x] and has looked at neither
		h.fail("rmcgroup", "before", errInjected)
		if err := s.Destroy(owner1, x.ID); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("destroy x: %v", err)
		}
		h.heal("rmcgroup")
		mark := len(tr.all())
		close(release)
		<-reaped
		for _, e := range tr.all()[mark:] {
			if e.id == x.ID {
				t.Fatalf("the reaper acted on a candidate quarantined meanwhile: %s", e)
			}
		}
		if rec, _ := held(s, x.ID); rec.State != StateQuarantined {
			t.Fatalf("x %+v", rec)
		}
	})
}

// The owner and the operator on one quarantined record: the owner is
// refused at once; while the operator's clear runs, the owner's destroy
// waits and then takes the clear's outcome — released (nil) or
// quarantined again (refused) — and never runs a teardown of its own.
//
// Stage 01 (recovery ruling A): the clear is the operator's cleanup retry,
// then a separate release. The owner waits on the retry and takes its
// outcome — the incident stays quarantined whatever the retry achieved, so
// the owner is refused either way — and the release follows on its own
// (with the combined seam, a waiter could take the record between the two
// steps, or after them: a race, not a scenario).
func TestOwnerAndOperatorConcurrently(t *testing.T) {
	for _, outcome := range []string{"the clear releases", "the clear fails again"} {
		t.Run(outcome, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			s := open(t, testConfig(t.TempDir()), h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			h.fail("rmdisk", "before", errInjected)
			if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("destroy: %v", err)
			}
			if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("the owner was not refused at once: %v", err)
			}
			if outcome == "the clear releases" {
				h.heal("rmdisk")
			}
			g := newGate()
			t.Cleanup(g.open)
			h.at("rmdisk/before", g.wait)
			cleared := make(chan error, 1)
			rec, _ := held(s, r.ID)
			go func() {
				_, err := s.RetryCleanup(rec.Selection())
				cleared <- err
			}()
			<-g.reached
			owner := make(chan error, 1)
			go func() { owner <- s.Destroy(owner1, r.ID) }()
			waitFor(t, 2*time.Second, "the owner waiting on the operator's clear", func() bool { return waiting(s) })
			g.open()
			cerr, oerr := <-cleared, <-owner
			if outcome == "the clear releases" {
				if cerr != nil || !errors.Is(oerr, ErrQuarantined) || used(s)[2] != 1 {
					t.Fatalf("retry %v, owner %v, used %v", cerr, oerr, used(s))
				}
				// the release is the operator's own, separate step, on the
				// incarnation and revision it inspects
				in, err := s.InspectIncident(rec.Selection())
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Release(in.Selection); err != nil || used(s)[2] != 0 {
					t.Fatalf("the release after the retry: %v, used %v", err, used(s))
				}
			} else if !errors.Is(cerr, ErrQuarantined) || !errors.Is(oerr, ErrQuarantined) || used(s)[2] != 1 {
				t.Fatalf("clear %v, owner %v, used %v", cerr, oerr, used(s))
			}
			if n := h.count("rmdisk"); n != 2 {
				t.Fatalf("rmdisk ran %d times, want 2 (the destroy's and the operator's): the owner ran its own teardown", n)
			}
		})
	}
}

// The operator's clear acts only on what the operator inspected: a
// quarantined record with no operation in progress. Asked while a teardown
// runs — which may end in a quarantine nobody has inspected yet — it is
// refused at once and changes nothing; the quarantine that teardown
// reaches stays charged until a clear asked after it.
func TestClearNeverWaitsIntoAQuarantine(t *testing.T) {
	for _, by := range []string{"an owner destroy", "the deadline reaper"} {
		t.Run(by, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			clock := withClock(&cfg)
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			if by == "the deadline reaper" {
				clock.pass(t, s, r.ID)
			}
			g := newGate()
			t.Cleanup(g.open)
			h.at("rmcgroup/before", g.wait)
			h.fail("rmcgroup", "before", errInjected)
			first := make(chan error, 1)
			if by == "an owner destroy" {
				go func() { first <- s.Destroy(owner1, r.ID) }()
			} else {
				go func() { s.ReapOnce(); first <- nil }()
			}
			<-g.reached // the teardown runs; its outcome is not known yet
			cleared := make(chan error, 1)
			go func() { cleared <- s.ClearQuarantine(r.ID) }()
			select {
			case err := <-cleared:
				if !errors.Is(err, ErrState) || !strings.Contains(err.Error(), "in progress") {
					t.Fatalf("a clear asked during a teardown: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the clear waited on a teardown in progress")
			}
			g.open()
			if err := <-first; by == "an owner destroy" && !errors.Is(err, ErrQuarantined) {
				t.Fatalf("the teardown did not quarantine: %v", err)
			}
			rec, ok := held(s, r.ID)
			if !ok || rec.State != StateQuarantined || used(s)[2] != 1 || h.count("rmcgroup") != 1 {
				t.Fatalf("the clear released a quarantine nobody inspected: %+v %v used %v rmcgroup %d", rec, ok, used(s), h.count("rmcgroup"))
			}
			// inspected, then cleared
			h.heal("rmcgroup")
			if err := s.ClearQuarantine(r.ID); err != nil || used(s)[2] != 0 || len(h.leaks(r.ID)) != 0 {
				t.Fatalf("the clear asked after the quarantine: %v used %v leaks %v", err, used(s), h.leaks(r.ID))
			}
		})
	}
}

// A teardown whose `stopping` record cannot be written decides nothing:
// it stops a known live VMM (that only ends execution), removes no
// holding, quarantines nothing and releases nothing. The record keeps its
// state and charge; after a certain failure a reopen finds it as it was,
// after an uncertain one it may find the `stopping` — loaded quarantined.
func TestTeardownHaltsWithoutADurableStoppingRecord(t *testing.T) {
	for _, fault := range []string{"sync", "syncdir"} { // certainly not written / uncertain
		t.Run(fault, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			x := fsys.inject(&fsFault{op: fault, mode: "before", err: errInjected, times: 1})
			err := s.Destroy(owner1, r.ID)
			tr.dump(t)
			if x.hits != 1 {
				t.Fatal("fault not reached")
			}
			if !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) {
				t.Fatalf("destroy without a durable stopping record: %v", err)
			}
			if h.exists("vmm", r.ID) {
				t.Fatal("the halted teardown did not stop the VMM")
			}
			if h.count("rmcgroup")+h.count("rmdisk")+h.count("rmjail") != 0 || !h.exists("jail", r.ID) || !h.exists("cgroup", r.ID) {
				t.Fatalf("a holding was removed without a durable stopping record: %v", h.leaks(r.ID))
			}
			rec, ok := held(s, r.ID)
			if !ok || rec.State != StateRunning || used(s)[2] != 1 {
				t.Fatalf("the halted teardown changed the record: %+v %v", rec, ok)
			}
			s2 := reopen(t, s, h)
			got, _ := held(s2, r.ID)
			if fault == "sync" {
				if got.State != StateRunning {
					t.Fatalf("reopened %+v", got)
				}
				if err := s2.Destroy(owner1, r.ID); err != nil {
					t.Fatalf("the owner's retry: %v", err)
				}
			} else {
				if got.State != StateQuarantined {
					t.Fatalf("an uncertain stopping record reopened as %+v", got)
				}
				s2.ReapOnce()
				if err := s2.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
					t.Fatalf("owner destroy: %v", err)
				}
				if err := s2.ClearQuarantine(r.ID); err != nil {
					t.Fatalf("operator clear: %v", err)
				}
			}
			if _, ok := held(s2, r.ID); ok || len(h.leaks(r.ID)) != 0 {
				t.Fatalf("not released at the end: %v", h.leaks(r.ID))
			}
		})
	}
}

// The same halt on the reaper's and the rollback's paths: nothing is
// decided without a durable stopping record, and the next pass after the
// store heals finishes the job.
func TestHaltedTeardownsAreRetried(t *testing.T) {
	t.Run("deadline reaper", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		s := open(t, cfg, h, fsys)
		r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		clock.pass(t, s, r.ID)
		x := fsys.inject(&fsFault{op: "sync", mode: "before", err: errInjected, times: 1})
		s.ReapOnce()
		if rec, _ := held(s, r.ID); x.hits != 1 || rec.State != StateRunning || h.exists("vmm", r.ID) || !h.exists("cgroup", r.ID) {
			t.Fatalf("halted reaping: %+v, leaks %v", rec, h.leaks(r.ID))
		}
		s.ReapOnce()
		if _, ok := held(s, r.ID); ok || len(h.leaks(r.ID)) != 0 {
			t.Fatalf("the next pass did not reap: %v", h.leaks(r.ID))
		}
	})
	t.Run("create rollback", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		s := open(t, testConfig(t.TempDir()), h, fsys)
		r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
		h.fail("cgroup", "after", errInjected)
		x := &fsFault{op: "sync", mode: "before", err: errInjected, times: 1}
		h.at("cgroup/after", func(string) { fsys.inject(x) })
		_, err := s.Create(owner1, r.ID)
		if x.hits != 1 || errors.Is(err, ErrQuarantined) || strings.Contains(err.Error(), "(rolled back)") || !strings.Contains(err.Error(), "rollback not finished") {
			t.Fatalf("a halted rollback: %v", err)
		}
		if rec, _ := held(s, r.ID); rec.State != StatePreparing || !rec.holds() || !h.exists("cgroup", r.ID) {
			t.Fatalf("after the halted rollback: %+v, leaks %v", rec, h.leaks(r.ID))
		}
		s.ReapOnce()
		if _, ok := held(s, r.ID); ok || len(h.leaks(r.ID)) != 0 {
			t.Fatalf("the interrupted create was not torn down: %v", h.leaks(r.ID))
		}
	})
}

// A withdrawal whose unlink succeeded but whose directory fsync failed is
// not a quarantine — nothing failed but the proof of durability: the
// record stays charged until the reaper (or its owner) confirms it, and a
// reopen before that finds no record, because the file is gone.
func TestUnconfirmedWithdrawalIsFinishedNotQuarantined(t *testing.T) {
	for _, by := range []string{"the reaper", "a reopen", "shutdown"} {
		t.Run(by, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			x := &fsFault{op: "syncdir", mode: "before", err: errInjected, times: 1}
			fsys.inject(&fsFault{op: "unlink", path: ".json", block: func() { fsys.inject(x) }, times: 1})
			err := s.Destroy(owner1, r.ID)
			rec, ok := held(s, r.ID)
			if x.hits != 1 || !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) || !ok || rec.State != StateStopping || used(s)[2] != 1 {
				t.Fatalf("an unconfirmed withdrawal: %v; %+v %v", err, rec, ok)
			}
			if _, ok := durable(t, cfg.StateDir, r.ID); ok {
				t.Fatal("the record file is still there")
			}
			switch by {
			case "the reaper":
				s.ReapOnce()
				if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
					t.Fatal("the reaper did not confirm the release")
				}
			case "a reopen":
				s2 := reopen(t, s, h)
				if _, ok := held(s2, r.ID); ok || len(s2.Problems()) != 0 {
					t.Fatalf("reopen: %+v %v", s2.All(), s2.Problems())
				}
			case "shutdown":
				if err := s.Shutdown(t.Context()); err != nil {
					t.Fatalf("shutdown: %v", err)
				}
				if _, ok := held(s, r.ID); ok {
					t.Fatal("shutdown did not confirm the release")
				}
			}
			if len(h.leaks(r.ID)) != 0 {
				t.Fatalf("leaks %v", h.leaks(r.ID))
			}
		})
	}
}

// A reservation whose first publication was uncertain and whose
// withdrawal failed stays charged as an unacknowledged reservation, not a
// quarantine: its owner may destroy it, and its deadline reaps it.
func TestUnacknowledgedReservationIsReapedNotQuarantined(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, newModelHost(t, tr), fsys)
	fsys.inject(&fsFault{op: "rename", mode: "after", err: errInjected, times: 1})
	fsys.inject(&fsFault{op: "unlink", path: ".json", mode: "before", err: errInjected, times: 1})
	_, err := s.Reserve(owner1, expiring("0v1", 1, 128))
	id := IDFor("t", "0v1")
	if rec, ok := held(s, id); errors.Is(err, ErrQuarantined) || !ok || rec.State != StatePreparing {
		t.Fatalf("an unacknowledged reservation: %v %+v %v", err, rec, ok)
	}
	clock.pass(t, s, id)
	s.ReapOnce()
	if _, ok := held(s, id); ok || used(s)[2] != 0 {
		t.Fatal("its deadline did not reap it")
	}
}
