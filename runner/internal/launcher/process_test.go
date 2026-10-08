package launcher

// Process death, for real: the test binary re-executes itself as a child
// (TestMain dispatches on LAUNCHER_TEST_CHILD before any test runs). The
// child opens a private state directory with the file-backed model host,
// stops at a barrier inside a publication or a host effect, prints READY,
// and is killed by the parent with SIGKILL through its exact pid. The
// parent then opens the same state as a new process would. Nothing here
// starts a VM, a jailer or any command but this test binary. Physical
// power loss is not simulated.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if scenario := os.Getenv("LAUNCHER_TEST_CHILD"); scenario != "" {
		os.Exit(runChild(scenario))
	}
	os.Exit(m.Run())
}

// runChild is the child's whole life. It reports READY at its barrier and
// then waits to be killed (a bounded sleep, never a forever block).
func runChild(scenario string) int {
	state, hostDir := os.Getenv("LAUNCHER_TEST_STATE"), os.Getenv("LAUNCHER_TEST_HOST")
	cfg := testConfig(state)
	h := openModelHost(hostDir, nil)
	ready := func(string) {
		fmt.Println("READY")
		time.Sleep(2 * time.Minute)
		fmt.Fprintln(os.Stderr, "child: not killed within 2 minutes")
		os.Exit(3)
	}
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "child:", err)
		return 2
	}
	fsys := newFaultFS(nil)
	if scenario == "reserve-rename" {
		fsys.inject(&fsFault{op: "rename", path: ".json", block: func() { ready("") }})
	}
	s, err := openService(cfg, h, fsys)
	if err != nil {
		return fail(err)
	}
	if scenario == "reserve-syncdir" {
		// stage 01 (triage 08's restart proof): the open certifies the
		// records directory itself, so the barrier is armed after it — the
		// scenario is the reservation's own publication's directory fsync
		fsys.inject(&fsFault{op: "syncdir", path: "attempts", block: func() { ready("") }})
	}
	switch scenario {
	case "hold":
		ready("")
	case "reserve-rename", "reserve-syncdir":
		_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
		return fail(fmt.Errorf("reserve returned: %v", err))
	case "create-cgroup", "create-start":
		r, err := s.Reserve(owner1, lockedReq("0v1", 2, 1024))
		if err != nil {
			return fail(err)
		}
		h.at(strings.TrimPrefix(scenario, "create-")+"/after", ready)
		_, err = s.Create(owner1, r.ID)
		return fail(fmt.Errorf("create returned: %v", err))
	case "teardown":
		r, err := s.Reserve(owner1, lockedReq("0v1", 2, 1024))
		if err != nil {
			return fail(err)
		}
		if _, err := s.Create(owner1, r.ID); err != nil {
			return fail(err)
		}
		h.at("rmcgroup/before", ready)
		return fail(fmt.Errorf("destroy returned: %v", s.Destroy(owner1, r.ID)))
	case "efbig":
		// a real kernel write fault: the record cannot fit in 64 bytes
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 64, Max: 64}); err != nil {
			return fail(err)
		}
		_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
		fmt.Printf("RESULT efbig=%v notdurable=%v used=%v err=%v\n", errors.Is(err, syscall.EFBIG), errors.Is(err, ErrNotDurable), used(s), err)
		return 0
	}
	return fail(errors.New("unknown scenario " + scenario))
}

type child struct {
	cmd    *exec.Cmd
	stderr *bytes.Buffer
	out    *bufio.Reader
}

// startChild runs a scenario and waits for its first output line.
func startChild(t *testing.T, scenario, state, hostDir string) (*child, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "LAUNCHER_TEST_CHILD="+scenario, "LAUNCHER_TEST_STATE="+state, "LAUNCHER_TEST_HOST="+hostDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &child{cmd: cmd, stderr: &stderr, out: bufio.NewReader(pipe)}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	lines := make(chan string, 1)
	go func() {
		line, _ := c.out.ReadString('\n')
		lines <- strings.TrimSpace(line)
	}()
	select {
	case line := <-lines:
		t.Logf("child %s (pid %d): %q", scenario, cmd.Process.Pid, line)
		return c, line
	case <-time.After(60 * time.Second):
		t.Fatalf("child %s printed nothing; stderr: %s", scenario, stderr.String())
	}
	return nil, ""
}

// kill ends the child with SIGKILL through the pid this test started,
// after checking it is still that test binary.
func (c *child) kill(t *testing.T) {
	t.Helper()
	pid := c.cmd.Process.Pid
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || !strings.HasPrefix(string(cmdline), os.Args[0]+"\x00") {
		t.Fatalf("pid %d is not our child (%q, %v)", pid, cmdline, err)
	}
	if err := c.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	err = c.cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("child did not die by SIGKILL: %v (stderr %s)", err, c.stderr.String())
	}
}

