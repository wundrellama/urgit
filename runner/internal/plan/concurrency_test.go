package plan

import (
	"os"
	"strings"
	"testing"
)

// CI-CONCURRENCY-1: the workflow-level concurrency: compiled into the
// trigger catalog, and the job-level form refused by the planner.

func concurrencyOf(t *testing.T, yamlText string) *Concurrency {
	t.Helper()
	w := TriggersOf([]byte(yamlText), "w.yml")
	if w.Concurrency == nil {
		t.Fatalf("the catalog workflow carries no concurrency: %+v", w)
	}
	return w.Concurrency
}

// no concurrency: key is "none", always sent; never omitted
func TestConcurrencyNoneIsSent(t *testing.T) {
	c := concurrencyOf(t, "on: push\njobs: {a: {runs-on: x, steps: [{run: a}]}}\n")
	if got := wire(t, c); got != `{"v":2,"kind":"none"}` {
		t.Fatalf("none: %s", got)
	}
	w := TriggersOf([]byte("on: push\njobs: {}\n"), "w.yml")
	if !strings.Contains(wire(t, w), `"concurrency":{"v":2,"kind":"none"}`) {
		t.Fatalf("the catalog workflow must name its concurrency: %s", wire(t, w))
	}
}

// the string form: the whole value is the group template, cancel false
func TestConcurrencyStringForm(t *testing.T) {
	c := concurrencyOf(t, "on: push\nconcurrency: ci-${{ github.ref }}\njobs: {}\n")
	if c.Kind != "group" || c.Group == nil || c.Cancel == nil || c.Cancel.Value == nil || *c.Cancel.Value || c.Cancel.Expr != nil {
		t.Fatalf("string form: %+v", c)
	}
	want := `{"v":2,"kind":"group","group":{"text":"ci-${{ github.ref }}","line":2,"col":14,"parts":[{"lit":"ci-"},{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"}}]},"cancel":{"text":"","line":0,"col":0,"value":false}}`
	if got := wire(t, c); got != want {
		t.Fatalf("string form wire:\n got %s\nwant %s", got, want)
	}
	// a plain literal group has one literal part
	c = concurrencyOf(t, "on: push\nconcurrency: deploy\njobs: {}\n")
	if c.Kind != "group" || len(c.Group.Parts) != 1 || c.Group.Parts[0].Lit == nil || *c.Group.Parts[0].Lit != "deploy" {
		t.Fatalf("literal group: %+v", c.Group)
	}
}

// the mapping form: group and a boolean cancel-in-progress
func TestConcurrencyMappingForm(t *testing.T) {
	c := concurrencyOf(t, "on: push\nconcurrency:\n  group: ${{ github.workflow }}\n  cancel-in-progress: true\njobs: {}\n")
	if c.Kind != "group" || c.Cancel.Value == nil || !*c.Cancel.Value || c.Cancel.Text != "true" || c.Cancel.Line != 4 {
		t.Fatalf("mapping form: %+v %+v", c, c.Cancel)
	}
	if len(c.Group.Parts) != 1 || c.Group.Parts[0].Expr == nil || c.Group.Parts[0].Expr.K != "prop" || c.Group.Parts[0].Expr.S != "workflow" {
		t.Fatalf("group parts: %+v", c.Group.Parts)
	}
	// cancel-in-progress absent is false
	c = concurrencyOf(t, "on: push\nconcurrency:\n  group: g\njobs: {}\n")
	if c.Kind != "group" || c.Cancel.Value == nil || *c.Cancel.Value {
		t.Fatalf("absent cancel-in-progress: %+v", c.Cancel)
	}
	// an expression for cancel-in-progress, blanks around it allowed
	c = concurrencyOf(t, "on: push\nconcurrency:\n  group: g\n  cancel-in-progress: \"  ${{ inputs.cancel }} \"\njobs: {}\n")
	if c.Kind != "group" || c.Cancel.Expr == nil || c.Cancel.Value != nil || c.Cancel.Expr.K != "prop" {
		t.Fatalf("expression cancel: %+v", c.Cancel)
	}
	// vars is a context GitHub gives concurrency
	if c := concurrencyOf(t, "on: push\nconcurrency: ${{ vars.LANE }}\njobs: {}\n"); c.Kind != "group" {
		t.Fatalf("vars: %+v", c)
	}
	// YAML anchors are read as the values they stand for
	if c := concurrencyOf(t, "x: &g lane\non: push\nconcurrency: {group: *g}\njobs: {}\n"); c.Kind != "group" || *c.Group.Parts[0].Lit != "lane" {
		t.Fatalf("alias: %+v", c)
	}
}

