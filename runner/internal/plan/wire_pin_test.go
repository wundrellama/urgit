package plan

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// the compiled wire of every Delivery 1B fixture workflow, pinned: the
// ship's vectors (desk/gen/ci-expr-vector, ci-plan-vector) embed these
// exact bytes, so a compiler change that alters what the ship receives
// fails here first. `-args -update-wires` rewrites them after a reviewed
// change.
var updateWires = flag.Bool("update-wires", false, "rewrite the pinned fixture wires")

func TestFixtureWiresArePinned(t *testing.T) {
	files, err := filepath.Glob("testdata/d1b/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v %v", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		walked, err := Walk(data)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		type wireJob struct {
			ID      string     `json:"id"`
			Needs   []string   `json:"needs"`
			Cond    *Condition `json:"cond"`
			Outputs []Output   `json:"outputs"`
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		jobs := mappingValue(topMapping(&doc), "jobs")
		var out []wireJob
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			id := jobs.Content[i].Value
			info := walked[id]
			if info.Outputs == nil {
				info.Outputs = []Output{}
			}
			out = append(out, wireJob{ID: id, Needs: info.Needs, Cond: info.Cond, Outputs: info.Outputs})
		}
		got, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		pin := strings.TrimSuffix(file, ".yml") + ".wire.json"
		if *updateWires {
			if err := os.WriteFile(pin, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(pin)
		if err != nil {
			t.Fatalf("%s: %v (-args -update-wires writes it)", pin, err)
		}
		if strings.TrimSuffix(string(want), "\n") != string(got) {
			t.Errorf("%s drifted from its pinned wire:\n got %s\nwant %s", file, got, want)
		}
	}
}
