package daemon

// Legacy-replay-upgrade ruling 01, Q15 (runner/launcher/INTEGRATION.md
// §11.15; independent review 11): a runner whose execution history is
// incomplete — its ledger's history began with a state file kept before it,
// or cannot be read — runs nothing until its transition is confirmed. It
// polls advertising no capacity, reports its history with its evidence,
// stays recoverable, and gives back every assignment it receives. The
// owner's transition is a command the ship signs for this daemon, bound to
// the history as inspected and naming an authorization epoch E: the runner
// records it durably, answers, and from then on refuses every assignment
// whose signed nonce is below E·2^128 — every assignment its ship signed
// before — while work signed after it runs. A runner enrolled with this
// version never waits.
//
// Through the real Run loop, verification, the recovery channel and the
// real Microvm backend and launcher over private files, with the in-process
// ship of legacy_test.go; its commands are signed here with the key the
// state file pins.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// polled is a daemon's ship with every request heard, and the capacity each
// poll for work advertised.
type polled struct {
	heard      *heardPaths
	mu         sync.Mutex
	capacities []string
}

func (p *polled) since(n int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n > len(p.capacities) {
		return nil
	}
	return append([]string(nil), p.capacities[n:]...)
}

func (p *polled) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.capacities)
}

// startPolled is f's daemon with its polls watched.
func (f *legacyFixture) startPolled(t *testing.T) (*Daemon, *polled) {
	t.Helper()
	d := f.start(t)
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
	return d, p
}

// upgradedRunner is a runner upgraded from a version without the ledger:
// its state file (this version's format), and no ledger yet.
func upgradedRunner(t *testing.T, capacity int, entries ...map[string]any) *legacyFixture {
	t.Helper()
	return newStateFixture(t, capacity, 1, entries...)
}

// enrolledRunner is a runner enrolled with this version: its ledger's
// history complete, and its state file naming it.
func enrolledRunner(t *testing.T, capacity int) *legacyFixture {
	t.Helper()
	f := newStateFixture(t, capacity, 1)
	if _, err := state.LedgerFor(f.cfg.StateFile).Open(state.History{Since: time.Now().Unix(), Enrolled: true, StateFormat: state.CurrentFormat}, false); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	st.Ledger = true
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	return f
}

// epochNonce is a nonce of authorization epoch e: e above bit 128, r below.
func epochNonce(e, r int64) string {
	n := new(big.Int).Lsh(big.NewInt(e), 128)
	return sig.FormatUV(n.Add(n, big.NewInt(r)))
}

