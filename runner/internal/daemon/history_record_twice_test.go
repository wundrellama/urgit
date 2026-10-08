package daemon

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16): a
// field named twice, found by the correction's own audit after its first
// final runs. Go's decoder keeps the last of two names it takes for one
// field. A transition record naming one of its fields twice — the first
// wrong, the last right — was taken for a transition, and lifted the pause;
// a state file naming its transition twice kept the last record, and a save
// erased the first. Now the first is no transition: the runner starts,
// waits, runs nothing and says why, keeping the record with both names,
// until the owner's transition supersedes it, and it loads the file it
// wrote then. The second refuses the start, and the file is left as it is.
//
// Through the real Run loop, verification, the recovery channel and the
// real Microvm backend and launcher over private files, as
// history_record_test.go.

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// namedTwiceTransitions are records naming one field twice, each built on
// this runner's own history evidence e: the last of the two names right,
// the first wrong, so that a reading that keeps the last finds a transition.
var namedTwiceTransitions = []struct {
	name string
	raw  func(e string) string
}{
	{"its epoch, 0 then 1", func(e string) string {
		return `{"epoch":0,"command":"0v5hist","evidence":"` + e + `","at":1790000000,"epoch":1}`
	}},
	{"its command, no atom then one", func(e string) string {
		return `{"epoch":1,"command":"hist","command":"0v5hist","evidence":"` + e + `","at":1790000000}`
	}},
	{"its evidence, another history's then its own", func(e string) string {
		return `{"epoch":1,"command":"0v5hist","evidence":"` + strings.Repeat("e", 64) + `","evidence":"` + e + `","at":1790000000}`
	}},
}

func TestARecordNamingAFieldTwiceLiftsNoPause(t *testing.T) {
	for _, c := range namedTwiceTransitions {
		t.Run(c.name, func(t *testing.T) {
			f := upgradedRunner(t, 2)
			evidence, _ := f.firstLife(t)["evidence"].(string)
			if len(evidence) != 64 {
				t.Fatalf("fixture: the runner reports no history evidence: %q", evidence)
			}
			raw := c.raw(evidence)
			writeTransitionRecord(t, f.cfg.StateFile, raw)
			reports := f.ship.reported()
			d, p, err := f.startPolledOrRefused(t)
			if err != nil {
				t.Fatalf("A TRANSITION RECORD NAMING A FIELD TWICE REFUSED THE START (%s): %v", c.name, err)
			}
			defer closeDaemon(d)
			stop := running(t, d)
			h := f.historySince(t, reports)
			f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"}, delivery{"0v6cmd", epochNonce(1, 7)})
			waitUntil(t, 10*time.Second, "polls after the deliveries", func() bool { return p.count() >= 3 })
			stop()
			if n, m := prepared(f.vmFixture, attemptAtom), prepared(f.vmFixture, "0v6cmd"); n != 0 || m != 0 {
				t.Fatalf("A TRANSITION RECORD NAMING A FIELD TWICE LIFTED THE PAUSE (%s): prepared %d and %d; launcher ops %v", c.name, n, m, f.host.Ops())
			}
			if !givenBackOnce(p.heard, attemptAtom) || !givenBackOnce(p.heard, "0v6cmd") {
				t.Fatalf("AN ASSIGNMENT TO A RUNNER WITH A RECORD NAMING A FIELD TWICE WAS NOT GIVEN BACK ONCE (%s): %v %v", c.name, answered(p.heard, attemptAtom), answered(p.heard, "0v6cmd"))
			}
			for _, capacity := range p.since(0) {
				if capacity != "0" {
					t.Fatalf("A RUNNER WITH A RECORD NAMING A FIELD TWICE ADVERTISED CAPACITY (%s): %v", c.name, p.since(0))
				}
			}
			inv, _ := h["invalidTransition"].(map[string]any)
			if h["paused"] != true || h["transition"] != nil || inv == nil || !strings.Contains(inv["problem"].(string), "more than once") || !sameJSON([]byte(inv["record"].(string)), []byte(raw)) {
				t.Fatalf("A RECORD NAMING A FIELD TWICE WAS NOT REPORTED AS NO TRANSITION, OR NOT SHOWN (%s): %v", c.name, h)
			}
			if got := fileField(t, f.cfg.StateFile, "transition"); !sameJSON(got, []byte(raw)) {
				t.Fatalf("A RECORD NAMING A FIELD TWICE WAS NOT KEPT AS THE FILE HELD IT (%s): wrote %s, the file holds %s", c.name, raw, got)
			}
		})
	}
}

