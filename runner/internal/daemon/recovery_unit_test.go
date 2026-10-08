package daemon

// The legacy release's pieces (legacy-recovery UI ruling 01; runner/
// launcher/INTEGRATION.md §11.12): the judgement names every condition and
// binds the evidence digest to everything it stands on; a change to the
// entry while the launcher is asked refuses the release; a command already
// in progress is not carried out twice; a decision stands for the daemon's
// life; the report says how every slot returns.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

func legacyQ() state.Quarantine {
	return state.Quarantine{Handle: "ci-0v4.att", Reason: "lost", At: 1700000000, Backend: "microvm", Attempt: "0v4.att", Rev: 1, Legacy: &state.Legacy{Format: 0, Found: 1790000000}}
}

func TestTheLegacyJudgementNamesEveryCondition(t *testing.T) {
	inv := sandbox.Inventory{Socket: "l.sock", Launcher: "test", Protocol: launcher.WireProtocol, Records: []launcher.Record{{ID: "t-x", Attempt: "0v9.other", State: launcher.StateRunning}}}
	names := func(j legacyJudgement) map[string]bool {
		out := map[string]bool{}
		for _, c := range j.conditions {
			out[c.Name] = c.Met
		}
		return out
	}
	base := judgeLegacy(legacyQ(), "microvm", inv, nil, false)
	if !base.eligible || len(base.evidence) != 64 || len(base.conditions) != 6 {
		t.Fatalf("a legacy retention on a clean inventory: %+v", base)
	}
	for name, met := range names(base) {
		if !met {
			t.Fatalf("condition %s unmet on a clean inventory", name)
		}
	}
	held := inv
	held.Records = append(held.Records, launcher.Record{ID: "t-y", Attempt: "0v4.att", State: launcher.StateQuarantined})
	notMarked := legacyQ()
	notMarked.Legacy = nil
	for _, c := range []struct {
		name string
		j    legacyJudgement
		cond string
	}{
		{"unmarked", judgeLegacy(notMarked, "microvm", inv, nil, false), "provenance"},
		{"a docker runner", judgeLegacy(legacyQ(), "docker-rootless", inv, nil, false), "backend"},
		{"another protocol", judgeLegacy(legacyQ(), "microvm", sandbox.Inventory{Socket: "l.sock", Protocol: 3}, nil, false), "protocol"},
		{"no inventory", judgeLegacy(legacyQ(), "microvm", sandbox.Inventory{}, errors.New("not authoritative"), false), "inventory"},
		{"a record of the attempt", judgeLegacy(legacyQ(), "microvm", held, nil, false), "attempt"},
		{"its attempt runs here", judgeLegacy(legacyQ(), "microvm", inv, nil, true), "idle"},
	} {
		if c.j.eligible || names(c.j)[c.cond] {
			t.Fatalf("%s: A LEGACY RELEASE WAS JUDGED PROVEN WITH %q UNMET: %+v", c.name, c.cond, c.j.conditions)
		}
	}
	if j := judgeLegacy(legacyQ(), "microvm", sandbox.Inventory{}, errors.New("x"), false); j.evidence != "" {
		t.Fatalf("an evidence digest without an inventory: %s", j.evidence)
	}
	// the digest binds everything the release stands on
	moved := legacyQ()
	moved.Rev = 2
	refound := legacyQ()
	refound.Legacy = &state.Legacy{Format: 0, Found: 1790000001}
	other := inv
	other.Socket = "other.sock"
	for name, j := range map[string]legacyJudgement{
		"the revision":  judgeLegacy(moved, "microvm", inv, nil, false),
		"the mark":      judgeLegacy(refound, "microvm", inv, nil, false),
		"the launcher":  judgeLegacy(legacyQ(), "microvm", other, nil, false),
		"what it holds": judgeLegacy(legacyQ(), "microvm", held, nil, false),
	} {
		if j.evidence == base.evidence {
			t.Fatalf("THE EVIDENCE DIGEST DOES NOT BIND %s", strings.ToUpper(name))
		}
	}
	// and nothing else: an unrelated record, or a claim, is no new evidence
	more := inv
	more.Records = append(more.Records, launcher.Record{ID: "t-z", Attempt: "0v8.other", State: launcher.StateRunning})
	if judgeLegacy(legacyQ(), "microvm", more, nil, false).evidence != base.evidence {
		t.Fatalf("an unrelated record changed the evidence digest")
	}
}

