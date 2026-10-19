package plan

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// actRun is what act 0.2.89 reads from a projection for a job's identity:
// github.workflow is the workflow's name, or its file name when it has
// none (pkg/model/planner.go); github.job is the job's id; the run name is
// the job's `name:` or else its id (pkg/model/planner.go Run.String).
// act names the job container, its env volume, a services network and
// each service container from "<workflow>/<run name>"
// (pkg/runner/run_context.go String, jobContainerName, networkName).
type actRun struct {
	workflow, jobID, runName string
}

func (r actRun) identity() string { return r.workflow + "/" + r.runName }

func readRun(t *testing.T, projected []byte, fileName string) actRun {
	t.Helper()
	var doc struct {
		Name string `yaml:"name"`
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(projected, &doc); err != nil {
		t.Fatalf("the projection is not YAML: %v\n%s", err, projected)
	}
	if len(doc.Jobs) != 1 {
		t.Fatalf("the projection holds %d jobs:\n%s", len(doc.Jobs), projected)
	}
	r := actRun{workflow: doc.Name}
	if r.workflow == "" {
		r.workflow = fileName
	}
	for id, job := range doc.Jobs {
		r.jobID, r.runName = id, job.Name
		if r.runName == "" {
			r.runName = id
		}
	}
	return r
}

// textBeforeJobs is a workflow's text up to its top-level jobs: key.
func textBeforeJobs(t *testing.T, data []byte) string {
	t.Helper()
	i := strings.Index(string(data), "\njobs:\n")
	if i < 0 {
		t.Fatalf("no top-level jobs: in\n%s", data)
	}
	return string(data[:i+1])
}

// the parent review's counterexample (parent-review-01): the projection
// leaves the workflow's own name to github.workflow, and every line before
// jobs: is the original's
func TestProjectionKeepsTheWorkflowName(t *testing.T) {
	source := []byte("name: Operator context check\non: [push, workflow_dispatch]\njobs:\n  context:\n    runs-on: ubuntu-latest\n    steps:\n      - run: printf '%s\\n' '${{ github.workflow }}'\n")
	projected, err := ProjectImages(source, "context", "parent-attempt", "manual.yml", nil)
	if err != nil {
		t.Fatal(err)
	}
	run := readRun(t, projected, "manual.yml")
	if run.workflow != "Operator context check" {
		t.Fatalf("github.workflow is %q, want the original %q", run.workflow, "Operator context check")
	}
	if run.jobID != "context" {
		t.Fatalf("github.job is %q, want context", run.jobID)
	}
	if got, want := textBeforeJobs(t, projected), textBeforeJobs(t, source); got != want {
		t.Fatalf("the text before jobs: changed\n--- want\n%s--- got\n%s", want, got)
	}
}

// a workflow with no name stays unnamed: act names it after its file, as
// `act -l` named it when the run was planned
func TestProjectionLeavesAnUnnamedWorkflowUnnamed(t *testing.T) {
	source := []byte("on: [push]\njobs:\n  a:\n    runs-on: x\n    steps: []\n")
	projected, err := Project(source, "a", "0v1.att", "w.yml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(projected, &doc); err != nil {
		t.Fatal(err)
	}
	if _, named := doc["name"]; named {
		t.Fatalf("the projection named an unnamed workflow:\n%s", projected)
	}
	if run := readRun(t, projected, "w.yml"); run.workflow != "w.yml" || run.jobID != "a" {
		t.Fatalf("act would read %+v", run)
	}
}

// isolation (CI-PROJECT-1.1): two attempts of one job never share act's
// identity, because the attempt names the job's run; the workflow's name
// and the job's id are untouched. A job's own name, whatever YAML spelled
// it, is kept after the attempt, expressions and all, and its old lines
// are gone with the job's needs and if.
func TestProjectionRunNameIsTheAttempts(t *testing.T) {
	source := []byte(`name: Release train
on: [push]
jobs:
  plain:
    runs-on: x
    steps: []
  quoted:
    name: 'deploy: ${{ inputs.level }} #1 "now"'
    needs: plain
    if: needs.plain.result == 'success'
    runs-on: x
    steps: []
  folded:
    needs: [plain]
    name: >-
      a long
      name
    runs-on: x
    steps: []
`)
	original := map[string]string{"plain": "plain", "quoted": `deploy: ${{ inputs.level }} #1 "now"`, "folded": "a long name"}
	for job, runName := range original {
		one, err := Project(source, job, "0v1.aaa", "w.yml")
		if err != nil {
			t.Fatalf("%s: %v", job, err)
		}
		two, err := Project(source, job, "0v1.bbb", "w.yml")
		if err != nil {
			t.Fatalf("%s: %v", job, err)
		}
		r1, r2 := readRun(t, one, "w.yml"), readRun(t, two, "w.yml")
		if r1.identity() == r2.identity() {
			t.Fatalf("%s: two attempts share act's identity %q", job, r1.identity())
		}
		if r1.workflow != "Release train" || r1.jobID != job {
			t.Fatalf("%s: github.workflow %q, github.job %q", job, r1.workflow, r1.jobID)
		}
		if want := ProjectionName("0v1.aaa", runName); r1.runName != want {
			t.Fatalf("%s: the run name is %q, want %q", job, r1.runName, want)
		}
		var doc struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(one, &doc); err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"needs", "if"} {
			if _, has := doc.Jobs[job][gone]; has {
				t.Fatalf("%s: %s survived:\n%s", job, gone, one)
			}
		}
		if strings.Contains(string(one), "a long\n") {
			t.Fatalf("%s: the folded name's old lines survived:\n%s", job, one)
		}
	}
}

// a job written as a flow mapping has no line of its own to carry the run
// name: refused, never run under a name another attempt shares
func TestProjectionRefusesAFlowStyleJob(t *testing.T) {
	_, err := Project([]byte("on: [push]\njobs:\n  a: {runs-on: x, steps: []}\n"), "a", "0v1.att", "w.yml")
	if err == nil || !strings.Contains(err.Error(), "block mapping") {
		t.Fatalf("a flow-style job: %v", err)
	}
}
