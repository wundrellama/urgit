package plan

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// the needs/if walk on both ERPit workflows: the four job-level gates,
// their needs lists, no matrix anywhere (suite.yml:13 says so)
func TestWalkERPit(t *testing.T) {
	cases := []struct {
		file string
		want map[string]JobInfo
	}{
		{"suite.yml", map[string]JobInfo{
			"plan":       {Needs: []string{}},
			"structural": {Needs: []string{}},
			"suite":      {Needs: []string{"plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "suite", Literal: "true"}},
		}},
		{"fixtures.yml", map[string]JobInfo{
			"pins":    {Needs: []string{}},
			"plan":    {Needs: []string{}},
			"replay":  {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "replay", Literal: "true"}},
			"erasure": {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "erasure", Literal: "true"}},
			"duo":     {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "duo", Literal: "true"}},
		}},
		{"fixture-chain.yml", map[string]JobInfo{
			"a": {Needs: []string{}},
			"b": {Needs: []string{"a"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "a", Output: "go", Literal: "true"}},
		}},
	}
	for _, c := range cases {
		data, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Walk(data)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.file, got, c.want)
		}
	}
}

func TestCompileIf(t *testing.T) {
	cases := map[string]Cond{
		"needs.plan.outputs.suite == 'true'":        {V: 1, Kind: "output-eq", Job: "plan", Output: "suite", Literal: "true"},
		"${{ needs.a.outputs.go == 'yes' }}":        {V: 1, Kind: "output-eq", Job: "a", Output: "go", Literal: "yes"},
		"github.event_name == 'push'":               {V: 1, Kind: "unsupported", Raw: "github.event_name == 'push'"},
		"always()":                                  {V: 1, Kind: "unsupported", Raw: "always()"},
		"needs.a.outputs.go == 'true' && success()": {V: 1, Kind: "unsupported", Raw: "needs.a.outputs.go == 'true' && success()"},
		"needs.a.outputs.go == \"true\"":            {V: 1, Kind: "unsupported", Raw: "needs.a.outputs.go == \"true\""},
	}
	for in, want := range cases {
		if got := CompileIf(in); got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
	// a matrix is flagged, never compiled
	data := []byte("on: [push]\njobs:\n  m:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        n: [1, 2]\n    steps:\n      - run: echo\n")
	walked, err := Walk(data)
	if err != nil || !walked["m"].Matrix {
		t.Fatalf("matrix not flagged: %+v %v", walked, err)
	}
}

func TestPrereqEnv(t *testing.T) {
	got := PrereqEnv(map[string]map[string]string{"plan": {"suite": "true", "run-it": "no"}, "pins": {}})
	want := []string{"NEEDS_PLAN_OUTPUTS_RUN_IT=no", "NEEDS_PLAN_OUTPUTS_SUITE=true"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// CI-PROJECT-1: for each of ERPit's eight jobs, the projection has exactly
// one job, no needs, no job-level if, and the job's subtree is byte-identical
// to the original's minus those lines
func TestProjectERPit(t *testing.T) {
	files := map[string][]string{
		"suite.yml":         {"plan", "structural", "suite"},
		"fixtures.yml":      {"pins", "plan", "replay", "erasure", "duo"},
		"fixture-chain.yml": {"a", "b"},
	}
	for file, jobs := range files {
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range jobs {
			projected, err := Project(data, job)
			if err != nil {
				t.Fatalf("%s %s: %v", file, job, err)
			}
			var doc struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
				On   any                       `yaml:"on"`
				Env  any                       `yaml:"env"`
			}
			if err := yaml.Unmarshal(projected, &doc); err != nil {
				t.Fatalf("%s %s: projection is not YAML: %v\n%s", file, job, err, projected)
			}
			if len(doc.Jobs) != 1 {
				t.Errorf("%s %s: projection has %d jobs", file, job, len(doc.Jobs))
			}
			body, ok := doc.Jobs[job]
			if !ok {
				t.Fatalf("%s %s: projection lost the job", file, job)
			}
			if _, has := body["needs"]; has {
				t.Errorf("%s %s: needs survived", file, job)
			}
			if _, has := body["if"]; has {
				t.Errorf("%s %s: job-level if survived", file, job)
			}
			if body["runs-on"] == nil || body["steps"] == nil {
				t.Errorf("%s %s: runs-on/steps missing", file, job)
			}
			// byte-identity: the original block minus its needs/if lines
			original, err := JobText(data, job, false)
			if err != nil {
				t.Fatal(err)
			}
			var kept []string
			skipping := false
			for _, line := range strings.SplitAfter(original, "\n") {
				trimmed := strings.TrimLeft(line, " ")
				indent := len(line) - len(trimmed)
				if indent == 4 && (strings.HasPrefix(trimmed, "needs:") || strings.HasPrefix(trimmed, "if:")) {
					skipping = true
					continue
				}
				if skipping && indent > 4 && strings.TrimSpace(line) != "" {
					continue
				}
				skipping = false
				kept = append(kept, line)
			}
			want := strings.Join(kept, "")
			got, err := JobText(projected, job, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s %s: projected job subtree differs from the original minus needs/if\n--- want\n%s\n--- got\n%s", file, job, want, got)
			}
			// everything outside the jobs block is the original text
			originalHead := data[:strings.Index(string(data), "\njobs:\n")+1]
			if !strings.HasPrefix(string(projected), string(originalHead)) {
				t.Errorf("%s %s: the text before jobs: changed", file, job)
			}
		}
	}
	if _, err := Project([]byte("on: [push]\njobs:\n  a:\n    runs-on: x\n    steps: []\n"), "zz"); err == nil {
		t.Fatal("projecting a job that does not exist must fail")
	}
}
