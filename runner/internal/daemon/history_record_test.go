package daemon

// Independent review 12, R12-1 (runner/launcher/INTEGRATION.md §11.16): a
// transition record this version did not write is no transition. The
// runner's state file is local; the records here are written into it by
// the test between two lives — corruption, a hand edit — not a remote path.
// Reopened over such a record, a runner with an incomplete history waits as
// one with no record does: it gives back an assignment its ship signed
// before and one of a later epoch, never preparing either, advertises no
// capacity, and reports its history waiting. It keeps the record exactly,
// through every save. The owner's transition, authentic, supersedes it and
// keeps it: fresh work runs again, old work stays fenced, and a reopen
// changes nothing. A record never answers the command it names.
//
// Through the real Run loop, verification, the recovery channel and the
// real Microvm backend and launcher over private files (legacy_test.go's
// in-process ship).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/ship"
)

// startPolledOrRefused is startPolled, with New's refusal returned rather
// than fatal: a transition record must be no reason to refuse the start.
func (f *legacyFixture) startPolledOrRefused(t *testing.T) (*Daemon, *polled, error) {
	t.Helper()
	d, err := New(context.Background(), f.cfg, log.New(f.logs, "", 0))
	if err != nil {
		return nil, nil, err
	}
	d.checkout = func(context.Context, *ship.Assignment, string) error { return errors.New("no checkout in this test") }
	d.reconcileEvery = 50 * time.Millisecond
	p := &polled{}
	p.heard = &heardPaths{next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/assignment") {
			p.mu.Lock()
			p.capacities = append(p.capacities, r.Header.Get("x-ci-capacity"))
			p.mu.Unlock()
		}
		f.ship.ServeHTTP(w, r)
	})}
	d.client.HTTP = &http.Client{Transport: lossy{inproc: inproc{"ship.test": p.heard}, ship: f.ship}}
	return d, p, nil
}

// firstLife starts f's runner once — its ledger made, its history begun —
// and returns its history as reported, then ends that life.
func (f *legacyFixture) firstLife(t *testing.T) map[string]any {
	t.Helper()
	d, _ := f.startPolled(t)
	stop := running(t, d)
	h := f.history(t)
	stop()
	closeDaemon(d)
	return h
}

// historySince is the history of the first report after n reports.
func (f *legacyFixture) historySince(t *testing.T, n int) map[string]any {
	t.Helper()
	waitUntil(t, 10*time.Second, "a report of this life", func() bool { return f.ship.reported() > n })
	return f.history(t)
}

