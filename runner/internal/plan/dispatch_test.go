package plan

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func text(s string) *string { return &s }

// a real manual definition: a boolean whose default is false, a required
// number whose default is zero, a choice, a string whose type is omitted
// (so string) and an optional environment with no default. Each default
// is carried as the text the workflow wrote, and "no default" stays
// distinct from an empty one.
func TestDispatchOfManualDefinition(t *testing.T) {
	data, err := os.ReadFile("testdata/manual.yml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DispatchOf(data, "manual.yml")
	if err != nil {
		t.Fatal(err)
	}
	want := &Dispatch{
		Workflow: "manual.yml",
		Name:     "Manual release",
		Inputs: []DispatchInput{
			{ID: "dry_run", Type: "boolean", Description: "Rehearse without publishing", Default: text("false"), Options: []string{}},
			{ID: "count", Type: "number", Description: "How many artifacts to build", Required: true, Default: text("0"), Options: []string{}},
			{ID: "level", Type: "choice", Description: "Log level", Default: text("warning"), Options: []string{"info", "warning", "debug"}},
			{ID: "note", Type: "string", Description: "A note for the run record", Default: text("hello"), Options: []string{}},
			{ID: "target", Type: "environment", Description: "Where to publish", Options: []string{}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// `on:` as a string, a list or a mapping; a workflow without
// workflow_dispatch is no manual workflow at all
func TestDispatchOfTriggerShapes(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       *Dispatch
	}{
		{"scalar", "on: workflow_dispatch\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n",
			&Dispatch{Workflow: "w.yml", Name: "w.yml", Inputs: []DispatchInput{}}},
		{"list", "name: Both\non: [push, workflow_dispatch]\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n",
			&Dispatch{Workflow: "w.yml", Name: "Both", Inputs: []DispatchInput{}}},
		{"empty mapping value", "on:\n  workflow_dispatch:\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n",
			&Dispatch{Workflow: "w.yml", Name: "w.yml", Inputs: []DispatchInput{}}},
		{"push only", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n", nil},
		{"push mapping", "on:\n  push:\n    branches: [main]\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n", nil},
	}
	for _, c := range cases {
		got, err := DispatchOf([]byte(c.yaml), "w.yml")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// an empty default is a default; the type defaults to string; the
// declaration order is the order the form shows
func TestDispatchOfEmptyDefaultAndOrder(t *testing.T) {
	data := []byte("on:\n  workflow_dispatch:\n    inputs:\n      zeta:\n        default: ''\n      alpha:\n        required: true\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n")
	got, err := DispatchOf(data, "w.yml")
	if err != nil || got == nil {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(got.Inputs) != 2 || got.Inputs[0].ID != "zeta" || got.Inputs[1].ID != "alpha" {
		t.Fatalf("inputs out of declaration order: %+v", got.Inputs)
	}
	if got.Inputs[0].Default == nil || *got.Inputs[0].Default != "" || got.Inputs[0].Type != "string" {
		t.Errorf("an empty default must be a present empty string of type string: %+v", got.Inputs[0])
	}
	if got.Inputs[1].Default != nil || !got.Inputs[1].Required {
		t.Errorf("no default must stay absent: %+v", got.Inputs[1])
	}
}

// what the pinned compiler cannot carry is a refusal on the workflow,
// named, never a guess: a key act's schema refuses, a default that is not
// one scalar, a required flag that is not a boolean, and the job-level
// constructs the ship does not run in this release
func TestDispatchOfRefusals(t *testing.T) {
	job := "jobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n"
	cases := []struct {
		name, yaml, refusal string
	}{
		{"unknown key", "on:\n  workflow_dispatch:\n    inputs:\n      x:\n        colour: red\n" + job, "input x: unknown key colour"},
		{"list default", "on:\n  workflow_dispatch:\n    inputs:\n      x:\n        default: [a, b]\n" + job, "input x: default must be a single value"},
		{"required not boolean", "on:\n  workflow_dispatch:\n    inputs:\n      x:\n        required: maybe\n" + job, "input x: required must be true or false"},
		{"inputs not a mapping", "on:\n  workflow_dispatch:\n    inputs: [x]\n" + job, "workflow_dispatch inputs must be a mapping of input ids"},
		{"input not a mapping", "on:\n  workflow_dispatch:\n    inputs:\n      x: yes\n" + job, "input x: must be a mapping"},
		{"options not a list", "on:\n  workflow_dispatch:\n    inputs:\n      x:\n        type: choice\n        options: one\n" + job, "input x: options must be a list of values"},
		{"job if", "on: workflow_dispatch\njobs:\n  a:\n    runs-on: ubuntu-latest\n    if: ${{ env.GO }}\n    steps: [{run: true}]\n", "job a: the job-level if ${{ env.GO }} is refused: context env is not available in a job-level if (github, needs, vars and inputs are)"},
		{"job output", "on: workflow_dispatch\njobs:\n  a:\n    runs-on: ubuntu-latest\n    outputs:\n      url: ${{ env.URL }}\n    steps: [{run: true}]\n", "job a: output url is refused: context env is not available to a job output on the ship in this release (github, needs, vars, inputs and steps.<id>.outputs are)"},
		{"matrix", "on: workflow_dispatch\njobs:\n  a:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        os: [x]\n    steps: [{run: true}]\n", "job a: a matrix strategy is not supported in this release"},
	}
	for _, c := range cases {
		got, err := DispatchOf([]byte(c.yaml), "w.yml")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got == nil || got.Refusal != c.refusal {
			t.Errorf("%s: got %+v, want refusal %q", c.name, got, c.refusal)
		}
	}
}

// a gate the ship evaluates is no refusal: an output comparison, a typed
// input, a status function
func TestDispatchOfSupportedGate(t *testing.T) {
	for _, cond := range []string{"needs.a.outputs.go == 'true'", "${{ inputs.go }}", "always() && !inputs.dry"} {
		data := []byte("on: workflow_dispatch\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n  b:\n    needs: a\n    if: " + cond + "\n    runs-on: ubuntu-latest\n    steps: [{run: true}]\n")
		got, err := DispatchOf(data, "w.yml")
		if err != nil || got == nil || got.Refusal != "" {
			t.Fatalf("%s: got %+v, %v", cond, got, err)
		}
	}
}

func TestDispatchOfBadYAML(t *testing.T) {
	if _, err := DispatchOf([]byte("on: [workflow_dispatch\n"), "w.yml"); err == nil || !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("a workflow that is not YAML must be an error, got %v", err)
	}
}
