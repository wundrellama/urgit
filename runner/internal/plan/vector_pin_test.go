package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The wires the ship's Delivery 1C vectors embed, pinned byte for byte:
// desk/gen/ci-trigger-vector carries testdata/selection/catalog.json and
// globs.json, desk/gen/ci-cron-vector testdata/schedules/schedules.json and
// occurrences.json. A compiler change that alters what the ship receives
// fails here first; `-args -write-golden` rewrites them after a reviewed
// change, and the vectors embed the new bytes.

func pinCompact(t *testing.T, path string, v any) {
	t.Helper()
	got, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if *writeGolden {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (-args -write-golden writes it)", path, err)
	}
	if strings.TrimSuffix(string(want), "\n") != string(got) {
		t.Fatalf("%s drifted from its pinned wire:\n got %s", path, got)
	}
}

// the catalog of every workflow of a fixture directory, in name order, as a
// resolver posts it
func catalogOfDir(t *testing.T, dir string) TriggerCatalog {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures in %s: %v %v", dir, files, err)
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	sort.Strings(names)
	cat, err := CatalogOf(names, func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, name)) })
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// the selection fixtures: ordered negation and re-inclusion, -ignore lists,
// a tags-only filter, no filter, a refused push trigger, a workflow that
// declares neither push nor pull_request, and an on: that cannot be read
func TestSelectionCatalogIsPinned(t *testing.T) {
	cat := catalogOfDir(t, "testdata/selection")
	if len(cat.Workflows) != 7 || len(cat.Zones) != 0 {
		t.Fatalf("seven workflows and no zone table: %+v", cat)
	}
	for _, w := range cat.Workflows {
		broken := w.Workflow == "g-broken.yml"
		if broken != (w.Refusal != "") || (broken && !strings.Contains(w.Refusal, "push twice")) {
			t.Fatalf("only g-broken.yml's on: is refused: %s %q", w.Workflow, w.Refusal)
		}
		if push := eventNamed(w, "push"); w.Workflow == "e-refused.yml" && (push == nil || !strings.Contains(push.Refusal, "branches and branches-ignore")) {
			t.Fatalf("e-refused.yml's push is refused, alone: %+v", w)
		}
	}
	pinCompact(t, "testdata/selection/catalog.json", cat)
}

// the cheat-sheet tables with their compiled tokens; the compiler's own
// matcher agrees with every row before it is pinned
func TestGlobTableIsPinned(t *testing.T) {
	type row struct {
		Text    string   `json:"text"`
		Negated bool     `json:"negated"`
		Tokens  []Token  `json:"tokens"`
		Match   []string `json:"match"`
		Miss    []string `json:"miss"`
	}
	rows := []row{}
	for _, table := range [][]globCase{globBranchCases, globPathCases, globEscapeCases} {
		for _, c := range table {
			p, err := CompileGlob(c.pattern)
			if err != nil {
				t.Fatalf("CompileGlob(%q): %v", c.pattern, err)
			}
			for _, name := range c.match {
				if !p.Match(name) {
					t.Fatalf("%q should match %q", c.pattern, name)
				}
			}
			for _, name := range c.miss {
				if p.Match(name) {
					t.Fatalf("%q should not match %q", c.pattern, name)
				}
			}
			match, miss := append([]string{}, c.match...), append([]string{}, c.miss...)
			rows = append(rows, row{p.Text, p.Negated, p.Tokens, match, miss})
		}
	}
	pinCompact(t, "testdata/selection/globs.json", rows)
}

// the instants the occurrence table starts from: the spring-forward and
// fall-back weeks of New York, London and Lord Howe in 2027, a leap year's
// February, and the last days of the horizon
var occurrenceStarts = []string{
	"2026-10-16T00:00:00Z",
	"2027-03-13T12:00:00Z", "2027-11-06T12:00:00Z",
	"2027-03-27T12:00:00Z", "2027-10-30T12:00:00Z",
	"2027-04-03T00:00:00Z", "2027-10-02T00:00:00Z",
	"2028-02-27T00:00:00Z",
	"2039-12-29T00:00:00Z",
}

// every accepted schedule's next four occurrences from each start (each
// strictly after the one before), or the refusal that ended the run; the
// ship's next-after must find the same instants over the same tables
func TestScheduleVectorIsPinned(t *testing.T) {
	cat := catalogOfDir(t, "testdata/schedules")
	pinCompact(t, "testdata/schedules/schedules.json", cat)
	if len(cat.Workflows) != 1 {
		t.Fatalf("one schedule workflow: %+v", cat.Workflows)
	}
	sched := eventNamed(cat.Workflows[0], "schedule")
	if sched == nil || sched.Refusal != "" || len(sched.Schedules) != 17 {
		t.Fatalf("schedule event: %+v", sched)
	}
	if sched.Schedules[15].Refusal == "" || sched.Schedules[16].Refusal == "" {
		t.Fatalf("the five-minute and unknown-zone entries are refused: %+v", sched.Schedules[15:])
	}
	type row struct {
		Entry   int     `json:"entry"`
		After   int64   `json:"after"`
		Next    []int64 `json:"next"`
		Refusal string  `json:"refusal"`
	}
	rows := []row{}
	for i, s := range sched.Schedules {
		if s.Refusal != "" {
			continue
		}
		var z *Zone
		if s.Timezone != "" {
			zone, ok := cat.Zones[s.Timezone]
			if !ok {
				t.Fatalf("no table for %s", s.Timezone)
			}
			z = &zone
		}
		for _, start := range occurrenceStarts {
			after := utc(start)
			r := row{Entry: i, After: after.Unix(), Next: []int64{}}
			for k := 0; k < 4; k++ {
				next, err := NextOccurrence(s, z, after)
				if err != nil {
					r.Refusal = err.Error()
					break
				}
				r.Next = append(r.Next, next.Unix())
				after = next
			}
			rows = append(rows, r)
		}
	}
	pinCompact(t, "testdata/schedules/occurrences.json", rows)
}
