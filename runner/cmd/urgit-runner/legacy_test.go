package main

// Legacy-recovery UI ruling 01 (QUESTIONS-SOURCE-01 §11; runner/launcher/
// INTEGRATION.md §11.12): a legacy retention is released from Urgit, by the
// running daemon, never by this tool — the approved origin is the UI, and a
// release here needs the daemon stopped. Its inspection shows the entry's
// provenance and says where its release comes from; a save of an older state
// file keeps the marks its loading set.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher/launchertest"
)

// writePreRuling writes path as a runner before settled admission left it:
// no format stamp, and entries as that runner recorded them.
func writePreRuling(t *testing.T, path string, entries ...map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{"daemon_id": daemonID, "bearer": "b", "ship_url": "http://ship.test", "quarantined": entries}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func legacyEntry(attempt string) map[string]any {
	return map[string]any{"handle": "ci-" + attempt, "reason": "sandbox prepare failed and could not be rolled back: reserve: launcher: connection reset by peer",
		"at": time.Now().Add(-48 * time.Hour).Unix(), "backend": "microvm", "attempt": attempt, "label": "owner/repo · ci.yml · build"}
}

func TestRecoverPointsALegacyRetentionToUrgit(t *testing.T) {
	h := launchertest.NewHost()
	_, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	statePath := filepath.Join(t.TempDir(), "state.json")
	writePreRuling(t, statePath, legacyEntry("0v4.old"))
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
	sel := loaded(t, statePath).Quarantined[0].Selection()
	var out strings.Builder
	// its provenance line, not the handle's text: the mark an earlier
	// runner's file gave it
	if code := rc.scripted(sel, "inspect", &out); code != 0 || !strings.Contains(out.String(), "provenance") || !strings.Contains(out.String(), "earlier runner") {
		t.Fatalf("A LEGACY RETENTION'S INSPECTION DOES NOT SHOW ITS PROVENANCE: exit %d\n%s", code, out.String())
	}
	out.Reset()
	// the launcher holds nothing of its attempt, and still this tool refuses
	code := rc.act(sel, "release", &out)
	if code != 1 || len(handles(t, statePath)) != 1 {
		t.Fatalf("THE OPERATOR'S TOOL RELEASED A LEGACY RETENTION: exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "Urgit") {
		t.Fatalf("A LEGACY RETENTION'S REFUSAL DOES NOT POINT TO URGIT:\n%s", out.String())
	}
}

// The tool's save of an older file (a Docker retention's retry beside a
// legacy retention) stamps the file and keeps the legacy mark: the mark is
// never lost to a save that did not set it.
func TestRecoverSavesAnOlderFileWithItsLegacyMarks(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	docker := dockerRetention()
	writePreRuling(t, statePath, legacyEntry("0v4.old"), map[string]any{"handle": docker.Handle, "reason": docker.Reason, "at": docker.At,
		"backend": docker.Backend, "attempt": docker.Attempt, "network": docker.Network, "volume": docker.Volume, "container": docker.Container, "label": docker.Label})
	fd := &fakeDocker{objects: map[string]bool{"container " + docker.Container: true}}
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", StateFile: statePath}, docker: fd, now: time.Now}
	var dockerSel string
	for _, q := range loaded(t, statePath).Quarantined {
		if q.Handle == docker.Handle {
			dockerSel = q.Selection()
		}
	}
	var out strings.Builder
	if code := rc.act(dockerSel, "retry", &out); code != 0 {
		t.Fatalf("fixture: the retry resolves the Docker retention: exit %d\n%s", code, out.String())
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Format      int              `json:"format"`
		Quarantined []map[string]any `json:"quarantined"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	marked := false
	for _, e := range file.Quarantined {
		if e["handle"] == "ci-0v4.old" && e["legacy"] != nil {
			marked = true
		}
	}
	if file.Format != 1 || !marked {
		t.Fatalf("THE OPERATOR'S TOOL SAVED A PRE-RULING FILE WITHOUT ITS LEGACY MARKS: format %d, entries %v", file.Format, file.Quarantined)
	}
}
