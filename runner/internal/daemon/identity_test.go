package daemon

// Independent review 08, R8-1 (runner/launcher/INTEGRATION.md §11.12, "One
// command, one identity"): the signature binds a command's atom, so every
// spelling of that atom — a separator anywhere, a leading zero — is the same
// command. It has one decision: a refusal, a release, a refusal not recorded
// yet, an uncertain release, a delivery in progress, an answer waiting for
// the ship. A record written under an earlier spelling keeps its
// protection. Every replay below reuses the original signed wire map with
// only its id respelled: no signing key makes an alias.
//
// Through the real Microvm backend and launcher core over private files,
// with the in-process ship of legacy_test.go.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// canonicalID is a command id as the ship writes it (Hoon's @uv: its
// digits grouped by five from the right); the aliases are other spellings
// of its atom.
const canonicalID = "0v5cmd"

var aliasIDs = []string{"0v5.cmd", "0v05cmd", "0v5c.md"}

// sameAtom says whether a and b are spellings of one @uv atom.
func sameAtom(a, b any) bool {
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	if !ok1 || !ok2 {
		return false
	}
	p, err1 := sig.ParseUV(x)
	q, err2 := sig.ParseUV(y)
	return err1 == nil && err2 == nil && p.Cmp(q) == 0
}

// answersOfAtom is every answer the ship holds for id's atom.
func (s *legacyShip) answersOfAtom(id string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, a := range s.answers {
		if sameAtom(a["command"], id) {
			out = append(out, a)
		}
	}
	return out
}

// refusalsOfAtom is the state file's recorded refusals of id's atom.
func refusalsOfAtom(t *testing.T, path, id string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, r := range refusalsOf(t, path) {
		if sameAtom(r["command"], id) {
			out = append(out, r)
		}
	}
	return out
}

// respelled is the signed wire map with its id respelled, and nothing
// else changed: the same signed bytes, the original signature.
func respelled(t *testing.T, wire map[string]any, id string) *ship.RecoveryCommand {
	t.Helper()
	copied := map[string]any{}
	for k, v := range wire {
		copied[k] = v
	}
	copied["id"] = id
	return toCommand(t, copied)
}

// A refused command is refused under every spelling of its atom, with its
// recorded reason — in this life and after a reopen — and the answer names
// the command as the ship writes it. Nothing is released, and the refusal
// is recorded once.
func TestARefusedCommandIsRefusedUnderEverySpelling(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		for _, alias := range aliasIDs {
			name := "same life/" + alias
			if reopen {
				name = "after a reopen/" + alias
			}
			t.Run(name, func(t *testing.T) {
				f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
				d := f.start(t)
				e := reportedEntry(t, d)
				spec := bound(e)
				spec.id = canonicalID
				wire := f.command(t, spec)
				restore := launcherAway(t, f)
				first := d.recover(context.Background(), toCommand(t, wire))
				restore()
				if first.Status != ship.AnswerRefused || len(refusalsOfAtom(t, f.cfg.StateFile, canonicalID)) != 1 {
					closeDaemon(d)
					t.Fatalf("fixture: the command is refused and recorded: %+v", first)
				}
				if reopen {
					closeDaemon(d)
					d = f.start(t)
				}
				defer closeDaemon(d)
				second := d.recover(context.Background(), respelled(t, wire, alias))
				if second.Status != ship.AnswerRefused || second.Detail != first.Detail || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
					t.Fatalf("AN ALIAS OF A REFUSED COMMAND WAS JUDGED AGAIN (%s): first %+v, alias %+v, released %v, advertised %d",
						name, first, second, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
				}
				if second.Command != canonicalID {
					t.Fatalf("AN ANSWER DID NOT NAME ITS COMMAND AS THE SHIP WRITES IT (%s): %q", name, second.Command)
				}
				if got := refusalsOfAtom(t, f.cfg.StateFile, canonicalID); len(got) != 1 {
					t.Fatalf("ONE COMMAND WAS RECORDED MORE THAN ONCE (%s): %v", name, got)
				}
			})
		}
	}
}