// history is the latest report's execution history, waited for.
func (f *legacyFixture) history(t *testing.T) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.ship.mu.Lock()
		var h map[string]any
		if n := len(f.ship.reports); n > 0 {
			h, _ = f.ship.reports[n-1]["history"].(map[string]any)
		}
		f.ship.mu.Unlock()
		if h != nil {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("THE RUNNER'S REPORT DOES NOT SHOW ITS HISTORY: %d report(s); log:\n%s", f.ship.reported(), f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// otherKey is a signing key that is not the one the state file pins.
func otherKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

// transitionTo is the owner's transition to epoch as the ship signs it,
// bound to history as the report showed it.
func (f *legacyFixture) transitionTo(t *testing.T, id string, epoch int, history map[string]any, change func(*cmdSpec)) map[string]any {
	t.Helper()
	sel, _ := history["selection"].(string)
	evidence, _ := history["evidence"].(string)
	sp := cmdSpec{id: id, operation: "confirm-history", selection: sel, revision: revisionOf(history), evidence: fmt.Sprintf("epoch %d history %s", epoch, evidence)}
	if change != nil {
		change(&sp)
	}
	return f.command(t, sp)
}

// transitionRecord is the state file's record of the runner's transition.
func transitionRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	tr, _ := file["transition"].(map[string]any)
	return tr
}

// deliver hands each assignment to Run and waits until each is answered or
// has run, and no claim is left.
func (f *legacyFixture) deliver(t *testing.T, d *Daemon, p *polled, list ...struct{ attempt, nonce string }) {
	t.Helper()
	for _, a := range list {
		f.offerAssignment(f.signedWith(t, a.attempt, a.nonce))
	}
	waitUntil(t, 20*time.Second, "every delivery's answer", func() bool {
		for _, a := range list {
			if len(answered(p.heard, a.attempt)) == 0 {
				return false
			}
		}
		return claims(d) == 0
	})
}

type delivery = struct{ attempt, nonce string }

// A runner whose history is incomplete runs nothing: upgraded from a
// version without the ledger, its HISTORY unreadable, or missing from the
// ledger it kept. Every assignment — its ship's earlier one, or one of a
// later epoch — is given back once verified and never prepared; its polls
// advertise no capacity; and its report shows its history waiting.
func TestARunnerWithAnIncompleteHistoryRunsNothing(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, f *legacyFixture)
	}{
		{"upgraded from a version without the ledger", func(*testing.T, *legacyFixture) {}},
		{"its HISTORY unreadable", func(t *testing.T, f *legacyFixture) {
			dir := filepath.Join(filepath.Dir(f.cfg.StateFile), "executions")
			for _, sub := range []string{"taken", "finished"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "HISTORY"), []byte("\x00{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"its HISTORY missing from the ledger it kept", func(t *testing.T, f *legacyFixture) {
			dir := filepath.Join(filepath.Dir(f.cfg.StateFile), "executions")
			for _, sub := range []string{"taken", "finished"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			st, err := state.Load(f.cfg.StateFile)
			if err != nil {
				t.Fatal(err)
			}
			st.Ledger = true
			if err := state.Save(f.cfg.StateFile, st); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := upgradedRunner(t, 2)
			c.setup(t, f)
			d, p := f.startPolled(t)
			defer closeDaemon(d)
			stop := running(t, d)
			f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"}, delivery{"0v6cmd", epochNonce(1, 7)})
			waitUntil(t, 10*time.Second, "polls after the deliveries", func() bool { return p.count() >= 3 })
			stop()
			if n, m := prepared(f.vmFixture, attemptAtom), prepared(f.vmFixture, "0v6cmd"); n != 0 || m != 0 {
				t.Fatalf("A RUNNER WITH AN INCOMPLETE HISTORY RAN AN ASSIGNMENT (%s): prepared %d and %d; launcher ops %v", c.name, n, m, f.host.Ops())
			}
			if !givenBackOnce(p.heard, attemptAtom) || !givenBackOnce(p.heard, "0v6cmd") {
				t.Fatalf("AN ASSIGNMENT TO A RUNNER WITH AN INCOMPLETE HISTORY WAS NOT GIVEN BACK ONCE (%s): %v %v", c.name, answered(p.heard, attemptAtom), answered(p.heard, "0v6cmd"))
			}
			for _, capacity := range p.since(0) {
				if capacity != "0" {
					t.Fatalf("A RUNNER WITH AN INCOMPLETE HISTORY ADVERTISED CAPACITY (%s): %v", c.name, p.since(0))
				}
			}
			h := f.history(t)
			if h["paused"] != true || h["complete"] != false {
				t.Fatalf("THE RUNNER'S REPORT DOES NOT SHOW ITS HISTORY WAITING (%s): %v", c.name, h)
			}
		})
	}
}

// A runner enrolled with this version — its ledger's history complete —
// runs its ship's work without any transition, advertises its capacity,
// and reports its history complete.
func TestARunnerEnrolledWithThisVersionRunsWithoutATransition(t *testing.T) {
	f := enrolledRunner(t, 2)
	d, p := f.startPolled(t)
	defer closeDaemon(d)
	stop := running(t, d)
	f.deliver(t, d, p, delivery{attemptAtom, "0v7.old"})
	stop()
	if n := prepared(f.vmFixture, attemptAtom); n != 1 {
		t.Fatalf("A RUNNER ENROLLED WITH THIS VERSION WAS KEPT FROM RUNNING: prepared %d; log:\n%s", n, f.logs.String())
	}
	if caps := p.since(0); len(caps) == 0 || caps[0] == "0" {
		t.Fatalf("A RUNNER ENROLLED WITH THIS VERSION ADVERTISED NO CAPACITY: %v", caps)
	}
	h := f.history(t)
	if h["complete"] != true || h["paused"] != false {
		t.Fatalf("THE REPORT DOES NOT SHOW A RUNNER ENROLLED WITH THIS VERSION COMPLETE: %v", h)
	}
}

// The owner's transition, confirmed: the runner records it durably and
// answers completed. From then on — in its life, and after a restart —
// every assignment its ship signed before (a nonce below E·2^128, the
// largest such one included, respelled too) is given back and never
// prepared, and work of its epoch runs; its polls advertise its capacity
// again.
func TestATransitionFencesEveryAuthorizationBeforeIt(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "in its life"
		if restart {
			name = "after a restart"
		}
		t.Run(name, func(t *testing.T) {
			f := upgradedRunner(t, 2)
			d, p := f.startPolled(t)
			stop := running(t, d)
			h := f.history(t)
			f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, h, nil))
			a := final(f.answered(t, "0v5.hist"))
			if a["status"] != "completed" {
				stop()
				closeDaemon(d)
				t.Fatalf("A CONFIRMED TRANSITION WAS NOT COMPLETED: %v; log:\n%s", a, f.logs.String())
			}
			if tr := transitionRecord(t, f.cfg.StateFile); tr == nil || tr["epoch"] != float64(1) {
				stop()
				closeDaemon(d)
				t.Fatalf("THE TRANSITION IS NOT IN THE STATE FILE: %v", tr)
			}
			if restart {
				stop()
				closeDaemon(d)
				d, p = f.startPolled(t)
				stop = running(t, d)
			}
			defer closeDaemon(d)
			floor := new(big.Int).Lsh(big.NewInt(1), 128)
			below := sig.FormatUV(new(big.Int).Sub(floor, big.NewInt(1)))
			old := f.signedWith(t, attemptAtom, "0v7.old")
			f.offerAssignment(respelledAssignment(old, "0v5.cmd"))
			f.deliver(t, d, p, delivery{"0v4cmd", below}, delivery{"0v6cmd", epochNonce(1, 7)}, delivery{"0v8cmd", sig.FormatUV(floor)})
			waitUntil(t, 20*time.Second, "the respelled delivery's answer", func() bool { return len(answered(p.heard, attemptAtom)) > 0 && claims(d) == 0 })
			polls := p.count()
			waitUntil(t, 10*time.Second, "polls after the deliveries", func() bool { return p.count() >= polls+2 })
			stop()
			for _, a := range []string{attemptAtom, "0v4cmd"} {
				if n := prepared(f.vmFixture, a); n != 0 {
					t.Fatalf("AN AUTHORIZATION FROM BEFORE THE TRANSITION RAN (%s, %s): prepared %d; launcher ops %v", name, a, n, f.host.Ops())
				}
				if !givenBackOnce(p.heard, a) {
					t.Fatalf("AN AUTHORIZATION FROM BEFORE THE TRANSITION WAS NOT GIVEN BACK ONCE (%s, %s): %v", name, a, answered(p.heard, a))
				}
			}
			for _, a := range []string{"0v6cmd", "0v8cmd"} {
				if n := prepared(f.vmFixture, a); n != 1 {
					t.Fatalf("WORK AUTHORIZED AFTER THE TRANSITION DID NOT RUN (%s, %s): prepared %d; log:\n%s", name, a, n, f.logs.String())
				}
			}
			if caps := p.since(polls); len(caps) == 0 || caps[len(caps)-1] == "0" {
				t.Fatalf("A TRANSITIONED RUNNER ADVERTISED NO CAPACITY (%s): %v", name, caps)
			}
		})
	}
}

