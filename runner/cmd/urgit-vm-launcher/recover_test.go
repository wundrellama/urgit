package main

// The operator's incident tooling (recovery ruling A; INTEGRATION.md §8.4)
// through its entry points — the interactive session and the scripted
// selection — over the real host adapter on private roots: files only, no
// host command, and, where a VMM is looked for and stopped, only this test
// binary's own inert children.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// incident writes a quarantined record of attempt that holds its disk (a
// file in its private jail) and its cgroup, with label and an incarnation
// token (INTEGRATION.md §11.1).
func incident(t *testing.T, h *realHost, attempt, label string, cid uint32, created int64) launcher.Record {
	t.Helper()
	r := interrupted(attempt, launcher.StateQuarantined)
	r.Label, r.CID, r.Created, r.Updated = label, cid, created, created
	r.Incarnation = fmt.Sprintf("%032x", uint64(cid)*1000003+uint64(created))
	r.Reason = "cleanup failed: disk busy"
	writeRecord(t, h.cfg.StateDir, r)
	disk := filepath.Join(h.jailRoot(r.ID), "disk.ext4")
	if err := os.MkdirAll(filepath.Dir(disk), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

func evidenceOf(h *realHost, r launcher.Record) string {
	if r.Incarnation != "" {
		return filepath.Join(h.cfg.StateDir, "released", fmt.Sprintf("%s.%s.json", r.ID, r.Incarnation))
	}
	return filepath.Join(h.cfg.StateDir, "released", fmt.Sprintf("%s.%d.%d.json", r.ID, r.CID, r.Created))
}

func selectionFor(r launcher.Record) launcher.Selection {
	return r.Selection()
}

// The session lists the incidents by job label with their exact
// incarnations, shows the one selected in full, refuses a release that is
// not resolved, retries its cleanup without releasing it, does not release
// it — resolved now — on an answer other than "release", and releases it,
// confirmed, on the incarnation and revision shown; the other incident is
// not touched.
func TestRecoverSessionListsSelectsRetriesAndReleases(t *testing.T) {
	rec := &recorder{}
	h := testHost(t, rec)
	now := time.Now().Unix()
	a := incident(t, h, "0va", "owner/repo · ci.yml · lint", 3, now)
	b := incident(t, h, "0vb", "owner/repo · ci.yml · build", 4, now+1)
	var out strings.Builder
	code := recoverInteractive(h, strings.NewReader("2\nR\nrelease\nr\nR\nnope\nR\nrelease\nq\n"), &out)
	text := out.String()
	for _, want := range []string{
		"[1] owner/repo · ci.yml · lint", "[2] owner/repo · ci.yml · build",
		fmt.Sprintf("%s · incarnation %s · cid %d", b.ID, b.Incarnation, b.CID), fmt.Sprintf("select      %s/%s/%d/%d/", b.ID, b.Incarnation, b.CID, b.Created),
		"release     refused: it still holds cgroup, disk", "NOT RELEASED", "cleanup retried",
		"release     allowed", "not released", "released " + b.ID,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("THE SESSION DOES NOT SHOW %q (exit %d):\n%s", want, code, text)
		}
	}
	if i, j := strings.Index(text, "release     allowed"), strings.Index(text, "not released"); j < i {
		t.Fatalf("the unconfirmed release was not asked while the incident was releasable:\n%s", text)
	}
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, text)
	}
	snap, err := launcher.ReadState(h.cfg.StateDir, "t")
	if err != nil || len(snap.Records) != 1 || snap.Records[0].ID != a.ID || snap.Records[0].State != launcher.StateQuarantined {
		t.Fatalf("after the session: %+v %v", snap, err)
	}
	if _, err := os.Stat(evidenceOf(h, b)); err != nil {
		t.Fatalf("the release kept no evidence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.jailRoot(a.ID), "disk.ext4")); err != nil {
		t.Fatalf("A RECOVERY TOUCHED ANOTHER INCIDENT: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("the session executed host commands: %v", rec.calls)
	}
}

// The scripted form takes the same checks and no shortcut: a malformed
// selection, a release of an incident that still holds something, a
// selection a retry has outdated, another incarnation's, and a replay
// are all refused; inspection says when the selection is stale.
func TestRecoverScriptedHoldsTheSelectionExact(t *testing.T) {
	h := testHost(t, &recorder{})
	q := incident(t, h, "0vq", "owner/repo · ci.yml · test", 3, time.Now().Unix())
	sel := selectionFor(q)
	var out strings.Builder
	if code := recoverScripted(h, "not-a-selection", "release", &out); code != 2 {
		t.Fatalf("a malformed selection: exit %d", code)
	}
	if code := recoverScripted(h, sel.String(), "release", &out); code != 1 {
		t.Fatalf("A RELEASE OF AN UNRESOLVED INCIDENT WAS NOT REFUSED: exit %d\n%s", code, out.String())
	}
	if code := recoverScripted(h, sel.String(), "retry", &out); code != 0 {
		t.Fatalf("retry: exit %d\n%s", code, out.String())
	}
	now, ok, err := lookup(h.cfg, sel)
	if err != nil || !ok {
		t.Fatalf("A CLEANUP RETRY RELEASED THE INCIDENT: %v %v", ok, err)
	}
	if now.Selection.Rev == sel.Rev || !now.Releasable {
		t.Fatalf("after the retry: %+v", now)
	}
	out.Reset()
	if code := recoverScripted(h, sel.String(), "inspect", &out); code != 0 || !strings.Contains(out.String(), "changed since that selection") {
		t.Fatalf("inspection of a stale selection: exit %d\n%s", code, out.String())
	}
	other := now.Selection
	other.CID++
	token := now.Selection
	token.Incarnation = strings.Repeat("0", 32)
	for name, s := range map[string]launcher.Selection{"another cid": other, "another token": token} {
		out.Reset()
		if code := recoverScripted(h, s.String(), "inspect", &out); code != 1 || !strings.Contains(out.String(), "no incident of") {
			t.Fatalf("AN INSPECTION SHOWED ANOTHER INCARNATION FOR A SELECTION (%s): exit %d\n%s", name, code, out.String())
		}
	}
	for name, s := range map[string]launcher.Selection{"a stale revision": sel, "another incarnation": other} {
		out.Reset()
		if code := recoverScripted(h, s.String(), "release", &out); code != 1 || !strings.Contains(out.String(), "stale") {
			t.Fatalf("A STALE SELECTION WAS RELEASED (%s): exit %d\n%s", name, code, out.String())
		}
	}
	if code := recoverScripted(h, now.Selection.String(), "release", &out); code != 0 {
		t.Fatalf("release: exit %d\n%s", code, out.String())
	}
	out.Reset()
	if code := recoverScripted(h, now.Selection.String(), "release", &out); code != 1 {
		t.Fatalf("a replayed release: exit %d\n%s", code, out.String())
	}
}

// FindVMMs reads the proc root for the exact `--id <id>` argument pair: a
// longer id, another flag or a joined `--id=` form is not the id's VMM, and
// — independent review 01, R1 — since each still mentions the id, it is not
// provably another's either: the look lists the one VMM it verified and is
// incomplete, naming them. A vanished entry and a non-numeric one are no
// processes; a proc root it cannot list is a failed look.
func TestFindVMMsLooksForTheExactID(t *testing.T) {
	h := testHost(t, &recorder{})
	id := launcher.IDFor("t", "0v1")
	write := func(pid, cmdline string) {
		dir := filepath.Join(h.procDir, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if cmdline != "" {
			if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("101", "firecracker\x00--id\x00"+id+"\x00--no-api\x00")
	write("102", "firecracker\x00--id\x00"+id+"0\x00")
	write("103", "firecracker\x00--idx\x00"+id+"\x00")
	write("104", "firecracker\x00--id="+id+"\x00")
	write("105", "")
	write("self", "firecracker\x00--id\x00"+id+"\x00")
	pids, err := h.FindVMMs(id)
	if !slices.Equal(pids, []int{101}) || err == nil || !strings.Contains(err.Error(), "pid 102") || !strings.Contains(err.Error(), "pid 103") || !strings.Contains(err.Error(), "pid 104") {
		t.Fatalf("THE SCAN DID NOT FIND EXACTLY THE ID'S VMM, OR TOOK AN AMBIGUOUS PROCESS FOR ANOTHER'S: %v %v", pids, err)
	}
	h.procDir = filepath.Join(t.TempDir(), "gone")
	if pids, err := h.FindVMMs(id); err == nil {
		t.Fatalf("a proc root that cannot be listed was an empty look: %v", pids)
	}
}

// A VMM whose pid was never recorded is found by the operator's retry and
// stopped — verified by its command line, its exit seen — before anything
// is removed; the release then follows. The children are this test
// binary's own, inert; an unrelated one of another id is left alive.
func TestRecoverStopsAVMMOfUnknownPidItFinds(t *testing.T) {
	h := testHost(t, &recorder{})
	h.procDir = "/proc"
	r := incident(t, h, "0vu", "owner/repo · ci.yml · deploy", 3, time.Now().Unix())
	r.HasVMM, r.PID = true, 0
	writeRecord(t, h.cfg.StateDir, r)
	start := func(id string) (identity, chan struct{}) {
		path := filepath.Join(t.TempDir(), "vmm.id")
		cmd := exec.Command(os.Args[0], "--id", id)
		cmd.Env = append(os.Environ(), childMode+"=sleep", childID+"="+path)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		reaped := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(reaped)
		}()
		vmm := readIdentity(t, path)
		t.Cleanup(vmm.kill)
		return vmm, reaped
	}
	vmm, reaped := start(r.ID)
	sentinel, _ := start(launcher.IDFor("t", "0vother"))
	in, err := act(h, selectionFor(r), "retry")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("THE VMM THE RETRY FOUND WAS NOT STOPPED")
	}
	if vmm.alive() || !sentinel.alive() {
		t.Fatalf("the found VMM alive %v; the unrelated sentinel alive %v", vmm.alive(), sentinel.alive())
	}
	inc := in.Record.Incident
	if inc == nil || !strings.Contains(inc.Attempts[len(inc.Attempts)-1].Detail, "found by the host") || !in.Releasable {
		t.Fatalf("the retry's finding: %+v", in)
	}
	if _, err := act(h, in.Selection, "release"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(evidenceOf(h, r)); err != nil {
		t.Fatal(errors.Join(errors.New("no evidence"), err))
	}
}
