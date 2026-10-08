package state

// Legacy-replay-upgrade ruling 01 (runner/launcher/INTEGRATION.md §11.15):
// the state file keeps a runner's transition exactly — its epoch, command,
// evidence and time — under "transition"; a file without one loads none,
// and names none.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestATransitionIsKeptInTheStateFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	want := Transition{Epoch: 3, Command: "0v5hist", Evidence: "e", At: 7}
	if err := Save(p, &State{DaemonID: "0v1.d", Bearer: "b", Ledger: true, Transition: &want}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Transition == nil || *got.Transition != want {
		t.Fatalf("A TRANSITION WAS NOT KEPT IN THE STATE FILE: %+v %v", got, err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if tr, _ := raw["transition"].(map[string]any); tr["epoch"] != float64(3) || tr["command"] != "0v5hist" || tr["evidence"] != "e" || tr["at"] != float64(7) {
		t.Fatalf("the state file does not name the transition's fields: %v", raw["transition"])
	}
	q := filepath.Join(t.TempDir(), "state.json")
	if err := Save(q, &State{DaemonID: "0v1.d", Bearer: "b"}); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(q); err != nil || got.Transition != nil {
		t.Fatalf("A STATE FILE WITHOUT A TRANSITION LOADED ONE: %+v %v", got, err)
	}
	if data, _ := os.ReadFile(q); strings.Contains(string(data), "transition") {
		t.Fatalf("a state file without a transition names one: %s", data)
	}
}
