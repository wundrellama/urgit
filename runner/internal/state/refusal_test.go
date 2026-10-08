package state

// Independent review 07, R7-1 (runner/launcher/INTEGRATION.md §11.12, "Every
// final answer is recorded first"): the daemon's recorded refusals of
// authenticated recovery commands are part of the state file. Load reads
// them and every Save writes them back, whole, so no save forgets one.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestASaveKeepsTheRecordedRefusals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	refusal := map[string]any{"command": "0v5.cmd", "selection": "ci-0v4.att/microvm///0/0//1700000000/1", "revision": 1,
		"evidence": strings.Repeat("a", 64), "found": strings.Repeat("b", 64), "reason": "its release is not proven now", "at": 1790000000}
	writeRaw(t, path, map[string]any{"format": 1, "daemon_id": "0v1.daemon", "bearer": "0v1.bearer", "ship_url": "http://ship.test", "refused": []any{refusal}})
	st, err := Load(path)
	if err != nil || st == nil {
		t.Fatalf("load: %v", err)
	}
	if err := Save(path, st); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Refused []map[string]any `json:"refused"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	raw, _ := json.Marshal(refusal)
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(back.Refused) != 1 || !reflect.DeepEqual(back.Refused[0], want) {
		t.Fatalf("A SAVE DROPPED THE RECORDED REFUSALS: %v, want %v", back.Refused, want)
	}
}
