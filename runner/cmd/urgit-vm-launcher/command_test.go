package main

// runCommand's process, pipe and deadline behaviour and Kill's target,
// proved on inert children (runner/launcher/INTEGRATION.md §7.1). Every
// child is this test binary itself, re-executed in a mode that only
// sleeps, starts one more such child, calls setsid, prints or exits —
// never a host command. Each records its identity (pid and start time);
// the tests signal only processes whose identity they recorded, verify
// deaths by that identity, keep an unrelated sentinel process alive, and
// bound every wait. These prove adapter properties, not confinement.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	childMode = "URGIT_STAGE01_INERT_CHILD"
	childID   = "URGIT_STAGE01_CHILD_ID_FILE"
	grandID   = "URGIT_STAGE01_GRANDCHILD_ID_FILE"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(childMode); mode != "" {
		os.Exit(inertChild(mode))
	}
	os.Exit(m.Run())
}

// inertChild is what a re-executed test binary does; nothing lasts more
// than 30 s.
func inertChild(mode string) int {
	switch mode {
	case "sleep":
		recordIdentity(os.Getenv(childID))
		time.Sleep(30 * time.Second)
		return 0
	case "print":
		os.Stdout.Write(bytes.Repeat([]byte("x"), 4*maxOutput))
		return 0
	case "exit3":
		return 3
	case "grandchild", "setsid-grandchild":
		// start one more inert child that inherits this one's output, wait
		// until it has recorded itself, then exit: it holds the pipe
		recordIdentity(os.Getenv(childID))
		g := exec.Command(os.Args[0])
		g.Env = append(os.Environ(), childMode+"=sleep", childID+"="+os.Getenv(grandID))
		g.Stdout, g.Stderr = os.Stdout, os.Stderr
		if mode == "setsid-grandchild" {
			g.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		}
		if err := g.Start(); err != nil {
			return 5
		}
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if _, err := os.Stat(os.Getenv(grandID)); err == nil {
				break
			}
		}
		return 0
	case "setsid-sleep":
		// what the jailer's --daemonize does first
		if _, err := syscall.Setsid(); err != nil {
			fmt.Fprintln(os.Stderr, "setsid:", err)
			return 7
		}
		recordIdentity(os.Getenv(childID))
		time.Sleep(30 * time.Second)
		return 0
	}
	return 2
}

// identity is a process as the test recorded it: pid and start time.
type identity struct {
	pid   int
	start string
}

// procStat is a pid's start time and state from /proc/<pid>/stat.
func procStat(pid int) (start string, state byte, ok bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", 0, false
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return "", 0, false
	}
	return f[19], f[0][0], true
}