func TestTheOwnersTransitionSupersedesARecordNamingAFieldTwice(t *testing.T) {
	f := upgradedRunner(t, 2)
	evidence, _ := f.firstLife(t)["evidence"].(string)
	raw := namedTwiceTransitions[0].raw(evidence)
	writeTransitionRecord(t, f.cfg.StateFile, raw)
	reports := f.ship.reported()
	d, p, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A TRANSITION RECORD NAMING A FIELD TWICE REFUSED THE START: %v", err)
	}
	stop := running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v9.hist", 1, f.historySince(t, reports), nil))
	if a := final(f.answered(t, "0v9.hist")); a["status"] != "completed" {
		stop()
		closeDaemon(d)
		t.Fatalf("THE OWNER'S TRANSITION WAS NOT CARRIED OUT OVER A RECORD NAMING A FIELD TWICE: %v; log:\n%s", a, f.logs.String())
	}
	f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"}, delivery{"0v6cmd", epochNonce(1, 7)})
	stop()
	closeDaemon(d)
	if n, m := prepared(f.vmFixture, attemptAtom), prepared(f.vmFixture, "0v6cmd"); n != 0 || m != 1 {
		t.Fatalf("THE TRANSITION OVER A RECORD NAMING A FIELD TWICE DID NOT FENCE OLD WORK AND RUN FRESH WORK: old prepared %d, fresh %d", n, m)
	}
	var kept []map[string]json.RawMessage
	_ = json.Unmarshal(fileField(t, f.cfg.StateFile, "superseded_transitions"), &kept)
	if len(kept) != 1 || !sameJSON(kept[0]["record"], []byte(raw)) || !bytes.Contains(kept[0]["problem"], []byte("more than once")) {
		t.Fatalf("THE RECORD NAMING A FIELD TWICE WAS NOT KEPT, BOTH NAMES, WHEN THE TRANSITION SUPERSEDED IT: %s", fileField(t, f.cfg.StateFile, "superseded_transitions"))
	}
	// reopened: the runner loads the file it wrote, the superseded record,
	// both names, in it; the transition stands
	d, p, err = f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A RUNNER REFUSED THE STATE FILE IT WROTE, A SUPERSEDED RECORD NAMING A FIELD TWICE IN IT: %v", err)
	}
	defer closeDaemon(d)
	stop = running(t, d)
	f.deliver(t, d, p, delivery{"0v8cmd", "0v7.older"}, delivery{"0v9cmd", epochNonce(1, 9)})
	stop()
	if n, m := prepared(f.vmFixture, "0v8cmd"), prepared(f.vmFixture, "0v9cmd"); n != 0 || m != 1 {
		t.Fatalf("AFTER A REOPEN, THE TRANSITION OVER A RECORD NAMING A FIELD TWICE DID NOT STAND: old prepared %d, fresh %d", n, m)
	}
}

func TestAStateFileNamingItsTransitionTwiceRefusesTheStart(t *testing.T) {
	f := upgradedRunner(t, 2)
	evidence, _ := f.firstLife(t)["evidence"].(string)
	// the last record a transition this runner could have written, the first
	// one of epoch 0: a reading that keeps the last finds a transition, and a
	// save erases the first
	valid := `{"epoch":1,"command":"0v5hist","evidence":"` + evidence + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, valid)
	data, err := os.ReadFile(f.cfg.StateFile)
	if err != nil || !bytes.HasPrefix(data, []byte("{")) {
		t.Fatalf("fixture: the state file: %v %q", err, data)
	}
	twice := append([]byte(`{"transition":{"epoch":0,"command":"0v5hist","evidence":"`+evidence+`","at":1790000000},`), data[1:]...)
	if err := os.WriteFile(f.cfg.StateFile, twice, 0o600); err != nil {
		t.Fatal(err)
	}
	d, _, err := f.startPolledOrRefused(t)
	if err == nil {
		closeDaemon(d)
		t.Fatalf("A STATE FILE NAMING ITS TRANSITION TWICE WAS NOT REFUSED AT THE START")
	}
	if !strings.Contains(err.Error(), `"transition" more than once`) {
		t.Fatalf("THE START'S REFUSAL DOES NOT SAY THE STATE FILE NAMES ITS TRANSITION TWICE: %v", err)
	}
	if got, _ := os.ReadFile(f.cfg.StateFile); !bytes.Equal(got, twice) {
		t.Fatalf("A STATE FILE REFUSED AT THE START WAS NOT LEFT AS IT IS: %s", got)
	}
}