// writeTransitionRecord puts raw into the state file at path as its
// transition, as written.
func writeTransitionRecord(t *testing.T, path, raw string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	file["transition"] = json.RawMessage(raw)
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// fileField is the state file's field as it holds it, or nil.
func fileField(t *testing.T, path, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file[name]
}

// sameJSON says whether a and b are the same JSON, token for token.
func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// malformedTransitions are records this version never writes, each built
// on this runner's own history evidence e, so that each is wrong in one way
// only.
var malformedTransitions = []struct {
	name string
	raw  func(e string) string
}{
	{"an empty object", func(string) string { return `{}` }},
	{"epoch 0", func(e string) string { return `{"epoch":0,"command":"0v5hist","evidence":"` + e + `","at":1790000000}` }},
	{"no epoch", func(e string) string { return `{"command":"0v5hist","evidence":"` + e + `","at":1790000000}` }},
	{"an epoch of another type", func(e string) string {
		return `{"epoch":"1","command":"0v5hist","evidence":"` + e + `","at":1790000000}`
	}},
	{"no command", func(e string) string { return `{"epoch":1,"evidence":"` + e + `","at":1790000000}` }},
	{"a command in another spelling", func(e string) string {
		return `{"epoch":1,"command":"0v5.hist","evidence":"` + e + `","at":1790000000}`
	}},
	{"a command that is no atom", func(e string) string { return `{"epoch":1,"command":"hist","evidence":"` + e + `","at":1790000000}` }},
	{"no evidence", func(string) string { return `{"epoch":1,"command":"0v5hist","at":1790000000}` }},
	{"evidence that is no sha256", func(e string) string {
		return `{"epoch":1,"command":"0v5hist","evidence":"` + e[:12] + `","at":1790000000}`
	}},
	{"another history's evidence", func(string) string {
		return `{"epoch":1,"command":"0v5hist","evidence":"` + strings.Repeat("e", 64) + `","at":1790000000}`
	}},
	{"no time", func(e string) string { return `{"epoch":1,"command":"0v5hist","evidence":"` + e + `"}` }},
	{"a time of 0", func(e string) string { return `{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":0}` }},
	{"a field this version does not write", func(e string) string {
		return `{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":1790000000,"trusted":true}`
	}},
	{"a string", func(string) string { return `"a transition"` }},
	{"a number", func(string) string { return `1` }},
	{"a list", func(e string) string {
		return `[{"epoch":1,"command":"0v5hist","evidence":"` + e + `","at":1790000000}]`
	}},
}

func TestAMalformedTransitionRecordLiftsNoPause(t *testing.T) {
	for _, c := range malformedTransitions {
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
				t.Fatalf("A MALFORMED TRANSITION RECORD REFUSED THE START (%s): %v", c.name, err)
			}
			defer closeDaemon(d)
			stop := running(t, d)
			h := f.historySince(t, reports)
			f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"}, delivery{"0v6cmd", epochNonce(1, 7)})
			// an authenticated refusal is recorded in the state file: a save
			// the record must survive
			f.ship.enqueue(f.transitionTo(t, "0v5.stale", 1, h, func(sp *cmdSpec) { sp.evidence = "epoch 1 history " + strings.Repeat("f", 64) }))
			a := final(f.answered(t, "0v5.stale"))
			waitUntil(t, 10*time.Second, "polls after the deliveries", func() bool { return p.count() >= 3 })
			stop()
			if n, m := prepared(f.vmFixture, attemptAtom), prepared(f.vmFixture, "0v6cmd"); n != 0 || m != 0 {
				t.Fatalf("A MALFORMED TRANSITION RECORD LIFTED THE PAUSE (%s): prepared %d and %d; launcher ops %v", c.name, n, m, f.host.Ops())
			}
			if !givenBackOnce(p.heard, attemptAtom) || !givenBackOnce(p.heard, "0v6cmd") {
				t.Fatalf("AN ASSIGNMENT TO A RUNNER WITH A MALFORMED TRANSITION RECORD WAS NOT GIVEN BACK ONCE (%s): %v %v", c.name, answered(p.heard, attemptAtom), answered(p.heard, "0v6cmd"))
			}
			for _, capacity := range p.since(0) {
				if capacity != "0" {
					t.Fatalf("A RUNNER WITH A MALFORMED TRANSITION RECORD ADVERTISED CAPACITY (%s): %v", c.name, p.since(0))
				}
			}
			if h := f.history(t); h["paused"] != true || h["transition"] != nil || h["epoch"] != float64(0) {
				t.Fatalf("A MALFORMED TRANSITION RECORD WAS REPORTED AS A TRANSITION (%s): %v", c.name, h)
			}
			if a["status"] != "refused" {
				t.Fatalf("fixture: the stale transition is refused, and recorded (%s): %v", c.name, a)
			}
			if got := fileField(t, f.cfg.StateFile, "transition"); !sameJSON(got, []byte(raw)) {
				t.Fatalf("A MALFORMED TRANSITION RECORD WAS NOT KEPT AS THE FILE HELD IT (%s): wrote %s, the file holds %s", c.name, raw, got)
			}
		})
	}
}

