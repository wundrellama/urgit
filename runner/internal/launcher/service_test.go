package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost records every privileged operation the service asks for and
// can be told to fail any of them, so rollback, quarantine and the
// deadline reaper are exercised without root (BRIEF S2.1).
type fakeHost struct {
	mu       sync.Mutex
	ops      []string
	fail     map[string]error
	alive    map[string]bool
	pids     map[string]int
	nextPID  int
	stopHang map[string]bool // Kill "succeeds" but the process stays alive
}

func newFakeHost() *fakeHost {
	return &fakeHost{fail: map[string]error{}, alive: map[string]bool{}, pids: map[string]int{}, nextPID: 1000, stopHang: map[string]bool{}}
}

func (f *fakeHost) op(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, name)
	if err, ok := f.fail[strings.SplitN(name, " ", 2)[0]]; ok {
		return err
	}
	return nil
}

func (f *fakeHost) PrepareDisk(id string, img Image, totalMiB int) (string, error) {
	return "/fake/" + id + "/disk", f.op("disk " + id)
}
func (f *fakeHost) CreateCgroup(id string, cpus, memMiB int) error { return f.op("cgroup " + id) }
func (f *fakeHost) CreateNetwork(id string, index int, allow []string) (NetInfo, error) {
	return NetInfo{GuestIP: "172.16.0.2", Gateway: "172.16.0.1", TAP: "tap0"}, f.op("net " + id)
}
func (f *fakeHost) StartVM(id string, spec VMSpec) (int, error) {
	if err := f.op("start " + id); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextPID++
	f.pids[id] = f.nextPID
	f.alive[id] = true
	return f.nextPID, nil
}
func (f *fakeHost) Connect(id string, port uint32) (*os.File, error) {
	if err := f.op("connect " + id); err != nil {
		return nil, err
	}
	r, w, _ := os.Pipe()
	w.Close()
	return r, nil
}

// Liveness is exact in this fake (stage 01, independent review 01: the
// three-valued Host.Liveness replaced Alive): running or gone.
func (f *fakeHost) Liveness(id string, pid int) (Liveness, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.alive[id] && f.pids[id] == pid {
		return Running, nil
	}
	return Gone, nil
}
func (f *fakeHost) Kill(id string, pid int, sig string) error {
	if err := f.op("kill " + id + " " + sig); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopHang[id] {
		f.alive[id] = false
	}
	return nil
}
func (f *fakeHost) RemoveNetwork(id string, index int) error { return f.op("rmnet " + id) }
func (f *fakeHost) RemoveCgroup(id string) error             { return f.op("rmcgroup " + id) }
func (f *fakeHost) RemoveDisk(id string) error               { return f.op("rmdisk " + id) }
func (f *fakeHost) RemoveJail(id string) error               { return f.op("rmjail " + id) }

func (f *fakeHost) has(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.ops {
		if strings.HasPrefix(o, prefix) {
			return true
		}
	}
	return false
}

func newService(t *testing.T, h Host, cfg Config) *Service {
	t.Helper()
	if cfg.StateDir == "" {
		cfg.StateDir = t.TempDir()
	}
	if cfg.Images == nil {
		cfg.Images = map[string]Image{"img": {Manifest: "img", Kernel: "/fake/vmlinux", Rootfs: "/fake/rootfs", BaselineMiB: 3000, BootArgs: "console=ttyS0"}}
	}
	if cfg.BudgetCPUs == 0 {
		cfg.BudgetCPUs = 8
	}
	if cfg.BudgetMemoryMiB == 0 {
		cfg.BudgetMemoryMiB = 18432
	}
	if cfg.MaxGuests == 0 {
		cfg.MaxGuests = 2
	}
	if cfg.CleanupBound == 0 {
		cfg.CleanupBound = 2 * time.Second
	}
	s, err := NewService(cfg, h)
	if err != nil {
		t.Fatal(err)
	}
	closeAtEnd(t, s)
	return s
}

