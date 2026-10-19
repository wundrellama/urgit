package plan

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"
)

func compileOn(t *testing.T, yaml string) TriggerWorkflow {
	t.Helper()
	return TriggersOf([]byte(yaml), "w.yml")
}

func eventNamed(w TriggerWorkflow, name string) *TriggerEvent {
	for i := range w.Events {
		if w.Events[i].Event == name {
			return &w.Events[i]
		}
	}
	return nil
}

func patternTexts(f *Filter) []string {
	if f == nil {
		return nil
	}
	out := []string{}
	for _, p := range f.Patterns {
		out = append(out, p.Text)
	}
	return out
}

// `on:` as a string, a list and a mapping (the captured reference's
// three forms), in the order written
func TestTriggersOnForms(t *testing.T) {
	w := compileOn(t, "name: CI\non: push\njobs: {a: {runs-on: x, steps: [{run: a}]}}\n")
	if w.Refusal != "" || w.Name != "CI" || len(w.Events) != 1 || w.Events[0].Event != "push" || w.Events[0].Branches != nil {
		t.Fatalf("scalar on: %+v", w)
	}
	w = compileOn(t, "on: [push, pull_request, workflow_dispatch]\njobs: {}\n")
	var names []string
	for _, e := range w.Events {
		names = append(names, e.Event)
	}
	if w.Refusal != "" || !reflect.DeepEqual(names, []string{"push", "pull_request", "workflow_dispatch"}) || w.Name != "w.yml" {
		t.Fatalf("list on: %+v", w)
	}
	pr := eventNamed(w, "pull_request")
	if !reflect.DeepEqual(pr.Types, []string{"opened", "synchronize", "reopened"}) || pr.TypesDeclared {
		t.Fatalf("pull_request types default to opened, synchronize, reopened: %+v", pr)
	}
	w = compileOn(t, `on:
  push:
    branches: [main, 'releases/**', '!releases/**-alpha']
    paths: ['src/**', '!src/docs/**']
  pull_request:
    types: [opened, closed]
    branches-ignore: [wip]
  schedule:
    - cron: '30 5 * * 1-5'
      timezone: America/New_York
    - cron: '0 0 * * *'
  workflow_dispatch:
    inputs: {x: {type: string}}
jobs: {}
`)
	if w.Refusal != "" {
		t.Fatalf("mapping on: refused %q", w.Refusal)
	}
	push := eventNamed(w, "push")
	if push.Refusal != "" || !reflect.DeepEqual(patternTexts(push.Branches), []string{"main", "releases/**", "!releases/**-alpha"}) || push.Branches.Ignore || !push.Branches.Patterns[2].Negated {
		t.Fatalf("push branches: %+v", push)
	}
	if !reflect.DeepEqual(patternTexts(push.Paths), []string{"src/**", "!src/docs/**"}) || push.Tags != nil || push.Line != 2 || push.Col != 3 {
		t.Fatalf("push paths and place: %+v", push)
	}
	pr = eventNamed(w, "pull_request")
	if pr.Refusal != "" || !reflect.DeepEqual(pr.Types, []string{"opened", "closed"}) || !pr.TypesDeclared || !pr.Branches.Ignore || !reflect.DeepEqual(patternTexts(pr.Branches), []string{"wip"}) {
		t.Fatalf("pull_request: %+v", pr)
	}
	sched := eventNamed(w, "schedule")
	if sched.Refusal != "" || len(sched.Schedules) != 2 || sched.Schedules[0].Timezone != "America/New_York" || sched.Schedules[1].Timezone != "" || sched.Schedules[0].Refusal != "" || !reflect.DeepEqual(sched.Schedules[0].Weekdays, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("schedule: %+v", sched)
	}
	if d := eventNamed(w, "workflow_dispatch"); d == nil || d.Refusal != "" || d.Branches != nil {
		t.Fatalf("workflow_dispatch is listed with no filter of its own: %+v", d)
	}
}

// a manual run never inherits a push filter: the filters are the event's
func TestTriggersFiltersBelongToTheirEvent(t *testing.T) {
	w := compileOn(t, "on:\n  push:\n    branches: [main]\n    paths: [src/**]\n  workflow_dispatch:\njobs: {}\n")
	d := eventNamed(w, "workflow_dispatch")
	if d == nil || d.Branches != nil || d.Paths != nil || d.Tags != nil {
		t.Fatalf("workflow_dispatch carries no filter: %+v", d)
	}
}

// the mutually exclusive and malformed forms, refused on their event with
// the reason; another event of the workflow is not refused with it
func TestTriggersRefusals(t *testing.T) {
	cases := map[string]string{
		"push: {branches: [a], branches-ignore: [b]}":         "branches and branches-ignore",
		"push: {tags: [a], tags-ignore: [b]}":                 "tags and tags-ignore",
		"push: {paths: [a], paths-ignore: [b]}":               "paths and paths-ignore",
		"push: {branches: ['!a']}":                            "only ! patterns",
		"push: {branches-ignore: ['!a']}":                     "branches-ignore",
		"push: {branches: []}":                                "no pattern",
		"push: {branches: ['a??']}":                           "quantifier",
		"push: {branch: [a]}":                                 "unknown key branch",
		"push: {types: [created]}":                            "unknown key types",
		"push: {branches: {a: b}}":                            "list of patterns",
		"push: [main]":                                        "mapping",
		"pull_request: {types: [opend]}":                      "opend is not a pull_request activity type",
		"pull_request: {types: []}":                           "no activity type",
		"pull_request: {tags: [v1]}":                          "unknown key tags",
		"pull_request: {branches: [a], branches-ignore: [b]}": "branches and branches-ignore",
		"schedule: {cron: '0 0 * * *'}":                       "list of",
		"schedule: []":                                        "no cron",
		"pull_request_target:":                                "pull_request_target is not implemented",
		"issues:":                                             "no native source",
		"workflow_call:":                                      "reusable",
		"made_up_event:":                                      "not an event",
	}
	for on, want := range cases {
		w := compileOn(t, "on:\n  "+on+"\n  workflow_dispatch:\njobs: {}\n")
		if w.Refusal != "" {
			t.Errorf("%s: the workflow itself is refused (%s); only its event should be", on, w.Refusal)
			continue
		}
		name := strings.SplitN(strings.TrimSpace(on), ":", 2)[0]
		e := eventNamed(w, name)
		if e == nil || !strings.Contains(e.Refusal, want) {
			t.Errorf("%s: refusal %+v, want one naming %q", on, e, want)
		}
		if d := eventNamed(w, "workflow_dispatch"); d == nil || d.Refusal != "" {
			t.Errorf("%s: the workflow's other event is refused too: %+v", on, d)
		}
	}
}

