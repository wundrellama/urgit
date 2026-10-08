package main

// The operator's views read the evidence of releases (INTEGRATION.md
// §11.8): an incarnation no longer held is called released only when its
// evidence is kept — its disposition said, a late bookkeeping as such —
// and `list` reports every release whose accounting was confirmed late.
// Private state; no host command.

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

func TestTheOperatorsViewsReadTheEvidence(t *testing.T) {
	h := testHost(t, &recorder{})
	now := time.Now()
	plant := func(attempt, token string, rel launcher.Release) launcher.Record {
		t.Helper()
		ev := interrupted(attempt, launcher.StateReleased)
		ev.Incarnation, ev.HasDisk, ev.HasCgroup, ev.Release = token, false, false, &rel
		data, err := json.MarshalIndent(struct {
			Format int             `json:"format"`
			Record launcher.Record `json:"record"`
		}{1, ev}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(h.cfg.StateDir, "released")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ev.ID+"."+token+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		return ev
	}
	byOperator := plant("0vop", strings.Repeat("a", 32), launcher.Release{By: launcher.ByOperator, Final: launcher.StateReleased, DecidedUnixNano: now.UnixNano(), ConfirmedUnixNano: now.UnixNano()})
	late := plant("0vlate", strings.Repeat("b", 32), launcher.Release{By: launcher.ByTimelyCleanup, Final: launcher.StateDestroyed, DueUnixNano: now.UnixNano(),
		CleanedUnixNano: now.Add(-time.Second).UnixNano(), DecidedUnixNano: now.Add(-time.Second).UnixNano(), ConfirmedUnixNano: now.Add(time.Second).UnixNano(), LateAccounting: true})
	var out strings.Builder
	if code := recoverScripted(h, byOperator.Selection().String(), "inspect", &out); code != 1 || !strings.Contains(out.String(), "released (operator release") || strings.Contains(out.String(), "LATE") {
		t.Fatalf("an operator's release, from its evidence: exit %d\n%s", code, out.String())
	}
	out.Reset()
	if code := recoverScripted(h, late.Selection().String(), "inspect", &out); code != 1 || !strings.Contains(out.String(), "released (timely cleanup; LATE BOOKKEEPING") {
		t.Fatalf("A LATE BOOKKEEPING WAS NOT SAID: exit %d\n%s", code, out.String())
	}
	var logs bytes.Buffer
	if err := list(h.cfg, io.Discard, log.New(&logs, "", 0)); err != nil {
		t.Fatal(err)
	}
	if s := logs.String(); !strings.Contains(s, "RELEASED, ACCOUNTING CONFIRMED LATE "+late.ID) || strings.Contains(s, byOperator.ID) {
		t.Fatalf("THE LIST DID NOT REPORT THE LATE BOOKKEEPING APART:\n%s", s)
	}
}
