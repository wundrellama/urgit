package daemon

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"urgit/runner/internal/ship"
)

var concurrencyCapture = flag.String("concurrency-capture-dir", "", "directory for production plan and lock POST captures")

const concurrencyWorkflow = "name: fixture-chain\non: [push, workflow_dispatch]\nconcurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n  cancel-in-progress: ${{ github.ref != 'refs/heads/master' }}\njobs:\n  a:\n    concurrency: forbidden\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"

func concurrencyCheckout(t *testing.T) func(context.Context, *ship.Assignment, string) error {
	t.Helper()
	return func(_ context.Context, _ *ship.Assignment, dir string) error {
		wf := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(wf, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(wf, "fixture-chain.yml"), []byte(concurrencyWorkflow), 0600)
	}
}

func requireConcurrencyWire(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var c map[string]json.RawMessage
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"group", "cancelInProgress", "refusal"} {
		if _, ok := c[key]; !ok {
			t.Fatalf("missing %s on actual posted wire: %s", key, raw)
		}
	}
	var g struct {
		Text  string            `json:"text"`
		Parts []json.RawMessage `json:"parts"`
	}
	var flag struct {
		Text string          `json:"text"`
		Expr json.RawMessage `json:"expr"`
	}
	if err := json.Unmarshal(c["group"], &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(c["cancelInProgress"], &flag); err != nil {
		t.Fatal(err)
	}
	if g.Text != "${{ github.workflow }}-${{ github.ref }}" || len(g.Parts) != 3 || len(flag.Expr) == 0 || flag.Text != "${{ github.ref != 'refs/heads/master' }}" {
		t.Fatalf("empty/changed actual wire: %s", raw)
	}
}

func TestConcurrencyProductionPlanAndLockCapture(t *testing.T) {
	// fakeShip captures the REAL client POST; fakeBox supplies only act -l's
	// listing. No runtime, listener or network is started.
	sh := &fakeShip{}
	box := &fakeBox{stream: "Stage  Job ID  Job name  Workflow name  Workflow file      Events\n0      a       a         fixture-chain  fixture-chain.yml  push,workflow_dispatch\n"}
	d := newTestDaemon(t, box, sh, 1)
	d.checkout = concurrencyCheckout(t)
	manifest := jobManifest
	manifest.Workflow, manifest.Job, manifest.Mode = "", "", "plan"
	d.handle(context.Background(), &ship.Assignment{ID: "0v2", Attempt: "0v2.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/heads/master", OID: "0000000000000000000000000000000000000001", Kind: "plan", DeadlineSeconds: 300, Manifest: &manifest})
	if len(sh.plans) != 1 {
		t.Fatalf("plans %v abandon %v", sh.plans, sh.abandons)
	}
	var p struct {
		Concurrency []struct {
			Workflow    string          `json:"workflow"`
			Name        string          `json:"name"`
			Concurrency json.RawMessage `json:"concurrency"`
		} `json:"concurrency"`
		Jobs []map[string]json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(sh.plans[0]), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Concurrency) != 1 || p.Concurrency[0].Workflow != "fixture-chain.yml" || p.Concurrency[0].Name != "fixture-chain" {
		t.Fatalf("workflow envelope lost: %s", sh.plans[0])
	}
	requireConcurrencyWire(t, p.Concurrency[0].Concurrency)
	if len(p.Jobs) != 1 || string(p.Jobs[0]["concurrency"]) != "true" {
		t.Fatalf("job-level refusal flag lost: %s", sh.plans[0])
	}
	planBody := sh.plans[0]
	// Resolver POST is a distinct path; prove both catalogs carry the fields.
	sh = &fakeShip{}
	d = newTestDaemon(t, &fakeBox{}, sh, 1)
	d.cfg.Resolver = true
	d.checkout = concurrencyCheckout(t)
	d.handle(context.Background(), testResolverAssignment("resolve", "r", "0000000000000000000000000000000000000001"))
	if len(sh.locks) != 2 {
		t.Fatalf("lock posts %v abandon %v", sh.locks, sh.abandons)
	}
	var l struct {
		Triggers struct {
			Workflows []map[string]json.RawMessage `json:"workflows"`
		} `json:"triggers"`
		Dispatch []map[string]json.RawMessage `json:"dispatch"`
	}
	if err := json.Unmarshal([]byte(sh.locks[1]), &l); err != nil {
		t.Fatal(err)
	}
	if len(l.Triggers.Workflows) != 1 || len(l.Dispatch) != 1 {
		t.Fatalf("catalogs lost: %s", sh.locks[1])
	}
	requireConcurrencyWire(t, l.Triggers.Workflows[0]["concurrency"])
	requireConcurrencyWire(t, l.Dispatch[0]["concurrency"])
	if len(l.Dispatch[0]["refusal"]) < 3 {
		t.Fatal("job-level refusal lost from inventory")
	}
	// Optional packet capture feeds the parent native decoder acceptance.
	if dir := *concurrencyCapture; dir != "" {
		for name, body := range map[string]string{"plan.json": planBody, "lock.json": sh.locks[1]} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