// A carried-out command is answered completed under every spelling of its
// atom, from its record — in this life and after a reopen — and is never
// judged again: no second release, and no refusal recorded for it.
func TestACarriedOutCommandIsAnsweredUnderEverySpelling(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		for _, alias := range aliasIDs {
			name := "same life/" + alias
			if reopen {
				name = "after a reopen/" + alias
			}
			t.Run(name, func(t *testing.T) {
				f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
				d := f.start(t)
				e := reportedEntry(t, d)
				spec := bound(e)
				spec.id = canonicalID
				wire := f.command(t, spec)
				first := d.recover(context.Background(), toCommand(t, wire))
				if first.Status != ship.AnswerCompleted || len(released(t, f.cfg.StateFile)) != 1 {
					closeDaemon(d)
					t.Fatalf("fixture: the command is carried out: %+v", first)
				}
				if reopen {
					closeDaemon(d)
					d = f.start(t)
				}
				defer closeDaemon(d)
				second := d.recover(context.Background(), respelled(t, wire, alias))
				if second.Status != ship.AnswerCompleted || second.ReleasedAt != first.ReleasedAt || len(released(t, f.cfg.StateFile)) != 1 || d.remainingCapacity() != 2 {
					t.Fatalf("AN ALIAS OF A CARRIED-OUT COMMAND WAS NOT ANSWERED FROM ITS RECORD (%s): first %+v, alias %+v, released %v, advertised %d",
						name, first, second, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
				}
				if got := refusalsOfAtom(t, f.cfg.StateFile, canonicalID); len(got) != 0 {
					t.Fatalf("A CARRIED-OUT COMMAND WAS ALSO RECORDED REFUSED (%s): %v", name, got)
				}
			})
		}
	}
}

// A command not decided yet is not judged again under another spelling:
// its refusal not recorded yet, its release's save uncertain, its delivery
// in progress. Each stays the one decision it is.
func TestAnUndecidedCommandIsNotJudgedAgainUnderAnotherSpelling(t *testing.T) {
	t.Run("its refusal not recorded yet", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		defer closeDaemon(d)
		e := reportedEntry(t, d)
		spec := bound(e)
		spec.id = canonicalID
		wire := f.command(t, spec)
		var mu sync.Mutex
		failing := true
		d.saveState = func(path string, st *state.State) error {
			mu.Lock()
			defer mu.Unlock()
			if failing {
				return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
			}
			return state.Save(path, st)
		}
		restore := launcherAway(t, f)
		first := d.recover(context.Background(), toCommand(t, wire))
		restore()
		if first.Status != ship.AnswerUncertain {
			t.Fatalf("fixture: a refusal whose save failed is not final: %+v", first)
		}
		// saves succeed again, and the proof holds now: the alias comes
		mu.Lock()
		failing = false
		mu.Unlock()
		second := d.recover(context.Background(), respelled(t, wire, "0v5.cmd"))
		if second.Status != ship.AnswerRefused || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 || len(refusalsOfAtom(t, f.cfg.StateFile, canonicalID)) != 1 {
			t.Fatalf("AN ALIAS OF A COMMAND WHOSE REFUSAL WAS NOT RECORDED YET WAS JUDGED AGAIN: first %+v, alias %+v, released %v, advertised %d",
				first, second, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
		}
	})
	t.Run("its release's save uncertain", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		defer closeDaemon(d)
		e := reportedEntry(t, d)
		spec := bound(e)
		spec.id = canonicalID
		wire := f.command(t, spec)
		var mu sync.Mutex
		uncertainOnce := true
		d.saveState = func(path string, st *state.State) error {
			mu.Lock()
			defer mu.Unlock()
			if uncertainOnce && len(st.Released) > 0 {
				uncertainOnce = false
				if err := state.Save(path, st); err != nil {
					return err
				}
				return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
			}
			return state.Save(path, st)
		}
		first := d.recover(context.Background(), toCommand(t, wire))
		if first.Status != ship.AnswerUncertain || d.remainingCapacity() != 1 {
			t.Fatalf("fixture: the release's save is uncertain: %+v, advertised %d", first, d.remainingCapacity())
		}
		// saves succeed now; the alias comes before any other save settles the doubt
		second := d.recover(context.Background(), respelled(t, wire, "0v05cmd"))
		if second.Status != ship.AnswerUncertain || d.remainingCapacity() != 1 {
			t.Fatalf("AN ALIAS OF A COMMAND WHOSE RELEASE WAS UNCERTAIN WAS JUDGED AGAIN: first %+v, alias %+v, advertised %d", first, second, d.remainingCapacity())
		}
	})
	t.Run("its delivery in progress", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		defer closeDaemon(d)
		e := reportedEntry(t, d)
		spec := bound(e)
		spec.id = canonicalID
		wire := f.command(t, spec)
		entered, proceed := make(chan struct{}), make(chan struct{})
		var once sync.Once
		d.box = hookedBox{Microvm: d.box.(*sandbox.Microvm), hook: func() {
			once.Do(func() { close(entered); <-proceed })
		}}
		d.client.HTTP = f.ship.client()
		var wg sync.WaitGroup
		d.startRecovery(context.Background(), toCommand(t, wire), &wg)
		<-entered
		d.startRecovery(context.Background(), respelled(t, wire, "0v5c.md"), &wg) // an alias delivered meanwhile
		close(proceed)
		wg.Wait()
		answers := f.ship.answersOfAtom(canonicalID)
		if len(answers) != 1 || answers[0]["status"] != "completed" || len(released(t, f.cfg.StateFile)) != 1 || len(refusalsOfAtom(t, f.cfg.StateFile, canonicalID)) != 0 {
			t.Fatalf("AN ALIAS OF A COMMAND IN PROGRESS WAS CARRIED OUT AGAIN: answers %v, released %v, refused %v",
				answers, handlesOf(released(t, f.cfg.StateFile)), refusalsOfAtom(t, f.cfg.StateFile, canonicalID))
		}
	})
}

