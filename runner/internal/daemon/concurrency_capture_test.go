package daemon

// CI-CONCURRENCY-1: the new wire fields as the real producers post them.
// The resolver's lock body and the planner's plan body are captured from
// the fake ship's handler (the bytes the daemon actually sent), checked,
// and pinned under testdata/concurrency; desk/gen/ci-concurrency-vector
// embeds those exact bytes (plan vector_pin_test checks the embedding) and
// parses them with the ship's own parsers. `-args -write-capture` rewrites
// the pins after a reviewed change.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"urgit/runner/internal/plan"
	"urgit/runner/internal/ship"
)

var writeCapture = flag.Bool("write-capture", false, "rewrite the pinned concurrency wire captures")

// ERPit's workflow file cut before its env: block: its name, on: and
// concurrency: are byte for byte the file's own, at the same lines, and a
// one-step job replaces its jobs (the resolver would fetch their uses:
// actions; this test makes no network request)
func erpitPrefix(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "plan", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, []byte("\nenv:\n"))
	if at < 0 {
		t.Fatalf("%s has no env: block", file)
	}
	return append(append([]byte{}, data[:at+1]...), []byte("jobs:\n  probe:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo probe\n")...)
}

const laneWorkflow = `name: Lane
on: [push, workflow_dispatch]
concurrency: lane-${{ github.ref_name }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps: [{run: echo build}]
  deploy:
    needs: build
    runs-on: ubuntu-latest
    concurrency: deploy
    steps: [{run: echo deploy}]
`

const plainWorkflow = `name: Plain
on: push
jobs:
  plain:
    runs-on: ubuntu-latest
    steps: [{run: echo plain}]
`

const refusedWorkflow = `name: Refused group
on: push
concurrency:
  group: g
  cancel-in-progress: 'yes'
jobs:
  never:
    runs-on: ubuntu-latest
    steps: [{run: echo never}]
`

func concurrencyFiles(t *testing.T) map[string][]byte {
	return map[string][]byte{
		"fixtures.yml": erpitPrefix(t, "fixtures.yml"),
		"suite.yml":    erpitPrefix(t, "suite.yml"),
		"lane.yml":     []byte(laneWorkflow),
		"plain.yml":    []byte(plainWorkflow),
		"refused.yml":  []byte(refusedWorkflow),
	}
}