// hookedBox is the real Microvm, with hook run after each inventory is
// answered and before the daemon looks at the entry again.
type hookedBox struct {
	*sandbox.Microvm
	hook func()
}

func (h hookedBox) Inventory(ctx context.Context) (sandbox.Inventory, error) {
	inv, err := h.Microvm.Inventory(ctx)
	if h.hook != nil {
		h.hook()
	}
	return inv, err
}

// toCommand is a command map as the daemon decodes it.
func toCommand(t *testing.T, m map[string]any) *ship.RecoveryCommand {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var cmd ship.RecoveryCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		t.Fatal(err)
	}
	return &cmd
}

// reportedEntry is the daemon's report of its only retention.
func reportedEntry(t *testing.T, d *Daemon) ship.RetentionEntry {
	t.Helper()
	r := d.retentionReport(context.Background())
	if len(r.Retentions) != 1 {
		t.Fatalf("fixture: one retention reported, got %+v", r.Retentions)
	}
	return r.Retentions[0]
}

// While the launcher is asked, the entry changes — a teardown of its
// attempt failing meanwhile names its launcher identity: the release is
// refused on the entry as it is now, and the retention stays, with its
// identity, charged.
func TestAChangeWhileTheEvidenceIsTakenRefusesTheRelease(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	defer closeDaemon(d)
	e := reportedEntry(t, d)
	if !e.Eligible {
		t.Fatalf("fixture: releasable: %+v", e)
	}
	d.box = hookedBox{Microvm: d.box.(*sandbox.Microvm), hook: func() {
		d.retain(sandbox.Handle{ID: "ci-" + legacyAttempt, Attempt: legacyAttempt, VM: "t-late", Incarnation: strings.Repeat("a", 32), CID: 9, Created: time.Now().Unix()}, "teardown failed meanwhile")
	}}
	a := d.recover(context.Background(), toCommand(t, f.command(t, cmdSpec{selection: e.Selection, revision: e.Revision, evidence: e.Evidence})))
	kept := retentions(t, f.cfg.StateFile)
	if a.Status != ship.AnswerRefused || len(released(t, f.cfg.StateFile)) != 0 || len(kept) != 1 || kept[0]["vm"] != "t-late" || d.remainingCapacity() != 1 {
		t.Fatalf("A RETENTION CHANGED WHILE ITS EVIDENCE WAS TAKEN WAS RELEASED: %+v; kept %v, released %v, advertised %d", a, kept, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
	}
}

// A second delivery of a command while it is carried out is ignored: one
// release, one answer per decision.
func TestADeliveryOfACommandInProgressIsIgnored(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	defer closeDaemon(d)
	e := reportedEntry(t, d)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d.box = hookedBox{Microvm: d.box.(*sandbox.Microvm), hook: func() {
		once.Do(func() { close(entered); <-proceed })
	}}
	cmd := toCommand(t, f.command(t, cmdSpec{selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
	d.client.HTTP = f.ship.client()
	var wg sync.WaitGroup
	d.startRecovery(context.Background(), cmd, &wg)
	<-entered
	d.startRecovery(context.Background(), cmd, &wg) // delivered again meanwhile
	close(proceed)
	wg.Wait()
	answers := f.ship.answersFor("0v5.cmd")
	if len(answers) != 1 || answers[0]["status"] != "completed" || len(released(t, f.cfg.StateFile)) != 1 {
		t.Fatalf("A COMMAND IN PROGRESS WAS CARRIED OUT AGAIN: answers %v, released %v", answers, handlesOf(released(t, f.cfg.StateFile)))
	}
	if !strings.Contains(f.logs.String(), "is being carried out already") {
		t.Fatalf("the second delivery was not said to be ignored:\n%s", f.logs.String())
	}
}

// A refusal of an authenticated command stands for the daemon's life: the
// same command delivered again once its conditions would hold is answered
// the same, and releases nothing — the operator confirms again.
func TestADecisionStandsForTheDaemonsLife(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	defer closeDaemon(d)
	e := reportedEntry(t, d)
	cmd := toCommand(t, f.command(t, cmdSpec{selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
	away := f.srv.Socket + ".away"
	if err := os.Rename(f.srv.Socket, away); err != nil {
		t.Fatal(err)
	}
	first := d.recover(context.Background(), cmd)
	if err := os.Rename(away, f.srv.Socket); err != nil {
		t.Fatal(err)
	}
	second := d.recover(context.Background(), cmd)
	if first.Status != ship.AnswerRefused || second.Status != first.Status || second.Detail != first.Detail || len(released(t, f.cfg.StateFile)) != 0 {
		t.Fatalf("A DECIDED COMMAND WAS DECIDED AGAIN: %+v then %+v; released %v", first, second, handlesOf(released(t, f.cfg.StateFile)))
	}
	// a fresh command, confirmed again, is carried out
	again := toCommand(t, f.command(t, cmdSpec{id: "0v7.cmd", selection: e.Selection, revision: e.Revision, evidence: e.Evidence}))
	if a := d.recover(context.Background(), again); a.Status != ship.AnswerCompleted {
		t.Fatalf("a fresh command on the same evidence: %+v", a)
	}
}

// The report lists every retention with how its slot returns; only a legacy
// retention is judged, and only it carries an evidence digest.
func TestTheReportSaysHowEachSlotReturns(t *testing.T) {
	f := newLegacyFixture(t, 6, preRuling(legacyAttempt),
		map[string]any{"handle": "ci-0v2.adm", "reason": "sent", "at": 2, "backend": "microvm", "attempt": "0v2.adm", "request": strings.Repeat("c", 32)},
		map[string]any{"handle": "ci-0v3.vm", "reason": "teardown failed", "at": 3, "backend": "microvm", "attempt": "0v3.vm", "vm": "t-3", "incarnation": strings.Repeat("b", 32), "cid": 3, "created": 3},
		map[string]any{"handle": "ci-0v5.dock", "reason": "teardown failed", "at": 5, "backend": "docker-rootless", "attempt": "0v5.dock", "network": "ci-0v5.dock", "volume": "ci-0v5.dock-work", "container": "ci-0v5.dock"},
		map[string]any{"handle": "ci-0v6.old", "reason": "teardown failed", "at": 6})
	d := f.start(t)
	defer closeDaemon(d)
	r := d.retentionReport(context.Background())
	want := map[string][2]string{
		"ci-" + legacyAttempt: {"legacy", "urgit"}, "ci-0v2.adm": {"admission", "settlement"}, "ci-0v3.vm": {"vm", "cli"},
		"ci-0v5.dock": {"docker", "cli"}, "ci-0v6.old": {"unknown-backend", "none"},
	}
	if len(r.Retentions) != len(want) || r.Capacity.Configured != 6 || r.Capacity.Withheld != 5 || r.Capacity.Advertised != 1 {
		t.Fatalf("the report's retentions and capacity: %+v", r)
	}
	for _, e := range r.Retentions {
		w := want[e.Handle]
		if e.Kind != w[0] || e.Release != w[1] || e.Explanation == "" {
			t.Fatalf("%s: reported %s/%s, want %s/%s", e.Handle, e.Kind, e.Release, w[0], w[1])
		}
		legacy := e.Kind == "legacy"
		if (e.Evidence != "") != legacy || (len(e.Conditions) > 0) != legacy || e.Eligible != legacy {
			t.Fatalf("ONLY A LEGACY RETENTION IS JUDGED AND OFFERED: %+v", e)
		}
	}
}

// client is an HTTP client serving the ship in process.
func (s *legacyShip) client() *http.Client {
	return &http.Client{Transport: lossy{inproc: inproc{"ship.test": s}, ship: s}}
}
