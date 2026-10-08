package daemon

// Stage 01, orchestrator triage 01 items 4 and 5 (runner/launcher/
// INTEGRATION.md §§4–5): the local accounting of held orphans in every
// state the ship can report — never an overcommit, never a double charge —
// and the durability rule across effect-then-error saves, a crash before
// any save, assignments waiting for a slot, and attempts already running.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

// orphanOf leaves a running launcher record of this daemon for attempt, as
// a previous daemon life would have (its process gone).
func (f *vmFixture) orphanOf(t *testing.T, attempt string) launcher.Record {
	t.Helper()
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r, err := cl.Reserve(launcher.ReserveRequest{Attempt: attempt, Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.srv.Held(t, vmDaemon) {
		if rec.ID == r.ID {
			return rec
		}
	}
	t.Fatal("fixture: not held")
	return launcher.Record{}
}

func (d *Daemon) free() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.freeLocked()
}

// An orphan the launcher still charges to this daemon withholds its local
// slot whatever the ship says of its attempt — still running, status
// unreadable, or another daemon's — and never the advertised capacity,
// which the ship counts itself. When the ship later calls it terminal, the
// next pass destroys it and frees the slot; when that destroy fails, the
// slot passes from held to retained — counted once, never twice.
func TestHeldOrphansAcrossTheShipsAnswers(t *testing.T) {
	for _, c := range []struct{ name, status string }{
		{"the ship calls it running", "running"},
		{"the ship cannot say", "!unreadable"},
		{"the ship calls it another daemon's", "!not-ours"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			rec := f.orphanOf(t, "0v5.att")
			f.ship.status["0v5.att"] = c.status
			d := f.start(t)
			defer closeDaemon(d)
			for i := 0; i < 2; i++ { // repeated reconciliation holds it once
				if err := d.Reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if d.free() != 1 || d.remainingCapacity() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 {
				t.Fatalf("A HELD ORPHAN WAS MISCOUNTED: free %d (want 1 of 2), advertised %d (want 2), retentions %v", d.free(), d.remainingCapacity(), retentions(t, f.cfg.StateFile))
			}
			if recs := f.srv.Held(t, vmDaemon); len(recs) != 1 || recs[0].ID != rec.ID || f.host.Count("rmjail", rec.ID) != 0 {
				t.Fatalf("a held orphan must be left alone: %+v", recs)
			}
		})
	}
	t.Run("the ship later calls it terminal", func(t *testing.T) {
		f := newVMFixture(t, 2)
		rec := f.orphanOf(t, "0v5.att")
		other := f.orphanOf(t, "0v6.att")
		f.ship.status["0v5.att"], f.ship.status["0v6.att"] = "running", "running"
		d := f.start(t)
		defer closeDaemon(d)
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 0 {
			t.Fatalf("two held orphans: free %d", d.free())
		}
		f.ship.mu.Lock()
		f.ship.status["0v5.att"], f.ship.status["0v6.att"] = "failed", "failed"
		f.ship.mu.Unlock()
		// both destroys fail: each orphan moves from held to retained,
		// counted once
		f.host.SetFail("rmjail", launchertest.ErrInjected)
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		kept := retentions(t, f.cfg.StateFile)
		if d.free() != 0 || d.remainingCapacity() != 0 || len(kept) != 2 {
			t.Fatalf("HELD ORPHANS MOVED TO RETAINED WERE MISCOUNTED: free %d, advertised %d, retentions %v", d.free(), d.remainingCapacity(), kept)
		}
		for _, e := range kept {
			if !names(e, rec) && !names(e, other) {
				t.Fatalf("a retention without its launcher identity: %v", e)
			}
		}
	})
	t.Run("the ship later calls it terminal and its destroy succeeds", func(t *testing.T) {
		f := newVMFixture(t, 1)
		f.orphanOf(t, "0v5.att")
		f.ship.status["0v5.att"] = "running"
		d := f.start(t)
		defer closeDaemon(d)
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.ship.mu.Lock()
		f.ship.status["0v5.att"] = "failed"
		f.ship.mu.Unlock()
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 1 || d.remainingCapacity() != 1 || len(f.srv.Held(t, vmDaemon)) != 0 || len(retentions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("A DESTROYED ORPHAN WAS NOT DESTROYED, OR ITS SLOT NOT FREED: free %d, advertised %d, launcher %+v", d.free(), d.remainingCapacity(), f.srv.Held(t, vmDaemon))
		}
	})
}

// A save that renamed the file and then failed has proven nothing: the
// daemon stays not durable — no new work — until a save succeeds, even
// though the file already holds the retention.
func TestEffectThenErrorSaveIsNotTrusted(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	// ruling-dependent fixture (settled-admission ruling 01; INTEGRATION.md
	// §11.10): the first save is now the admission's, before the reserve,
	// and must hold; the retention's is the second, and fails after its
	// effect — it was the first
	saves, failures := 0, 1
	d.saveState = func(path string, st *state.State) error {
		saves++
		err := state.Save(path, st)
		if err == nil && saves > 1 && failures > 0 {
			failures--
			return errors.New("injected: the directory's fsync failed after the rename")
		}
		return err
	}
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	d.handle(context.Background(), vmAssignment("0v1.att"))
	if len(retentions(t, f.cfg.StateFile)) != 1 {
		t.Fatal("fixture: the save's effect is in the file")
	}
	d.mu.Lock()
	unsaved := d.unsaved
	d.mu.Unlock()
	if unsaved == nil {
		t.Fatal("A SAVE THAT FAILED AFTER ITS EFFECT WAS TAKEN FOR DURABLE")
	}
	if err := d.retrySave(); err != nil {
		t.Fatalf("the retry: %v", err)
	}
}

// A retention whose save never succeeded is not lost with a crash: the
// launcher still charges its record to this daemon, and the next start's
// reconcile retains it again — durably, once.
func TestUnsavedRetentionComesBackAfterACrash(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	dir := filepath.Dir(f.cfg.StateFile)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	// ruling-dependent fixture (settled-admission ruling 01; INTEGRATION.md
	// §11.10): the admission is saved before its reserve, and a daemon that
	// cannot save it sends none, so the state file turns unwritable once the
	// reservation exists — at its disk step — where it did before the attempt
	release := f.host.Hold(t, "disk")
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		d.handle(context.Background(), vmAssignment("0v1.att"))
	}()
	<-f.host.Reached()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	release()
	<-handled
	// the file holds the attempt's admission, saved before its reserve, and
	// no retention naming the reservation: that one is in memory only
	identified := 0
	for _, e := range retentions(t, f.cfg.StateFile) {
		if e["vm"] != nil {
			identified++
		}
	}
	if d.retrySave() == nil || identified != 0 {
		t.Fatal("fixture: the retention is in memory only")
	}
	closeDaemon(d) // the process dies; memory is gone
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f.host.SetFail("disk", nil)
	f.host.SetFail("rmjail", nil)
	f.ship.status["0v1.att"] = "failed"
	again := f.start(t)
	defer closeDaemon(again)
	if err := again.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs := f.srv.Held(t, vmDaemon)
	kept := retentions(t, f.cfg.StateFile)
	if again.remainingCapacity() != 1 || len(recs) != 1 || len(kept) != 1 || !names(kept[0], recs[0]) {
		t.Fatalf("A RETENTION LOST WITH THE CRASH DID NOT COME BACK: advertised %d, launcher %+v, state %v", again.remainingCapacity(), recs, kept)
	}
}

