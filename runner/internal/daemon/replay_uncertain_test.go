package daemon

// Assignment-replay ruling 01, Q14 (runner/launcher/INTEGRATION.md §11.14,
// items 2, 5 and 7): the uncertain outcomes. A take whose write fails —
// before its file exists, or after — leaves the attempt unstarted and given
// back to the ship, and never run in this life; a record that exists after
// such a failure counts after a restart. A finish that cannot be recorded
// leaves the attempt answered in its life and interrupted after it. A full
// ledger starts nothing and evicts nothing. Deliveries while an attempt is
// taken or finished are that attempt. A malformed ledger, an orphan an
// earlier life left, and an earlier version's state file are read
// conservatively. A new attempt runs throughout.
//
// The failures are injected at the daemon's take and finish, around the
// ledger's real calls (the ledger's own steps are state/ledger_test.go's);
// through the real Run loop, the real Microvm backend and the launcher's
// real core and wire over private files, with the in-process ship of
// retained_test.go. A stop is a daemon object abandoned without its
// cleanup: not a process's crash, nor a power loss.

import (
	"context"
	"encoding/json"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// givenBackOnce says whether the ship heard exactly one answer about
// attempt, and that answer an abandon.
func givenBackOnce(h *heardPaths, attempt string) bool {
	got := answered(h, attempt)
	return len(got) == 1 && strings.HasSuffix(got[0], "/abandon")
}

// ignoredTimes is how many deliveries Run ignored as running here.
func ignoredTimes(f *vmFixture) int {
	return strings.Count(f.logs.String(), "is already running here; ignored")
}

// ledgerFiles is every record under the ledger's taken/ and finished/: its
// bytes, by its path.
func ledgerFiles(t *testing.T, f *vmFixture) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, sub := range []string{"taken", "finished"} {
		entries, err := os.ReadDir(filepath.Join(executionsOf(f), sub))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			data, _ := os.ReadFile(filepath.Join(executionsOf(f), sub, e.Name()))
			out[sub+"/"+e.Name()] = string(data)
		}
	}
	return out
}

// hasRecord says whether the ledger's sub directory holds a record named
// attempt.
func hasRecord(f *vmFixture, sub, attempt string) bool {
	_, err := os.Lstat(filepath.Join(executionsOf(f), sub, attempt))
	return err == nil
}

// A take whose write had its effect, and then failed (a sync, say): the
// attempt is not started, and is given back to the ship once; delivered
// again in its life, it is ignored. Its record exists, and after a restart
// it counts: a delivery signed anew is given back, never run.
func TestATakeThatFailedAfterItsEffectIsNeverRun(t *testing.T) {
	f := newVMFixture(t, 2)
	d, heard := f.startHeard(t)
	d.take = func(e state.Execution) error {
		if !sameAtom(e.Attempt, attemptAtom) {
			return d.ledger.Take(e)
		}
		if err := d.ledger.Take(e); err != nil {
			return err
		}
		return &state.LedgerError{Step: "sync", Path: e.Attempt, Created: true, Err: syscall.EIO}
	}
	orig := f.signed(t, attemptAtom)
	f.offerAssignment(orig)
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
	f.offerAssignment(respelledAssignment(orig, "0v5.cmd")) // delivered again, in its life
	f.offerAssignment(f.signed(t, "0v6cmd"))
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	closeDaemon(d)
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT RAN THOUGH ITS RECORD'S WRITE FAILED: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard, attemptAtom) {
		t.Fatalf("AN ATTEMPT WHOSE RECORD'S WRITE FAILED WAS NOT GIVEN BACK ONCE IN ITS LIFE: %v", answered(heard, attemptAtom))
	}
	if !hasRecord(f, "taken", attemptAtom) {
		t.Fatalf("fixture: the take's effect happened")
	}
	d2, heard2 := f.startHeard(t)
	defer closeDaemon(d2)
	f.offerAssignment(f.signedWith(t, attemptAtom, "0v8.fresh"))
	f.offerAssignment(f.signed(t, "0v7cmd"))
	stop2 := run(t, d2)
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard2, "0v7cmd")) > 0 && claims(d2) == 0 })
	stop2()
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT WHOSE RECORD'S DURABILITY WAS UNPROVEN RAN AFTER A RESTART: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard2, attemptAtom) {
		t.Fatalf("AN ATTEMPT WHOSE RECORD'S DURABILITY WAS UNPROVEN WAS NOT GIVEN BACK AFTER A RESTART: %v", answered(heard2, attemptAtom))
	}
	if prepared(f, "0v6cmd") != 1 || prepared(f, "0v7cmd") != 1 {
		t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
	}
}

