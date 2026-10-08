package daemon

// Legacy-replay-upgrade ruling 01, Q15 (runner/launcher/INTEGRATION.md
// §11.15): what the correction adds, observed after it — the submitted
// bytes have none of it to run RED against. An attempt old state names is
// still given back after the transition, even delivered with a nonce of
// the new epoch (no honest ship signs one: its old attempts keep their
// epoch); a transitioned runner reports its epoch and its transition; and
// the evidence a transition binds is read, and bound, exactly.

import (
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/state"
)

func TestAnAttemptOldStateNamesIsGivenBackAfterTheTransition(t *testing.T) {
	// a retention of the legacy shape: its handle alone names its attempt
	entry := map[string]any{"handle": "ci-0v4leg", "reason": "teardown failed", "at": time.Now().Add(-48 * time.Hour).Unix()}
	f := newLegacyFixture(t, 3, entry)
	d, p := f.startPolled(t)
	defer closeDaemon(d)
	stop := running(t, d)
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, f.history(t), nil))
	if a := final(f.answered(t, "0v5.hist")); a["status"] != "completed" {
		stop()
		t.Fatalf("fixture: the transition completes: %v; log:\n%s", a, f.logs.String())
	}
	f.deliver(t, d, p, delivery{"0v4leg", epochNonce(1, 3)}, delivery{"0v6cmd", epochNonce(1, 7)})
	stop()
	if n := prepared(f.vmFixture, "0v4leg"); n != 0 {
		t.Fatalf("AN ATTEMPT AN EARLIER STATE FILE NAMES RAN AFTER THE TRANSITION: prepared %d; launcher ops %v", n, f.host.Ops())
	}
	if !givenBackOnce(p.heard, "0v4leg") {
		t.Fatalf("an attempt an earlier state file names was not given back once: %v", answered(p.heard, "0v4leg"))
	}
	if n := prepared(f.vmFixture, "0v6cmd"); n != 1 {
		t.Fatalf("WORK AUTHORIZED AFTER THE TRANSITION DID NOT RUN: prepared %d; log:\n%s", n, f.logs.String())
	}
}

func TestATransitionedRunnerReportsItsEpoch(t *testing.T) {
	f := upgradedRunner(t, 2)
	d, _ := f.startPolled(t)
	defer closeDaemon(d)
	stop := running(t, d)
	before := f.history(t)
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, before, nil))
	if a := final(f.answered(t, "0v5.hist")); a["status"] != "completed" {
		stop()
		t.Fatalf("fixture: the transition completes: %v", a)
	}
	deadline := time.Now().Add(10 * time.Second)
	var after map[string]any
	for {
		if after = f.history(t); after["paused"] == false {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("THE REPORT STILL SHOWS A TRANSITIONED RUNNER WAITING: %v", after)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	tr, _ := after["transition"].(map[string]any)
	if after["epoch"] != float64(1) || tr == nil || tr["epoch"] != float64(1) || !sameAtom(tr["command"], "0v5.hist") {
		t.Fatalf("THE REPORT DOES NOT SHOW THE RUNNER'S TRANSITION: %v", after)
	}
	if after["complete"] != false || after["evidence"] != before["evidence"] || after["selection"] != before["selection"] {
		t.Fatalf("A TRANSITION CHANGED WHAT THE RUNNER'S HISTORY SAYS OF ITSELF: %v, was %v", after, before)
	}
}

func TestTheTransitionEvidenceIsReadExactly(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for text, want := range map[string]uint64{"epoch 1 history " + digest: 1, "epoch 42 history " + digest: 42} {
		if e, d, ok := parseTransitionEvidence(text); !ok || e != want || d != digest {
			t.Fatalf("A TRANSITION'S EVIDENCE WAS NOT READ: %q gave %d %q %v", text, e, d, ok)
		}
	}
	for _, text := range []string{"", "history " + digest, "epoch history " + digest, "epoch x history " + digest, "epoch -1 history " + digest,
		"epoch 1 history", "epoch 1 hist " + digest, "epoch 1 history " + digest + " more", "epoch 18446744073709551616 history " + digest} {
		if _, _, ok := parseTransitionEvidence(text); ok {
			t.Fatalf("A MALFORMED TRANSITION EVIDENCE WAS READ: %q", text)
		}
	}
}

func TestTheHistoryEvidenceBindsTheRunnerAndItsHistory(t *testing.T) {
	base := state.History{Since: 100, Enrolled: false, StateFormat: 1}
	e := historyEvidenceOf("0v1.d", base)
	if len(e) != 64 || e != historyEvidenceOf("0v1.d", base) {
		t.Fatalf("the history evidence is not one sha256 of its inputs: %q", e)
	}
	for name, other := range map[string]string{
		"another runner":         historyEvidenceOf("0v2.d", base),
		"another start":          historyEvidenceOf("0v1.d", state.History{Since: 101, StateFormat: 1}),
		"a complete history":     historyEvidenceOf("0v1.d", state.History{Since: 100, Enrolled: true, StateFormat: 1}),
		"another format":         historyEvidenceOf("0v1.d", state.History{Since: 100, StateFormat: 0}),
		"a history not readable": historyEvidenceOf("0v1.d", state.History{}),
	} {
		if other == e {
			t.Fatalf("THE HISTORY EVIDENCE DOES NOT BIND %s", strings.ToUpper(name))
		}
	}
}
