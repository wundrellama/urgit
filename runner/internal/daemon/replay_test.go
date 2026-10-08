package daemon

// Assignment-replay ruling 01, Q14 (runner/launcher/INTEGRATION.md §11.14,
// "An assignment runs at most once here"; independent review 10, R10-1): an
// attempt this runner takes for execution is recorded durably before it
// crosses into execution, and is never run here again — finished, or
// interrupted by a stop — whatever the spelling, nonce, signature or expiry
// of its next delivery. A record that cannot be written leaves the attempt
// unstarted, and nothing is evicted to make room. A new attempt runs.
//
// Through the real Run loop, the real Microvm backend and the launcher's
// real core and wire over private files, with the in-process ship of
// retained_test.go. A stop is a daemon object abandoned without its
// cleanup, its state lock given back: not a process's crash, nor a power
// loss.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// executionsOf is the execution ledger of f's runner: beside its state file.
func executionsOf(f *vmFixture) string {
	return filepath.Join(filepath.Dir(f.cfg.StateFile), "executions")
}

// signedWith is f.signed with its own nonce: the ship signs every hand-over
// of an attempt anew.
func (f *vmFixture) signedWith(t *testing.T, attempt, nonce string) *ship.Assignment {
	t.Helper()
	a := vmAssignment(attempt)
	expiry := time.Now().Add(10 * time.Minute).Unix()
	msg := sig.ManifestMessage{Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: nonce, Manifest: *a.Manifest}
	b, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	a.Sig = &ship.Signature{Version: sig.ManifestVersion, Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: nonce, Sig: hex.EncodeToString(ed25519.Sign(f.priv, b))}
	return a
}

// prepared is how many times the launcher prepared a sandbox for attempt.
func prepared(f *vmFixture, attempt string) int { return f.host.Count("disk", f.at(attempt)) }

// answered is the ship's requests about attempt, in its own spelling.
func answered(h *heardPaths, attempt string) []string {
	var out []string
	for _, p := range h.attemptPaths() {
		if strings.Contains(p, " /attempt/"+attempt+"/") {
			out = append(out, p)
		}
	}
	return out
}

// A finished attempt's assignment, delivered again while its signature is
// valid — exactly, respelled, or signed anew with a fresh nonce, in the
// same life or after a restart — is never run again, and nothing is said
// about it to the ship. A new attempt after it runs.
func TestAFinishedAttemptIsNeverRunAgain(t *testing.T) {
	cases := []struct {
		name, spelling string
		restart, anew  bool
	}{
		{"same life/exact", attemptAtom, false, false},
		{"same life/respelled", "0v5.cmd", false, false},
		{"same life/signed anew", attemptAtom, false, true},
		{"after a restart/exact", attemptAtom, true, false},
		{"after a restart/respelled", "0v05cmd", true, false},
		{"after a restart/signed anew", attemptAtom, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			d, heard := f.startHeard(t)
			orig := f.signed(t, attemptAtom)
			f.offerAssignment(orig)
			stop := run(t, d)
			// it runs: prepared, its checkout fails in this fixture, it is
			// answered and torn down, and its claim goes
			waitUntil(t, 20*time.Second, "the attempt's answer", f.ship.abandoned(1))
			waitUntil(t, 20*time.Second, "its claim released", func() bool { return !d.claimed(attemptAtom) })
			if prepared(f, attemptAtom) != 1 {
				stop()
				closeDaemon(d)
				t.Fatalf("fixture: the attempt ran once: launcher ops %v", f.host.Ops())
			}
			if c.restart {
				stop()
				closeDaemon(d)
				d, heard = f.startHeard(t)
				stop = run(t, d)
			}
			life := d
			defer func() { stop(); closeDaemon(life) }()
			before := len(answered(heard, attemptAtom))
			again := respelledAssignment(orig, c.spelling)
			if c.anew {
				again = f.signedWith(t, attemptAtom, "0v8.fresh")
			}
			f.offerAssignment(again)
			f.offerAssignment(f.signed(t, "0v6cmd")) // a new attempt after it
			waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool {
				return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0
			})
			stop()
			if n := prepared(f, attemptAtom); n != 1 {
				t.Fatalf("A FINISHED ATTEMPT RAN AGAIN (%s): its sandbox prepared %d times; launcher ops %v", c.name, n, f.host.Ops())
			}
			if got := answered(heard, attemptAtom); len(got) != before {
				t.Fatalf("A FINISHED ATTEMPT'S DELIVERY WAS ANSWERED (%s): %v", c.name, got)
			}
			if prepared(f, "0v6cmd") != 1 {
				t.Fatalf("a new attempt did not run: launcher ops %v", f.host.Ops())
			}
		})
	}
}

