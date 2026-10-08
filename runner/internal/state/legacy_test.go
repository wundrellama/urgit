package state

// The legacy mark (legacy-recovery UI ruling 01; runner/launcher/
// INTEGRATION.md §11.12): Load marks, in a file an earlier runner wrote
// last, exactly the entries of the legacy shape — a microvm slot naming
// neither a launcher identity nor a reserve request — and every save by
// this version stamps the file, so a file of this version is never marked.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeRaw(t *testing.T, path string, file map[string]any) {
	t.Helper()
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMarksOnlyAnEarlierRunnersLegacyShapedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	shaped := map[string]any{"handle": "ci-a", "reason": "lost", "at": 1, "backend": "microvm", "attempt": "a"}
	entries := []any{
		shaped,
		map[string]any{"handle": "ci-b", "reason": "teardown failed", "at": 2, "backend": "microvm", "attempt": "b", "vm": "t-b", "incarnation": "i"},
		map[string]any{"handle": "ci-c", "reason": "admitted?", "at": 3, "backend": "microvm", "attempt": "c", "request": "r"},
		map[string]any{"handle": "ci-d", "reason": "teardown failed", "at": 4},
		map[string]any{"handle": "ci-e", "reason": "teardown failed", "at": 5, "backend": "docker-rootless", "attempt": "e", "network": "ci-e"},
	}
	writeRaw(t, path, map[string]any{"daemon_id": "0v1", "bearer": "b", "quarantined": entries})
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Upgraded() || st.Format != CurrentFormat {
		t.Fatalf("an earlier runner's file loaded as this version's: upgraded %v, format %d", st.Upgraded(), st.Format)
	}
	for _, q := range st.Quarantined {
		want := q.Handle == "ci-a"
		if (q.Legacy != nil) != want || q.IsLegacy() != want {
			t.Fatalf("ONLY THE LEGACY SHAPE IS MARKED: %s marked %v, legacy %v", q.Handle, q.Legacy != nil, q.IsLegacy())
		}
		if want && (q.Legacy.Format != 0 || q.Legacy.Found == 0 || q.Rev != 1) {
			t.Fatalf("the mark names the file's format and when it was found, and moves the revision: %+v rev %d", q.Legacy, q.Rev)
		}
	}
	// saved by this version: stamped, the mark kept, nothing marked again
	if err := Save(path, st); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Upgraded() || again.Quarantined[0].Legacy == nil || again.Quarantined[0].Rev != 1 {
		t.Fatalf("a saved file was upgraded again or lost its mark: upgraded %v, %+v", again.Upgraded(), again.Quarantined[0])
	}
	// the same shape in a file this version wrote is not legacy
	path2 := filepath.Join(t.TempDir(), "state.json")
	writeRaw(t, path2, map[string]any{"format": CurrentFormat, "daemon_id": "0v1", "bearer": "b", "quarantined": []any{shaped}})
	current, err := Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if current.Quarantined[0].Legacy != nil || current.Quarantined[0].IsLegacy() || current.Quarantined[0].Rev != 0 {
		t.Fatalf("A LEGACY-SHAPED ENTRY OF THIS VERSION'S FILE WAS MARKED: %+v", current.Quarantined[0])
	}
	// the shape alone is never legacy, and the mark alone neither
	marked := Quarantine{Handle: "ci-f", Backend: "microvm", VM: "t-f", Legacy: &Legacy{}}
	if marked.IsLegacy() || (Quarantine{Handle: "ci-g", Backend: "microvm"}).IsLegacy() {
		t.Fatalf("A RETENTION WAS TAKEN FOR LEGACY WITHOUT BOTH ITS MARK AND ITS SHAPE")
	}
}

// Every save stamps this version's format, whatever the State said.
func TestSaveStampsTheFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Save(path, &State{DaemonID: "0v1", Bearer: "b"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file["format"] != float64(CurrentFormat) {
		t.Fatalf("A SAVE BY THIS VERSION CARRIES NO FORMAT STAMP: %v", file["format"])
	}
}
