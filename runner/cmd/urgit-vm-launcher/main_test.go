package main

// The command's lifecycle wiring, with nothing executed: serve runs over
// the production launcher core with a fake host; the real host adapter is
// driven through a recording command runner (and, once, through the real
// runner at a jailer path that does not exist, so no program runs).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// fakeHost is a nonexecuting launcher.Host that records its operations.
type fakeHost struct {
	mu    sync.Mutex
	ops   []string
	alive map[string]int
	next  int
}

func newFakeHost() *fakeHost { return &fakeHost{alive: map[string]int{}, next: 500} }

func (f *fakeHost) op(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, s)
}

func (f *fakeHost) saw(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.ops, s)
}

func (f *fakeHost) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ops...)
}

func (f *fakeHost) PrepareDisk(id string, img launcher.Image, total int) (string, error) {
	f.op("disk " + id)
	return "/fake/" + id, nil
}
func (f *fakeHost) CreateCgroup(id string, cpus, mem int) error { f.op("cgroup " + id); return nil }
func (f *fakeHost) CreateNetwork(id string, index int, allow []string) (launcher.NetInfo, error) {
	f.op("net " + id)
	return launcher.NetInfo{}, nil
}
func (f *fakeHost) StartVM(id string, spec launcher.VMSpec) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.alive[id] = f.next
	f.ops = append(f.ops, "start "+id)
	return f.next, nil
}
func (f *fakeHost) Connect(id string, port uint32) (*os.File, error) {
	return nil, errors.New("no vsock here")
}

// Liveness is exact in this fake (stage 01, independent review 01: the
// three-valued Host.Liveness replaced Alive): running or gone.
func (f *fakeHost) Liveness(id string, pid int) (launcher.Liveness, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.alive[id] == pid && pid != 0 {
		return launcher.Running, nil
	}
	return launcher.Gone, nil
}
func (f *fakeHost) Kill(id string, pid int, sig string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.alive, id)
	f.ops = append(f.ops, "kill "+id)
	return nil
}
func (f *fakeHost) RemoveNetwork(id string, index int) error { f.op("rmnet " + id); return nil }
func (f *fakeHost) RemoveCgroup(id string) error             { f.op("rmcgroup " + id); return nil }
func (f *fakeHost) RemoveDisk(id string) error               { f.op("rmdisk " + id); return nil }
func (f *fakeHost) RemoveJail(id string) error               { f.op("rmjail " + id); return nil }

func coreConfig(state string) launcher.Config {
	return launcher.Config{StateDir: state, IDPrefix: "t", Images: map[string]launcher.Image{"img": {Manifest: "img", BaselineMiB: 1}},
		BudgetCPUs: 8, BudgetMemoryMiB: 18432, MaxGuests: 2, CleanupBound: time.Second}
}

// writeRecord leaves a record the way a previous launcher process would.
func writeRecord(t *testing.T, state string, r launcher.Record) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"format": 1, "record": r})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "attempts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "attempts", r.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func interrupted(attempt string, state launcher.State) launcher.Record {
	now := time.Now().Unix()
	return launcher.Record{ID: launcher.IDFor("t", attempt), Attempt: attempt, Owner: launcher.Owner{UID: 1000, Daemon: "0vd"}, Image: "img",
		CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: now + 3600, Network: "locked", CID: 3, State: state,
		HasDisk: true, HasCgroup: true, Created: now, Updated: now}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// serve recovers what a previous process left before its first tick,