// A take that failed before its file existed (the filesystem refused the
// create): the attempt is not started, and is given back to the ship once,
// with the reason; delivered again in its life — respelled, or signed anew
// — it is ignored, and never run.
func TestATakeThatFailedBeforeItsFileIsNotStarted(t *testing.T) {
	f := newVMFixture(t, 2)
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	d.take = func(e state.Execution) error {
		if sameAtom(e.Attempt, attemptAtom) {
			return &state.LedgerError{Step: "create", Path: e.Attempt, Err: syscall.EACCES}
		}
		return d.ledger.Take(e)
	}
	orig := f.signed(t, attemptAtom)
	f.offerAssignment(orig)
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
	f.offerAssignment(respelledAssignment(orig, "0v05cmd"))
	f.offerAssignment(f.signedWith(t, attemptAtom, "0v8.fresh"))
	f.offerAssignment(f.signed(t, "0v6cmd"))
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT RAN WITHOUT ITS RECORD: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard, attemptAtom) {
		t.Fatalf("AN ATTEMPT NOT STARTED WAS NOT GIVEN BACK ONCE IN ITS LIFE: %v", answered(heard, attemptAtom))
	}
	if reason := f.ship.abandon(0); !strings.Contains(reason, "not started") {
		t.Fatalf("the ship was not told the attempt was not started: %s", reason)
	}
	if hasRecord(f, "taken", attemptAtom) {
		t.Fatalf("fixture: the take had no effect")
	}
	if prepared(f, "0v6cmd") != 1 {
		t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
	}
}

// A finish that cannot be recorded: the attempt ran once and was answered;
// delivered again in its life, it is ignored. After a restart it counts as
// interrupted: a delivery is given back, never run.
func TestAFinishThatCannotBeRecordedCountsAsInterrupted(t *testing.T) {
	f := newVMFixture(t, 2)
	d, heard := f.startHeard(t)
	d.finish = func(attempt string, at int64) error {
		if sameAtom(attempt, attemptAtom) {
			return &state.LedgerError{Step: "create", Path: attempt, Err: syscall.ENOSPC}
		}
		return d.ledger.Finish(attempt, at)
	}
	orig := f.signed(t, attemptAtom)
	f.offerAssignment(orig)
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
	if prepared(f, attemptAtom) != 1 {
		stop()
		closeDaemon(d)
		t.Fatalf("fixture: the attempt ran once: launcher ops %v", f.host.Ops())
	}
	f.offerAssignment(f.signedWith(t, attemptAtom, "0v8.fresh"))
	f.offerAssignment(f.signed(t, "0v6cmd"))
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	closeDaemon(d)
	if n := prepared(f, attemptAtom); n != 1 {
		t.Fatalf("AN ATTEMPT WHOSE FINISH WAS NOT RECORDED RAN AGAIN IN ITS LIFE: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if got := answered(heard, attemptAtom); len(got) != 1 {
		t.Fatalf("AN ATTEMPT WHOSE FINISH WAS NOT RECORDED WAS ANSWERED AGAIN IN ITS LIFE: %v", got)
	}
	if !hasRecord(f, "taken", attemptAtom) || hasRecord(f, "finished", attemptAtom) {
		t.Fatalf("fixture: taken, and its finish not recorded")
	}
	d2, heard2 := f.startHeard(t)
	defer closeDaemon(d2)
	f.offerAssignment(respelledAssignment(orig, "0v5.cmd"))
	f.offerAssignment(f.signed(t, "0v7cmd"))
	stop2 := run(t, d2)
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard2, "0v7cmd")) > 0 && claims(d2) == 0 })
	stop2()
	if n := prepared(f, attemptAtom); n != 1 {
		t.Fatalf("AN ATTEMPT WHOSE FINISH WAS NOT RECORDED RAN AGAIN AFTER A RESTART: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard2, attemptAtom) {
		t.Fatalf("AN ATTEMPT WHOSE FINISH WAS NOT RECORDED WAS NOT GIVEN BACK AFTER A RESTART: %v", answered(heard2, attemptAtom))
	}
}

