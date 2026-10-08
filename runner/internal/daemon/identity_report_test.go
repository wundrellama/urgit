package daemon

// Independent review 08, R8-1 (runner/launcher/INTEGRATION.md §11.12, "One
// command, one identity"): the retention report names the command of each
// release as the ship writes it, whatever spelling its record was written
// under — as every answer does — and the record is kept as written.
//
// Through the real Microvm backend and launcher core over private files,
// with the in-process ship of legacy_test.go.

import (
	"context"
	"testing"
	"time"
)

func TestAReleaseIsReportedUnderTheSpellingTheShipWrites(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	e := reportedEntry(t, d)
	closeDaemon(d)
	editState(t, f.cfg.StateFile, func(file map[string]any) {
		entries, _ := file["quarantined"].([]any)
		entry, _ := entries[0].(map[string]any)
		entry["released_at"] = time.Now().Add(-time.Hour).Unix()
		entry["command"] = "0v5.cmd"
		entry["evidence"] = e.Evidence
		file["released"] = []any{entry}
		delete(file, "quarantined")
	})
	d2 := f.start(t)
	defer closeDaemon(d2)
	r := d2.retentionReport(context.Background())
	if len(r.Released) != 1 || r.Released[0].Command != canonicalID {
		t.Fatalf("A RELEASE WAS REPORTED UNDER ANOTHER SPELLING THAN THE SHIP WRITES: %+v", r.Released)
	}
	if gone := released(t, f.cfg.StateFile); len(gone) != 1 || gone[0]["command"] != "0v5.cmd" {
		t.Fatalf("A RECORD UNDER ANOTHER SPELLING WAS REWRITTEN OR DROPPED: %v", gone)
	}
}
