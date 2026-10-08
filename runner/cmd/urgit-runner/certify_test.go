package main

// The runner's release of a microvm retention takes the launcher's list as
// its proof. A launcher that answers but refuses that list — as a reopened
// launcher whose records directory is not certified does (runner/
// launcher/INTEGRATION.md §11.6) — proves nothing: the release is refused
// and the retention kept. The launcher here is a scripted reply on a fresh
// private unix socket; the real launcher's refusal is the launcher
// package's TestReopenCertifiesTheRecordsDirectory.

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/state"
)

// refusingLauncher answers hello, and refuses every other request with
// refusal.
func refusingLauncher(t *testing.T, path, refusal string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req launcher.Request
					if json.Unmarshal(line, &req) != nil {
						return
					}
					reply := launcher.Reply{Error: refusal}
					if req.Op == "hello" {
						reply = launcher.Reply{OK: true, Launcher: "scripted", Protocol: launcher.WireProtocol}
					}
					data, _ := json.Marshal(reply)
					if _, err := c.Write(append(data, '\n')); err != nil {
						return
					}
				}
			}(c)
		}
	}()
}

func TestRecoverReleasesNothingOnARefusedList(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Chdir(t.TempDir()) // a short, private socket path
	refusal := "launcher: state change is not durable: the records directory is not certified (input/output error), so a record's absence from the list is no proof of a release"
	refusingLauncher(t, "l.sock", refusal)
	q := retention(launcher.Record{ID: "t-0v9.att", Incarnation: strings.Repeat("a", 32), Attempt: "0v9.att", CID: 3, Created: time.Now().Unix(), Label: "owner/repo · ci.yml · refused"})
	// and one whose launcher identity was never learnt: the list is its
	// only proof (late-accounting ruling 01 made a known one's the evidence)
	anon := state.Quarantine{Handle: "ci-0v8.att", Reason: "reserve answer lost", At: time.Now().Unix(), Backend: "microvm", Attempt: "0v8.att"}
	saveState(t, statePath, q, anon)
	rc := recovery{cfg: &config.Config{Sandbox: "microvm", LauncherSocket: "l.sock", StateFile: statePath}, records: launcherRecords, now: time.Now}
	for _, r := range []state.Quarantine{anon, q} {
		var out strings.Builder
		if code := rc.act(r.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 2 || !strings.Contains(out.String(), "not certified") {
			t.Fatalf("A RETENTION WAS RELEASED ON A REFUSED LIST: %s: exit %d, kept %v\n%s", r.Handle, code, handles(t, statePath), out.String())
		}
	}
}