func TestTheOwnersTransitionSupersedesAMalformedRecordAndKeepsIt(t *testing.T) {
	f := upgradedRunner(t, 2)
	evidence, _ := f.firstLife(t)["evidence"].(string)
	raw := `{"epoch":0,"command":"0v5hist","evidence":"` + evidence + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, raw)
	reports := f.ship.reported()
	d, p, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A MALFORMED TRANSITION RECORD REFUSED THE START: %v", err)
	}
	stop := running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v9.hist", 1, f.historySince(t, reports), nil))
	if a := final(f.answered(t, "0v9.hist")); a["status"] != "completed" {
		stop()
		closeDaemon(d)
		t.Fatalf("THE OWNER'S TRANSITION WAS NOT CARRIED OUT OVER A MALFORMED RECORD: %v; log:\n%s", a, f.logs.String())
	}
	f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"}, delivery{"0v6cmd", epochNonce(1, 7)})
	stop()
	closeDaemon(d)
	if n := prepared(f.vmFixture, attemptAtom); n != 0 || !givenBackOnce(p.heard, attemptAtom) {
		t.Fatalf("AN AUTHORIZATION FROM BEFORE THE TRANSITION RAN, OR WAS NOT GIVEN BACK ONCE: prepared %d; %v", n, answered(p.heard, attemptAtom))
	}
	if n := prepared(f.vmFixture, "0v6cmd"); n != 1 {
		t.Fatalf("WORK AUTHORIZED AFTER THE TRANSITION DID NOT RUN: prepared %d; log:\n%s", n, f.logs.String())
	}
	tr := transitionRecord(t, f.cfg.StateFile)
	if tr == nil || tr["epoch"] != float64(1) || tr["command"] != "0v9hist" || tr["evidence"] != evidence {
		t.Fatalf("THE OWNER'S TRANSITION IS NOT THE STATE FILE'S RECORD: %v", tr)
	}
	var kept []map[string]json.RawMessage
	_ = json.Unmarshal(fileField(t, f.cfg.StateFile, "superseded_transitions"), &kept)
	if len(kept) != 1 || !sameJSON(kept[0]["record"], []byte(raw)) || !sameJSON(kept[0]["by"], []byte(`"0v9hist"`)) || len(kept[0]["problem"]) < 3 {
		t.Fatalf("THE MALFORMED RECORD WAS NOT KEPT WHEN THE TRANSITION SUPERSEDED IT: %s", fileField(t, f.cfg.StateFile, "superseded_transitions"))
	}
	// reopened: the transition stands, and the superseded record stays kept
	d, p = f.startPolled(t)
	defer closeDaemon(d)
	stop = running(t, d)
	f.deliver(t, d, p, delivery{"0v8cmd", "0v7.older"}, delivery{"0v9cmd", epochNonce(1, 9)})
	stop()
	if n, m := prepared(f.vmFixture, "0v8cmd"), prepared(f.vmFixture, "0v9cmd"); n != 0 || m != 1 {
		t.Fatalf("AFTER A REOPEN, THE TRANSITION OVER A MALFORMED RECORD DID NOT STAND: old prepared %d, fresh %d", n, m)
	}
	if got := fileField(t, f.cfg.StateFile, "superseded_transitions"); !bytes.Contains(got, []byte(`"0v9hist"`)) {
		t.Fatalf("THE SUPERSEDED RECORD WAS NOT KEPT ACROSS A REOPEN: %s", got)
	}
}

func TestAMalformedRecordNeverAnswersTheCommandItNames(t *testing.T) {
	f := upgradedRunner(t, 2)
	evidence, _ := f.firstLife(t)["evidence"].(string)
	raw := `{"epoch":0,"command":"0v5hist","evidence":"` + evidence + `","at":1790000000}`
	writeTransitionRecord(t, f.cfg.StateFile, raw)
	reports := f.ship.reported()
	d, p, err := f.startPolledOrRefused(t)
	if err != nil {
		t.Fatalf("A MALFORMED TRANSITION RECORD REFUSED THE START: %v", err)
	}
	defer closeDaemon(d)
	stop := running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, f.historySince(t, reports), nil))
	a := final(f.answered(t, "0v5.hist"))
	f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"})
	stop()
	tr := transitionRecord(t, f.cfg.StateFile)
	if a["status"] != "completed" || tr == nil || tr["epoch"] != float64(1) || tr["command"] != "0v5hist" {
		t.Fatalf("A MALFORMED RECORD ANSWERED THE COMMAND IT NAMES: answer %v; the state file's record %v", a, tr)
	}
	if n := prepared(f.vmFixture, attemptAtom); n != 0 {
		t.Fatalf("AN AUTHORIZATION FROM BEFORE THE TRANSITION RAN: prepared %d", n)
	}
	var kept []map[string]json.RawMessage
	_ = json.Unmarshal(fileField(t, f.cfg.StateFile, "superseded_transitions"), &kept)
	if len(kept) != 1 || !sameJSON(kept[0]["record"], []byte(raw)) {
		t.Fatalf("THE MALFORMED RECORD WAS NOT KEPT WHEN ITS COMMAND WAS CARRIED OUT: %s", fileField(t, f.cfg.StateFile, "superseded_transitions"))
	}
}