// A full ledger (ENOSPC at every take) starts nothing: each attempt is
// given back to the ship once, and the records it holds — of attempts
// taken, and finished, before — all stay, byte for byte: nothing is evicted
// to make room. Once the filesystem has room, a new attempt runs and is
// recorded; one given back while it was full stays unrun in its life.
func TestAFullLedgerStartsNothingAndEvictsNothing(t *testing.T) {
	f := newVMFixture(t, 2)
	for _, sub := range []string{"taken", "finished"} {
		if err := os.MkdirAll(filepath.Join(executionsOf(f), sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for n := int64(1); n <= 128; n++ {
		a := sig.FormatUV(big.NewInt(n))
		if err := os.WriteFile(filepath.Join(executionsOf(f), "taken", a), []byte(`{"attempt":"`+a+`"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if n%2 == 1 {
			if err := os.WriteFile(filepath.Join(executionsOf(f), "finished", a), []byte(`{"attempt":"`+a+`"}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := ledgerFiles(t, f)
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	var full atomic.Bool
	full.Store(true)
	d.take = func(e state.Execution) error {
		if full.Load() {
			return &state.LedgerError{Step: "create", Path: e.Attempt, Err: syscall.ENOSPC}
		}
		return d.ledger.Take(e)
	}
	f.offerAssignment(f.signed(t, attemptAtom))
	f.offerAssignment(f.signed(t, "0v6cmd"))
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "both answers", func() bool {
		return len(answered(heard, attemptAtom)) > 0 && len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0
	})
	full.Store(false)
	f.offerAssignment(f.signedWith(t, attemptAtom, "0v8.fresh"))
	f.offerAssignment(f.signed(t, "0v7cmd"))
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v7cmd")) > 0 && claims(d) == 0 })
	stop()
	if n := prepared(f, "0v6cmd"); n != 0 {
		t.Fatalf("AN ATTEMPT RAN ON A FULL LEDGER: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT GIVEN BACK ON A FULL LEDGER RAN, THEN OR ONCE IT HAD ROOM: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard, attemptAtom) || !givenBackOnce(heard, "0v6cmd") {
		t.Fatalf("AN ATTEMPT NOT STARTED ON A FULL LEDGER WAS NOT GIVEN BACK ONCE: %v %v", answered(heard, attemptAtom), answered(heard, "0v6cmd"))
	}
	after := ledgerFiles(t, f)
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("A RECORD WAS EVICTED OR REWRITTEN ON A FULL LEDGER: %s is %q, was %q", k, after[k], v)
		}
	}
	if prepared(f, "0v7cmd") != 1 || !hasRecord(f, "taken", "0v7cmd") || !hasRecord(f, "finished", "0v7cmd") {
		t.Fatalf("A NEW ATTEMPT DID NOT RUN, RECORDED, ONCE THE LEDGER HAD ROOM: launcher ops %v; records %v", f.host.Ops(), after)
	}
}

// Deliveries of an attempt while its record is being written, and while its
// finish is — exact, respelled, signed anew — are that attempt: ignored.
// It runs once and is answered once.
func TestDeliveriesWhileAnAttemptIsTakenOrFinishedRunItOnce(t *testing.T) {
	for _, window := range []string{"take", "finish"} {
		t.Run(window, func(t *testing.T) {
			f := newVMFixture(t, 2)
			d, heard := f.startHeard(t)
			defer closeDaemon(d)
			inside, proceed := make(chan struct{}), make(chan struct{})
			var once, unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(proceed) }) })
			hold := func(attempt string) {
				if sameAtom(attempt, attemptAtom) {
					once.Do(func() { close(inside) })
					<-proceed
				}
			}
			if window == "take" {
				d.take = func(e state.Execution) error { hold(e.Attempt); return d.ledger.Take(e) }
			} else {
				d.finish = func(attempt string, at int64) error { hold(attempt); return d.ledger.Finish(attempt, at) }
			}
			orig := f.signed(t, attemptAtom)
			f.offerAssignment(orig)
			stop := run(t, d)
			select {
			case <-inside:
			case <-time.After(20 * time.Second):
				t.Fatalf("fixture: the attempt did not reach its %s; log:\n%s", window, f.logs.String())
			}
			heardBefore := len(answered(heard, attemptAtom))
			f.offerAssignment(orig)
			f.offerAssignment(respelledAssignment(orig, "0v5c.md"))
			f.offerAssignment(f.signedWith(t, attemptAtom, "0v8.fresh"))
			// each delivery is handled: ignored, or answered
			waitUntil(t, 20*time.Second, "the three deliveries' handling", func() bool {
				return ignoredTimes(f)+len(answered(heard, attemptAtom))-heardBefore >= 3
			})
			unblock.Do(func() { close(proceed) })
			waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
			f.offerAssignment(orig) // and once it finished
			f.offerAssignment(f.signed(t, "0v6cmd"))
			waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
			stop()
			if n := prepared(f, attemptAtom); n != 1 {
				t.Fatalf("AN ATTEMPT DELIVERED WHILE ITS %s WAS RECORDED DID NOT RUN ONCE: its sandbox prepared %d times; launcher ops %v", strings.ToUpper(window), n, f.host.Ops())
			}
			if got := answered(heard, attemptAtom); len(got) != 1 {
				t.Fatalf("AN ATTEMPT DELIVERED WHILE ITS %s WAS RECORDED WAS ANSWERED MORE THAN ONCE: %v", strings.ToUpper(window), got)
			}
			if prepared(f, "0v6cmd") != 1 {
				t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
			}
		})
	}
}

