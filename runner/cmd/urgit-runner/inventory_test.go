package main

// The runner takes the launcher's list as its proof in two places: the
// release of a microvm retention, and the daemon's first reconcile at
// start. A launcher whose inventory is partial — an entry it could not
// account for, the retention's own record or another — refuses that list
// (runner/launcher/INTEGRATION.md §11.7; independent review 02): the
// release is refused and the retention kept; the start fails, and is not
// called an enrollment loss. Through the real daemon, Microvm backend and
// launcher core, wire and client, the launcher restarted over the same
// private files. With every entry accounted for, the launcher's own
// release proves it, and the daemon starts.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/daemon"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/state"
)

func TestRecoverReleasesNothingOnAPartialInventory(t *testing.T) {
	for _, c := range []struct {
		name     string
		released bool   // the launcher's operator released it first
		spoil    string // the attempt whose launcher record is spoiled ("" none)
		content  string
		allowed  bool
		anon     bool // the retention never learnt its launcher identity: the list is its only proof
	}{
		{"its own launcher record of an unsupported format", false, "0v3.att", `{"format": 99}`, false, false},
		{"another launcher record truncated, its own released", true, "0vother.att", "{", false, false},
		{"identity-less, another launcher record truncated, its own released", true, "0vother.att", "{", false, true},
		{"its own released, every entry accounted for (control)", true, "", "", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := launchertest.NewHost()
			_, digest := launchertest.Image(t, "img")
			lc := launchertest.Config(digest, 2)
			lc.StateDir = t.TempDir()
			srv := launchertest.Serve(t, h, lc)
			rec := quarantined(t, h, srv, digest, "0v3.att")
			q := retention(rec)
			if c.anon {
				q.VM, q.Incarnation, q.CID, q.Created = "", "", 0, 0
			}
			statePath := filepath.Join(t.TempDir(), "state.json")
			saveState(t, statePath, q)
			if c.released {
				launcherRelease(t, srv, rec)
			}
			if c.spoil != "" {
				p := filepath.Join(lc.StateDir, "attempts", launcher.IDFor(lc.IDPrefix, c.spoil)+".json")
				if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			srv.Restart(t)
			if c.spoil != "" && len(srv.Service.Problems()) == 0 {
				t.Fatal("fixture: the spoiled record is unaccounted for")
			}
			rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
			var out strings.Builder
			code := rc.act(q.Selection(), "release", &out)
			left := handles(t, statePath)
			if c.allowed {
				if code != 0 || len(left) != 0 {
					t.Fatalf("the launcher's release, every entry accounted for: exit %d, kept %v\n%s", code, left, out.String())
				}
				return
			}
			if code != 1 || len(left) != 1 {
				t.Fatalf("A RETENTION WAS RELEASED ON A PARTIAL INVENTORY: exit %d, kept %v\n%s", code, left, out.String())
			}
		})
	}
}

func TestStartOnAPartialInventoryIsNoEnrollmentLoss(t *testing.T) {
	for _, partial := range []bool{true, false} {
		name := "a partial inventory"
		if !partial {
			name = "every entry accounted for (control)"
		}
		t.Run(name, func(t *testing.T) {
			h := launchertest.NewHost()
			dir, digest := launchertest.Image(t, "img")
			srv := launchertest.Serve(t, h, launchertest.Config(digest, 2))
			work := t.TempDir()
			cfg := &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, ImagePath: dir, ActImage: "img", ActBinary: "/bin/sh",
				Capacity: 1, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, WorkDir: filepath.Join(work, "work"),
				StateFile: filepath.Join(work, "state", "state.json"), ShipURL: "http://ship.test"}
			if err := state.Save(cfg.StateFile, &state.State{DaemonID: "0v1.daemon", Bearer: "0v1.bearer", ShipURL: cfg.ShipURL}); err != nil {
				t.Fatal(err)
			}
			if partial {
				p := filepath.Join(srv.StateDir(), "attempts", launcher.IDFor("t", "0v8.att")+".json")
				if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				srv.Restart(t)
				if len(srv.Service.Problems()) == 0 {
					t.Fatal("fixture: the spoiled record is unaccounted for")
				}
			}
			var logs strings.Builder
			logger := log.New(&logs, "", 0)
			d, err := daemon.New(context.Background(), cfg, logger)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			status := reconcileAtStart(context.Background(), d, logger)
			if !partial {
				if status != 0 {
					t.Fatalf("every entry accounted for: status %d\n%s", status, logs.String())
				}
				return
			}
			if status == 0 {
				t.Fatalf("A DAEMON STARTED ON A PARTIAL INVENTORY:\n%s", logs.String())
			}
			if status == daemon.ExitEnrollmentLost || strings.Contains(logs.String(), "enrollment lost; re-enroll") {
				t.Fatalf("A PARTIAL INVENTORY WAS CALLED AN ENROLLMENT LOSS: status %d\n%s", status, logs.String())
			}
		})
	}
}