func reserve(t *testing.T, s *Service, owner Owner, attempt string, cpus, mem int, deadline time.Time) ReserveReply {
	t.Helper()
	r, err := s.Reserve(owner, ReserveRequest{Attempt: attempt, Image: "img", CPUs: cpus, MemoryMiB: mem, DiskMiB: 20480, DeadlineUnix: deadline.Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var ownerA = Owner{UID: 1000, Daemon: "0va"}
var ownerB = Owner{UID: 1000, Daemon: "0vb"}

func TestLifecycleOwnsEveryResourceThenRemovesIt(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{})
	r := reserve(t, s, ownerA, "0v1", 2, 4096, time.Now().Add(time.Hour))
	if r.CID < 3 {
		t.Fatalf("cid %d", r.CID)
	}
	if _, err := s.Create(ownerA, r.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"disk " + r.ID, "cgroup " + r.ID, "start " + r.ID} {
		if !h.has(want) {
			t.Fatalf("missing host op %q in %v", want, h.ops)
		}
	}
	if h.has("net ") {
		t.Fatal("a locked VM must get no network")
	}
	rec, err := s.Inspect(ownerA, r.ID)
	if err != nil || rec.State != StateRunning || !rec.Alive {
		t.Fatalf("inspect: %v %+v", err, rec)
	}
	if err := s.Destroy(ownerA, r.ID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kill " + r.ID, "rmcgroup " + r.ID, "rmdisk " + r.ID, "rmjail " + r.ID} {
		if !h.has(want) {
			t.Fatalf("missing host op %q in %v", want, h.ops)
		}
	}
	if _, err := s.Inspect(ownerA, r.ID); !errors.Is(err, ErrUnknown) {
		t.Fatalf("destroyed VM still known: %v", err)
	}
	// destroy is idempotent
	if err := s.Destroy(ownerA, r.ID); err != nil {
		t.Fatalf("second destroy: %v", err)
	}
}

func TestOwnershipIsEnforced(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{})
	r := reserve(t, s, ownerA, "0v1", 2, 4096, time.Now().Add(time.Hour))
	s.Create(ownerA, r.ID)
	if _, err := s.Inspect(ownerB, r.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("foreign inspect: %v", err)
	}
	if err := s.Destroy(ownerB, r.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("foreign destroy: %v", err)
	}
	if _, err := s.Connect(ownerB, r.ID, 5000); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("foreign connect: %v", err)
	}
	if h.has("kill ") {
		t.Fatal("a foreign destroy touched the VM")
	}
	// a different uid with the same daemon string is a different owner
	if _, err := s.Inspect(Owner{UID: 1001, Daemon: "0va"}, r.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("other uid: %v", err)
	}
	// stage 01 (independent review 02): List answers an error while the
	// inventory is not authoritative; this one is
	list, err := s.List(ownerB)
	if err != nil || len(list) != 0 {
		t.Fatalf("B lists A's VM: %+v %v", list, err)
	}
	if got, err := s.List(ownerA); err != nil || len(got) != 1 || got[0].ID != r.ID {
		t.Fatalf("A's list: %+v %v", got, err)
	}
}

func TestAdmissionCountsEveryReservationState(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{BudgetCPUs: 4, BudgetMemoryMiB: 10240, MaxGuests: 3})
	deadline := time.Now().Add(time.Hour)
	// 2 cpus + 4096+1024 → fits; second the same → 4 cpus, 10240 → fits
	a := reserve(t, s, ownerA, "0v1", 2, 4096, deadline)
	reserve(t, s, ownerA, "0v2", 2, 4096, deadline)
	// third: over both budgets while the first two are merely preparing
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v3", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline.Unix(), Network: "locked"}); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("over budget accepted: %v", err)
	}
	// a quarantined reservation still counts
	h.fail["rmdisk"] = errors.New("disk busy")
	s.Create(ownerA, a.ID)
	if err := s.Destroy(ownerA, a.ID); err == nil {
		t.Fatal("destroy must report the failed disk removal")
	}
	rec, _ := s.Inspect(ownerA, a.ID)
	if rec.State != StateQuarantined || rec.Reason == "" {
		t.Fatalf("not quarantined: %+v", rec)
	}
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v4", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline.Unix(), Network: "locked"}); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("quarantined capacity was handed out again: %v", err)
	}
	// the operator's clear releases it (the CLI path)
	delete(h.fail, "rmdisk")
	if err := s.ClearQuarantine(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v4", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline.Unix(), Network: "locked"}); err != nil {
		t.Fatalf("after clear: %v", err)
	}
}

