package main

// The runner's release of a microvm retention takes the launcher's durable
// evidence of exactly its incarnation's release as its only proof
// (runner/launcher/INTEGRATION.md §11.8): a record the launcher no longer
// holds, gone without evidence (the fixture's own file removed between two
// launcher lives), proves nothing, and the retention is kept; one the
// launcher's operator released is proven, and its inspection shows the
// disposition — a late bookkeeping said as such. Through the real launcher
// core, wire and client.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

func TestRecoverReleasesOnlyOnTheLaunchersEvidence(t *testing.T) {
	for _, c := range []struct {
		name    string
		how     string // gone | operator | late
		allowed bool
		shows   string
	}{
		{"its launcher record gone without evidence", "gone", false, "its release is not proven"},
		{"released by the launcher's operator (control)", "operator", true, "released by the launcher, on its evidence: operator release"},
		{"released on timely cleanup, its accounting late", "late", true, "LATE BOOKKEEPING"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := launchertest.NewHost()
			_, digest := launchertest.Image(t, "img")
			lc := launchertest.Config(digest, 2)
			lc.StateDir = t.TempDir()
			srv := launchertest.Serve(t, h, lc)
			rec := quarantined(t, h, srv, digest, "0v3.att")
			q := retention(rec)
			statePath := filepath.Join(t.TempDir(), "state.json")
			saveState(t, statePath, q)
			switch c.how {
			case "gone":
				if err := os.Remove(filepath.Join(lc.StateDir, "attempts", rec.ID+".json")); err != nil {
					t.Fatal(err)
				}
				srv.Restart(t)
			case "operator":
				launcherRelease(t, srv, rec)
			case "late":
				// the launcher's evidence of a timely cleanup whose accounting
				// was confirmed late (its making is the launcher's own tests')
				launcherRelease(t, srv, rec)
				ev := rec
				now := time.Now()
				ev.State, ev.Incident = launcher.StateReleased, nil
				ev.Release = &launcher.Release{By: launcher.ByTimelyCleanup, Final: launcher.StateDestroyed, DueUnixNano: now.UnixNano(), CleanedUnixNano: now.Add(-time.Second).UnixNano(),
					DecidedUnixNano: now.Add(-time.Second).UnixNano(), ConfirmedUnixNano: now.Add(time.Second).UnixNano(), LateAccounting: true}
				data, err := json.MarshalIndent(struct {
					Format int             `json:"format"`
					Record launcher.Record `json:"record"`
				}{1, ev}, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(lc.StateDir, "released", rec.ID+"."+rec.Incarnation+".json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
			var out strings.Builder
			if code := rc.scripted(q.Selection(), "inspect", &out); code != 0 || !strings.Contains(out.String(), c.shows) {
				if !c.allowed {
					t.Fatalf("A RETENTION WAS RELEASED ON AN ABSENCE: its inspection: exit %d\n%s", code, out.String())
				}
				t.Fatalf("its inspection: exit %d\n%s", code, out.String())
			}
			out.Reset()
			code := rc.act(q.Selection(), "release", &out)
			left := handles(t, statePath)
			if c.allowed {
				if code != 0 || len(left) != 0 {
					t.Fatalf("a release the launcher proves: exit %d, kept %v\n%s", code, left, out.String())
				}
				return
			}
			if code != 1 || len(left) != 1 {
				t.Fatalf("A RETENTION WAS RELEASED ON AN ABSENCE: exit %d, kept %v\n%s", code, left, out.String())
			}
		})
	}
}
