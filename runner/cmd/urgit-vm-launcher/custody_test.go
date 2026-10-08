package main

// Process custody (runner/launcher/INTEGRATION.md §11.2; independent review
// 01, R1): what the host says about a pid is verified running, verified
// gone, or unknown — and an unknown answer is never proof that a VMM is
// gone, nor grounds to signal a process. Every proc tree here is a private
// fixture; the only real processes are this test binary's own inert
// children.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// procEntry writes a private proc-shaped entry: dir/<pid>/cmdline with data
// ("" writes the directory only: the process vanished as it was read).
func procEntry(t *testing.T, dir, pid, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, pid), 0o755); err != nil {
		t.Fatal(err)
	}
	if data != "" {
		if err := os.WriteFile(filepath.Join(dir, pid, "cmdline"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A scan proves a VMM of unknown pid gone only when it is complete: every
// numeric entry read and classified. A command line it may not read, a
// non-file where the command line is, or one that mentions the id but not
// as `--id <id>` leaves the scan incomplete — an error naming the entry —
// while the VMM it did verify is still listed. A vanished entry and a
// verified foreign process are not the id's, and a scan of only those is
// complete.
func TestFindVMMsNeverTakesAnUnreadableEntryForAbsence(t *testing.T) {
	id := launcher.IDFor("t", "0v1")
	base := func(t *testing.T) *realHost {
		h := testHost(t, &recorder{})
		h.procDir = t.TempDir()
		procEntry(t, h.procDir, "101", "firecracker\x00--id\x00"+id+"\x00--no-api\x00")
		procEntry(t, h.procDir, "102", "sshd\x00-D\x00")
		procEntry(t, h.procDir, "103", "") // vanished
		return h
	}
	t.Run("complete: only the id's VMM, a foreign process and a vanished one", func(t *testing.T) {
		h := base(t)
		pids, err := h.FindVMMs(id)
		if err != nil || !slices.Equal(pids, []int{101}) {
			t.Fatalf("a complete scan: %v %v", pids, err)
		}
	})
	for _, c := range []struct {
		name string
		make func(t *testing.T, proc string)
	}{
		{"a command line it may not read", func(t *testing.T, proc string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads a mode-0 file")
			}
			procEntry(t, proc, "201", "firecracker\x00--id\x00"+id+"\x00")
			if err := os.Chmod(filepath.Join(proc, "201", "cmdline"), 0); err != nil {
				t.Fatal(err)
			}
		}},
		{"a non-file where the command line is", func(t *testing.T, proc string) {
			if err := os.MkdirAll(filepath.Join(proc, "201", "cmdline"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"the id not as --id <id>", func(t *testing.T, proc string) {
			procEntry(t, proc, "201", "firecracker\x00--id="+id+"\x00")
		}},
		{"the id in another argument", func(t *testing.T, proc string) {
			procEntry(t, proc, "201", "tail\x00-f\x00/srv/jailer/firecracker/"+id+"/root/log\x00")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := base(t)
			c.make(t, h.procDir)
			pids, err := h.FindVMMs(id)
			if err == nil || !strings.Contains(err.Error(), "201") {
				t.Fatalf("AN INCOMPLETE SCAN WAS TAKEN FOR A COMPLETE ONE: %v %v", pids, err)
			}
			if !slices.Equal(pids, []int{101}) {
				t.Fatalf("the VMM the scan verified is not listed: %v", pids)
			}
		})
	}
	t.Run("a look cut short", func(t *testing.T) {
		h := base(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		bound := h.Bound(ctx).(*realHost)
		if pids, err := bound.FindVMMs(id); err == nil {
			t.Fatalf("AN INCOMPLETE SCAN WAS TAKEN FOR A COMPLETE ONE: %v", pids)
		}
	})
}

// Kill signals only a process verified to be the id's VMM: one whose
// command line cannot be read is never signalled — an error says why — and
// one verified foreign is left alone; the verified one is signalled.
func TestKillNeverSignalsAnUnverifiedProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmm.id")
	cmd := exec.Command(os.Args[0], "--id", "t-abc")
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
	defer vmm.kill()
	h := testHost(t, &recorder{})
	h.procDir = t.TempDir()
	pid := vmm.pid
	// the child's entry in the private tree: its command line unreadable
	if err := os.MkdirAll(filepath.Join(h.procDir, strconv.Itoa(pid), "cmdline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Kill("t-abc", pid, "KILL"); err == nil {
		t.Fatal("AN UNVERIFIED PROCESS WAS TAKEN FOR GONE OR SIGNALLED: no error")
	}
	select {
	case <-reaped:
		t.Fatal("AN UNVERIFIED PROCESS WAS SIGNALLED: it exited")
	case <-time.After(2 * time.Second):
	}
	// verified foreign: nothing to signal
	if err := os.RemoveAll(filepath.Join(h.procDir, strconv.Itoa(pid))); err != nil {
		t.Fatal(err)
	}
	procEntry(t, h.procDir, strconv.Itoa(pid), "sshd\x00-D\x00")
	if err := h.Kill("t-abc", pid, "KILL"); err != nil {
		t.Fatalf("a verified foreign process: %v", err)
	}
	select {
	case <-reaped:
		t.Fatal("A FOREIGN PROCESS WAS SIGNALLED: it exited")
	case <-time.After(time.Second):
	}
	// verified the id's: signalled
	if err := os.RemoveAll(filepath.Join(h.procDir, strconv.Itoa(pid))); err != nil {
		t.Fatal(err)
	}
	procEntry(t, h.procDir, strconv.Itoa(pid), os.Args[0]+"\x00--id\x00t-abc\x00")
	if err := h.Kill("t-abc", pid, "TERM"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("the verified process did not end on TERM")
	}
}

// The operator's retry of a VMM of unknown pid resolves nothing while the
// scan is incomplete — the holding and the disk stay, the retry says why —
// and resolves it once a complete scan finds none; the release follows.
func TestRecoverRetryKeepsCustodyOnAnIncompleteScan(t *testing.T) {
	h := testHost(t, &recorder{})
	h.procDir = t.TempDir()
	q := incident(t, h, "0vscan", "owner/repo · ci.yml · scan", 3, time.Now().Unix())
	q.HasVMM, q.PID = true, 0
	writeRecord(t, h.cfg.StateDir, q)
	disk := filepath.Join(h.jailRoot(q.ID), "disk.ext4")
	if err := os.MkdirAll(filepath.Join(h.procDir, "4242", "cmdline"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := recoverScripted(h, selectionFor(q).String(), "retry", &out); code != 1 || !strings.Contains(out.String(), "4242") {
		t.Fatalf("AN INCOMPLETE SCAN RESOLVED THE VMM: exit %d\n%s", code, out.String())
	}
	snap, err := launcher.ReadState(h.cfg.StateDir, "t")
	if err != nil || len(snap.Records) != 1 || !snap.Records[0].HasVMM {
		t.Fatalf("AN INCOMPLETE SCAN RESOLVED THE VMM: %+v %v", snap, err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("A DISK WAS REMOVED FROM UNDER A VMM OF UNKNOWN CUSTODY: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(h.procDir, "4242")); err != nil {
		t.Fatal(err)
	}
	now, ok, err := lookup(h.cfg, selectionFor(q))
	if err != nil || !ok {
		t.Fatalf("lookup: %v %v", ok, err)
	}
	out.Reset()
	if code := recoverScripted(h, now.Selection.String(), "retry", &out); code != 0 {
		t.Fatalf("the retry after a complete scan: exit %d\n%s", code, out.String())
	}
	now, _, _ = lookup(h.cfg, selectionFor(q))
	if code := recoverScripted(h, now.Selection.String(), "release", &out); code != 0 {
		t.Fatalf("release: exit %d\n%s", code, out.String())
	}
}

// A missing process proves nothing while the proc root that would list it
// is not there (not mounted, misconfigured): its liveness is unknown, never
// gone. With the proc root there, the same missing entry is a process that
// exited. (A proc root that is not a directory fails the read itself:
// unknown too.)
func TestAMissingProcRootProvesNoProcessGone(t *testing.T) {
	id := launcher.IDFor("t", "0v1")
	h := testHost(t, &recorder{})
	h.procDir = filepath.Join(t.TempDir(), "not-mounted")
	if l, err := h.Liveness(id, 4242); l != launcher.Unknown || err == nil || !strings.Contains(err.Error(), h.procDir) {
		t.Fatalf("A MISSING PROC ROOT PROVED A PROCESS GONE: %v %v", l, err)
	}
	h.procDir = t.TempDir()
	if l, err := h.Liveness(id, 4242); l != launcher.Gone || err != nil {
		t.Fatalf("a proc root without the entry: %v %v", l, err)
	}
}

// StartVM answers only a pid verified as the id's VMM (§11.2): a pid file
// naming a process whose command line cannot be read, a foreign process or
// none at all, or one that names no pid, is waited past until the start's
// bound — the start is then uncertain, never a VMM of that pid; the
// verified one is answered. No jailer runs: the recorder answers it.
func TestStartVMAnswersOnlyAVerifiedPid(t *testing.T) {
	id := launcher.IDFor("t", "0v1")
	spec := launcher.VMSpec{Image: launcher.Image{BootArgs: "console=ttyS0"}, CPUs: 1, MemoryMiB: 128, CID: 3, Attempt: "0v1"}
	for _, c := range []struct {
		name     string
		make     func(t *testing.T, proc string)
		verified bool
	}{
		{"a command line that cannot be read", func(t *testing.T, proc string) {
			if err := os.MkdirAll(filepath.Join(proc, "4242", "cmdline"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"the id not as --id <id>", func(t *testing.T, proc string) { procEntry(t, proc, "4242", "firecracker\x00--id="+id+"\x00") }, false},
		{"a foreign process", func(t *testing.T, proc string) { procEntry(t, proc, "4242", "sshd\x00-D\x00") }, false},
		{"no process", func(t *testing.T, proc string) {}, false},
		{"a pid file that names no pid", func(t *testing.T, proc string) {
			procEntry(t, proc, "4242", "firecracker\x00--id\x00"+id+"\x00--no-api\x00") // not the pid it names
		}, false},
		{"the id's VMM (control)", func(t *testing.T, proc string) {
			procEntry(t, proc, "4242", "firecracker\x00--id\x00"+id+"\x00--no-api\x00")
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := &recorder{}
			h := testHost(t, rec)
			if err := os.MkdirAll(h.jailRoot(id), 0o750); err != nil {
				t.Fatal(err)
			}
			pidFile := "4242\n"
			if c.name == "a pid file that names no pid" {
				pidFile = "firecracker\n"
			}
			if err := os.WriteFile(filepath.Join(h.jailRoot(id), "firecracker.pid"), []byte(pidFile), 0o644); err != nil {
				t.Fatal(err)
			}
			c.make(t, h.procDir)
			ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			defer cancel()
			pid, err := h.Bound(ctx).(*realHost).StartVM(id, spec)
			if len(rec.unowned) != 1 {
				t.Fatalf("fixture: the jailer was not asked for once: %q", rec.unowned)
			}
			if c.verified {
				if err != nil || pid != 4242 {
					t.Fatalf("the verified VMM: %d %v", pid, err)
				}
				return
			}
			if err == nil || pid != 0 || errors.Is(err, launcher.ErrNoEffect) {
				t.Fatalf("A START ANSWERED A PID NOT VERIFIED AS ITS VMM: %d %v", pid, err)
			}
		})
	}
}