// An attempt a stop interrupts during its run — the ship then times it
// out, and the next life reconciles its leftover away — is never run again
// here: a copy of its assignment, still valid, is given back to the ship,
// once verified, and not prepared. A record written only when an attempt
// finishes would let it run again.
func TestAnAttemptInterruptedByAStopIsNeverRunAgain(t *testing.T) {
	for _, spelling := range []string{attemptAtom, "0v5.cmd"} {
		t.Run(spelling, func(t *testing.T) {
			f := newVMFixture(t, 2)
			d := f.start(t)
			gone := make(chan struct{})
			d.client.HTTP = &http.Client{Transport: inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-gone: // the first life says nothing more
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				default:
				}
				f.ship.ServeHTTP(w, r)
			})}}
			inside, never := make(chan struct{}), make(chan struct{})
			d.checkout = func(context.Context, *ship.Assignment, string) error {
				close(inside)
				<-never // a stopped process does nothing more: its goroutines are never resumed
				return errors.New("no checkout in this test")
			}
			orig := f.signed(t, attemptAtom)
			f.offerAssignment(orig)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			go d.Run(ctx) // its life ends in the stop, never waited for
			select {
			case <-inside:
			case <-time.After(20 * time.Second):
				t.Fatalf("fixture: the attempt did not start; log:\n%s", f.logs.String())
			}
			// the stop: the first life is cut off, its lock given back
			close(gone)
			cancel()
			closeDaemon(d)
			// the ship times the attempt out; the next life reconciles its
			// leftover away
			f.ship.mu.Lock()
			f.ship.status[attemptAtom] = "failed"
			f.ship.mu.Unlock()
			d2, heard := f.startHeard(t)
			defer closeDaemon(d2)
			if err := d2.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if recs := f.srv.Held(t, vmDaemon); len(recs) != 0 {
				t.Fatalf("fixture: the leftover is reconciled away: %+v", recs)
			}
			f.offerAssignment(respelledAssignment(orig, spelling))
			f.offerAssignment(f.signed(t, "0v6cmd"))
			stop := run(t, d2)
			waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool {
				return len(answered(heard, "0v6cmd")) > 0 && claims(d2) == 0
			})
			stop()
			if n := prepared(f, attemptAtom); n != 1 {
				t.Fatalf("AN ATTEMPT INTERRUPTED BY A STOP RAN AGAIN (%s): its sandbox prepared %d times; launcher ops %v", spelling, n, f.host.Ops())
			}
			if got := answered(heard, attemptAtom); len(got) != 1 || !strings.HasSuffix(got[0], "/abandon") {
				t.Fatalf("AN INTERRUPTED ATTEMPT'S DELIVERY WAS NOT GIVEN BACK TO THE SHIP (%s): %v", spelling, got)
			}
		})
	}
}

// The attempt's record exists, durably, before it crosses into execution:
// when the launcher prepares its sandbox, the ledger holds it.
func TestAnAttemptIsRecordedBeforeItRuns(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	release := f.host.Hold(t, "disk")
	f.offerAssignment(f.signed(t, attemptAtom))
	stop := run(t, d)
	if _, ok := f.reachedWithin(20 * time.Second); !ok {
		release()
		stop()
		t.Fatalf("fixture: the attempt did not reach its disk step; log:\n%s", f.logs.String())
	}
	_, err := os.Lstat(filepath.Join(executionsOf(f), "taken", attemptAtom))
	release()
	stop()
	if err != nil {
		t.Fatalf("AN ATTEMPT CROSSED INTO EXECUTION BEFORE ITS RECORD EXISTED: %v", err)
	}
}

// A record the ledger cannot write — its directory refuses it — leaves the
// attempt unstarted: it is given back to the ship, and not run again in
// this life. The records the ledger holds already stay: nothing is evicted.
func TestAnAttemptWhoseRecordCannotBeWrittenIsNotStarted(t *testing.T) {
	f := newVMFixture(t, 2)
	taken := filepath.Join(executionsOf(f), "taken")
	if err := os.MkdirAll(taken, 0o700); err != nil {
		t.Fatal(err)
	}
	held := []string{"0v1abcd", "0v2abcd"} // records of attempts an earlier life took
	for _, a := range held {
		if err := os.WriteFile(filepath.Join(taken, a), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(taken, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(taken, 0o700) })
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	orig := f.signed(t, attemptAtom)
	f.offerAssignment(orig)
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 })
	f.offerAssignment(respelledAssignment(orig, "0v5.cmd")) // delivered again, in this life
	f.offerAssignment(f.signed(t, "0v6cmd"))
	waitUntil(t, 20*time.Second, "the next attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
	stop()
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT RAN WITHOUT ITS RECORD: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
	if got := answered(heard, attemptAtom); len(got) != 1 || !strings.HasSuffix(got[0], "/abandon") {
		t.Fatalf("AN ATTEMPT NOT STARTED WAS NOT GIVEN BACK ONCE: %v", got)
	}
	names, err := os.ReadDir(taken)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(held) {
		t.Fatalf("A RECORD WAS EVICTED, OR ONE ADDED THROUGH A REFUSING DIRECTORY: %v", names)
	}
}