// An assignment that was waiting for a slot when a retention could not be
// saved does not start when the slot frees: it is abandoned with the
// reason, before any reservation. An attempt already running goes on to
// its end meanwhile.
func TestQueuedAssignmentFacesTheDurabilityRule(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	inside, finishRunning := make(chan struct{}), make(chan struct{})
	d.checkout = func(_ context.Context, a *ship.Assignment, _ string) error {
		if sameAtom(a.Attempt, "0vr.att") {
			close(inside)
			<-finishRunning
		}
		return errors.New("no checkout in this test")
	}
	stop := run(t, d)
	f.offer(t, "0vr.att") // the running attempt, holding one slot
	<-inside
	releaseDisk := f.host.Hold(t, "disk")
	f.offer(t, "0va.att") // it will be retained, and its save will fail
	select {
	case op := <-f.host.Reached():
		if op != "disk "+f.at("0va.att") {
			t.Fatalf("held %s", op)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the second attempt did not reach its disk step")
	}
	f.offer(t, "0vq.att") // queued: no slot is free
	waitUntil(t, 20*time.Second, "the queued attempt claimed", func() bool { return d.claimed("0vq.att") })
	dir := filepath.Dir(f.cfg.StateFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	releaseDisk()
	waitUntil(t, 20*time.Second, "the retained attempt's answer", f.ship.abandoned(1))
	f.host.SetFail("disk", nil)
	f.host.SetFail("rmjail", nil)
	close(finishRunning) // the running attempt ends, cleanly; its slot frees
	waitUntil(t, 20*time.Second, "both remaining answers", f.ship.abandoned(3))
	stop()
	var queued, running string
	for i := 0; i < 3; i++ {
		switch a := f.ship.abandon(i); {
		case strings.Contains(a, "0vq") || strings.Contains(a, "not durable"):
			queued = a
		case strings.Contains(a, "no checkout"):
			running = a
		}
	}
	if n := f.host.Count("disk", f.at("0vq.att")); n != 0 || !strings.Contains(queued, "not durable") {
		t.Fatalf("A QUEUED ASSIGNMENT STARTED ON UNSAVED ACCOUNTING: it reached the launcher %d time(s); its abandon %q", n, queued)
	}
	if running == "" || f.host.Count("kill", f.at("0vr.att")) == 0 {
		t.Fatalf("the running attempt did not run to its end: %q; ops %v", running, f.host.Ops())
	}
}

// While the state file is behind memory, an assignment offered is answered
// at once, before it is claimed: it does not wait for a slot, so the ship
// can offer it elsewhere without delay. Here every slot is busy — a running
// attempt, a retention whose save failed — and the answer still comes
// while the attempt runs.
func TestUnsavedAccountingAnswersNewWorkAtOnce(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	inside, finishRunning := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(finishRunning) }) }
	defer finish()
	d.checkout = func(_ context.Context, a *ship.Assignment, _ string) error {
		if sameAtom(a.Attempt, "0vr.att") {
			close(inside)
			<-finishRunning
		}
		return errors.New("no checkout in this test")
	}
	stop := run(t, d)
	f.offer(t, "0vr.att") // the running attempt, holding one slot
	<-inside
	dir := filepath.Dir(f.cfg.StateFile)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	// ruling-dependent fixture (settled-admission ruling 01; INTEGRATION.md
	// §11.10): the admission is saved before its reserve, and a daemon that
	// cannot save it sends none, so the state file turns unwritable once the
	// second attempt's reservation exists — at its disk step — where it did
	// before that attempt (full-11: with no retention made, a slot stayed
	// free, and G04's mutant passed on the post-acquire gate)
	release := f.host.Hold(t, "disk")
	f.offer(t, "0va.att") // retained, its save failing: no slot is free now
	select {
	case op := <-f.host.Reached():
		if op != "disk "+f.at("0va.att") {
			t.Fatalf("held %s", op)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the second attempt did not reach its disk step")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	release()
	waitUntil(t, 20*time.Second, "the retained attempt's answer", f.ship.abandoned(1))
	if d.free() != 0 || d.retrySave() == nil {
		t.Fatalf("fixture: every slot busy — the running attempt and a retention whose save failed: free %d", d.free())
	}
	f.offer(t, "0vn.att")
	answered := false
	for limit := time.Now().Add(10 * time.Second); !answered && time.Now().Before(limit); time.Sleep(10 * time.Millisecond) {
		answered = f.ship.abandoned(2)()
	}
	claimed := d.claimed("0vn.att")
	finish()
	stop()
	if !answered || !strings.Contains(f.ship.abandon(1), "not durable") {
		t.Fatalf("NEW WORK WAITED FOR A SLOT ON UNSAVED ACCOUNTING: answered while the attempt ran: %v (claimed meanwhile: %v); the ship's answers %q", answered, claimed, []string{f.ship.abandon(0), f.ship.abandon(1)})
	}
}

// After the two operator releases, in their order (INTEGRATION.md §8.5) —
// the launcher's of its record, then the runner's of its retention, as
// `urgit-runner -recover` performs it on the state file — the next start
// offers the slot again; and the daemon's own later saves keep the released
// entry as evidence: nothing the daemon writes drops it.
func TestReleasedRetentionFreesItsSlotAndStaysEvidence(t *testing.T) {
	f := newVMFixture(t, 2)
	rec := f.quarantineAtLauncher(t, "0v4.att")
	q := state.Quarantine{Handle: "ci-0v4.att", Reason: "teardown failed", At: time.Now().Unix(), Backend: "microvm", Attempt: "0v4.att", VM: rec.ID, CID: rec.CID, Created: rec.Created}
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	st.Quarantined = []state.Quarantine{q}
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	d := f.start(t)
	if d.remainingCapacity() != 1 || d.free() != 1 {
		t.Fatalf("fixture: the retention withholds its slot: advertised %d, free %d", d.remainingCapacity(), d.free())
	}
	closeDaemon(d)
	// the launcher's operator: a retry, then the release of the exact
	// incarnation it inspected
	in, err := f.srv.Service.RetryCleanup(rec.Selection())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.Service.Release(in.Selection); err != nil {
		t.Fatal(err)
	}
	// the runner's operator: the release -recover makes, on the selection
	// its inspection showed
	st, err = state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	i, err := st.Find(q.Selection(), true)
	if err != nil {
		t.Fatal(err)
	}
	st.Release(i, time.Now().Unix())
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	d = f.start(t)
	defer closeDaemon(d)
	if d.remainingCapacity() != 2 || d.free() != 2 {
		t.Fatalf("A RELEASED RETENTION STILL WITHHOLDS ITS SLOT: advertised %d, free %d (want 2 of 2)", d.remainingCapacity(), d.free())
	}
	// a later save of the daemon's own — a new retention — keeps it
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	d.handle(context.Background(), vmAssignment("0v5.att"))
	after, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Quarantined) != 1 || after.Quarantined[0].Attempt != "0v5.att" || len(after.Released) != 1 || after.Released[0].VM != rec.ID || after.Released[0].ReleasedAt == 0 {
		t.Fatalf("THE DAEMON'S SAVE DROPPED THE RELEASED EVIDENCE: retained %+v, released %+v", after.Quarantined, after.Released)
	}
}

// The job's label — its repository, workflow and job — reaches the
// launcher's record at reserve and the daemon's retention: what the
// operator selects an incident or a retention by (recovery ruling A).
func TestTheJobLabelReachesTheLauncherAndTheRetention(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	d.handle(context.Background(), vmAssignment("0v1.att"))
	recs := f.srv.Held(t, vmDaemon)
	kept := retentions(t, f.cfg.StateFile)
	const want = "r · fixture-chain.yml · b"
	if len(recs) != 1 || recs[0].Label != want || len(kept) != 1 || kept[0]["label"] != want {
		t.Fatalf("THE JOB LABEL DID NOT REACH THE LAUNCHER AND THE RETENTION: launcher %+v, retention %v", recs, kept)
	}
}
