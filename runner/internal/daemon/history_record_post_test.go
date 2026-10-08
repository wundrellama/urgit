package daemon

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16):
// what the correction adds, observed after it — the submitted bytes have
// none of it to run RED against. A runner whose state file holds a record
// that is no transition reports why, with the record as the file holds it,
// and logs it at its start; a record of another runner's history — every
// field right, its evidence another's — is none either; and a transition
// the owner confirmed stands on its own record across a reopen, while the
// superseded record stays kept.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestARecordThatIsNoTransitionIsReportedAndLogged(t *testing.T) {
	f := upgradedRunner(t, 2)
	evidence, _ := f.firstLife(t)["evidence"].(string)
	raw := `{"epoch":0,"command":"0v5hist","evidence":"` + evidence + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, raw)
	reports := f.ship.reported()
	d, _, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A MALFORMED TRANSITION RECORD REFUSED THE START: %v", err)
	}
	defer closeDaemon(d)
	stop := running(t, d)
	h := f.historySince(t, reports)
	stop()
	inv, _ := h["invalidTransition"].(map[string]any)
	if inv == nil || !strings.Contains(inv["problem"].(string), "authorization epoch is 0") || !sameJSON([]byte(inv["record"].(string)), []byte(raw)) {
		t.Fatalf("THE REPORT DOES NOT SAY WHY THE RECORD IS NO TRANSITION, OR SHOW IT: %v", h)
	}
	if expl, _ := h["explanation"].(string); !strings.Contains(expl, "no transition") {
		t.Fatalf("THE REPORT'S EXPLANATION DOES NOT SAY THE RECORD IS NO TRANSITION: %q", expl)
	}
	if !strings.Contains(f.logs.String(), "TRANSITION RECORD IS NO TRANSITION: its authorization epoch is 0") {
		t.Fatalf("THE START DOES NOT LOG A RECORD THAT IS NO TRANSITION; log:\n%s", f.logs.String())
	}
}

// A record right in every field but bound to another history — a state
// file copied from another runner, or its HISTORY since unreadable — is no
// transition: the runner waits, and says why.
func TestARecordOfAnotherHistoryIsNoTransition(t *testing.T) {
	f := upgradedRunner(t, 2)
	f.firstLife(t)
	raw := `{"epoch":1,"command":"0v5hist","evidence":"` + strings.Repeat("e", 64) + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, raw)
	reports := f.ship.reported()
	d, p, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A TRANSITION RECORD OF ANOTHER HISTORY REFUSED THE START: %v", err)
	}
	defer closeDaemon(d)
	stop := running(t, d)
	h := f.historySince(t, reports)
	f.deliver(t, d, p, delivery{"0v6cmd", epochNonce(1, 7)})
	stop()
	if n := prepared(f.vmFixture, "0v6cmd"); n != 0 {
		t.Fatalf("A TRANSITION RECORD OF ANOTHER HISTORY LIFTED THE PAUSE: prepared %d", n)
	}
	inv, _ := h["invalidTransition"].(map[string]any)
	if h["paused"] != true || inv == nil || !strings.Contains(inv["problem"].(string), "another history") {
		t.Fatalf("A TRANSITION RECORD OF ANOTHER HISTORY WAS NOT SAID TO BE NONE: %v", h)
	}
}

// Superseded records accumulate, each kept: a second malformed record,
// found after a first was superseded by a transition since lost, joins it.
func TestSupersededRecordsAreAllKept(t *testing.T) {
	f := upgradedRunner(t, 2)
	history := f.firstLife(t)
	evidence, _ := history["evidence"].(string)
	first := `{"epoch":0,"command":"0v5hist","evidence":"` + evidence + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, first)
	reports := f.ship.reported()
	d, _, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatal(err)
	}
	stop := running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v9.hist", 1, f.historySince(t, reports), nil))
	if a := final(f.answered(t, "0v9.hist")); a["status"] != "completed" {
		stop()
		closeDaemon(d)
		t.Fatalf("fixture: the transition completes: %v", a)
	}
	stop()
	closeDaemon(d)
	// the transition since damaged: another record that is none
	second := `{"epoch":1,"command":"0v9hist","evidence":"` + evidence + `"}`
	writeTransitionRecord(t, f.cfg.StateFile, second)
	reports = f.ship.reported()
	d, _, err = f.startPolledOrRefused(t)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDaemon(d)
	stop = running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v10.hist", 2, f.historySince(t, reports), nil))
	if a := final(f.answered(t, "0v10.hist")); a["status"] != "completed" {
		stop()
		t.Fatalf("fixture: the second transition completes: %v", a)
	}
	waitUntil(t, 5*time.Second, "the second transition's record", func() bool { return transitionRecord(t, f.cfg.StateFile)["epoch"] == float64(2) })
	stop()
	var kept []map[string]json.RawMessage
	_ = json.Unmarshal(fileField(t, f.cfg.StateFile, "superseded_transitions"), &kept)
	if len(kept) != 2 || !sameJSON(kept[0]["record"], []byte(first)) || !sameJSON(kept[1]["record"], []byte(second)) {
		t.Fatalf("A SUPERSEDED RECORD WAS NOT KEPT: %s", fileField(t, f.cfg.StateFile, "superseded_transitions"))
	}
}
