package daemon

// The rest of stage 01's failure/restart/capacity matrix at the daemon
// (runner/launcher/INTEGRATION.md §§3–6): rollbacks that succeed, a
// teardown that fails, attempts waiting for a slot, an orphan held until
// the launcher's deadline reaps it, a launcher restart in the middle of an
// attempt, and Docker-shaped retentions.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
)

// run is d.Run in the background; stop ends it and waits (bounded).
func run(t *testing.T, d *Daemon) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan int, 1)
	go func() { ran <- d.Run(ctx) }()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Error("Run did not return")
		}
	}
	t.Cleanup(stop)
	return stop
}

func (f *vmFixture) offer(t *testing.T, attempts ...string) {
	t.Helper()
	f.ship.mu.Lock()
	defer f.ship.mu.Unlock()
	for _, a := range attempts {
		f.ship.queue = append(f.ship.queue, queued{a: f.signed(t, a)})
	}
}

// at is the launcher's record id of an attempt this fixture's ship
// delivered: the daemon runs a verified attempt in the ship's own spelling
// (stage 01 fixture adaptation, assignment-identity ruling 01). An attempt
// handed to handle directly keeps its spelling: launcher.IDFor("t", …).
func (f *vmFixture) at(attempt string) string {
	if c, err := sig.CanonicalUV(attempt); err == nil {
		attempt = c
	}
	return launcher.IDFor("t", attempt)
}