// A transition whose save is uncertain: answered uncertain, and the runner
// keeps waiting — no work runs; once saves succeed, the command is refused,
// not applied, and answered so from its record; a new transition, to the
// next epoch, completes, and fences the epoch before it too.
func TestATransitionWhoseSaveIsUncertainLeavesTheRunnerWaiting(t *testing.T) {
	f := upgradedRunner(t, 2)
	d, p := f.startPolled(t)
	defer closeDaemon(d)
	var mu sync.Mutex
	failing := true
	d.saveState = func(path string, st *state.State) error {
		data, _ := json.Marshal(st)
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(string(data), `"transition"`) && failing {
			if err := state.Save(path, st); err != nil {
				return err
			}
			return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
		}
		if failing && strings.Contains(string(data), `"refused"`) {
			return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
		}
		return state.Save(path, st)
	}
	stop := running(t, d)
	h := f.history(t)
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, h, nil))
	first := f.firstAnswer(t, "0v5.hist")
	if first["status"] != "uncertain" {
		stop()
		t.Fatalf("AN UNCERTAIN TRANSITION WAS NOT ANSWERED UNCERTAIN: %v", first)
	}
	f.offerAssignment(f.signedWith(t, "0v6cmd", epochNonce(1, 7)))
	waitUntil(t, 20*time.Second, "the delivery's answer", func() bool { return len(answered(p.heard, "0v6cmd")) > 0 && claims(d) == 0 })
	if n := prepared(f.vmFixture, "0v6cmd"); n != 0 {
		stop()
		t.Fatalf("A RUNNER WHOSE TRANSITION WAS NOT DURABLE RAN AN ASSIGNMENT: prepared %d", n)
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	a := final(f.answered(t, "0v5.hist"))
	if a["status"] != "refused" || transitionRecord(t, f.cfg.StateFile) != nil {
		stop()
		t.Fatalf("AN UNCERTAIN TRANSITION WAS NOT REFUSED, NOT APPLIED, ONCE THE STATE WAS DURABLE: %v, record %v", a, transitionRecord(t, f.cfg.StateFile))
	}
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, h, nil)) // delivered again
	f.ship.enqueue(f.transitionTo(t, "0v6.hist", 2, f.history(t), nil))
	b := final(f.answered(t, "0v6.hist"))
	if again := f.ship.answersFor("0v5.hist"); final(again)["status"] != "refused" {
		stop()
		t.Fatalf("A REFUSED TRANSITION DELIVERED AGAIN WAS NOT ANSWERED FROM ITS RECORD: %v", again)
	}
	if b["status"] != "completed" {
		stop()
		t.Fatalf("A NEW TRANSITION AFTER AN UNCERTAIN ONE WAS NOT COMPLETED: %v; log:\n%s", b, f.logs.String())
	}
	f.deliver(t, d, p, delivery{"0v7cmd", epochNonce(1, 9)}, delivery{"0v8cmd", epochNonce(2, 9)})
	stop()
	if n := prepared(f.vmFixture, "0v7cmd"); n != 0 {
		t.Fatalf("AN AUTHORIZATION OF AN EARLIER EPOCH RAN AFTER THE TRANSITION: prepared %d", n)
	}
	if n := prepared(f.vmFixture, "0v8cmd"); n != 1 {
		t.Fatalf("WORK OF THE TRANSITION'S EPOCH DID NOT RUN: prepared %d; log:\n%s", n, f.logs.String())
	}
}

