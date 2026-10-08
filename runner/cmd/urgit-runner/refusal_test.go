package main

// Independent review 07, R7-1 (runner/launcher/INTEGRATION.md §11.12, "Every
// final answer is recorded first"): the daemon's recorded refusals of
// recovery commands are in the state file this tool also writes. Its saves
// — a Docker retention's retry and its release — keep every one of them, so
// a command the daemon refused is still refused when the daemon starts
// again.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
)

func TestRecoverKeepsTheDaemonsRecordedRefusals(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	docker := dockerRetention()
	refusal := map[string]any{"command": "0v5.cmd", "selection": "ci-0v4.old/microvm///0/0//1700000000/1", "revision": 1,
		"evidence": strings.Repeat("a", 64), "reason": "its release is not proven now", "at": 1790000000}
	data, err := json.MarshalIndent(map[string]any{"format": 1, "daemon_id": daemonID, "bearer": "b", "ship_url": "http://ship.test",
		"quarantined": []any{map[string]any{"handle": docker.Handle, "reason": docker.Reason, "at": docker.At, "backend": docker.Backend, "attempt": docker.Attempt,
			"network": docker.Network, "volume": docker.Volume, "container": docker.Container, "label": docker.Label}},
		"refused": []any{refusal}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	raw, _ := json.Marshal(refusal)
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	kept := func(step string) {
		t.Helper()
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Refused []map[string]any `json:"refused"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatal(err)
		}
		if len(file.Refused) != 1 || !reflect.DeepEqual(file.Refused[0], want) {
			t.Fatalf("THE OPERATOR'S TOOL DROPPED THE DAEMON'S RECORDED REFUSALS (%s): %v", step, file.Refused)
		}
	}
	fd := &fakeDocker{objects: map[string]bool{"container " + docker.Container: true}}
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", StateFile: statePath}, docker: fd, now: time.Now}
	sel := loaded(t, statePath).Quarantined[0].Selection()
	var out strings.Builder
	if code := rc.act(sel, "retry", &out); code != 0 {
		t.Fatalf("fixture: the retry resolves the Docker retention: exit %d\n%s", code, out.String())
	}
	kept("the retry")
	out.Reset()
	sel = loaded(t, statePath).Quarantined[0].Selection()
	if code := rc.act(sel, "release", &out); code != 0 || len(handles(t, statePath)) != 0 {
		t.Fatalf("fixture: the resolved Docker retention is released: exit %d\n%s", code, out.String())
	}
	kept("the release")
}