func recordIdentity(path string) {
	start, _, _ := procStat(os.Getpid())
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(fmt.Sprintf("%d %s", os.Getpid(), start)), 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func readIdentity(t *testing.T, path string) identity {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil {
			if f := strings.Fields(string(data)); len(f) == 2 {
				if pid, err := strconv.Atoi(f[0]); err == nil {
					return identity{pid, f[1]}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no identity at %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// alive: the recorded process still runs (not a zombie, not a pid
// someone else has now).
func (id identity) alive() bool {
	start, state, ok := procStat(id.pid)
	return ok && start == id.start && state != 'Z'
}

// kill signals the recorded process only: pinned first (pidfd), then
// checked against the recorded identity.
func (id identity) kill() {
	p, err := os.FindProcess(id.pid)
	if err != nil {
		return
	}
	defer p.Release()
	if id.alive() {
		_ = p.Signal(syscall.SIGKILL)
	}
}

func (id identity) waitDead(d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if !id.alive() {
			return true
		}
	}
	return !id.alive()
}

// sentinel is an unrelated inert process in its own group, which no
// command's kill may reach; the test ends it itself, and reaps it.
func sentinel(t *testing.T) identity {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sentinel.id")
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), childMode+"=sleep", childID+"="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	id := readIdentity(t, path)
	t.Cleanup(func() {
		id.kill()
		select {
		case <-reaped:
		case <-time.After(5 * time.Second):
			t.Error("the sentinel was not reaped")
		}
	})
	return id
}

// A command that outlives its deadline is killed with its whole process
// group; the call returns within the kill grace, reports that it may have
// acted, and nothing else is signalled.
func TestRunCommandKillsItsGroupAtTheDeadline(t *testing.T) {
	s := sentinel(t)
	path := filepath.Join(t.TempDir(), "child.id")
	t.Setenv(childMode, "sleep")
	t.Setenv(childID, path)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	begun := time.Now()
	_, started, err := runCommand(ctx, true, os.Args[0])
	took := time.Since(begun)
	child := readIdentity(t, path)
	defer child.kill()
	if !started || !errors.Is(err, context.DeadlineExceeded) || took > 500*time.Millisecond+killGrace || child.alive() {
		t.Fatalf("THE COMMAND OUTLIVED ITS DEADLINE: returned after %v (deadline 500ms + grace %v): started %v, %v; child %d alive %v", took, killGrace, started, err, child.pid, child.alive())
	}
	if !s.alive() {
		t.Fatal("an unrelated process was killed")
	}
}

// A descendant that stays in the command's group and keeps its output
// open does not hold the call: once the command exits, the group's
// members are killed before the command is reaped.
func TestRunCommandDoesNotWaitOnItsGroupsInheritedPipe(t *testing.T) {
	s := sentinel(t)
	dir := t.TempDir()
	t.Setenv(childMode, "grandchild")
	t.Setenv(childID, filepath.Join(dir, "child.id"))
	t.Setenv(grandID, filepath.Join(dir, "grand.id"))
	begun := time.Now()
	_, started, err := runCommand(context.Background(), true, os.Args[0])
	took := time.Since(begun)
	grand := readIdentity(t, filepath.Join(dir, "grand.id"))
	defer grand.kill()
	if !started || err != nil || took >= killGrace {
		t.Fatalf("A DESCENDANT'S INHERITED PIPE HELD THE COMMAND: %v after %v (started %v)", err, took, started)
	}
	if !grand.waitDead(2 * time.Second) {
		t.Fatalf("THE GROUP'S DESCENDANT SURVIVED ITS COMMAND: pid %d", grand.pid)
	}
	if !s.alive() {
		t.Fatal("an unrelated process was killed")
	}
}

// A descendant that left the group (setsid) is beyond the group kill; its
// open pipe is waited for the kill grace at most, and the call says the
// command may have acted.
func TestRunCommandBoundsAPipeHeldOutsideItsGroup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(childMode, "setsid-grandchild")
	t.Setenv(childID, filepath.Join(dir, "child.id"))
	t.Setenv(grandID, filepath.Join(dir, "grand.id"))
	begun := time.Now()
	_, started, err := runCommand(context.Background(), true, os.Args[0])
	took := time.Since(begun)
	grand := readIdentity(t, filepath.Join(dir, "grand.id"))
	t.Cleanup(func() {
		grand.kill()
		if !grand.waitDead(5 * time.Second) {
			t.Errorf("the escaped descendant %d did not die", grand.pid)
		}
	})
	if !started || !errors.Is(err, exec.ErrWaitDelay) || took < killGrace-time.Second || took > killGrace+2*time.Second {
		t.Fatalf("AN OUTPUT HELD OUTSIDE THE GROUP WAS NOT BOUNDED BY THE GRACE: %v after %v (grace %v, started %v)", err, took, killGrace, started)
	}
}

// Nothing started, nothing acted: a command that cannot be started, and
// one whose deadline had passed before it could be.
func TestRunCommandNotStarted(t *testing.T) {
	if _, started, err := runCommand(context.Background(), true, filepath.Join(t.TempDir(), "no-such-program")); started || err == nil {
		t.Fatalf("a program that does not exist: started %v, %v", started, err)
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv(childMode, "exit3")
	if _, started, err := runCommand(ended, true, os.Args[0]); started || !errors.Is(err, context.Canceled) {
		t.Fatalf("a command past its deadline: started %v, %v", started, err)
	}
	if _, started, err := runCommand(context.Background(), true, os.Args[0]); !started || err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("a command that failed: started %v, %v", started, err)
	}
}

// Why the jailer is not a group leader: a group leader cannot setsid, as
// the jailer's --daemonize does first. Unowned, the command can — and at
// its deadline it is still killed, by its own pid and its new group.
func TestRunCommandUnownedAllowsSetsidAndIsStillBounded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(childMode, "setsid-sleep")
	t.Setenv(childID, filepath.Join(dir, "owned.id"))
	out, started, err := runCommand(context.Background(), true, os.Args[0])
	if !started || err == nil || !strings.Contains(out, "setsid: operation not permitted") {
		t.Fatalf("an owned command's setsid: %q, started %v, %v", out, started, err)
	}
	t.Setenv(childID, filepath.Join(dir, "unowned.id"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	begun := time.Now()
	_, started, err = runCommand(ctx, false, os.Args[0])
	took := time.Since(begun)
	child := readIdentity(t, filepath.Join(dir, "unowned.id")) // written only after its setsid succeeded
	defer child.kill()
	if !started || !errors.Is(err, context.DeadlineExceeded) || took > 500*time.Millisecond+killGrace || child.alive() {
		t.Fatalf("THE UNOWNED COMMAND OUTLIVED ITS DEADLINE: after %v: started %v, %v; child %d alive %v", took, started, err, child.pid, child.alive())
	}
}

// A command's output is kept up to maxOutput.
func TestRunCommandBoundsItsOutput(t *testing.T) {
	t.Setenv(childMode, "print")
	out, started, err := runCommand(context.Background(), true, os.Args[0])
	if !started || err != nil || len(out) != maxOutput {
		t.Fatalf("output %d bytes (want %d), started %v, %v", len(out), maxOutput, started, err)
	}
}

// Kill reaches this VM's VMM only: a process whose command line does not
// name the id is not signalled; the one that does is; a process gone is
// no error.
func TestKillSignalsOnlyTheVerifiedProcess(t *testing.T) {
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
	h.procDir = "/proc"
	if err := h.Kill("t-other", vmm.pid, "KILL"); err != nil {
		t.Fatalf("A PROCESS OF ANOTHER ID WAS SIGNALLED: %v", err)
	}
	// a signal is delivered asynchronously: one sent would have ended the
	// child well within this wait (full-02's A14 saw it still running
	// right after a mutant's kill)
	select {
	case <-reaped:
		t.Fatal("A PROCESS OF ANOTHER ID WAS SIGNALLED: it exited")
	case <-time.After(2 * time.Second):
	}
	if !vmm.alive() {
		t.Fatal("A PROCESS OF ANOTHER ID WAS SIGNALLED: it is gone")
	}
	if err := h.Kill("t-abc", vmm.pid, "TERM"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("the verified process did not end on TERM")
	}
	if err := h.Kill("t-abc", vmm.pid, "KILL"); err != nil {
		t.Fatalf("a process gone: %v", err)
	}
}
