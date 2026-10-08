package daemon

// Independent review 07, R7-1 (runner/launcher/INTEGRATION.md §11.12, "Every
// final answer is recorded first"): an authenticated recovery command's
// refusal is an outcome, recorded in the state file before it is answered,
// bound to the command, its selection, revision and evidence. The same
// command — handed over again, replayed, after a restart, after its expiry —
// is answered from that record and never judged again; a new confirmation is
// a new command. A refusal not recorded yet is not final, a release settled
// as not applied is recorded in the save that proves it, and nothing a
// daemon cannot authenticate is recorded or answered as final.
//
// Through the real Microvm backend and launcher core over private files,
// with the in-process ship of legacy_test.go.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

// refusalsOf is the state file's recorded refusals, decoded generically.
func refusalsOf(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Refused []map[string]any `json:"refused"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Refused
}

// recordedRefusal is the state file's refusal of command id, or nil: by
// its atom, whatever spelling it was recorded under (INTEGRATION.md §11.12,
// "One command, one identity").
func recordedRefusal(t *testing.T, path, id string) map[string]any {
	t.Helper()
	for _, r := range refusalsOf(t, path) {
		if r["command"] == id || sameAtom(r["command"], id) {
			return r
		}
	}
	return nil
}

// bound is a command for the entry exactly as the daemon reported it.
func bound(e ship.RetentionEntry) cmdSpec {
	return cmdSpec{selection: e.Selection, revision: e.Revision, evidence: e.Evidence}
}

// launcherAway makes the fixture's launcher unreachable until restore.
func launcherAway(t *testing.T, f *legacyFixture) (restore func()) {
	t.Helper()
	away := f.srv.Socket + ".away"
	if err := os.Rename(f.srv.Socket, away); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Rename(away, f.srv.Socket); err != nil {
			t.Fatal(err)
		}
	}
}

// Every refusal of an authenticated command is in the state file, bound to
// the command, before it is answered; every later save keeps it; and after
// a restart the same signed command is answered from it — refused, with the
// same reason — and releases nothing. A new confirmation is a new command.
func TestAnAuthenticatedRefusalIsRecordedAndStandsAcrossRestarts(t *testing.T) {
	none := func(*testing.T, *legacyFixture, *Daemon) func() { return func() {} }
	cases := []struct {
		name   string
		spec   func(e ship.RetentionEntry) cmdSpec
		around func(t *testing.T, f *legacyFixture, d *Daemon) (restore func())
		fresh  bool // a new confirmation of the entry as reported is carried out after the restart
	}{
		{"the proof unmet: the launcher cannot be asked", bound, func(t *testing.T, f *legacyFixture, d *Daemon) func() { return launcherAway(t, f) }, true},
		{"expired", func(e ship.RetentionEntry) cmdSpec {
			s := bound(e)
			s.expiry = time.Now().Add(-time.Minute).Unix()
			return s
		}, none, true},
		{"a stale revision", func(e ship.RetentionEntry) cmdSpec {
			return cmdSpec{selection: e.Selection[:strings.LastIndex(e.Selection, "/")+1] + fmt.Sprint(e.Revision-1), revision: e.Revision - 1, evidence: e.Evidence}
		}, none, true},
		{"evidence it was not inspected on", func(e ship.RetentionEntry) cmdSpec {
			s := bound(e)
			s.evidence = strings.Repeat("e", 64)
			return s
		}, none, true},
		{"a change while the evidence was taken", bound, func(t *testing.T, f *legacyFixture, d *Daemon) func() {
			d.box = hookedBox{Microvm: d.box.(*sandbox.Microvm), hook: func() {
				d.retain(sandbox.Handle{ID: "ci-" + legacyAttempt, Attempt: legacyAttempt, VM: "t-late", Incarnation: strings.Repeat("a", 32), CID: 9, Created: time.Now().Unix()}, "teardown failed meanwhile")
			}}
			return func() {}
		}, false},
		{"the release's save failed before its rename", bound, func(t *testing.T, f *legacyFixture, d *Daemon) func() {
			d.saveState = func(path string, st *state.State) error {
				for _, q := range st.Released {
					if q.Handle == "ci-"+legacyAttempt {
						return &state.SaveError{Step: "write", Path: path, Err: errInjectedSave}
					}
				}
				return state.Save(path, st)
			}
			return func() {}
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
			d := f.start(t)
			e := reportedEntry(t, d)
			if !e.Eligible {
				closeDaemon(d)
				t.Fatalf("fixture: the entry is releasable: %+v", e)
			}
			cmd := toCommand(t, f.command(t, c.spec(e)))
			restore := c.around(t, f, d)
			first := d.recover(context.Background(), cmd)
			restore()
			if first.Status != ship.AnswerRefused || first.Detail == "" || len(released(t, f.cfg.StateFile)) != 0 {
				closeDaemon(d)
				t.Fatalf("fixture: the command is refused (%s): %+v", c.name, first)
			}
			r := recordedRefusal(t, f.cfg.StateFile, cmd.ID)
			if r == nil || r["selection"] != cmd.Selection || r["revision"] != float64(cmd.Revision) || r["evidence"] != cmd.Evidence || r["reason"] != first.Detail || r["at"] == nil {
				closeDaemon(d)
				t.Fatalf("AN AUTHENTICATED REFUSAL WAS NOT RECORDED IN THE STATE FILE BEFORE IT WAS ANSWERED (%s): answered %+v, recorded %v", c.name, first, refusalsOf(t, f.cfg.StateFile))
			}
			// a later save, of another retention, carries it forward
			d.retain(sandbox.Handle{ID: "ci-0v8.other", Attempt: "0v8.other", VM: "t-other", Incarnation: strings.Repeat("b", 32), CID: 8, Created: time.Now().Unix()}, "another retention, saved after the refusal")
			if recordedRefusal(t, f.cfg.StateFile, cmd.ID) == nil {
				closeDaemon(d)
				t.Fatalf("A LATER SAVE DROPPED A RECORDED REFUSAL (%s): %v", c.name, refusalsOf(t, f.cfg.StateFile))
			}
			closeDaemon(d)
			// a restart: the same signed command, replayed, is answered from
			// its record
			d2 := f.start(t)
			defer closeDaemon(d2)
			second := d2.recover(context.Background(), cmd)
			if second.Status != ship.AnswerRefused || len(released(t, f.cfg.StateFile)) != 0 {
				t.Fatalf("A REFUSED COMMAND WAS CARRIED OUT AFTER A RESTART (%s): first %+v, replayed %+v, released %v", c.name, first, second, handlesOf(released(t, f.cfg.StateFile)))
			}
			if second.Detail != first.Detail {
				t.Fatalf("A RECORDED REFUSAL WAS ANSWERED WITH ANOTHER REASON (%s): first %q, replayed %q", c.name, first.Detail, second.Detail)
			}
			if !c.fresh {
				return
			}
			// a new confirmation is a new command, judged on its own
			again := toCommand(t, f.command(t, cmdSpec{id: "0v7.cmd", selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
			if a := d2.recover(context.Background(), again); a.Status != ship.AnswerCompleted {
				t.Fatalf("a new confirmation after a recorded refusal (%s): %+v", c.name, a)
			}
		})
	}
}

// Through the ship: a refusal delivered (its first reply lost, so it is
// posted again), then the command handed over again before and after a
// restart, is answered refused every time; a refusal lost with the daemon
// before it reached the ship is answered from the record by the next one.
// Nothing is released either way.
func TestARefusalIsAnsweredFromItsRecordWhenHandedOverAgain(t *testing.T) {
	t.Run("delivered, then handed over again before and after a restart", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		e := reportedEntry(t, d)
		cmd := f.command(t, bound(e))
		restore := launcherAway(t, f)
		f.ship.mu.Lock()
		f.ship.loseNext = 1 // the ship records the first answer; its reply is lost
		f.ship.mu.Unlock()
		stop := running(t, d)
		f.ship.enqueue(cmd)
		first := final(f.answered(t, "0v5.cmd"))
		restore()
		if first["status"] != "refused" {
			stop()
			closeDaemon(d)
			t.Fatalf("fixture: the command is refused while the launcher cannot be asked: %v", first)
		}
		// handed over again in this life, the proof now holding
		f.ship.mu.Lock()
		f.ship.answers = nil
		f.ship.again = cmd
		f.ship.mu.Unlock()
		if a := final(f.answered(t, "0v5.cmd")); a["status"] != "refused" || a["detail"] != first["detail"] {
			stop()
			closeDaemon(d)
			t.Fatalf("A REFUSED COMMAND HANDED OVER AGAIN WAS ANSWERED OTHERWISE: first %v, again %v", first, a)
		}
		stop()
		closeDaemon(d)
		if recordedRefusal(t, f.cfg.StateFile, "0v5.cmd") == nil {
			t.Fatalf("AN AUTHENTICATED REFUSAL WAS NOT RECORDED IN THE STATE FILE BEFORE IT WAS ANSWERED: %v", refusalsOf(t, f.cfg.StateFile))
		}
		f.ship.mu.Lock()
		f.ship.answers = nil
		f.ship.mu.Unlock()
		d2 := f.start(t)
		defer closeDaemon(d2)
		stop2 := running(t, d2)
		a := final(f.answered(t, "0v5.cmd"))
		stop2()
		if a["status"] != "refused" || len(released(t, f.cfg.StateFile)) != 0 || d2.remainingCapacity() != 1 {
			t.Fatalf("A REFUSED COMMAND WAS CARRIED OUT AFTER A RESTART: %v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
		}
	})
	t.Run("lost before it reached the ship", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		e := reportedEntry(t, d)
		cmd := f.command(t, bound(e))
		restore := launcherAway(t, f)
		first := d.recover(context.Background(), toCommand(t, cmd))
		restore()
		// the daemon stops before it answers: the answer is lost with it
		closeDaemon(d)
		if first.Status != ship.AnswerRefused {
			t.Fatalf("fixture: the command is refused while the launcher cannot be asked: %+v", first)
		}
		d2 := f.start(t)
		defer closeDaemon(d2)
		f.ship.mu.Lock()
		f.ship.again = cmd
		f.ship.mu.Unlock()
		stop2 := running(t, d2)
		a := final(f.answered(t, "0v5.cmd"))
		stop2()
		if a["status"] != "refused" || a["detail"] != first.Detail || len(released(t, f.cfg.StateFile)) != 0 || d2.remainingCapacity() != 1 {
			t.Fatalf("A REFUSAL LOST BEFORE IT REACHED THE SHIP BECAME A RELEASE AFTER A RESTART: decided %+v, answered %v, released %v, advertised %d",
				first, a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
		}
	})
}

// A refusal whose save fails, or whose save's outcome is uncertain, is not
// final: answered uncertain, nothing released, and not judged again when
// the command comes again. The next save that succeeds records it, and only
// then is it answered refused — then, and after a restart, from its record.
// A restart before it was recorded leaves nothing final said: the command,
// handed over again, is judged again on its proof then.
func TestARefusalNotRecordedYetIsNotFinal(t *testing.T) {
	for _, c := range []struct {
		name      string
		uncertain bool
	}{{"its save failed", false}, {"its save's outcome is uncertain", true}} {
		t.Run(c.name, func(t *testing.T) {
			f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
			d := f.start(t)
			e := reportedEntry(t, d)
			cmd := toCommand(t, f.command(t, bound(e)))
			var mu sync.Mutex
			failing := true
			d.saveState = func(path string, st *state.State) error {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case !failing:
					return state.Save(path, st)
				case c.uncertain:
					if err := state.Save(path, st); err != nil {
						return err
					}
					return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
				}
				return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
			}
			restore := launcherAway(t, f)
			first := d.recover(context.Background(), cmd)
			restore()
			if first.Status != ship.AnswerUncertain || first.Detail == "" || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
				closeDaemon(d)
				t.Fatalf("A REFUSAL THAT IS NOT RECORDED DURABLY WAS ANSWERED FINAL (%s): %+v, released %v, advertised %d", c.name, first, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
			}
			// handed over again meanwhile, the proof now holding: still not
			// final, and not judged again
			if again := d.recover(context.Background(), cmd); again.Status != ship.AnswerUncertain || len(released(t, f.cfg.StateFile)) != 0 {
				closeDaemon(d)
				t.Fatalf("A REFUSAL NOT RECORDED YET WAS JUDGED AGAIN OR ANSWERED FINAL WHEN HANDED OVER AGAIN (%s): %+v", c.name, again)
			}
			mu.Lock()
			failing = false
			mu.Unlock()
			if err := d.retrySave(); err != nil {
				closeDaemon(d)
				t.Fatalf("fixture: saves succeed again: %v", err)
			}
			r := recordedRefusal(t, f.cfg.StateFile, cmd.ID)
			d.mu.Lock()
			queued, ok := d.outbox[commandKey(cmd.ID)]
			d.mu.Unlock()
			if r == nil || !ok || queued.Status != ship.AnswerRefused || r["reason"] != queued.Detail {
				closeDaemon(d)
				t.Fatalf("A REFUSAL WAS NOT RECORDED AND ANSWERED ONCE THE STATE FILE WAS DURABLE AGAIN (%s): recorded %v, queued %+v", c.name, r, queued)
			}
			if a := d.recover(context.Background(), cmd); a.Status != ship.AnswerRefused || a.Detail != queued.Detail {
				closeDaemon(d)
				t.Fatalf("A RECORDED REFUSAL WAS ANSWERED WITH ANOTHER REASON (%s): %+v, recorded %v", c.name, a, r)
			}
			closeDaemon(d)
			d2 := f.start(t)
			defer closeDaemon(d2)
			if a := d2.recover(context.Background(), cmd); a.Status != ship.AnswerRefused || len(released(t, f.cfg.StateFile)) != 0 || d2.remainingCapacity() != 1 {
				t.Fatalf("A REFUSED COMMAND WAS CARRIED OUT AFTER A RESTART (%s): %+v, released %v, advertised %d", c.name, a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
			}
		})
	}
	t.Run("a restart before it was recorded", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		e := reportedEntry(t, d)
		cmd := toCommand(t, f.command(t, bound(e)))
		d.saveState = func(path string, st *state.State) error {
			return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
		}
		restore := launcherAway(t, f)
		first := d.recover(context.Background(), cmd)
		restore()
		closeDaemon(d)
		if first.Status != ship.AnswerUncertain || recordedRefusal(t, f.cfg.StateFile, cmd.ID) != nil {
			t.Fatalf("A REFUSAL THAT IS NOT RECORDED DURABLY WAS ANSWERED FINAL: %+v, recorded %v", first, refusalsOf(t, f.cfg.StateFile))
		}
		// nothing final was said: the command is judged again, on its proof
		// now
		d2 := f.start(t)
		defer closeDaemon(d2)
		if a := d2.recover(context.Background(), cmd); a.Status != ship.AnswerCompleted || len(released(t, f.cfg.StateFile)) != 1 || d2.remainingCapacity() != 2 {
			t.Fatalf("a command whose refusal was never final, judged again on its proof: %+v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
		}
	})
}

// A release whose save was uncertain, settled as not applied by a save of
// the daemon's view, is recorded refused in that same save: after a restart
// the same command is answered refused, and releases nothing. A new
// confirmation is judged on its own.
func TestAnUncertainReleaseSettledAsNotAppliedStaysRefused(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	e := reportedEntry(t, d)
	cmd := toCommand(t, f.command(t, bound(e)))
	var mu sync.Mutex
	failing := true
	d.saveState = func(path string, st *state.State) error {
		mu.Lock()
		defer mu.Unlock()
		for _, q := range st.Released {
			if q.Handle == "ci-"+legacyAttempt {
				if err := state.Save(path, st); err != nil {
					return err
				}
				return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
			}
		}
		if failing {
			return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
		}
		return state.Save(path, st)
	}
	first := d.recover(context.Background(), cmd)
	if first.Status != ship.AnswerUncertain || d.remainingCapacity() != 1 {
		closeDaemon(d)
		t.Fatalf("fixture: the release's save is uncertain: %+v, advertised %d", first, d.remainingCapacity())
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	if err := d.retrySave(); err != nil {
		closeDaemon(d)
		t.Fatalf("fixture: saves succeed again: %v", err)
	}
	r := recordedRefusal(t, f.cfg.StateFile, cmd.ID)
	d.mu.Lock()
	queued := d.outbox[commandKey(cmd.ID)]
	d.mu.Unlock()
	if r == nil || queued.Status != ship.AnswerRefused || r["reason"] != queued.Detail || len(released(t, f.cfg.StateFile)) != 0 {
		closeDaemon(d)
		t.Fatalf("A RELEASE SETTLED AS NOT APPLIED WAS NOT RECORDED REFUSED: recorded %v, queued %+v, released %v", r, queued, handlesOf(released(t, f.cfg.StateFile)))
	}
	closeDaemon(d)
	d2 := f.start(t)
	defer closeDaemon(d2)
	if a := d2.recover(context.Background(), cmd); a.Status != ship.AnswerRefused || len(released(t, f.cfg.StateFile)) != 0 || d2.remainingCapacity() != 1 {
		t.Fatalf("A RELEASE SETTLED AS NOT APPLIED WAS CARRIED OUT AFTER A RESTART: %+v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
	}
	again := toCommand(t, f.command(t, cmdSpec{id: "0v7.cmd", selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
	if a := d2.recover(context.Background(), again); a.Status != ship.AnswerCompleted || d2.remainingCapacity() != 2 {
		t.Fatalf("a new confirmation after a release settled as not applied: %+v, advertised %d", a, d2.remainingCapacity())
	}
}

// A message the daemon cannot authenticate is no command of its own: it is
// answered uncertain — never refused or completed — and nothing of it is
// recorded or done. The genuine command of that id is then decided on its
// own.
func TestUnauthenticatedInputDecidesNothing(t *testing.T) {
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		bad  func(s cmdSpec) cmdSpec
	}{
		{"unsigned", func(s cmdSpec) cmdSpec { s.unsigned = true; return s }},
		{"signed by another key", func(s cmdSpec) cmdSpec { s.key = otherKey; return s }},
		{"signed for another daemon", func(s cmdSpec) cmdSpec { s.recipient = "0v2.other"; return s }},
		{"another operation", func(s cmdSpec) cmdSpec { s.operation = "release"; return s }},
		{"another message version", func(s cmdSpec) cmdSpec { s.version = 2; return s }},
		{"a field changed after signing", func(s cmdSpec) cmdSpec {
			rev := s.revision
			s.tamper = func(m map[string]any) { m["revision"] = rev + 1 }
			return s
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
			d := f.start(t)
			defer closeDaemon(d)
			e := reportedEntry(t, d)
			a := d.recover(context.Background(), toCommand(t, f.command(t, c.bad(bound(e)))))
			if a.Status != ship.AnswerUncertain || a.Detail == "" {
				t.Fatalf("A MESSAGE THE DAEMON COULD NOT AUTHENTICATE WAS ANSWERED AS A FINAL OUTCOME (%s): %+v", c.name, a)
			}
			if len(refusalsOf(t, f.cfg.StateFile)) != 0 || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
				t.Fatalf("UNAUTHENTICATED INPUT WAS RECORDED OR ACTED ON (%s): refused %v, released %v, advertised %d", c.name, refusalsOf(t, f.cfg.StateFile), handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
			}
			if g := d.recover(context.Background(), toCommand(t, f.command(t, bound(e)))); g.Status != ship.AnswerCompleted {
				t.Fatalf("THE GENUINE COMMAND WAS DECIDED BY AN UNAUTHENTICATED MESSAGE OF ITS ID (%s): %+v", c.name, g)
			}
		})
	}
}

// A final answer waiting for the ship is never replaced by a non-final one
// for the same command; a final one does replace a doubt.
func TestAFinalAnswerIsNeverReplacedByANonFinalOne(t *testing.T) {
	d := &Daemon{}
	d.mu.Lock()
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: "0v5.cmd", Status: ship.AnswerRefused, Detail: "recorded"})
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: "0v5.cmd", Status: ship.AnswerUncertain, Detail: "a message this daemon could not authenticate"})
	got := d.outbox[commandKey("0v5.cmd")]
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: "0v6.cmd", Status: ship.AnswerUncertain, Detail: "not recorded yet"})
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: "0v6.cmd", Status: ship.AnswerRefused, Detail: "recorded"})
	settled := d.outbox[commandKey("0v6.cmd")]
	d.mu.Unlock()
	if got.Status != ship.AnswerRefused || got.Detail != "recorded" {
		t.Fatalf("A FINAL ANSWER WAITING FOR THE SHIP WAS REPLACED BY A NON-FINAL ONE: %+v", got)
	}
	if settled.Status != ship.AnswerRefused {
		t.Fatalf("a final answer did not replace a doubt: %+v", settled)
	}
}

// A repeat after the command's expiry, and after a restart, is answered
// from its record: completed for a release (once), refused with its
// recorded reason for a refusal — never judged again as expired.
func TestARepeatAfterItsExpiryIsAnsweredFromItsRecord(t *testing.T) {
	for _, c := range []struct {
		name   string
		refuse bool
	}{{"a release", false}, {"a refusal", true}} {
		t.Run(c.name, func(t *testing.T) {
			f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
			d := f.start(t)
			e := reportedEntry(t, d)
			s := bound(e)
			s.expiry = time.Now().Unix() + 2
			cmd := toCommand(t, f.command(t, s))
			restore := func() {}
			if c.refuse {
				restore = launcherAway(t, f)
			}
			first := d.recover(context.Background(), cmd)
			restore()
			want := ship.AnswerCompleted
			if c.refuse {
				want = ship.AnswerRefused
			}
			if first.Status != want {
				closeDaemon(d)
				t.Fatalf("fixture: %s: %+v", c.name, first)
			}
			for time.Now().Unix() <= s.expiry {
				time.Sleep(50 * time.Millisecond)
			}
			closeDaemon(d)
			d2 := f.start(t)
			defer closeDaemon(d2)
			second := d2.recover(context.Background(), cmd)
			if second.Status != first.Status || (c.refuse && second.Detail != first.Detail) {
				t.Fatalf("A REPEAT AFTER ITS EXPIRY WAS NOT ANSWERED FROM ITS RECORD (%s): first %+v, repeat %+v", c.name, first, second)
			}
			if got := len(released(t, f.cfg.StateFile)); (c.refuse && got != 0) || (!c.refuse && got != 1) {
				t.Fatalf("A REPEAT AFTER ITS EXPIRY CHANGED THE RELEASES (%s): %v", c.name, handlesOf(released(t, f.cfg.StateFile)))
			}
		})
	}
}
