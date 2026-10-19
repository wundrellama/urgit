//go:build fixture

package daemon

// A FIXTURE DRIVER, not a unit test (Delivery 1A; .scratch/workflow/out/
// VERIFY.md). It speaks to a running fake ship as the daemon a fixture
// enrolled (the "probe", sandbox microvm), takes the next assignment of the
// kind asked, checks it exactly as a runner does — the signature against
// the CI key pinned at enrollment, the payload against its signed context,
// the event — and carries it out with this package's real code on a FAKE
// sandbox: the git checkout from the ship, the resolve walk, the bundle and
// the act invocation are real; act itself is not run. A plan's `act -l`
// table is EMULATED from the workflow YAML (logged as such); a job's act
// exits 127 with no output, so the daemon gives the attempt back with its
// own reason. Nothing here fabricates a result. Excluded from every normal
// build by its tag:
//
//	URGIT_FIXTURE_SHIP=http://127.0.0.1:8642 URGIT_FIXTURE_PROBE=<probes.json> \
//	URGIT_FIXTURE_KIND=resolve|plan|job [URGIT_FIXTURE_CANDIDATE=<id>] URGIT_FIXTURE_OUT=<dir> \
//	go test -tags fixture -run TestFixtureLiveAssignment -count=1 -v ./internal/daemon/

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"urgit/runner/internal/config"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/ship"
)

// the probe's box claims the VM backend so the ship's VM-required work
// reaches it; it runs nothing (fakeBox records every call)
type fixtureBox struct{ *fakeBox }

func (fixtureBox) Kind() string { return "microvm" }

var workflowArg = regexp.MustCompile(`-W '\.github/workflows/([^']+)'`)