func TestReserveRefusesInvalidValuesAndUnknownImage(t *testing.T) {
	s := newService(t, newFakeHost(), Config{})
	deadline := time.Now().Add(time.Hour).Unix()
	bad := []ReserveRequest{
		{Attempt: "0v1", Image: "img", CPUs: 0, MemoryMiB: 4096, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"},
		{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: -1, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"},
		{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: 4096, DiskMiB: 0, DeadlineUnix: deadline, Network: "locked"},
		{Attempt: "0v1", Image: "nope", CPUs: 2, MemoryMiB: 4096, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"},
		{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: 4096, DiskMiB: 1024, DeadlineUnix: 0, Network: "locked"},
		{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: 4096, DiskMiB: 1024, DeadlineUnix: deadline, Network: "internet"},
		{Attempt: "", Image: "img", CPUs: 2, MemoryMiB: 4096, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"},
	}
	for i, req := range bad {
		if _, err := s.Reserve(ownerA, req); err == nil || errors.Is(err, ErrOverBudget) {
			t.Fatalf("request %d accepted or refused for the wrong reason: %v", i, err)
		}
	}
	// the same attempt reserved twice is one reservation, never two
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v9", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v9", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline, Network: "locked"}); !errors.Is(err, ErrExists) {
		t.Fatalf("double reservation: %v", err)
	}
}

func TestPartialCreateRollsBackOrQuarantines(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{})
	deadline := time.Now().Add(time.Hour)
	// the VMM fails to start: everything created before it is removed and
	// the reservation is released. The jailer could not be executed, so
	// nothing started, and the host says so (ErrNoEffect); a start failure
	// that may have left a VMM running is TestUncertainStartIsQuarantined
	// UntilTheOperatorClears.
	h.fail["start"] = fmt.Errorf("jailer: exec failed: %w", ErrNoEffect)
	r := reserve(t, s, ownerA, "0v1", 2, 4096, deadline)
	if _, err := s.Create(ownerA, r.ID); err == nil {
		t.Fatal("create must fail")
	}
	for _, want := range []string{"rmdisk " + r.ID, "rmcgroup " + r.ID, "rmjail " + r.ID} {
		if !h.has(want) {
			t.Fatalf("rollback missed %q: %v", want, h.ops)
		}
	}
	if _, err := s.Inspect(ownerA, r.ID); !errors.Is(err, ErrUnknown) {
		t.Fatal("a rolled-back reservation is still held")
	}
	// the VMM fails AND the rollback fails: the reservation is quarantined,
	// never silently released
	delete(h.fail, "start")
	h.fail["start"] = fmt.Errorf("jailer: exec failed: %w", ErrNoEffect)
	h.fail["rmdisk"] = errors.New("busy")
	r2 := reserve(t, s, ownerA, "0v2", 2, 4096, deadline)
	if _, err := s.Create(ownerA, r2.ID); err == nil {
		t.Fatal("create must fail")
	}
	rec, err := s.Inspect(ownerA, r2.ID)
	if err != nil || rec.State != StateQuarantined {
		t.Fatalf("not quarantined after failed rollback: %v %+v", err, rec)
	}
}

func TestDeadlineReaperStopsAndCleansWithinBound(t *testing.T) {
	h := newFakeHost()
	cfg := Config{CleanupBound: 2 * time.Second}
	clock := withClock(&cfg)
	s := newService(t, h, cfg)
	r := reserve(t, s, ownerA, "0v1", 2, 4096, time.Now().Add(time.Minute))
	if _, err := s.Create(ownerA, r.ID); err != nil {
		t.Fatal(err)
	}
	clock.pass(t, s, r.ID) // the VM runs past its deadline
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Reap(ctx, 100*time.Millisecond)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec, err := s.Inspect(ownerA, r.ID)
		if errors.Is(err, ErrUnknown) || (err == nil && rec.State == StateReaped) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !h.has("kill "+r.ID) || !h.has("rmdisk "+r.ID) {
		t.Fatalf("the reaper did not stop and clean the VM: %v", h.ops)
	}
	// the record says why, whatever the daemon does next
	hist := s.History()
	found := false
	for _, e := range hist {
		if e.ID == r.ID && e.State == StateReaped {
			found = true
		}
	}
	if !found {
		t.Fatalf("no reaped record in history: %+v", hist)
	}
}

// a process that survives SIGTERM and SIGKILL within the cleanup bound is
// quarantined with the failure visible — never reported stopped
func TestSurvivingProcessIsQuarantinedNotReportedStopped(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{CleanupBound: 300 * time.Millisecond})
	r := reserve(t, s, ownerA, "0v1", 2, 4096, time.Now().Add(time.Hour))
	s.Create(ownerA, r.ID)
	h.stopHang[r.ID] = true
	err := s.Destroy(ownerA, r.ID)
	if err == nil {
		t.Fatal("destroy reported success for a process that did not stop")
	}
	rec, _ := s.Inspect(ownerA, r.ID)
	if rec.State != StateQuarantined || !strings.Contains(rec.Reason, "still alive") {
		t.Fatalf("expected quarantine naming the live process: %+v", rec)
	}
}