func writeWorkflows(files map[string][]byte) func(context.Context, *ship.Assignment, string) error {
	return func(_ context.Context, _ *ship.Assignment, dir string) error {
		wf := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(wf, 0o755); err != nil {
			return err
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(wf, name), data, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
}

// pinCapture compares captured bytes with their pin, or rewrites it
func pinCapture(t *testing.T, path string, got []byte) {
	t.Helper()
	if *writeCapture {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(append([]byte{}, got...), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (-args -write-capture writes it)", path, err)
	}
	if string(bytes.TrimSuffix(want, []byte("\n"))) != string(got) {
		t.Fatalf("%s drifted from the captured wire:\n got %s", path, got)
	}
}

// the resolver posts every catalog workflow's concurrency, never an empty
// or absent field, beside the lock; and the manual inventory refuses a
// workflow whose job declares concurrency
func TestConcurrencyRidesTheRealLockWire(t *testing.T) {
	files := concurrencyFiles(t)
	sh := &fakeShip{}
	d := newTestDaemon(t, &fakeBox{}, sh, 1)
	d.cfg.Resolver = true
	d.checkout = writeWorkflows(files)
	d.handle(context.Background(), testResolverAssignment("resolve", "r", "0000000000000000000000000000000000000001"))
	if len(sh.locks) != 2 {
		t.Fatalf("the resolver posted %d lock bodies: %v (abandons %v)", len(sh.locks), sh.locks, sh.abandons)
	}
	var body struct {
		Phase    string          `json:"phase"`
		Triggers json.RawMessage `json:"triggers"`
		Dispatch json.RawMessage `json:"dispatch"`
	}
	if err := json.Unmarshal([]byte(sh.locks[1]), &body); err != nil {
		t.Fatal(err)
	}
	if body.Phase != "lock" || len(body.Triggers) == 0 {
		t.Fatalf("the lock phase carries the catalog: %s", sh.locks[1])
	}
	var cat struct {
		Workflows []struct {
			Workflow    string         `json:"workflow"`
			Concurrency map[string]any `json:"concurrency"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal(body.Triggers, &cat); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, w := range cat.Workflows {
		c := w.Concurrency
		if c == nil || c["v"] != float64(2) {
			t.Fatalf("%s: the real wire carries no versioned concurrency: %v", w.Workflow, c)
		}
		kinds[w.Workflow], _ = c["kind"].(string)
	}
	want := map[string]string{"fixtures.yml": "group", "lane.yml": "group", "plain.yml": "none", "refused.yml": "refused", "suite.yml": "group"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v", kinds)
	}
	for f, k := range want {
		if kinds[f] != k {
			t.Fatalf("%s: kind %q, want %q (%v)", f, kinds[f], k, kinds)
		}
	}
	// the ERPit groups are compiled, not empty: their parts and the
	// cancel-in-progress expression ride the wire
	for _, w := range cat.Workflows {
		if w.Workflow != "suite.yml" && w.Workflow != "fixtures.yml" {
			continue
		}
		group, _ := w.Concurrency["group"].(map[string]any)
		parts, _ := group["parts"].([]any)
		cancel, _ := w.Concurrency["cancel"].(map[string]any)
		if len(parts) != 5 || cancel["expr"] == nil || cancel["value"] != nil {
			t.Fatalf("%s: group parts %v, cancel %v", w.Workflow, parts, cancel)
		}
	}
	// the prefix copy compiles to what ERPit's whole file does: its name,
	// events and concurrency are untouched by the cut
	for _, file := range []string{"suite.yml", "fixtures.yml"} {
		whole, err := os.ReadFile(filepath.Join("..", "plan", "testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(plan.TriggersOf(whole, file))
		b, _ := json.Marshal(plan.TriggersOf(files[file], file))
		if string(a) != string(b) {
			t.Fatalf("%s: the cut changed what compiles:\n%s\n%s", file, a, b)
		}
	}
	if !strings.Contains(string(body.Dispatch), `"workflow":"lane.yml","name":"Lane","inputs":[],"refusal":"job deploy: a job-level concurrency is not supported in this release; only the workflow's concurrency group is"`) {
		t.Fatalf("the manual inventory refuses job-level concurrency: %s", body.Dispatch)
	}
	pinCapture(t, filepath.Join("testdata", "concurrency", "catalog.json"), body.Triggers)
}

// the planner posts every job's job-level concurrency flag, true for the
// job that declares one and false otherwise, on the real plan wire
func TestConcurrencyRidesTheRealPlanWire(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{stream: "Stage  Job ID  Job name  Workflow name  Workflow file  Events\n0      build   build     Lane           lane.yml       push,workflow_dispatch\n1      deploy  deploy    Lane           lane.yml       push,workflow_dispatch\n"}
	d := newTestDaemon(t, box, sh, 1)
	d.checkout = writeWorkflows(map[string][]byte{"lane.yml": []byte(laneWorkflow)})
	planManifest := jobManifest
	planManifest.Workflow, planManifest.Job, planManifest.Mode = "", "", "plan"
	d.handle(context.Background(), &ship.Assignment{ID: "0v2", Attempt: "0v2.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/heads/master", OID: "0000000000000000000000000000000000000001", Kind: "plan", DeadlineSeconds: 300, Manifest: &planManifest})
	if len(sh.plans) != 1 {
		t.Fatalf("plans=%v abandons=%v", sh.plans, sh.abandons)
	}
	var body struct {
		Jobs json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(sh.plans[0]), &body); err != nil {
		t.Fatal(err)
	}
	var jobs []struct {
		ID          string `json:"id"`
		Concurrency *bool  `json:"concurrency"`
	}
	if err := json.Unmarshal(body.Jobs, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].Concurrency == nil || jobs[1].Concurrency == nil || *jobs[0].Concurrency || !*jobs[1].Concurrency || jobs[1].ID != "deploy" {
		t.Fatalf("the real plan wire must carry each job's concurrency flag: %s", body.Jobs)
	}
	pinCapture(t, filepath.Join("testdata", "concurrency", "plan-jobs.json"), body.Jobs)
}