// ERPit's two workflows (testdata/suite.yml and fixtures.yml are its
// files byte for byte): the group and the flag, node by node
func TestConcurrencyERPit(t *testing.T) {
	for _, file := range []string{"testdata/suite.yml", "testdata/fixtures.yml"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		w := TriggersOf(data, file)
		c := w.Concurrency
		if w.Refusal != "" || c == nil || c.Kind != "group" {
			t.Fatalf("%s: %+v %+v", file, w.Refusal, c)
		}
		if c.Group.Text != "${{ github.workflow }}-${{ github.ref }}-${{ github.ref == 'refs/heads/master' && github.sha || 'head' }}" {
			t.Fatalf("%s group text %q", file, c.Group.Text)
		}
		p := c.Group.Parts
		if len(p) != 5 || p[0].Expr == nil || p[1].Lit == nil || *p[1].Lit != "-" || p[2].Expr == nil || p[3].Lit == nil || *p[3].Lit != "-" || p[4].Expr == nil {
			t.Fatalf("%s parts: %s", file, wire(t, p))
		}
		if got := wire(t, p[4].Expr); got != `{"k":"or","l":{"k":"and","l":{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}},"r":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"sha"}},"r":{"k":"str","s":"head"}}` {
			t.Fatalf("%s third part: %s", file, got)
		}
		if c.Cancel.Value != nil || c.Cancel.Expr == nil || c.Cancel.Text != "${{ github.ref != 'refs/heads/master' }}" {
			t.Fatalf("%s cancel: %+v", file, c.Cancel)
		}
		if got := wire(t, c.Cancel.Expr); got != `{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}}` {
			t.Fatalf("%s cancel expression: %s", file, got)
		}
	}
}

// every form the ship could not evaluate faithfully is refused with the
// reason, never compiled to "no group"
func TestConcurrencyRefusals(t *testing.T) {
	cases := map[string]string{
		"concurrency:\n":                                                           "concurrency is empty",
		"concurrency: 5\n":                                                         "is not text",
		"concurrency: true\n":                                                      "is not text",
		"concurrency: [a, b]\n":                                                    "concurrency must be",
		"concurrency: ''\n":                                                        "the concurrency group is empty",
		"concurrency: {group: g, cancel: true}\n":                                  "unknown key cancel",
		"concurrency: {cancel-in-progress: true}\n":                                "names no group",
		"concurrency: {group: [a]}\n":                                              "group is not text",
		"concurrency: {group: '  '}\n":                                             "the concurrency group is empty",
		"concurrency: {group: g, cancel-in-progress: 'true'}\n":                    "not \"true\"",
		"concurrency: {group: g, cancel-in-progress: 1}\n":                         "true, false or one",
		"concurrency: {group: g, cancel-in-progress: }\n":                          "cancel-in-progress is empty",
		"concurrency: {group: g, cancel-in-progress: 'x ${{ true }}'}\n":           "mixes text",
		"concurrency: {group: g, cancel-in-progress: '${{ true }}${{ false }}'}\n": "holds 2",
		"concurrency: ci-${{ needs.a.outputs.x }}\n":                               "context needs is not available to a workflow's concurrency",
		"concurrency: ci-${{ env.LANE }}\n":                                        "context env is not available to a workflow's concurrency",
		"concurrency: ci-${{ secrets.TOKEN }}\n":                                   "context secrets is not available",
		"concurrency: ci-${{ matrix.os }}\n":                                       "context matrix is not available",
		"concurrency: ci-${{ github.job }}\n":                                      "github.job is not defined",
		"concurrency: ci-${{ github.run_id }}\n":                                   "github.run_id is not available on the ship",
		"concurrency: {group: g, cancel-in-progress: '${{ always() }}'}\n":         "only valid in an if",
		"concurrency: ci-${{ hashFiles('x') }}\n":                                  "hashFiles is not available to a workflow's concurrency",
		"concurrency: ci-${{ github.ref\n":                                         "not closed",
		"concurrency: ci-${{ github.ref = 'x' }}\n":                                "use ==",
		"concurrency: ci-${{ nosuch.thing }}\n":                                    "unknown context nosuch",
	}
	for decl, want := range cases {
		c := concurrencyOf(t, "on: push\n"+decl+"jobs: {}\n")
		if c.Kind != "refused" || !strings.Contains(c.Refusal, want) || c.Group != nil || c.Cancel != nil {
			t.Errorf("%q: %+v, want a refusal naming %q", decl, c, want)
		}
		if c.Line == 0 {
			t.Errorf("%q: a refusal names where the workflow wrote it: %+v", decl, c)
		}
	}
}

