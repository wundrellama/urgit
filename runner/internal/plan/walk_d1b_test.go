package plan

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/d1b/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// the walk compiles each job's if where it was written and each declared
// output, in declaration order; nothing else of the YAML is interpreted
func TestWalkCompilesConditionsAndOutputs(t *testing.T) {
	walked, err := Walk(fixture(t, "outputs.yml"))
	if err != nil {
		t.Fatal(err)
	}
	producer := walked["producer"]
	if producer.Cond != nil {
		t.Fatalf("producer has no if: %+v", producer.Cond)
	}
	var names []string
	for _, o := range producer.Outputs {
		if o.Refusal != "" || len(o.Parts) == 0 {
			t.Errorf("output %s: %+v", o.Name, o)
		}
		names = append(names, o.Name)
	}
	if want := []string{"verdict", "first", "second", "empty", "quoted", "json", "composed"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("outputs %v, want %v", names, want)
	}
	if o := producer.Outputs[0]; o.Text != "${{ steps.emit.outputs.raw }}" || o.Line != 11 || o.Col != 16 {
		t.Errorf("verdict: %+v", o)
	}
	if o := producer.Outputs[6]; wire(t, o.Parts) != `[{"lit":"release-"},{"expr":{"k":"prop","of":{"k":"prop","of":{"k":"prop","of":{"k":"ctx","name":"steps"},"name":"one"},"name":"outputs"},"name":"same"}}]` {
		t.Errorf("composed: %s", wire(t, o.Parts))
	}
	consumer := walked["consumer"]
	if c := consumer.Cond; c == nil || c.Refusal != "" || c.Expr == nil || c.Line != 38 || c.Col != 9 || c.Text != "needs.producer.outputs.verdict == 'go' && needs.producer.result == 'success'" {
		t.Fatalf("consumer if: %+v", consumer.Cond)
	}
	if !reflect.DeepEqual(consumer.Needs, []string{"producer"}) {
		t.Fatalf("needs %v", consumer.Needs)
	}
	// every condition of the status and typed fixtures compiles
	for _, file := range []string{"status.yml", "typed.yml", "eval-error.yml"} {
		w, err := Walk(fixture(t, file))
		if err != nil {
			t.Fatal(err)
		}
		for id, info := range w {
			if info.Cond != nil && (info.Cond.Refusal != "" || info.Cond.Expr == nil) {
				t.Errorf("%s %s: %+v", file, id, info.Cond)
			}
		}
	}
	// the refusal fixtures are refused by name, with their place
	for file, want := range map[string]string{
		"refused-context.yml": "context env is not available in a job-level if (github, needs, vars and inputs are)",
		"malformed.yml":       "syntax error at column 15: double quotes are not string delimiters; use single quotes",
		"oversized.yml":       "the expression is 5036 bytes; the bound is 4096",
	} {
		w, err := Walk(fixture(t, file))
		if err != nil {
			t.Fatal(err)
		}
		c := w["deploy"].Cond
		if c == nil || c.Expr != nil || c.Refusal != want || c.Line != 6 || c.Col != 9 {
			t.Errorf("%s: %+v", file, c)
		}
	}
	// an empty if is no condition, as act reads it
	if w, _ := Walk([]byte("on: push\njobs:\n  a:\n    if:\n    runs-on: x\n    steps: []\n")); w["a"].Cond != nil {
		t.Errorf("an empty if: %+v", w["a"].Cond)
	}
}

