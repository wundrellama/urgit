package plan

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// the needs/if walk on both ERPit workflows: the four job-level gates,
// their needs lists, no matrix anywhere (suite.yml:13 says so); every
// ERPit job runs on ubuntu-latest and declares its timeout-minutes (P3:
// the ship matches the label and bounds the deadline with the timeout)
func TestWalkERPit(t *testing.T) {
	ubuntu := []string{"ubuntu-latest"}
	cases := []struct {
		file string
		want map[string]JobInfo
	}{
		{"suite.yml", map[string]JobInfo{
			"plan":       {Needs: []string{}, RunsOn: ubuntu, TimeoutMinutes: 5},
			"structural": {Needs: []string{}, RunsOn: ubuntu, TimeoutMinutes: 10},
			"suite":      {Needs: []string{"plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "suite", Literal: "true"}, RunsOn: ubuntu, TimeoutMinutes: 45},
		}},
		{"fixtures.yml", map[string]JobInfo{
			"pins":    {Needs: []string{}, RunsOn: ubuntu, TimeoutMinutes: 5},
			"plan":    {Needs: []string{}, RunsOn: ubuntu, TimeoutMinutes: 5},
			"replay":  {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "replay", Literal: "true"}, RunsOn: ubuntu, TimeoutMinutes: 60},
			"erasure": {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "erasure", Literal: "true"}, RunsOn: ubuntu, TimeoutMinutes: 60},
			"duo":     {Needs: []string{"pins", "plan"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "plan", Output: "duo", Literal: "true"}, RunsOn: ubuntu, TimeoutMinutes: 60},
		}},
		{"fixture-chain.yml", map[string]JobInfo{
			"a": {Needs: []string{}, RunsOn: ubuntu},
			"b": {Needs: []string{"a"}, Cond: &Cond{V: 1, Kind: "output-eq", Job: "a", Output: "go", Literal: "true"}, RunsOn: ubuntu},
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

// runs-on as a list, and as an expression the ship will find no daemon for
func TestWalkRunsOnList(t *testing.T) {
	data := []byte("on: push\njobs:\n  big:\n    runs-on: [self-hosted, big-mem]\n    timeout-minutes: 3\n    steps: [{run: true}]\n  expr:\n    runs-on: ${{ matrix.os }}\n    steps: [{run: true}]\n")
	got, err := Walk(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"self-hosted", "big-mem"}; !reflect.DeepEqual(got["big"].RunsOn, want) || got["big"].TimeoutMinutes != 3 {
		t.Errorf("big: got %+v", got["big"])
	}
	if want := []string{"${{ matrix.os }}"}; !reflect.DeepEqual(got["expr"].RunsOn, want) {
		t.Errorf("expr: got %+v", got["expr"])
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
			projected, err := Project(data, job, "0v1.att", file)
			if err != nil {
				t.Fatalf("%s %s: %v", file, job, err)
			}
			var doc struct {
				Name string                    `yaml:"name"`
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
			// CI-PROJECT-1.1: the name is prefixed with the attempt id; every
			// other top-level line before jobs: is the original text
			var originalDoc struct {
				Name string `yaml:"name"`
			}
			_ = yaml.Unmarshal(data, &originalDoc)
			if doc.Name != "0v1.att/"+originalDoc.Name {
				t.Errorf("%s %s: projected name %q, want %q", file, job, doc.Name, "0v1.att/"+originalDoc.Name)
			}
			originalHead := string(data[:strings.Index(string(data), "\njobs:\n")+1])
			projectedHead := string(projected[:strings.Index(string(projected), "\njobs:\n")+1])
			wantHead := strings.Replace(originalHead, "name: "+originalDoc.Name+"\n", "name: 0v1.att/"+originalDoc.Name+"\n", 1)
			if projectedHead != wantHead {
				t.Errorf("%s %s: the text before jobs: changed beyond the name line\n--- want\n%s\n--- got\n%s", file, job, wantHead, projectedHead)
			}
		}
	}
	if _, err := Project([]byte("on: [push]\njobs:\n  a:\n    runs-on: x\n    steps: []\n"), "zz", "0v1", "w.yml"); err == nil {
		t.Fatal("projecting a job that does not exist must fail")
	}
	// a workflow with no name: act would use the file name, so the
	// projection inserts one from it
	unnamed, err := Project([]byte("on: [push]\njobs:\n  a:\n    runs-on: x\n    steps: []\n"), "a", "0v1", "w.yml")
	if err != nil || !strings.HasPrefix(string(unnamed), "name: 0v1/w.yml\non: [push]\n") {
		t.Fatalf("unnamed workflow projection: %q %v", unnamed, err)
	}
	if ProjectionName("0v9", "suite") != "0v9/suite" {
		t.Fatal("ProjectionName")
	}
}

// a job's environment name is walked in both YAML shapes (D4: %env
// credentials are released by it) and absent when the job names none
func TestWalkEnvironment(t *testing.T) {
	doc := []byte("name: envs\non: [push]\njobs:\n  plain:\n    runs-on: ubuntu-latest\n    steps: []\n  short:\n    runs-on: ubuntu-latest\n    environment: staging\n    steps: []\n  long:\n    runs-on: ubuntu-latest\n    environment:\n      name: production\n      url: https://example\n    steps: []\n")
	walked, err := Walk(doc)
	if err != nil {
		t.Fatal(err)
	}
	if walked["plain"].Environment != "" || walked["short"].Environment != "staging" || walked["long"].Environment != "production" {
		t.Fatalf("%+v", walked)
	}
}