// A create that fails and rolls back leaves nothing charged: the slot is
// kept, nothing is retained, and the ship hears the plain reason.
func TestPrepareRolledBackKeepsTheSlot(t *testing.T) {
	f := newVMFixture(t, 1)
	d := f.start(t)
	defer closeDaemon(d)
	f.host.SetFail("cgroup", launchertest.ErrInjected) // acted, then failed: removed by the rollback
	keep := d.handle(context.Background(), vmAssignment("0v1.att"))
	if !keep || d.remainingCapacity() != 1 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
		t.Fatalf("a rolled-back prepare: keep %v, capacity %d, state %v, launcher %+v", keep, d.remainingCapacity(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
	}
	if a := f.ship.abandon(0); !strings.Contains(a, "sandbox prepare") || strings.Contains(a, "retained") {
		t.Fatalf("abandon: %q", a)
	}
}

// A teardown after the run that the launcher cannot finish retains the
// sandbox under its exact incarnation; the launcher keeps it quarantined.
func TestTeardownFailureRetainsTheExactIncarnation(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	keep := d.handle(context.Background(), vmAssignment("0v1.att")) // prepared; the checkout fails; torn down
	recs := f.srv.Held(t, vmDaemon)
	kept := retentions(t, f.cfg.StateFile)
	if keep || len(recs) != 1 || recs[0].State != launcher.StateQuarantined || len(kept) != 1 || !names(kept[0], recs[0]) || d.remainingCapacity() != 1 {
		t.Fatalf("A FAILED TEARDOWN WAS NOT RETAINED EXACTLY: keep %v, launcher %+v, state %v, capacity %d", keep, recs, kept, d.remainingCapacity())
	}
}

// Attempts wait for a slot no retention, held orphan or running attempt
// occupies: with one slot retained and one running, a third attempt does
// not reach the launcher until the running one has ended; the advertised
// capacity is the configured one less the retention throughout.
func TestAttemptsWaitForAFreeSlot(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	stop := run(t, d)
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	f.offer(t, "0v1.att")
	waitUntil(t, 20*time.Second, "the first attempt's answer", f.ship.abandoned(1))
	f.host.SetFail("disk", nil)
	f.host.SetFail("rmjail", nil)
	release := f.host.Hold(t, "connect")
	f.offer(t, "0v2.att")
	select {
	case op := <-f.host.Reached():
		if op != "connect "+f.at("0v2.att") {
			t.Fatalf("held %s", op)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the second attempt did not reach its connect")
	}
	f.offer(t, "0v3.att")
	time.Sleep(500 * time.Millisecond) // the third is claimed and waits
	if n := f.host.Count("disk", f.at("0v3.att")); n != 0 || d.remainingCapacity() != 1 {
		t.Fatalf("A THIRD ATTEMPT TOOK A SLOT THAT WAS NOT FREE: it reached the launcher %d time(s); advertised %d", n, d.remainingCapacity())
	}
	release()
	waitUntil(t, 20*time.Second, "the third attempt at the launcher", func() bool { return f.host.Count("disk", f.at("0v3.att")) > 0 })
	waitUntil(t, 20*time.Second, "all three answered", f.ship.abandoned(3))
	stop()
	if recs := f.srv.Held(t, vmDaemon); len(recs) != 1 || !sameAtom(recs[0].Attempt, "0v1.att") || d.remainingCapacity() != 1 {
		t.Fatalf("after: launcher %+v, advertised %d", recs, d.remainingCapacity())
	}
}

// An orphan the ship still calls running is left for its deadline, and
// while the launcher holds it its slot is not offered locally — the
// advertised capacity is unchanged: the ship counts that attempt already.
// When the launcher reaps it at its deadline, the next reconcile pass
// frees the slot and the waiting attempt runs.
func TestHeldOrphanWithholdsItsSlotUntilTheLauncherReapsIt(t *testing.T) {
	f := newVMFixture(t, 1)
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	orphan, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v5.att", Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: deadline.Unix(), Network: "locked"})
	if err == nil {
		_, err = cl.Create(orphan.Ref())
	}
	cl.Close()
	if err != nil {
		t.Fatal(err)
	}
	f.ship.status["0v5.att"] = "running"
	d := f.start(t)
	defer closeDaemon(d)
	d.reconcileEvery = 100 * time.Millisecond
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	stop := run(t, d)
	f.offer(t, "0v6.att")
	time.Sleep(500 * time.Millisecond)
	if n := f.host.Count("disk", f.at("0v6.att")); n != 0 || d.remainingCapacity() != 1 {
		t.Fatalf("A HELD ORPHAN'S SLOT WAS OFFERED: the new attempt reached the launcher %d time(s); advertised %d (want 1)", n, d.remainingCapacity())
	}
	// the launcher's own deadline enforcement, in time: just past the
	// deadline as the record keeps it (a whole second), within the cleanup
	// allowance — a pass after the allowance is a missed obligation, an
	// incident the launcher would keep (recovery ruling A)
	time.Sleep(time.Until(time.Unix(deadline.Unix(), 0).Add(100 * time.Millisecond)))
	f.srv.Service.ReapOnce()
	waitUntil(t, 20*time.Second, "the waiting attempt at the launcher", func() bool { return f.host.Count("disk", f.at("0v6.att")) > 0 })
	stop()
	if !strings.Contains(f.logs.String(), "reconcile ci-0v5.att: no longer held") {
		t.Fatalf("the freed slot is not in the log:\n%s", f.logs.String())
	}
}

// A launcher restart in the middle of an attempt: the daemon's next call
// is a new connection to the new process, which loaded the record; the
// attempt's teardown releases it, and nothing is retained.
func TestLauncherRestartInTheMiddleOfAnAttempt(t *testing.T) {
	f := newVMFixture(t, 1)
	d := f.start(t)
	defer closeDaemon(d)
	inside, resume := make(chan struct{}), make(chan struct{})
	d.checkout = func(context.Context, *ship.Assignment, string) error {
		close(inside)
		<-resume
		return errors.New("no checkout in this test")
	}
	done := make(chan bool, 1)
	go func() { done <- d.handle(context.Background(), vmAssignment("0v1.att")) }()
	<-inside // prepared: the VM runs
	f.srv.Restart(t)
	close(resume)
	if keep := <-done; !keep {
		t.Fatal("the teardown after the restart failed")
	}
	if recs, kept := f.srv.Held(t, vmDaemon), retentions(t, f.cfg.StateFile); len(recs) != 0 || len(kept) != 0 {
		t.Fatalf("after the restart: launcher %+v, state %v", recs, kept)
	}
	if f.host.Count("kill", launcher.IDFor("t", "0v1.att")) == 0 {
		t.Fatalf("the VM was not stopped: %v", f.host.Ops())
	}
}

// retainingBox is the compatibility backend's shape: its Prepare fails and
// its cleanup fails, retaining the attempt's Docker objects.
type retainingBox struct{ *fakeBox }

func (retainingBox) Kind() string { return "docker-rootless" }
func (b retainingBox) Prepare(_ context.Context, spec sandbox.Spec) (sandbox.Handle, error) {
	h := sandbox.Handle{ID: spec.Network, Attempt: spec.Attempt, Network: spec.Network, Volume: spec.Network + "-work", Container: spec.Network}
	return sandbox.Handle{}, &sandbox.RetainedError{Handle: h, Err: fmt.Errorf("docker run: no space left; and the sandbox's objects could not be removed: volume in use")}
}

// Docker compatibility keeps its shape: a retained Prepare is recorded
// with its Docker objects — never a launcher identity — once, however
// often the same attempt fails the same way.
func TestDockerRetentionKeepsTheCompatibilityShape(t *testing.T) {
	sh := &fakeShip{}
	box := retainingBox{&fakeBox{}}
	d := newTestDaemon(t, box.fakeBox, sh, 2)
	d.box = box
	for i := 0; i < 2; i++ {
		d.handle(context.Background(), jobAssignment)
	}
	if len(d.retained) != 1 || d.remainingCapacity() != 1 {
		t.Fatalf("A DOCKER RETENTION WAS CHARGED %d TIME(S): capacity %d", len(d.retained), d.remainingCapacity())
	}
	q := d.retained[0]
	if q.Backend != "docker-rootless" || q.Network != "ci-0v1.att" || q.Volume != "ci-0v1.att-work" || q.Container != "ci-0v1.att" || q.VM != "" || q.CID != 0 {
		t.Fatalf("the retention's shape: %+v", q)
	}
}
