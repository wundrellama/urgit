package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"urgit/runner/internal/plan"
)

// Delivery 1C: the explicit import posts, beside the lock and the manual
// inventory, every workflow file's compiled `on:` (the trigger catalog the
// promoted baseline's events and schedules are selected from), as the
// pinned compiler reads it at the revision
func TestResolveSendsTheTriggerCatalog(t *testing.T) {
	sh := &fakeShip{}
	d := newTestDaemon(t, &fakeBox{}, sh, 1)
	d.cfg.Resolver = true
	d.checkout = manualCheckout(t)
	a := testResolverAssignment("resolve", "r", "0000000000000000000000000000000000000001")
	d.handle(context.Background(), a)
	if len(sh.locks) != 2 {
		t.Fatalf("the resolver posted %d lock bodies, want the inventory and the lock: %v (abandons %v)", len(sh.locks), sh.locks, sh.abandons)
	}
	var inventory map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sh.locks[0]), &inventory); err != nil {
		t.Fatal(err)
	}
	if _, ok := inventory["triggers"]; ok {
		t.Fatal("the inventory phase carries no catalog; the lock phase does")
	}
	var body struct {
		Phase    string               `json:"phase"`
		Triggers *plan.TriggerCatalog `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(sh.locks[1]), &body); err != nil {
		t.Fatal(err)
	}
	if body.Phase != "lock" || body.Triggers == nil {
		t.Fatalf("the lock phase carries the trigger catalog: %+v", body)
	}
	cat := body.Triggers
	if cat.Version != plan.TriggerCatalogVersion || len(cat.Workflows) != 2 {
		t.Fatalf("catalog: %+v", cat)
	}
	// workflow files in name order, every one listed
	if cat.Workflows[0].Workflow != "fixture-chain.yml" || cat.Workflows[1].Workflow != "manual.yml" {
		t.Fatalf("catalog workflows: %s, %s", cat.Workflows[0].Workflow, cat.Workflows[1].Workflow)
	}
	// manual.yml (plan/testdata) declares a push to master and
	// workflow_dispatch; the push filter is the push's alone
	manual := cat.Workflows[1]
	if manual.Refusal != "" || len(manual.Events) != 2 || manual.Events[0].Event != "push" || manual.Events[1].Event != "workflow_dispatch" {
		t.Fatalf("manual.yml triggers: %+v", manual)
	}
	if b := manual.Events[0].Branches; b == nil || b.Ignore || len(b.Patterns) != 1 || b.Patterns[0].Text != "master" || manual.Events[1].Branches != nil {
		t.Fatalf("manual.yml filters: push %+v, workflow_dispatch %+v", manual.Events[0].Branches, manual.Events[1].Branches)
	}
}