// emulateActList answers `act -l -W .github/workflows/<file>` from the
// checked-out YAML: one row per job, in document order, with the
// workflow's events — act's 0.2.89 table shape, EMULATED
func emulateActList(work string) func(argv []string) (string, int) {
	return func(argv []string) (string, int) {
		m := workflowArg.FindStringSubmatch(strings.Join(argv, " "))
		if m == nil {
			return "fixture: not an act -l invocation", 2
		}
		files, _ := filepath.Glob(filepath.Join(work, "*", "bundle", "src", ".github", "workflows", m[1]))
		if len(files) != 1 {
			return "fixture: workflow not found in the bundle", 2
		}
		data, _ := os.ReadFile(files[0])
		var doc struct {
			Name string    `yaml:"name"`
			On   yaml.Node `yaml:"on"`
			Jobs yaml.Node `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return "fixture: " + err.Error(), 1
		}
		var events []string
		switch doc.On.Kind {
		case yaml.ScalarNode:
			events = []string{doc.On.Value}
		case yaml.SequenceNode:
			for _, n := range doc.On.Content {
				events = append(events, n.Value)
			}
		case yaml.MappingNode:
			for i := 0; i < len(doc.On.Content); i += 2 {
				events = append(events, doc.On.Content[i].Value)
			}
		}
		walked, _ := plan.Walk(data)
		name := doc.Name
		if name == "" {
			name = m[1]
		}
		var out bytes.Buffer
		fmt.Fprintf(&out, "Stage  Job ID  Job name  Workflow name  Workflow file  Events\n")
		for i := 0; i < len(doc.Jobs.Content); i += 2 {
			id := doc.Jobs.Content[i].Value
			stage := 0
			if len(walked[id].Needs) > 0 {
				stage = 1
			}
			fmt.Fprintf(&out, "%d      %s  %s  %s  %s  %s\n", stage, id, id, name, m[1], strings.Join(events, ","))
		}
		return out.String(), 0
	}
}

func TestFixtureLiveAssignment(t *testing.T) {
	url, probesPath, kind, out := os.Getenv("URGIT_FIXTURE_SHIP"), os.Getenv("URGIT_FIXTURE_PROBE"), os.Getenv("URGIT_FIXTURE_KIND"), os.Getenv("URGIT_FIXTURE_OUT")
	if url == "" || probesPath == "" || kind == "" || out == "" {
		t.Skip("a fixture driver: set URGIT_FIXTURE_SHIP, URGIT_FIXTURE_PROBE, URGIT_FIXTURE_KIND and URGIT_FIXTURE_OUT")
	}
	want := os.Getenv("URGIT_FIXTURE_CANDIDATE")
	var probes map[string]struct {
		ID     string `json:"id"`
		Bearer string `json:"bearer"`
		Key    string `json:"ci-public-key"`
	}
	raw, err := os.ReadFile(probesPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &probes); err != nil {
		t.Fatal(err)
	}
	probe := probes["a"]
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	var logged bytes.Buffer
	cfg := &config.Config{ShipURL: url, ActBinary: "/bin/sh", ActImage: "img", Capacity: 2, WorkDir: work, StateFile: filepath.Join(work, "state.json"), Resolver: true}
	client := ship.New(url, probe.Bearer)
	client.Capacity, client.Resolver = 2, true
	box := &fakeBox{exit: 127, answer: emulateActList(work)}
	d := &Daemon{cfg: cfg, box: fixtureBox{box}, log: log.New(&logged, "", 0), capacity: 2, client: client, daemonID: probe.ID, ciKey: probe.Key, inFlight: map[string]bool{}}
	d.checkout, d.mirror, d.fetchDownload = d.gitCheckout, d.gitMirror, d.storeDownload
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var a *ship.Assignment
	var skipped []string
	for i := 0; i < 8 && a == nil; i++ {
		got, err := client.Poll(ctx, probe.ID)
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if got == nil {
			continue
		}
		if got.Kind != kind || (want != "" && uvKey(got.Candidate) != uvKey(want)) {
			// another fixture's leftover work on this probe: given back, unrun,
			// with that reason, so it does not hold the probe's slots
			skipped = append(skipped, got.Kind+" "+got.Attempt+" of "+got.Candidate)
			_, _ = client.Abandon(ctx, got.Attempt, "fixture probe: given back unrun (not the assignment under test)")
			continue
		}
		a = got
	}
	if a == nil {
		t.Fatalf("no %s assignment arrived (skipped: %v)", kind, skipped)
	}
	// the assignment as delivered, its read capability and grant values
	// withheld from the record
	shown := *a
	shown.ReadToken = "(withheld)"
	for i := range shown.Grants {
		shown.Grants[i].Value = "(withheld)"
	}
	record, _ := json.MarshalIndent(shown, "", " ")
	verify := d.verifyAssignment(a)
	honour := error(nil)
	if verify == nil {
		honour = d.honourable(a)
	}
	report := map[string]any{"kind": kind, "attempt": a.Attempt, "candidate": a.Candidate, "skipped": skipped,
		"signature": fmt.Sprint(verify), "honourable": fmt.Sprint(honour), "manifest-version": a.Sig.Version,
		"event": a.Manifest.Event, "context": a.Manifest.Context, "payload": a.EventPayload, "grants": len(a.Grants), "mode": a.Manifest.Mode, "sandbox": a.Manifest.Sandbox}
	if verify != nil || honour != nil {
		_ = os.WriteFile(filepath.Join(out, kind+"-assignment.json"), record, 0o644)
		t.Fatalf("the runner refuses the ship's assignment: signature %v; honourable %v", verify, honour)
	}
	d.handle(ctx, a)
	var runs []string
	for _, op := range box.ops {
		if strings.HasPrefix(op, "run ") && !strings.Contains(op, " -l -W ") {
			runs = append(runs, op)
		}
	}
	sort.Strings(skipped)
	report["act"] = runs
	report["event.json"] = box.eventJSON
	report["bundle.json"] = box.bundleJSON
	report["daemon-log"] = strings.Split(strings.TrimSpace(logged.String()), "\n")
	report["act-l-emulated"] = kind == "plan"
	data, _ := json.MarshalIndent(report, "", " ")
	_ = os.WriteFile(filepath.Join(out, kind+"-"+a.Attempt+".json"), data, 0o644)
	_ = os.WriteFile(filepath.Join(out, kind+"-"+a.Attempt+"-assignment.json"), record, 0o644)
	t.Logf("%s", data)
	text := logged.String()
	switch kind {
	case "resolve":
		if !strings.Contains(text, "lock POST -> 200") {
			t.Fatalf("the ship did not take the lock:\n%s", text)
		}
	case "plan":
		if !strings.Contains(text, "plan POST -> 200") {
			t.Fatalf("the ship did not take the plan:\n%s", text)
		}
	case "job":
		if len(runs) != 1 {
			t.Fatalf("act runs: %v\n%s", runs, text)
		}
		switch a.Manifest.Event {
		case "workflow_dispatch":
			if !strings.HasPrefix(runs[0], "run /usr/local/bin/act workflow_dispatch -W /work/projected/") || !strings.Contains(runs[0], " -e /work/event.json --actor ") || box.eventJSON != a.EventPayload || len(a.Grants) != 0 {
				t.Fatalf("a manual job's act argv %q, event.json %q, grants %d", runs[0], box.eventJSON, len(a.Grants))
			}
		case "push":
			if !strings.HasPrefix(runs[0], "run /usr/local/bin/act push -W /work/projected/") || !strings.Contains(runs[0], " -e /work/event.json --actor ") || box.eventJSON == "" || box.eventJSON != a.EventPayload {
				t.Fatalf("a push job's act argv %q, event.json %q", runs[0], box.eventJSON)
			}
		default:
			t.Fatalf("event %q", a.Manifest.Event)
		}
		if !strings.Contains(text, "abandon POST -> 200") {
			t.Fatalf("the attempt was not given back:\n%s", text)
		}
	}
}