// serves, and on its context's end stops in order: the socket closed, the
// reaper stopped, the owned VM torn down, the state directory given back.
func TestServeRecoversThenShutsDownInOrder(t *testing.T) {
	state := t.TempDir()
	left := interrupted("0vleft", launcher.StateStopping)
	writeRecord(t, state, left)
	h := newFakeHost()
	svc, err := launcher.NewService(coreConfig(state), h)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // a short relative socket path (108-byte limit)
	cfg := &Config{Socket: "l.sock", StateDir: state, IDPrefix: "t", RunnerUIDs: []int{os.Getuid()}}
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, svc, nil, log.New(&logs, "", 0)) }()

	var cl *launcher.Client
	deadline := time.Now().Add(5 * time.Second)
	for cl == nil && time.Now().Before(deadline) {
		if c, err := launcher.Dial(context.Background(), "l.sock", "0vd"); err == nil {
			cl = c
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if cl == nil {
		t.Fatalf("serve never answered: %s", logs.String())
	}
	defer cl.Close()
	// the reaper's first tick is 5 s away: only the startup pass can
	// recover it within 2 s
	recovered := time.Now().Add(2 * time.Second)
	for time.Now().Before(recovered) && !h.saw("rmjail "+left.ID) {
		time.Sleep(10 * time.Millisecond)
	}
	if !h.saw("rmcgroup "+left.ID) || !h.saw("rmjail "+left.ID) {
		t.Fatalf("the interrupted teardown's owed cleanup was not performed: %v", h.all())
	}
	if !strings.Contains(logs.String(), "QUARANTINED "+left.ID) {
		t.Fatalf("serve did not report the retained quarantine: %s", logs.String())
	}
	r, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: 1024, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("serve did not stop: %s", logs.String())
	}
	if !h.saw("kill "+r.ID) || !h.saw("rmjail "+r.ID) {
		t.Fatalf("the owned VM was not torn down: %v", h.all())
	}
	// what is left is the interrupted teardown: cleaned up, still
	// quarantined, for the operator's clear (§9)
	snap, err := launcher.ReadState(state, "t")
	if err != nil || len(snap.Records) != 1 {
		t.Fatalf("records left: %+v %v", snap, err)
	}
	if got := snap.Records[0]; got.ID != left.ID || got.State != launcher.StateQuarantined || got.HasDisk || got.HasCgroup {
		t.Fatalf("the interrupted teardown after serve: %+v", got)
	}
	again, err := launcher.NewService(coreConfig(state), h)
	if err != nil {
		t.Fatalf("serve kept the state directory: %v", err)
	}
	// stage 01 (recovery ruling A): the recovery pass cleaned it; the
	// operator's release is its own, separate step
	in, err := again.InspectIncident(left.Selection())
	if err != nil || !in.Releasable {
		t.Fatalf("the cleaned interrupted teardown: %v %+v", err, in)
	}
	if err := again.Release(in.Selection); err != nil {
		t.Fatalf("operator release: %v", err)
	}
	again.Close()
	t.Logf("serve log:\n%s", logs.String())
}

// A socket whose group cannot be set is not served, and the state
// directory is given back.
func TestServeRefusesASocketItCannotSecure(t *testing.T) {
	state := t.TempDir()
	svc, err := launcher.NewService(coreConfig(state), newFakeHost())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cfg := &Config{Socket: "l.sock", StateDir: state, SocketGroup: "urgit-repair-test-no-such-group", RunnerUIDs: []int{os.Getuid()}}
	err = serve(context.Background(), cfg, svc, nil, log.New(&syncBuffer{}, "", 0))
	if err == nil || !strings.Contains(err.Error(), "socket_group") {
		t.Fatalf("served an unsecured socket: %v", err)
	}
	again, err := launcher.NewService(coreConfig(state), newFakeHost())
	if err != nil {
		t.Fatalf("the state directory was kept: %v", err)
	}
	again.Close()
}