// a refusal's wire carries its text, place and reason; an unreadable
// workflow's concurrency is refused with it
func TestConcurrencyRefusalWire(t *testing.T) {
	c := concurrencyOf(t, "on: push\nconcurrency: 5\njobs: {}\n")
	if got := wire(t, c); got != `{"v":2,"kind":"refused","text":"5","line":2,"col":14,"refusal":"concurrency 5 is not text: write a group (a string), or a mapping with group and cancel-in-progress"}` {
		t.Fatalf("refusal wire: %s", got)
	}
	w := TriggersOf([]byte("on: [\n"), "w.yml")
	if w.Concurrency == nil || w.Concurrency.Kind != "refused" || w.Refusal == "" {
		t.Fatalf("unreadable workflow: %+v", w)
	}
	w = TriggersOf([]byte("- a\n"), "w.yml")
	if w.Concurrency == nil || w.Concurrency.Kind != "refused" {
		t.Fatalf("a workflow that is no mapping: %+v", w)
	}
	// a workflow whose on: is refused still compiles its concurrency
	w = TriggersOf([]byte("concurrency: g\njobs: {}\n"), "w.yml")
	if w.Refusal == "" || w.Concurrency == nil || w.Concurrency.Kind != "group" {
		t.Fatalf("refused on: with a group: %+v", w)
	}
}

// job-level concurrency: is declared on the job and refused, in the plan's
// walk and in the manual-run inventory, in every form
func TestJobLevelConcurrencyIsRefused(t *testing.T) {
	src := "on: [push, workflow_dispatch]\njobs:\n  a:\n    runs-on: x\n    steps: [{run: a}]\n  b:\n    runs-on: x\n    concurrency:\n      group: lane\n    steps: [{run: b}]\n  c:\n    runs-on: x\n    concurrency:\n    steps: [{run: c}]\n"
	walked, err := Walk([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if walked["a"].Concurrency || !walked["b"].Concurrency || !walked["c"].Concurrency {
		t.Fatalf("walk: a %v b %v c %v", walked["a"].Concurrency, walked["b"].Concurrency, walked["c"].Concurrency)
	}
	d, err := DispatchOf([]byte(src), "w.yml")
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.Refusal != "job b: a job-level concurrency is not supported in this release; only the workflow's concurrency group is" {
		t.Fatalf("inventory refusal: %+v", d)
	}
	// a workflow-level group is no job-level refusal
	d, err = DispatchOf([]byte("on: workflow_dispatch\nconcurrency: lane\njobs: {a: {runs-on: x, steps: [{run: a}]}}\n"), "w.yml")
	if err != nil || d == nil || d.Refusal != "" {
		t.Fatalf("workflow-level group: %+v %v", d, err)
	}
}