// A ledger this runner cannot read cannot say whether an attempt ran here:
// the delivery is given back to the ship, and not run.
func TestALedgerThatCannotBeReadRefusesTheDelivery(t *testing.T) {
	f := newVMFixture(t, 2)
	taken := filepath.Join(executionsOf(f), "taken")
	if err := os.MkdirAll(taken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(taken, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(taken, 0o700) })
	d, heard := f.startHeard(t)
	defer closeDaemon(d)
	f.offerAssignment(f.signed(t, attemptAtom))
	stop := run(t, d)
	waitUntil(t, 20*time.Second, "the attempt's answer", func() bool { return len(answered(heard, attemptAtom)) > 0 })
	stop()
	if n := prepared(f, attemptAtom); n != 0 {
		t.Fatalf("AN ATTEMPT RAN ON A LEDGER THAT COULD NOT BE READ: its sandbox prepared %d times; launcher ops %v", n, f.host.Ops())
	}
}

// A ledger that is no ledger — its record directory a file — stops the
// daemon at its start: nothing runs on a ledger that cannot record.
func TestALedgerThatCannotBeOpenedStopsTheDaemon(t *testing.T) {
	f := newVMHost(t, 2) // no ledger before its first start (Q15 fixture adaptation)
	if err := os.MkdirAll(executionsOf(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(executionsOf(f), "taken"), []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := New(context.Background(), f.cfg, log.New(f.logs, "", 0))
	if err == nil {
		closeDaemon(d)
		t.Fatal("A DAEMON STARTED ON A LEDGER THAT CANNOT RECORD")
	}
}

// A state file from before the ledger is read as it is: the attempts its
// retentions and released entries name were taken by an earlier life, and
// are never run again — given back to the ship once verified.
func TestAnAttemptAnEarlierStateFileNamesIsNeverRunAgain(t *testing.T) {
	for _, c := range []struct{ name, attempt, spelling string }{
		{"a retention's", attemptAtom, attemptAtom},
		{"a retention's, respelled", attemptAtom, "0v5.cmd"},
		{"a released entry's", "0v7cmd", "0v7cmd"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 3)
			st, err := state.Load(f.cfg.StateFile)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().Unix()
			st.Quarantined = append(st.Quarantined, state.Quarantine{Handle: "ci-" + attemptAtom, Reason: "teardown failed", At: now - 3600, Backend: "microvm", Attempt: attemptAtom,
				VM: "t-earlier", Incarnation: strings.Repeat("a", 32), CID: 9, Created: now - 3600})
			st.Released = append(st.Released, state.Quarantine{Handle: "ci-0v7cmd", Reason: "teardown failed", At: now - 7200, Backend: "microvm", Attempt: "0v7cmd",
				VM: "t-released", Incarnation: strings.Repeat("b", 32), CID: 8, Created: now - 7200, ReleasedAt: now - 3600})
			if err := state.Save(f.cfg.StateFile, st); err != nil {
				t.Fatal(err)
			}
			d, heard := f.startHeard(t)
			defer closeDaemon(d)
			f.offerAssignment(respelledAssignment(f.signed(t, c.attempt), c.spelling))
			f.offerAssignment(f.signed(t, "0v6cmd"))
			stop := run(t, d)
			waitUntil(t, 20*time.Second, "the new attempt's answer", func() bool { return len(answered(heard, "0v6cmd")) > 0 && claims(d) == 0 })
			stop()
			if n := prepared(f, c.attempt); n != 0 {
				t.Fatalf("AN ATTEMPT AN EARLIER STATE FILE NAMES RAN AGAIN (%s): its sandbox prepared %d times; launcher ops %v", c.name, n, f.host.Ops())
			}
			if got := answered(heard, c.attempt); len(got) != 1 || !strings.HasSuffix(got[0], "/abandon") {
				t.Fatalf("an attempt an earlier state file names was not given back to the ship (%s): %v", c.name, got)
			}
		})
	}
}

// The ledger records when its history began: what an earlier version ran
// and finished before it left no trace (QUESTIONS §15).
func TestTheLedgerRecordsWhenItsHistoryBegan(t *testing.T) {
	f := newVMHost(t, 2) // no ledger before its first start (Q15 fixture adaptation)
	d := f.start(t)
	closeDaemon(d)
	data, err := os.ReadFile(filepath.Join(executionsOf(f), "HISTORY"))
	if err != nil {
		t.Fatalf("THE LEDGER DOES NOT SAY WHEN ITS HISTORY BEGAN: %v", err)
	}
	var h struct {
		Since int64 `json:"since"`
	}
	if err := json.Unmarshal(data, &h); err != nil || h.Since <= 0 || h.Since > time.Now().Unix() {
		t.Fatalf("THE LEDGER DOES NOT SAY WHEN ITS HISTORY BEGAN: %q %v", data, err)
	}
}