// A malformed ledger is read conservatively: a taken record of garbage, of
// nothing, a directory or a dangling symlink counts — the attempt is given
// back, never run; a finished record of garbage is an attempt answered —
// ignored. A HISTORY that cannot be read leaves the daemon running, saying
// so, and is kept — and, being no proof of a fresh runner, running nothing
// until its transition (INTEGRATION.md §11.15). A new attempt runs beside
// every other malformed ledger.
func TestAMalformedLedgerIsReadConservatively(t *testing.T) {
	for _, c := range []struct {
		name, sub string
		make      func(t *testing.T, path string) error
		given     bool // the attempt given back; else ignored
	}{
		{"a taken record of garbage", "taken", func(_ *testing.T, p string) error { return os.WriteFile(p, []byte("\x00garbage"), 0o600) }, true},
		{"a taken record a crash cut short", "taken", func(_ *testing.T, p string) error { return os.WriteFile(p, nil, 0o600) }, true},
		{"a taken record that is a directory", "taken", func(_ *testing.T, p string) error { return os.Mkdir(p, 0o700) }, true},
		{"a taken record that is a dangling symlink", "taken", func(t *testing.T, p string) error { return os.Symlink(filepath.Join(t.TempDir(), "gone"), p) }, true},
		{"a finished record of garbage", "finished", func(_ *testing.T, p string) error { return os.WriteFile(p, []byte("garbage"), 0o600) }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			dir := filepath.Join(executionsOf(f), c.sub)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := c.make(t, filepath.Join(dir, attemptAtom)); err != nil {
				t.Fatal(err)
			}
			d, heard := f.startHeard(t)
			defer closeDaemon(d)
			f.offerAssignment(respelledAssignment(f.signed(t, attemptAtom), "0v5.cmd"))
			f.offerAssignment(f.signed(t, "0v6cmd"))
			stop := run(t, d)
			waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
			stop()
			if n := prepared(f, attemptAtom); n != 0 {
				t.Fatalf("AN ATTEMPT A MALFORMED RECORD NAMES RAN (%s): its sandbox prepared %d times; launcher ops %v", c.name, n, f.host.Ops())
			}
			if got := answered(heard, attemptAtom); c.given != givenBackOnce(heard, attemptAtom) || (!c.given && len(got) != 0) {
				t.Fatalf("AN ATTEMPT A MALFORMED RECORD NAMES WAS ANSWERED OTHERWISE THAN ITS RECORD SAYS (%s): %v", c.name, got)
			}
			if prepared(f, "0v6cmd") != 1 {
				t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
			}
		})
	}
	t.Run("a HISTORY that cannot be read", func(t *testing.T) {
		f := newVMFixture(t, 2)
		for _, sub := range []string{"taken", "finished"} {
			if err := os.MkdirAll(filepath.Join(executionsOf(f), sub), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		history := filepath.Join(executionsOf(f), "HISTORY")
		if err := os.WriteFile(history, []byte("\x00{"), 0o600); err != nil {
			t.Fatal(err)
		}
		d, heard := f.startHeard(t)
		defer closeDaemon(d)
		if !strings.Contains(f.logs.String(), "when its history began is not recorded") {
			t.Fatalf("the daemon does not say its ledger's history is unknown; log:\n%s", f.logs.String())
		}
		f.offerAssignment(f.signed(t, "0v6cmd"))
		stop := run(t, d)
		waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
		stop()
		// a history that cannot be read is no proof of a fresh runner: it
		// runs nothing until its transition (Q15 fixture adaptation;
		// INTEGRATION.md §11.15)
		if prepared(f, "0v6cmd") != 0 || !givenBackOnce(heard, "0v6cmd") {
			t.Fatalf("A RUNNER WHOSE HISTORY CANNOT BE READ RAN AN ASSIGNMENT, OR DID NOT GIVE IT BACK: launcher ops %v; answers %v", f.host.Ops(), answered(heard, "0v6cmd"))
		}
		if data, err := os.ReadFile(history); err != nil || string(data) != "\x00{" {
			t.Fatalf("A HISTORY THAT CANNOT BE READ WAS REWRITTEN: %q %v", data, err)
		}
	})
}

// An orphan an earlier life left, whose attempt the ship says still runs,
// is held at the start; a delivery of its attempt — respelled — is given
// back to the ship, and never taken for execution again.
func TestAnAttemptAHeldOrphanNamesIsNeverTakenAgain(t *testing.T) {
	f := newVMFixture(t, 3)
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Reserve(launcher.ReserveRequest{Attempt: attemptAtom, Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}); err != nil {
		cl.Close()
		t.Fatal(err)
	}
	cl.Close()
	f.ship.status[attemptAtom] = "running"
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	_, held := d.held["ci-"+attemptAtom]
	d.mu.Unlock()
	if !held {
		t.Fatalf("fixture: the orphan is held; log:\n%s", f.logs.String())
	}
	f.offerAssignment(respelledAssignment(f.signed(t, attemptAtom), "0v05cmd"))
	f.offerAssignment(f.signed(t, "0v6cmd"))
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	if hasRecord(f, "taken", attemptAtom) {
		t.Fatalf("AN ATTEMPT A HELD ORPHAN NAMES WAS TAKEN FOR EXECUTION AGAIN; launcher ops %v", f.host.Ops())
	}
	if !givenBackOnce(heard, attemptAtom) || !strings.Contains(f.ship.abandon(0), "not run again") {
		t.Fatalf("AN ATTEMPT A HELD ORPHAN NAMES WAS NOT GIVEN BACK AS TAKEN BEFORE: %v %s", answered(heard, attemptAtom), f.ship.abandon(0))
	}
	if prepared(f, "0v6cmd") != 1 {
		t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
	}
}

// A state file an earlier version wrote — no format stamp, a retention of
// the legacy shape, its handle only — begins the ledger's history, which
// says so; the attempt its retention names is given back, never run.
func TestAnEarlierVersionsStateFileBeginsTheLedgersHistory(t *testing.T) {
	f := newVMHost(t, 3) // no ledger: an upgraded runner (Q15 fixture adaptation)
	data, err := os.ReadFile(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	delete(file, "format")
	file["quarantined"] = []map[string]any{{"handle": "ci-" + attemptAtom, "reason": "teardown failed", "at": time.Now().Unix() - 3600}}
	if data, err = json.Marshal(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cfg.StateFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	var h map[string]any
	if data, err := os.ReadFile(filepath.Join(executionsOf(f), "HISTORY")); err != nil || json.Unmarshal(data, &h) != nil {
		t.Fatalf("THE LEDGER DOES NOT SAY WHEN ITS HISTORY BEGAN: %q %v", data, err)
	}
	if h["enrolled"] != false || h["state_format"] != float64(0) {
		t.Fatalf("THE LEDGER'S HISTORY DOES NOT SAY IT BEGAN WITH AN EARLIER VERSION'S STATE FILE: %v", h)
	}
	f.offerAssignment(respelledAssignment(f.signed(t, attemptAtom), "0v5.cmd"))
	f.offerAssignment(f.signed(t, "0v6cmd"))
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT AN EARLIER VERSION'S RETENTION NAMES RAN AGAIN: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(heard, attemptAtom) {
		t.Fatalf("an attempt an earlier version's retention names was not given back: %v", answered(heard, attemptAtom))
	}
	// the runner waits for its transition (INTEGRATION.md §11.15): the new
	// attempt is given back too; the retention's attempt after a transition
	// is TestAnAttemptOldStateNamesIsGivenBackAfterTheTransition's
	if prepared(f, "0v6cmd") != 0 || !givenBackOnce(heard, "0v6cmd") {
		t.Fatalf("AN UPGRADED RUNNER RAN A NEW ATTEMPT BEFORE ITS TRANSITION: launcher ops %v; answers %v", f.host.Ops(), answered(heard, "0v6cmd"))
	}
}

// The state file names the ledger its runner keeps, from its first start.
// A ledger that is gone — or has lost a record directory — is not made
// again: the daemon does not start, and what it holds is left as it is; the
// attempts it took went with it, and one of them delivered again would run.
func TestALostOrDamagedLedgerStopsTheDaemon(t *testing.T) {
	for _, gone := range []string{"", "taken", "finished"} {
		name := "the ledger"
		if gone != "" {
			name = "its " + gone + " directory"
		}
		t.Run(name, func(t *testing.T) {
			f := newVMHost(t, 2) // its first start names the ledger (Q15 fixture adaptation)
			d, heard := f.startHeard(t)
			f.offerAssignment(f.signed(t, attemptAtom))
			stop := run(t, d)
			waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
			stop()
			closeDaemon(d)
			st, err := state.Load(f.cfg.StateFile)
			if err != nil || !st.Ledger {
				t.Fatalf("THE STATE FILE DOES NOT NAME THE LEDGER ITS RUNNER KEEPS: %+v %v", st, err)
			}
			p := executionsOf(f)
			if gone != "" {
				p = filepath.Join(p, gone)
			}
			if err := os.Rename(p, p+".aside"); err != nil { // the test's own files: set aside, not deleted
				t.Fatal(err)
			}
			if d2, err := New(context.Background(), f.cfg, log.New(f.logs, "", 0)); err == nil {
				closeDaemon(d2)
				t.Fatalf("A DAEMON STARTED ON A LEDGER THAT LOST ITS RECORDS (%s)", name)
			}
			if _, err := os.Lstat(p); err == nil {
				t.Fatalf("A LEDGER THAT LOST ITS RECORDS WAS MADE AGAIN (%s)", name)
			}
		})
	}
}

// A ledger this runner cannot read — its taken directory, or its finished
// one — cannot say whether an attempt ran here: a delivery is given back to
// the ship, with the reason, and not run, even where a record could still
// be written.
func TestALedgerThatCannotBeReadIsNoAnswer(t *testing.T) {
	for _, sub := range []string{"taken", "finished"} {
		t.Run("its "+sub+" directory", func(t *testing.T) {
			f := newVMFixture(t, 2)
			for _, s := range []string{"taken", "finished"} {
				if err := os.MkdirAll(filepath.Join(executionsOf(f), s), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(executionsOf(f), sub)
			if err := os.Chmod(dir, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o700) })
			d, heard := f.startHeard(t)
			defer closeDaemon(d)
			f.offerAssignment(f.signed(t, attemptAtom))
			stop := run(t, d)
			waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 && claims(d) == 0 })
			stop()
			if n := prepared(f, attemptAtom); n != 0 {
				t.Fatalf("AN ATTEMPT RAN ON A LEDGER THAT COULD NOT BE READ (%s): its sandbox prepared %d times; launcher ops %v", sub, n, f.host.Ops())
			}
			if !givenBackOnce(heard, attemptAtom) || !strings.Contains(f.ship.abandon(0), "cannot read") {
				t.Fatalf("AN ATTEMPT ON A LEDGER THAT COULD NOT BE READ WAS NOT GIVEN BACK AS SUCH (%s): %v %s", sub, answered(heard, attemptAtom), f.ship.abandon(0))
			}
		})
	}
}