// A transition that does not hold now is refused, recorded, and the runner
// keeps waiting: bound to other evidence, to another history, expired, of
// epoch 0, or with evidence that names no epoch. A completed transition
// delivered again is answered from its record, never applied again; and a
// transition of a runner no longer waiting is refused.
func TestAStaleOrReplayedTransitionIsRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*cmdSpec)
	}{
		{"bound to other evidence", func(sp *cmdSpec) { sp.evidence = "epoch 1 history " + strings.Repeat("e", 64) }},
		{"bound to another history", func(sp *cmdSpec) { sp.selection, sp.revision = "history/1", 1 }},
		{"expired", func(sp *cmdSpec) { sp.expiry = time.Now().Add(-time.Minute).Unix() }},
		{"of epoch 0", func(sp *cmdSpec) { sp.evidence = strings.Replace(sp.evidence, "epoch 1 ", "epoch 0 ", 1) }},
		{"naming no epoch", func(sp *cmdSpec) { sp.evidence = strings.Replace(sp.evidence, "epoch 1 ", "", 1) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := upgradedRunner(t, 2)
			d, p := f.startPolled(t)
			defer closeDaemon(d)
			stop := running(t, d)
			f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, f.history(t), c.change))
			a := final(f.answered(t, "0v5.hist"))
			f.deliver(t, d, p, delivery{"0v6cmd", epochNonce(1, 7)})
			stop()
			if a["status"] != "refused" || transitionRecord(t, f.cfg.StateFile) != nil {
				t.Fatalf("A TRANSITION THAT DOES NOT HOLD WAS NOT REFUSED (%s): %v, record %v", c.name, a, transitionRecord(t, f.cfg.StateFile))
			}
			if n := prepared(f.vmFixture, "0v6cmd"); n != 0 {
				t.Fatalf("A RUNNER RAN AFTER A REFUSED TRANSITION (%s): prepared %d", c.name, n)
			}
		})
	}
	t.Run("delivered again after it completed", func(t *testing.T) {
		f := upgradedRunner(t, 2)
		d, _ := f.startPolled(t)
		defer closeDaemon(d)
		stop := running(t, d)
		h := f.history(t)
		cmd := f.transitionTo(t, "0v5.hist", 1, h, nil)
		f.ship.enqueue(cmd)
		if a := final(f.answered(t, "0v5.hist")); a["status"] != "completed" {
			stop()
			t.Fatalf("A CONFIRMED TRANSITION WAS NOT COMPLETED: %v", a)
		}
		recorded := transitionRecord(t, f.cfg.StateFile)
		f.ship.enqueue(cmd)
		f.ship.enqueue(f.transitionTo(t, "0v6.hist", 2, h, nil)) // a later one, bound to the history as it was
		b := final(f.answered(t, "0v6.hist"))
		stop()
		answers := f.ship.answersFor("0v5.hist")
		if len(answers) < 2 || final(answers[1:])["status"] != "completed" {
			t.Fatalf("A COMPLETED TRANSITION DELIVERED AGAIN WAS NOT ANSWERED FROM ITS RECORD: %v", answers)
		}
		if b["status"] != "refused" {
			t.Fatalf("A TRANSITION OF A RUNNER NO LONGER WAITING WAS NOT REFUSED: %v", b)
		}
		if now := transitionRecord(t, f.cfg.StateFile); fmt.Sprint(now) != fmt.Sprint(recorded) {
			t.Fatalf("A TRANSITION WAS APPLIED AGAIN: %v, was %v", now, recorded)
		}
	})
}

