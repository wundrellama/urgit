package main

// The operator's path for an unsettled admission (runner/launcher/
// INTEGRATION.md §11.10; settled-admission ruling 01): urgit-runner
// -recover releases it only on the launcher's settlement of exactly its
// request — closed or released allow it; an admitted request refuses it and
// names its reservation; an answer that settles nothing refuses it. Its
// inspection settles nothing, and takes no list for an absence; a retry has
// nothing to clean up. Through the launcher's real core, wire and client
// over private files.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/state"
)

func TestRecoverReleasesAnAdmissionOnlyOnItsSettlement(t *testing.T) {
	for _, c := range []struct {
		name     string
		admitted bool   // the launcher admitted the request
		released bool   // and then released its reservation, on its evidence
		spoil    bool   // a record the launcher cannot account for
		socket   string // "" the launcher's; else one nothing serves
		code     int
		says     string
	}{
		{name: "never admitted", code: 0, says: "never admitted its request"},
		{name: "admitted, then released", admitted: true, released: true, code: 0, says: "has been released"},
		{name: "admitted and held", admitted: true, code: 1, says: "was admitted as"},
		{name: "a partial inventory", spoil: true, code: 1, says: "is not settled"},
		{name: "the launcher cannot be asked", socket: "none.sock", code: 1, says: "is not settled"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := launchertest.NewHost()
			_, digest := launchertest.Image(t, "img")
			srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
			request, err := launcher.NewRequest()
			if err != nil {
				t.Fatal(err)
			}
			if c.admitted {
				cl, err := launcher.Dial(context.Background(), srv.Socket, daemonID)
				if err != nil {
					t.Fatal(err)
				}
				r, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v8.att", Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked", Request: request})
				if err == nil && c.released {
					_, err = cl.DestroyOf(r.Ref())
				}
				cl.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if c.spoil {
				if err := os.WriteFile(filepath.Join(srv.StateDir(), "attempts", launcher.IDFor("t", "0v9.att")+".json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				srv.Restart(t)
			}
			statePath := filepath.Join(t.TempDir(), "state.json")
			q := state.Quarantine{Handle: "ci-0v8.att", Reason: "its reserve request was sent; its outcome is not settled yet", At: time.Now().Unix(), Backend: "microvm", Attempt: "0v8.att", Request: request}
			saveState(t, statePath, q)
			socket := srv.Socket
			if c.socket != "" {
				socket = c.socket
			}
			rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: socket, StateFile: statePath}, records: launcherRecords, now: time.Now}
			// inspection settles nothing: the ledger is as it was
			var out strings.Builder
			rc.scripted(q.Selection(), "inspect", &out)
			ledger := filepath.Join(srv.StateDir(), "requests", request+".json")
			if _, err := os.Stat(ledger); !c.admitted && err == nil {
				t.Fatalf("AN INSPECTION SETTLED A REQUEST:\n%s", out.String())
			}
			if !c.admitted && !c.spoil && c.socket == "" && !strings.Contains(out.String(), "a list is no settlement") {
				t.Fatalf("AN INSPECTION TOOK AN EMPTY LIST FOR AN ABSENCE:\n%s", out.String())
			}
			out.Reset()
			if code := rc.act(q.Selection(), "retry", &out); code != 1 || !strings.Contains(out.String(), "nothing to clean up") {
				t.Fatalf("a retry of an admission: exit %d\n%s", code, out.String())
			}
			out.Reset()
			code := rc.act(q.Selection(), "release", &out)
			if code == 0 {
				// released: its request is settled for good — a delayed or
				// replayed copy of its reserve is never admitted after the
				// slot returned
				cl, err := launcher.Dial(context.Background(), srv.Socket, daemonID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = cl.Reserve(launcher.ReserveRequest{Attempt: "0v8.att", Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked", Request: request})
				cl.Close()
				if err == nil || !strings.Contains(err.Error(), launcher.ErrSettled.Error()) {
					t.Fatalf("A RELEASED ADMISSION'S REQUEST COULD STILL BE ADMITTED: %v\n%s", err, out.String())
				}
			}
			st := loaded(t, statePath)
			switch {
			case code != c.code || !strings.Contains(out.String(), c.says):
				t.Fatalf("AN ADMISSION'S RELEASE DID NOT FOLLOW ITS SETTLEMENT: exit %d (want %d)\n%s", code, c.code, out.String())
			case c.code == 0 && (len(st.Quarantined) != 0 || len(st.Released) != 1 || st.Released[0].Request != request):
				t.Fatalf("its release is not recorded as evidence: %+v", st)
			case c.code != 0 && len(st.Quarantined) != 1:
				t.Fatalf("a refused release changed the state file: %+v", st)
			}
		})
	}
}