// the consumer's projection: no upstream job, no needs, no job-level if,
// and every needs root of its expressions reads the signed carrier as
// data; every other value of the job is exactly the original's
func TestProjectCarriesTheNeedsContext(t *testing.T) {
	data := fixture(t, "outputs.yml")
	p, err := ProjectJob(data, "consumer", "0v1.att", "outputs.yml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Needs != 11 {
		t.Errorf("%d needs roots carried, want 11", p.Needs)
	}
	var doc struct {
		Name string                    `yaml:"name"`
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(p.Data, &doc); err != nil {
		t.Fatalf("projection is not YAML: %v\n%s", err, p.Data)
	}
	if len(doc.Jobs) != 1 || doc.Name != "Outputs across jobs" {
		t.Fatalf("jobs %v name %q", len(doc.Jobs), doc.Name)
	}
	job := doc.Jobs["consumer"]
	for _, gone := range []string{"needs", "if"} {
		if _, has := job[gone]; has {
			t.Errorf("%s survived", gone)
		}
	}
	if job["name"] != "0v1.att/consumer" {
		t.Errorf("run name %v", job["name"])
	}
	steps := job["steps"].([]any)
	first := steps[0].(map[string]any)
	env := first["env"].(map[string]any)
	for key, want := range map[string]string{
		"VERDICT":         "${{ fromJSON(vars.URGIT_NEEDS).producer.outputs.verdict }}",
		"EMPTY_JSON":      "${{ toJSON(fromJSON(vars.URGIT_NEEDS).producer.outputs.empty) }}",
		"JSON_N":          "${{ fromJSON(fromJSON(vars.URGIT_NEEDS).producer.outputs.json).n }}",
		"UNDECLARED_JSON": "${{ toJSON(fromJSON(vars.URGIT_NEEDS).producer.outputs.undeclared) }}",
		"RESULT":          "${{ fromJSON(vars.URGIT_NEEDS).producer.result }}",
	} {
		if env[key] != want {
			t.Errorf("%s: %q, want %q", key, env[key], want)
		}
	}
	original := "printf 'verdict=%s first=%s second=%s empty=%s json.n=%s undeclared=%s composed=%s result=%s\\n' \"$VERDICT\" \"$FIRST\" \"$SECOND\" \"$EMPTY_JSON\" \"$JSON_N\" \"$UNDECLARED_JSON\" \"$COMPOSED\" \"$RESULT\"\nprintf 'quoted=%s\\n' \"$QUOTED\"\ntest \"$VERDICT\" = go && test \"$FIRST\" = from-one && test \"$SECOND\" = from-two\n"
	if first["run"] != original {
		t.Errorf("a script that reads no expression is changed:\n%q", first["run"])
	}
	second := steps[1].(map[string]any)
	if second["if"] != "fromJSON(vars.URGIT_NEEDS).producer.outputs.first == 'from-one'" || second["run"] != "echo \"step if saw first=${{ fromJSON(vars.URGIT_NEEDS).producer.outputs.first }}\"" {
		t.Errorf("step if/run: %q %q", second["if"], second["run"])
	}
	// the workflow's own text before jobs: is the original's, byte for byte
	head := func(b []byte) string { return string(b[:strings.Index(string(b), "\njobs:\n")+1]) }
	if head(p.Data) != head(data) {
		t.Errorf("the text before jobs: changed")
	}
	// a job that reads no needs keeps the byte-for-byte projection
	plain, err := ProjectJob(data, "producer", "0v1.att", "outputs.yml", nil)
	if err != nil || plain.Needs != 0 {
		t.Fatalf("producer: %v %d", err, plain.Needs)
	}
	if same, _ := Project(data, "producer", "0v1.att", "outputs.yml"); string(same) != string(plain.Data) {
		t.Errorf("Project and ProjectJob disagree on a job without needs")
	}
}

// a locked image is renamed in the carrying projection as in the
// byte-for-byte one, and an expression that reads needs but cannot be
// read refuses the projection rather than running without its context
func TestProjectCarriesNeedsWithImagesAndRefusesUnreadable(t *testing.T) {
	src := "name: w\non: push\njobs:\n  a:\n    runs-on: x\n    steps: [{run: true}]\n  b:\n    needs: a\n    runs-on: ubuntu-latest\n    container: alpine:3.19\n    steps:\n      - run: echo ${{ needs.a.result }}\n"
	p, err := ProjectJob([]byte(src), "b", "0v2", "w.yml", map[string]string{"alpine:3.19": "urgit-locked/alpine:sha256-bbbb"})
	if err != nil || p.Needs != 1 {
		t.Fatalf("%v %d", err, p.Needs)
	}
	if !strings.Contains(string(p.Data), "container: urgit-locked/alpine:sha256-bbbb") || strings.Contains(string(p.Data), "\n  a:") {
		t.Fatalf("projection:\n%s", p.Data)
	}
	bad := strings.Replace(src, "${{ needs.a.result }}", "${{ needs.a.result", 1)
	if _, err := ProjectJob([]byte(bad), "b", "0v2", "w.yml", nil); err == nil {
		t.Fatal("an unreadable needs expression must refuse the projection")
	}
}
