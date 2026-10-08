package daemon

// Independent review 07, R7-1 (runner/launcher/INTEGRATION.md §11.12, "Every
// final answer is recorded first"): a refusal whose save failed is decided,
// only not recorded. When the command comes again once saves succeed, that
// refusal is recorded and answered — the command is not judged again, even
// though its proof would hold by then.

import (
	"context"
	"sync"
	"testing"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

func TestARefusalPendingWhenSavesRecoverIsRecordedNotJudgedAgain(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	defer closeDaemon(d)
	e := reportedEntry(t, d)
	cmd := toCommand(t, f.command(t, cmdSpec{selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
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
	first := d.recover(context.Background(), cmd)
	restore()
	if first.Status != ship.AnswerUncertain {
		t.Fatalf("A REFUSAL THAT IS NOT RECORDED DURABLY WAS ANSWERED FINAL: %+v", first)
	}
	// saves succeed again, and the command comes again before any other
	// save: the proof holds now, and still the command is not judged again
	mu.Lock()
	failing = false
	mu.Unlock()
	again := d.recover(context.Background(), cmd)
	r := recordedRefusal(t, f.cfg.StateFile, cmd.ID)
	if again.Status != ship.AnswerRefused || r == nil || again.Detail != r["reason"] || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
		t.Fatalf("A REFUSAL NOT RECORDED YET WAS JUDGED AGAIN ONCE SAVES SUCCEEDED: first %+v, again %+v, recorded %v, released %v, advertised %d",
			first, again, r, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
	}
}