// A transition this runner cannot authenticate decides nothing and records
// nothing: signed by another key, for another runner, its epoch changed
// after it was signed, or unsigned. The runner keeps waiting.
func TestAnUnauthenticatedTransitionDecidesNothing(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*cmdSpec)
	}{
		{"signed by another key", func(sp *cmdSpec) { sp.key = otherKey(t) }},
		{"for another runner", func(sp *cmdSpec) { sp.recipient = "0v9.other" }},
		{"its epoch changed after it was signed", func(sp *cmdSpec) {
			sp.tamper = func(cmd map[string]any) {
				cmd["evidence"] = strings.Replace(fmt.Sprint(cmd["evidence"]), "epoch 1 ", "epoch 9 ", 1)
			}
		}},
		{"unsigned", func(sp *cmdSpec) { sp.unsigned = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := upgradedRunner(t, 2)
			d, p := f.startPolled(t)
			defer closeDaemon(d)
			stop := running(t, d)
			f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, f.history(t), c.change))
			first := f.firstAnswer(t, "0v5.hist")
			f.deliver(t, d, p, delivery{"0v6cmd", epochNonce(1, 7)})
			stop()
			if first["status"] == "completed" || transitionRecord(t, f.cfg.StateFile) != nil {
				t.Fatalf("AN UNAUTHENTICATED TRANSITION WAS CARRIED OUT (%s): %v, record %v", c.name, first, transitionRecord(t, f.cfg.StateFile))
			}
			if n := prepared(f.vmFixture, "0v6cmd"); n != 0 {
				t.Fatalf("A RUNNER RAN AFTER AN UNAUTHENTICATED TRANSITION (%s): prepared %d", c.name, n)
			}
		})
	}
}