func TestProcessDeath(t *testing.T) {
	setup := func(t *testing.T) (Config, *modelHost, string) {
		cfg := testConfig(t.TempDir())
		hostDir := t.TempDir()
		return cfg, openModelHost(hostDir, &tracer{}), hostDir
	}
	id := IDFor("t", "0v1")

	t.Run("hold: the lock follows the process", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "hold", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		if _, err := NewService(cfg, h); !errors.Is(err, ErrStateBusy) {
			t.Fatalf("opened state another live process holds: %v", err)
		}
		c.kill(t)
		s := open(t, cfg, h, nil)
		if len(s.Problems()) != 0 {
			t.Fatalf("problems %+v", s.Problems())
		}
	})

	t.Run("killed mid-publication: nothing acknowledged, nothing charged", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "reserve-rename", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		c.kill(t)
		names := stateEntries(t, cfg.StateDir)
		if names[id+".json.tmp"] != "regular file" || names[id+".json"] != "" {
			t.Fatalf("state after the kill: %v", names)
		}
		s := open(t, cfg, h, nil)
		if used(s)[2] != 0 || len(s.Problems()) != 0 || len(s.Leftovers()) != 1 {
			t.Fatalf("used %v problems %v leftovers %v", used(s), s.Problems(), s.Leftovers())
		}
		mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	})

	t.Run("killed after the rename: charged though never acknowledged", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "reserve-syncdir", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		c.kill(t)
		s := open(t, cfg, h, nil)
		s.ReapOnce()
		if rec, ok := held(s, id); !ok || rec.State != StatePreparing || used(s)[2] != 1 {
			t.Fatalf("an unacknowledged but published reservation must stay charged: %+v %v", rec, ok)
		}
		if err := s.Destroy(owner1, id); err != nil || used(s)[2] != 0 {
			t.Fatalf("its owner cannot release it: %v %v", err, used(s))
		}
	})

	t.Run("killed inside a host effect: torn down, not resumed", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "create-cgroup", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		c.kill(t)
		if leaks := h.leaks(id); strings.Join(leaks, ",") != "jail,cgroup" {
			t.Fatalf("the child's resources: %v", leaks)
		}
		d, _ := durable(t, cfg.StateDir, id)
		if d.State != StatePreparing || !d.HasDisk || !d.HasCgroup || d.HasVMM {
			t.Fatalf("durable after the kill %+v", d)
		}
		s := open(t, cfg, h, nil)
		if _, err := s.Create(owner1, id); !errors.Is(err, ErrState) {
			t.Fatalf("an interrupted create was resumed: %v", err)
		}
		s.ReapOnce()
		if _, ok := held(s, id); ok || len(h.leaks(id)) != 0 {
			t.Fatalf("not recovered: %+v %v", s.All(), h.leaks(id))
		}
	})

	t.Run("killed while starting the VMM: unknown until the operator clears", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "create-start", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		c.kill(t)
		d, _ := durable(t, cfg.StateDir, id)
		if !d.HasVMM || d.PID != 0 || !h.exists("vmm", id) {
			t.Fatalf("durable %+v, vmm marker %v", d, h.exists("vmm", id))
		}
		s := open(t, cfg, h, nil)
		s.ReapOnce()
		if rec, ok := held(s, id); !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
			t.Fatalf("a VMM of unknown pid was released by a restart: %+v %v", rec, ok)
		}
		if err := s.Destroy(owner1, id); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("owner destroy: %v", err)
		}
		if err := os.Remove(h.marker("vmm", id)); err != nil { // the operator found and stopped it
			t.Fatal(err)
		}
		if err := s.ClearQuarantine(id); err != nil || len(h.leaks(id)) != 0 {
			t.Fatalf("operator clear: %v leaks %v", err, h.leaks(id))
		}
	})

	t.Run("killed mid-teardown: the restart finishes the cleanup, the operator releases", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "teardown", cfg.StateDir, hostDir)
		if line != "READY" {
			t.Fatalf("child: %q %s", line, c.stderr.String())
		}
		c.kill(t)
		if d, _ := durable(t, cfg.StateDir, id); d.State != StateStopping {
			t.Fatalf("durable %+v", d)
		}
		// the dead teardown's outcome was never recorded: it may have been a
		// quarantine, so the record loads quarantined and stays so
		s := open(t, cfg, h, nil)
		if rec, _ := held(s, id); rec.State != StateQuarantined {
			t.Fatalf("an interrupted teardown loaded as %+v", rec)
		}
		s.ReapOnce()
		if rec, ok := held(s, id); !ok || rec.State != StateQuarantined || len(h.leaks(id)) != 0 {
			t.Fatalf("after the owed cleanup: %+v %v leaks %v", rec, ok, h.leaks(id))
		}
		if err := s.Destroy(owner1, id); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("owner destroy after a restart: %v", err)
		}
		if err := s.ClearQuarantine(id); err != nil || used(s)[2] != 0 {
			t.Fatalf("operator clear: %v used %v", err, used(s))
		}
	})

	t.Run("a real EFBIG from the kernel: not acknowledged", func(t *testing.T) {
		cfg, h, hostDir := setup(t)
		c, line := startChild(t, "efbig", cfg.StateDir, hostDir)
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("child: %v %s", err, c.stderr.String())
		}
		if !strings.HasPrefix(line, "RESULT efbig=true notdurable=true used=[0 0 0]") {
			t.Fatalf("child result %q (stderr %s)", line, c.stderr.String())
		}
		s := open(t, cfg, h, nil)
		if used(s)[2] != 0 || len(s.Problems()) != 0 {
			t.Fatalf("after the fault: used %v problems %v", used(s), s.Problems())
		}
		if names := stateEntries(t, cfg.StateDir); len(names) != 0 {
			t.Fatalf("left behind: %v", names)
		}
	})
}
