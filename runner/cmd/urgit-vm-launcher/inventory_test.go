package main

// The operator's inspection never calls an incident released, and the
// session never calls the state directory free of incidents, while it
// holds an entry the launcher could not account for (INTEGRATION.md §11.7;
// independent review 02): an absence from the records the launcher can
// read proves nothing. Private state; no host command.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoverDoesNotCallAnUnaccountedIncidentReleased(t *testing.T) {
	h := testHost(t, &recorder{})
	q := incident(t, h, "0vunread", "owner/repo · ci.yml · unread", 3, time.Now().Unix())
	sel := selectionFor(q)
	path := filepath.Join(h.cfg.StateDir, "attempts", q.ID+".json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	code := recoverScripted(h, sel.String(), "inspect", &out)
	if code != 1 || strings.Contains(out.String(), "released, or another incarnation") || !strings.Contains(out.String(), "could not be accounted for") {
		t.Fatalf("THE CLI CALLED AN UNACCOUNTED INCIDENT RELEASED: exit %d\n%s", code, out.String())
	}
	out.Reset()
	code = recoverInteractive(h, strings.NewReader("q\n"), &out)
	if code != 1 || strings.Contains(out.String(), "no reservation is quarantined") || !strings.Contains(out.String(), "could not be accounted for") {
		t.Fatalf("THE CLI CALLED A PARTIAL INVENTORY FREE OF INCIDENTS: exit %d\n%s", code, out.String())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{" {
		t.Fatalf("the unaccounted entry was altered: %q %v", data, err)
	}
	// control: with every entry accounted for, an incident no longer there
	// is no incident — policy-dependent (late-accounting ruling 01;
	// INTEGRATION.md §11.8): before it, the CLI said "released, or another
	// incarnation holds the id now"; the record here went without evidence
	// of a release, so it says that now
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := recoverScripted(h, sel.String(), "inspect", &out); code != 1 || !strings.Contains(out.String(), "no evidence of its release is kept") {
		t.Fatalf("THE CLI CALLED AN INCIDENT RELEASED WITHOUT EVIDENCE: exit %d\n%s", code, out.String())
	}
	out.Reset()
	if code := recoverInteractive(h, strings.NewReader("q\n"), &out); code != 0 || !strings.Contains(out.String(), "no incidents: no reservation is quarantined") {
		t.Fatalf("no incident, every entry accounted for: exit %d\n%s", code, out.String())
	}
}