// A transition enables execution and releases nothing: a runner waiting
// with a retention advertises no capacity; transitioned, it advertises its
// slots less the one the retention withholds, which it still keeps and
// reports.
func TestATransitionReleasesNoHeldCapacity(t *testing.T) {
	f := upgradedRunner(t, 2, preRuling(legacyAttempt))
	d, p := f.startPolled(t)
	defer closeDaemon(d)
	stop := running(t, d)
	f.inspected(t, "ci-"+legacyAttempt)
	f.ship.enqueue(f.transitionTo(t, "0v5.hist", 1, f.history(t), nil))
	if a := final(f.answered(t, "0v5.hist")); a["status"] != "completed" {
		stop()
		t.Fatalf("A CONFIRMED TRANSITION WAS NOT COMPLETED: %v", a)
	}
	polls := p.count()
	waitUntil(t, 10*time.Second, "polls after the transition", func() bool { return p.count() >= polls+2 })
	stop()
	caps := p.since(polls)
	if len(caps) == 0 || caps[len(caps)-1] != "1" {
		t.Fatalf("A TRANSITION RELEASED HELD CAPACITY, OR ENABLED NONE: advertised %v after it", caps)
	}
	if kept := retentions(t, f.cfg.StateFile); len(kept) != 1 || kept[0]["handle"] != "ci-"+legacyAttempt {
		t.Fatalf("A TRANSITION RELEASED A RETENTION: %v", kept)
	}
	if e, _ := f.ship.entry("ci-" + legacyAttempt); e == nil {
		t.Fatalf("THE RETENTION IS NO LONGER REPORTED AFTER THE TRANSITION")
	}
}

// A runner waiting for its transition stays inspectable and recoverable:
// it reports its retentions and its history, and carries out the owner's
// release of a legacy retention; the release returns its slot and enables
// no execution — the runner still advertises no capacity.
func TestAWaitingRunnerStaysInspectableAndRecoverable(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d, p := f.startPolled(t)
	defer closeDaemon(d)
	stop := running(t, d)
	e := f.inspected(t, "ci-"+legacyAttempt)
	if h := f.history(t); h["paused"] != true {
		stop()
		t.Fatalf("THE RUNNER'S REPORT DOES NOT SHOW ITS HISTORY WAITING: %v", h)
	}
	sel, _ := e["selection"].(string)
	evidence, _ := e["evidence"].(string)
	f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
	a := final(f.answered(t, "0v5.cmd"))
	polls := p.count()
	waitUntil(t, 10*time.Second, "polls after the release", func() bool { return p.count() >= polls+2 })
	stop()
	if a["status"] != "completed" || len(released(t, f.cfg.StateFile)) != 1 {
		t.Fatalf("A WAITING RUNNER DID NOT CARRY OUT A RECOVERY COMMAND: %v; log:\n%s", a, f.logs.String())
	}
	for _, capacity := range p.since(polls) {
		if capacity != "0" {
			t.Fatalf("A RELEASE ENABLED EXECUTION ON A WAITING RUNNER: advertised %v", p.since(polls))
		}
	}
}