// One command has one answer waiting for the ship, whatever the spellings
// its answers were made under: a final one is kept against a doubt.
func TestOneCommandHasOneAnswerWhateverItsSpelling(t *testing.T) {
	d := &Daemon{}
	d.mu.Lock()
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: canonicalID, Status: ship.AnswerRefused, Detail: "recorded"})
	d.queueAnswerLocked(ship.RecoveryAnswer{Command: "0v5.cmd", Status: ship.AnswerUncertain, Detail: "an alias"})
	n := len(d.outbox)
	var kept []ship.RecoveryAnswer
	for _, a := range d.outbox {
		kept = append(kept, a)
	}
	d.mu.Unlock()
	if n != 1 || kept[0].Status != ship.AnswerRefused {
		t.Fatalf("TWO SPELLINGS OF ONE COMMAND WERE KEPT AS TWO ANSWERS: %+v", kept)
	}
}

// editState rewrites the state file's JSON with edit: what an earlier
// version of this runner could have written.
func editState(t *testing.T, path string, edit func(file map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	edit(file)
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A record written under another spelling of a command's atom — by a
// version that recorded the command as delivered — keeps its protection:
// the command is answered from it, and the record is kept as written.
func TestARecordUnderAnotherSpellingKeepsItsProtection(t *testing.T) {
	t.Run("a refusal", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		e := reportedEntry(t, d)
		closeDaemon(d)
		editState(t, f.cfg.StateFile, func(file map[string]any) {
			file["refused"] = []any{map[string]any{"command": "0v05cmd", "selection": e.Selection, "revision": e.Revision, "evidence": e.Evidence,
				"reason": "refused by an earlier version", "at": time.Now().Add(-time.Hour).Unix()}}
		})
		d2 := f.start(t)
		defer closeDaemon(d2)
		spec := bound(e)
		spec.id = canonicalID
		a := d2.recover(context.Background(), toCommand(t, f.command(t, spec)))
		if a.Status != ship.AnswerRefused || a.Detail != "refused by an earlier version" || len(released(t, f.cfg.StateFile)) != 0 || d2.remainingCapacity() != 1 {
			t.Fatalf("A REFUSAL RECORDED UNDER ANOTHER SPELLING LOST ITS PROTECTION: %+v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
		}
		// a later save keeps the record, as it was written
		d2.retain(sandbox.Handle{ID: "ci-0v8.other", Attempt: "0v8.other", VM: "t-other", Incarnation: strings.Repeat("b", 32), CID: 8, Created: time.Now().Unix()}, "another retention, saved after")
		got := refusalsOf(t, f.cfg.StateFile)
		if len(got) != 1 || got[0]["command"] != "0v05cmd" {
			t.Fatalf("A RECORD UNDER ANOTHER SPELLING WAS REWRITTEN OR DROPPED: %v", got)
		}
	})
	t.Run("a release", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		e := reportedEntry(t, d)
		closeDaemon(d)
		editState(t, f.cfg.StateFile, func(file map[string]any) {
			entries, _ := file["quarantined"].([]any)
			entry, _ := entries[0].(map[string]any)
			entry["released_at"] = time.Now().Add(-time.Hour).Unix()
			entry["command"] = "0v5.cmd"
			entry["evidence"] = e.Evidence
			file["released"] = []any{entry}
			delete(file, "quarantined")
		})
		d2 := f.start(t)
		defer closeDaemon(d2)
		spec := bound(e)
		spec.id = canonicalID
		a := d2.recover(context.Background(), toCommand(t, f.command(t, spec)))
		if a.Status != ship.AnswerCompleted || len(released(t, f.cfg.StateFile)) != 1 || len(refusalsOfAtom(t, f.cfg.StateFile, canonicalID)) != 0 {
			t.Fatalf("A RELEASE RECORDED UNDER ANOTHER SPELLING WAS NOT ANSWERED FROM ITS RECORD: %+v, refused %v", a, refusalsOfAtom(t, f.cfg.StateFile, canonicalID))
		}
	})
}