// a refused schedule entry is refused alone, with its reason
func TestTriggersScheduleEntries(t *testing.T) {
	w := compileOn(t, `on:
  schedule:
    - cron: '@daily'
    - cron: '*/4 * * * *'
    - cron: '0 3 * * *'
      timezone: Mars/Olympus
    - cron: '0 3 * * *'
      tz: UTC
    - '0 3 * * *'
    - cron: '15 3 * * *'
      timezone: Europe/London
jobs: {}
`)
	s := eventNamed(w, "schedule")
	if s == nil || s.Refusal != "" || len(s.Schedules) != 6 {
		t.Fatalf("schedule: %+v", s)
	}
	want := []string{"non-standard", "five minutes", "pinned timezone database", "unknown key tz", "mapping", ""}
	for i, w := range want {
		got := s.Schedules[i].Refusal
		if (w == "" && got != "") || (w != "" && !strings.Contains(got, w)) {
			t.Errorf("schedule %d refusal %q, want %q", i, got, w)
		}
		if w != "" && (s.Schedules[i].Minutes != nil || s.Schedules[i].Weekdays != nil) {
			t.Errorf("refused schedule %d carries values: %+v", i, s.Schedules[i])
		}
	}
	if s.Schedules[5].Timezone != "Europe/London" || s.Schedules[5].Cron != "15 3 * * *" {
		t.Errorf("the accepted entry keeps its text and zone: %+v", s.Schedules[5])
	}
}

// the workflow itself is refused only when its `on:` cannot be read
func TestTriggersWorkflowRefusals(t *testing.T) {
	cases := map[string]string{
		"jobs: {}\n":                     "declares no on:",
		"on: [push, push]\njobs: {}\n":   "push twice",
		"on: [push, {a: b}]\njobs: {}\n": "list of event names",
		"on: 5\njobs: {}\n":              "not an event",
		"on: [\njobs: {}\n":              "not valid YAML",
		"- a\n":                          "not a mapping",
	}
	for yaml, want := range cases {
		w := compileOn(t, yaml)
		if !strings.Contains(w.Refusal, want) {
			t.Errorf("%q: refusal %q, want one naming %q", yaml, w.Refusal, want)
		}
	}
}

// YAML anchors are read as the values they stand for
func TestTriggersAliases(t *testing.T) {
	w := compileOn(t, "x: &main [main]\non:\n  push:\n    branches: *main\n  pull_request:\n    branches: *main\njobs: {}\n")
	push, pr := eventNamed(w, "push"), eventNamed(w, "pull_request")
	if push == nil || pr == nil || !reflect.DeepEqual(patternTexts(push.Branches), []string{"main"}) || !reflect.DeepEqual(patternTexts(pr.Branches), []string{"main"}) {
		t.Fatalf("aliases: %+v", w)
	}
}

// the catalog: every file in order, and the zone table of every zone an
// accepted schedule names, nothing else
func TestCatalogOfListsEveryFileAndItsZones(t *testing.T) {
	files := map[string]string{
		"a.yml": "on:\n  schedule:\n    - cron: '0 1 * * *'\n      timezone: America/New_York\n    - cron: '0 2 * * *'\n      timezone: Mars/Olympus\njobs: {}\n",
		"b.yml": "on: push\njobs: {}\n",
	}
	cat, err := CatalogOf([]string{"a.yml", "b.yml"}, func(f string) ([]byte, error) { return []byte(files[f]), nil })
	if err != nil {
		t.Fatal(err)
	}
	if cat.Version != TriggerCatalogVersion || len(cat.Workflows) != 2 || cat.Workflows[0].Workflow != "a.yml" || cat.Workflows[1].Workflow != "b.yml" {
		t.Fatalf("catalog: %+v", cat)
	}
	if len(cat.Zones) != 1 || cat.Zones["America/New_York"].Offset != -18000 {
		t.Fatalf("zones: %+v", cat.Zones)
	}
}

// `go test ./internal/plan -run Golden -args -write-golden` rewrites the
// pinned wire files after a deliberate change (then review the diff)
var writeGolden = flag.Bool("write-golden", false, "rewrite the pinned golden wire files")

// the wire the ship parses (lib/ci-trigger), pinned: the golden JSON of a
// representative workflow (testdata/triggers/ci.yml), byte for byte
func TestCatalogWireGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/triggers/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := CatalogOf([]string{"ci.yml"}, func(string) ([]byte, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if *writeGolden {
		if err := os.WriteFile("testdata/triggers/ci.json", append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/triggers/ci.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("the catalog wire changed:\n%s", got)
	}
}