func TestRestartRecoversReservationsAndReaps(t *testing.T) {
	h := newFakeHost()
	state := t.TempDir()
	cfg := Config{StateDir: state}
	clock := withClock(&cfg)
	s := newService(t, h, cfg)
	r := reserve(t, s, ownerA, "0v1", 2, 4096, time.Now().Add(time.Minute))
	if _, err := s.Create(ownerA, r.ID); err != nil {
		t.Fatal(err)
	}
	r2 := reserve(t, s, ownerA, "0v2", 2, 4096, time.Now().Add(time.Hour))
	clock.advance(expire) // 0v1 is now past its deadline
	// a new service over the same state dir (the launcher restarted): the
	// old one stops first, since one Service owns a state directory
	// (TestSecondOpenerIsRefusedWhileTheStateIsHeld)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := newService(t, h, cfg)
	if got, err := s2.List(ownerA); err != nil || len(got) != 2 {
		t.Fatalf("records lost across restart: %+v %v", got, err)
	}
	// budget still counts them: a third 4-cpu guest does not fit in 8
	if _, err := s2.Reserve(ownerA, ReserveRequest{Attempt: "0v3", Image: "img", CPUs: 4, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("restart forgot the reservations: %v", err)
	}
	// the expired one is reaped by the recovered service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s2.Reap(ctx, 50*time.Millisecond)
	time.Sleep(600 * time.Millisecond)
	if !h.has("kill " + r.ID) {
		t.Fatalf("expired VM not reaped after restart: %v", h.ops)
	}
	if h.has("kill " + r2.ID) {
		t.Fatal("a VM within its deadline was reaped")
	}
	// the record files are what survived
	files, _ := filepath.Glob(filepath.Join(state, "attempts", "*.json"))
	if len(files) == 0 {
		t.Fatal("no persisted attempt records")
	}
}

func TestNetworkPolicyIsIntersectedWithTheCeiling(t *testing.T) {
	h := newFakeHost()
	s := newService(t, h, Config{Ceiling: []string{"tcp:192.168.1.229:8472", "tcp:192.168.1.229:8474"}})
	deadline := time.Now().Add(time.Hour).Unix()
	// a destination outside the launcher's ceiling is refused, never trimmed silently
	_, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline, Network: "integration", Destinations: []string{"tcp:192.168.1.229:8472", "tcp:10.0.0.5:22"}})
	if err == nil || !strings.Contains(err.Error(), "10.0.0.5:22") {
		t.Fatalf("destination outside the ceiling accepted: %v", err)
	}
	r, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v2", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline, Network: "integration", Destinations: []string{"tcp:192.168.1.229:8472"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ownerA, r.ID); err != nil {
		t.Fatal(err)
	}
	if !h.has("net " + r.ID) {
		t.Fatal("a networked VM got no network")
	}
	// a networked profile with no destinations is refused, never "any"
	if _, err := s.Reserve(ownerA, ReserveRequest{Attempt: "0v3", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: deadline, Network: "integration"}); err == nil {
		t.Fatal("an empty destination list on a networked profile was accepted")
	}
}