// list is read-only: it works beside a service that holds the state, and
// it fails on state the launcher cannot account for.
func TestListIsReadOnlyAndFailsOnUnsafeState(t *testing.T) {
	state := t.TempDir()
	svc, err := launcher.NewService(coreConfig(state), newFakeHost())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	r, err := svc.Reserve(launcher.Owner{UID: 1000, Daemon: "0vd"}, launcher.ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{StateDir: state, IDPrefix: "t"}
	var out bytes.Buffer
	var logs syncBuffer
	if err := list(cfg, &out, log.New(&logs, "", 0)); err != nil {
		t.Fatalf("list of a sound state: %v", err)
	}
	if !strings.Contains(out.String(), r.ID) {
		t.Fatalf("list output: %s", out.String())
	}
	if err := os.WriteFile(filepath.Join(state, "attempts", "stray"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = list(cfg, &out, log.New(&logs, "", 0))
	if err == nil || !strings.Contains(logs.String(), "UNSAFE STATE foreign") || !strings.Contains(out.String(), r.ID) {
		t.Fatalf("list with a stray entry: %v; out %s; log %s", err, out.String(), logs.String())
	}
}

// recorder is a command runner that executes nothing.
type recorder struct {
	mu        sync.Mutex
	calls     []string
	unowned   []string // the calls not run as an owned process group
	deadlines []time.Time
	reply     func(cmd string) (string, bool, error)
}

func (r *recorder) run(name string, args ...string) (string, bool, error) {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.mu.Lock()
	r.calls = append(r.calls, cmd)
	r.mu.Unlock()
	if r.reply == nil {
		return "", true, nil
	}
	return r.reply(cmd)
}

// runCtx is run with realHost.command's signature: it records how the
// command would be bounded and grouped; a context already ended starts
// nothing, as with the real runner.
func (r *recorder) runCtx(ctx context.Context, owned bool, name string, args ...string) (string, bool, error) {
	d, _ := ctx.Deadline()
	r.mu.Lock()
	r.deadlines = append(r.deadlines, d)
	if !owned {
		r.unowned = append(r.unowned, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	}
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return r.run(name, args...)
}

// testHost is the real adapter on private roots — its jail base, state
// directory, named-namespace directory, /proc and cgroup filesystem are
// fresh test fixtures (the cgroup one a model) — with commands recorded,
// never executed.
func testHost(t *testing.T, rec *recorder) *realHost {
	base := t.TempDir()
	for _, d := range []string{"netns", "proc"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := &realHost{
		cfg: &Config{StateDir: filepath.Join(base, "state"), IDPrefix: "t", JailBase: filepath.Join(base, "jail"), BinDir: filepath.Join(base, "bin"),
			CgroupService: testService, CgroupParent: "jobs", NFTTable: "urgit-test", CIDRPool: "10.113.0.0/16",
			BudgetCPUs: 8, BudgetMemoryMiB: 18432, MaxGuests: 2},
		log: log.New(&syncBuffer{}, "", 0), images: map[string]launcher.Image{"img": {Manifest: "img", BaselineMiB: 1}},
		vmUID: os.Getuid(), vmGID: os.Getgid(), command: rec.runCtx, pid: testPid,
		cgroups: newCgroupModel(), netnsDir: filepath.Join(base, "netns"), procDir: filepath.Join(base, "proc"),
	}
	// C1: systemd's delegation, then serve's placement in it, on the model
	h.cgroups.(*cgroupModel).systemdDelegates(testService, testPid, true)
	if err := h.placeCgroups(); err != nil {
		t.Fatalf("placement in the delegated subtree: %v", err)
	}
	return h
}

// the test host's delegated service cgroup and its launcher's pid (C1)
const (
	testService = "system.slice/urgit-vm-launcher.service"
	testPid     = 4100
)

// StartVM says "no effect" only when no jailer process ran; a jailer that
// ran and failed may have left a VMM, and says nothing of the kind.
func TestStartVMSaysNoEffectOnlyWhenNothingRan(t *testing.T) {
	rec := &recorder{}
	h := testHost(t, rec)
	spec := launcher.VMSpec{Image: launcher.Image{BootArgs: "console=ttyS0"}, CPUs: 1, MemoryMiB: 128, CID: 3, Attempt: "0v1"}
	id := launcher.IDFor("t", "0v1")

	if _, err := h.StartVM(id, spec); !errors.Is(err, launcher.ErrNoEffect) || len(rec.calls) != 0 {
		t.Fatalf("no jail root: %v (calls %v)", err, rec.calls)
	}
	if err := os.MkdirAll(h.jailRoot(id), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.cfg.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// the production runner, at a jailer path that does not exist: the
	// start itself fails, nothing runs
	h.command = runCommand
	if _, err := h.StartVM(id, spec); !errors.Is(err, launcher.ErrNoEffect) {
		t.Fatalf("a jailer that cannot start: %v", err)
	}
	h.command = (&recorder{reply: func(string) (string, bool, error) {
		return "jailer: chroot failed", true, errors.New("exit status 1")
	}}).runCtx
	_, err := h.StartVM(id, spec)
	if err == nil || errors.Is(err, launcher.ErrNoEffect) || !strings.Contains(err.Error(), "chroot failed") {
		t.Fatalf("a jailer that ran and failed must stay uncertain: %v", err)
	}
}

// RemoveNetwork works from the index the record holds: with the namespace
// already gone it still deletes this VM's rules and chain, and only
// these (vh1 is not vh15, 10.113.0.6 is not 10.113.0.62).
func TestRemoveNetworkUsesTheRecordedIndexAndExactRules(t *testing.T) {
	listing := `table inet urgit-test { # handle 1
	chain forward { # handle 2
		type filter hook forward priority filter; policy accept;
		iifname "vh1" jump vm-1 # handle 5
		oifname "vh1" ct state established,related accept # handle 6
		oifname "vh1" drop # handle 7
		iifname "vh15" jump vm-15 # handle 8
		oifname "vh15" ct state established,related accept # handle 9
		oifname "vh15" drop # handle 10
	}
	chain post { # handle 3
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr 10.113.0.6 oifname "enp1s0" masquerade # handle 11
		ip saddr 10.113.0.62 oifname "enp1s0" masquerade # handle 12
	}
}`
	if netnsSaddr(1) != "10.113.0.6" || netnsSaddr(15) != "10.113.0.62" {
		t.Fatalf("fixture addresses: %s %s", netnsSaddr(1), netnsSaddr(15))
	}
	chainErr := ""
	rec := &recorder{reply: func(cmd string) (string, bool, error) {
		switch {
		case cmd == "nft -a list table inet urgit-test":
			return listing, true, nil
		case strings.HasPrefix(cmd, "ip netns del"):
			return `Cannot remove namespace file "/run/netns/x": No such file or directory`, true, errors.New("exit status 1")
		case strings.HasPrefix(cmd, "nft delete chain") && chainErr != "":
			return chainErr, true, errors.New("exit status 1")
		}
		return "", true, nil
	}}
	h := testHost(t, rec)
	id := launcher.IDFor("t", "0v1")
	if err := h.RemoveNetwork(id, 1); err != nil {
		t.Fatalf("remove: %v (calls %v)", err, rec.calls)
	}
	var deleted []string
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "nft delete rule") {
			f := strings.Fields(c)
			deleted = append(deleted, f[len(f)-3]+"/"+f[len(f)-1])
		}
		if strings.Contains(c, "link show") {
			t.Fatalf("the index was re-read from the namespace: %s", c)
		}
	}
	if strings.Join(deleted, " ") != "forward/5 forward/6 forward/7 post/11" {
		t.Fatalf("deleted rules %v (calls %v)", deleted, rec.calls)
	}
	for _, want := range []string{"nft delete chain inet urgit-test vm-1", "ip netns del " + id} {
		if !slices.Contains(rec.calls, want) {
			t.Fatalf("missing %q in %v", want, rec.calls)
		}
	}
	// a chain that cannot be deleted is a failed removal: the core keeps
	// the holding
	chainErr = "Error: Could not process rule: Device or resource busy"
	if err := h.RemoveNetwork(id, 1); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("a failed chain deletion was reported removed: %v", err)
	}
}

// A retry and a release own the state directory for their run: each is
// refused while a serving launcher holds it; once it is free, the retry
// cleans a quarantined record through the real host's removals (files only
// here: nothing is executed) and keeps it quarantined, and the release —
// its own step — releases it. (Stage 01, recovery ruling A: this was one
// `clear`; its scenario is kept on the two entry points.)
func TestClearWaitsForTheStateAndUsesTheCore(t *testing.T) {
	rec := &recorder{}
	h := testHost(t, rec)
	q := interrupted("0vq", launcher.StateQuarantined)
	q.Reason = "cleanup failed: disk busy"
	writeRecord(t, h.cfg.StateDir, q)
	disk := filepath.Join(h.jailRoot(q.ID), "disk.ext4")
	if err := os.MkdirAll(filepath.Dir(disk), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel := q.Selection()
	serving, err := h.service()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, sel, "retry"); !errors.Is(err, launcher.ErrStateBusy) {
		t.Fatalf("a retry beside a serving launcher: %v", err)
	}
	if _, err := act(h, sel, "release"); !errors.Is(err, launcher.ErrStateBusy) {
		t.Fatalf("a release beside a serving launcher: %v", err)
	}
	if err := serving.Close(); err != nil {
		t.Fatal(err)
	}
	in, err := act(h, sel, "retry")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.cfg.JailBase, "firecracker", q.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("jail left: %v", err)
	}
	snap, err := launcher.ReadState(h.cfg.StateDir, "t")
	if err != nil || len(snap.Records) != 1 || snap.Records[0].State != launcher.StateQuarantined {
		t.Fatalf("A CLEANUP RETRY RELEASED THE INCIDENT: %+v %v", snap, err)
	}
	if _, err := act(h, in.Selection, "release"); err != nil {
		t.Fatalf("release: %v", err)
	}
	snap, err = launcher.ReadState(h.cfg.StateDir, "t")
	if err != nil || len(snap.Records) != 0 {
		t.Fatalf("record left: %+v %v", snap, err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("the recovery executed host commands: %v", rec.calls)
	}
	if _, err := act(h, in.Selection, "release"); !errors.Is(err, launcher.ErrUnknown) {
		t.Fatalf("a second release: %v", err)
	}
	// an interrupted teardown loads quarantined, and the operator's retry
	// and release are what release it
	st := interrupted("0vst", launcher.StateStopping)
	writeRecord(t, h.cfg.StateDir, st)
	in, err = act(h, st.Selection(), "retry")
	if err != nil {
		t.Fatalf("retry of an interrupted teardown: %v", err)
	}
	if _, err := act(h, in.Selection, "release"); err != nil {
		t.Fatalf("release of an interrupted teardown: %v", err)
	}
	if snap, err := launcher.ReadState(h.cfg.StateDir, "t"); err != nil || len(snap.Records) != 0 || len(rec.calls) != 0 {
		t.Fatalf("after releasing the interrupted teardown: %+v %v, commands %v", snap, err, rec.calls)
	}
}

// list shows an interrupted teardown the way any launcher loads it:
// quarantined, with the reason.
func TestListShowsAnInterruptedTeardownQuarantined(t *testing.T) {
	state := t.TempDir()
	left := interrupted("0vleft", launcher.StateStopping)
	writeRecord(t, state, left)
	var out bytes.Buffer
	if err := list(&Config{StateDir: state, IDPrefix: "t"}, &out, log.New(&syncBuffer{}, "", 0)); err != nil {
		t.Fatal(err)
	}
	var got launcher.Record
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		t.Fatalf("list output %q: %v", out.String(), err)
	}
	if got.ID != left.ID || got.State != launcher.StateQuarantined || !strings.Contains(got.Reason, "teardown interrupted") {
		t.Fatalf("listed %+v", got)
	}
}

// The core hands out network indexes in [MinNetIndex, MaxNetIndex]
// because that is exactly where this adapter's addressing is one to one:
// every index there has its own veth and tap subnets, and the first index
// past the top aliases index 0's — so the core's bound is the adapter's.
func TestNetworkIndexDomainIsTheAdapterAddressing(t *testing.T) {
	seen := map[string]int{}
	for i := launcher.MinNetIndex; i <= launcher.MaxNetIndex; i++ {
		vhost, vns, tap, guest := netAddrs(i)
		for _, a := range []string{vhost, vns, tap, guest} {
			if j, dup := seen[a]; dup {
				t.Fatalf("indexes %d and %d share %s", j, i, a)
			}
			seen[a] = i
		}
	}
	a0, b0, c0, d0 := netAddrs(0)
	a1, b1, c1, d1 := netAddrs(launcher.MaxNetIndex + 1)
	if a0 != a1 || b0 != b1 || c0 != c1 || d0 != d1 {
		t.Fatalf("index %d does not alias index 0: the core's bound is not the adapter's", launcher.MaxNetIndex+1)
	}
}
