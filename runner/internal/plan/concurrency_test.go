package plan

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var erpitCapture = flag.String("concurrency-erpit-capture", "", "directory for compiled ERPit concurrency")

func TestWorkflowConcurrencyCompiler(t *testing.T) {
	for _, tc := range []struct {
		yaml    string
		group   bool
		cancel  bool
		refusal string
	}{
		{"", false, false, ""},
		{"concurrency: build-${{ github.ref }}\n", true, false, ""},
		{"concurrency: {group: build, cancel-in-progress: true}\n", true, true, ""},
		{"concurrency: {group: '${{ inputs.name || vars.fallback }}', cancel-in-progress: '${{ github.ref != ''refs/heads/master'' }}'}\n", true, true, ""},
		{"concurrency: {cancel-in-progress: false}\n", false, false, "group must"},
		{"concurrency: {group: x, cancel-in-progress: yes}\n", true, false, "boolean or expression"},
		{"concurrency: '${{ needs.a.result }}'\n", true, false, "only github"},
		{"concurrency: {group: x, cancel-in-progress: '${{ always() }}'}\n", true, true, "status functions"},
		{"concurrency: '${{ github.unavailable }}'\n", true, false, "not available"},
		{"concurrency: '${{ github.ref ' \n", true, false, "not closed"},
		{"concurrency: null\n", false, false, "string or expression"},
	} {
		t.Run(tc.yaml, func(t *testing.T) {
			c := ConcurrencyOf([]byte("name: example\non: workflow_dispatch\n"+tc.yaml+"jobs:\n  build:\n    steps: []\n"), "x.yml").Concurrency
			if (c.Group != nil) != tc.group || (c.CancelInProgress != nil) != tc.cancel {
				t.Fatalf("fields: %+v", c)
			}
			if tc.refusal == "" && c.Refusal != "" || tc.refusal != "" && !strings.Contains(c.Refusal, tc.refusal) {
				t.Fatalf("refusal %q want %q", c.Refusal, tc.refusal)
			}
		})
	}
}

func TestConcurrencyAllCatalogSourcesAndJobRefusal(t *testing.T) {
	data := []byte("name: Release\non: [push, workflow_dispatch]\nconcurrency: {group: release, cancel-in-progress: true}\njobs:\n  build:\n    concurrency: ignored-before\n    steps: []\n")
	tr := TriggersOf(data, "release.yml")
	d, err := DispatchOf(data, "release.yml")
	if err != nil {
		t.Fatal(err)
	}
	w, err := Walk(data)
	if err != nil {
		t.Fatal(err)
	}
	if !w["build"].Concurrency || !strings.Contains(d.Refusal, "job-level concurrency") {
		t.Fatalf("walk/inventory lost refusal: %+v %+v", w, d)
	}
	for _, c := range []Concurrency{tr.Concurrency, d.Concurrency} {
		if c.Group == nil || c.CancelInProgress == nil || c.Refusal != "" {
			t.Fatalf("lost declaration: %+v", c)
		}
	}
	wire, _ := json.Marshal(TriggersOf([]byte("on: push\njobs: {}\n"), "old.yml"))
	if !strings.Contains(string(wire), `"concurrency":{"group":null,"cancelInProgress":null,"refusal":""}`) {
		t.Fatalf("known none lost on wire: %s", wire)
	}
}

// These bytes are the supplied real workload, not an invented approximation.
func TestERPiTExactConcurrencyCompiles(t *testing.T) {
	const group = "${{ github.workflow }}-${{ github.ref }}-${{ github.ref == 'refs/heads/master' && github.sha || 'head' }}"
	const cancel = "${{ github.ref != 'refs/heads/master' }}"
	for _, file := range []string{"suite.yml", "fixtures.yml"} {
		// Existing testdata copies are byte-identical to the supplied references.
		data, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		c := ConcurrencyOf(data, file).Concurrency
		if *erpitCapture != "" {
			raw, err := json.Marshal(ConcurrencyOf(data, file))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(*erpitCapture, file+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if c.Refusal != "" || c.Group == nil || c.CancelInProgress == nil {
			t.Fatalf("%s: %+v", file, c)
		}
		if c.Group.Text != group || c.CancelInProgress.Text != cancel {
			t.Fatalf("workload changed: %+v", c)
		}
		if len(c.Group.Parts) != 5 || c.Group.Parts[4].Expr.K != "or" {
			t.Fatalf("interpolation lost: %+v", c.Group)
		}
	}
}
