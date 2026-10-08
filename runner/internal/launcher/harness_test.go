package launcher

// Test harness for the lifecycle model (runner/launcher/LIFECYCLE.md):
//
//   - tracer: one ordered log of the store's filesystem calls and the
//     host's operations, so a test can show what was durable before each
//     host effect;
//   - faultFS: the production osFS with faults around the REAL call —
//     "before" (the call is not made), "after" (the call is made, then an
//     error is returned: effect-then-error), "short" (half a write) —
//     and barriers, counting the real calls it made;
//   - modelHost: a nonexecuting host whose resources are marker files in
//     a private directory (so they outlive a killed child process), with
//     per-operation faults (before / noeffect / after) and barriers.
//
// Nothing here runs a command, a VM, a jailer or a network operation.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- tracer --------------------------------------------------------------

type event struct {
	kind   string // fs | host
	op     string
	id     string
	state  string   // fs rename: the published record's state
	holds  []string // fs rename: the published record's holdings
	err    string
	inject string // "", before, after, short
}

func (e event) String() string {
	s := e.kind + " " + e.op
	if e.id != "" {
		s += " " + e.id
	}
	if e.state != "" {
		s += fmt.Sprintf(" [%s holds=%v]", e.state, e.holds)
	}
	if e.inject != "" {
		s += " INJECTED-" + e.inject
	}
	if e.err != "" {
		s += " -> " + e.err
	}
	return s
}

type tracer struct {
	mu     sync.Mutex
	events []event
}

func (tr *tracer) add(e event) {
	if tr == nil {
		return
	}
	tr.mu.Lock()
	tr.events = append(tr.events, e)
	tr.mu.Unlock()
}

func (tr *tracer) all() []event {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]event(nil), tr.events...)
}

func (tr *tracer) dump(t *testing.T) {
	t.Helper()
	for i, e := range tr.all() {
		t.Logf("trace %02d %s", i, e)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// idOf is the record id a state path names (<id>.json or <id>.json.tmp).
func idOf(path string) string {
	b := filepath.Base(path)
	b = strings.TrimSuffix(b, ".tmp")
	return strings.TrimSuffix(b, ".json")
}

// ---- faultFS -------------------------------------------------------------

type fsFault struct {
	op    string // open write sync close rename unlink syncdir readdir lstat readfile
	path  string // suffix of the path (rename: the destination); "" = any
	mode  string // before | after | short | "" (barrier only)
	err   error
	times int    // how many matching calls to fault; 0 = all
	block func() // a barrier, called before the real call
	hits  int
}

type faultFS struct {
	inner  fileSystem
	tr     *tracer
	mu     sync.Mutex
	faults []*fsFault
	calls  map[string]int // real calls made, per op
}

func newFaultFS(tr *tracer) *faultFS {
	return &faultFS{inner: osFS{}, tr: tr, calls: map[string]int{}}
}

func (f *faultFS) inject(x *fsFault) *fsFault {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, x)
	return x
}

func (f *faultFS) clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = nil
}

func (f *faultFS) realCalls(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func (f *faultFS) match(op, path string) *fsFault {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.faults {
		if x.op == op && strings.HasSuffix(path, x.path) && (x.times == 0 || x.hits < x.times) {
			x.hits++
			return x
		}
	}
	return nil
}

func (f *faultFS) counted(op string) {
	f.mu.Lock()
	f.calls[op]++
	f.mu.Unlock()
}

// do runs one adapter call through the fault table.
func (f *faultFS) do(op, path string, e event, real func() error) error {
	x := f.match(op, path)
	if x != nil && x.block != nil {
		f.tr.add(event{kind: "fs", op: "barrier-" + op, id: idOf(path)})
		x.block()
	}
	e.kind, e.op, e.id = "fs", op, idOf(path)
	if x != nil && x.mode == "before" {
		e.inject, e.err = "before", x.err.Error()
		f.tr.add(e)
		return x.err
	}
	err := real()
	f.counted(op)
	if x != nil && x.mode == "after" {
		e.inject, e.err = "after", fmt.Sprintf("real: %v; injected: %v", err, x.err)
		f.tr.add(e)
		return x.err
	}
	e.err = errString(err)
	f.tr.add(e)
	return err
}

func (f *faultFS) OpenFile(name string, flag int, perm os.FileMode) (writableFile, error) {
	var file writableFile
	err := f.do("open", name, event{}, func() error {
		var err error
		file, err = f.inner.OpenFile(name, flag, perm)
		return err
	})
	if err != nil {
		if file != nil { // an "after" fault: the real open happened
			file.Close()
		}
		return nil, err
	}
	return &faultFile{inner: file, path: name, fs: f}, nil
}

// Rename also records what the published bytes say (state, holdings).
func (f *faultFS) Rename(oldpath, newpath string) error {
	e := event{}
	if data, err := os.ReadFile(oldpath); err == nil {
		var rf recordFile
		if json.Unmarshal(data, &rf) == nil {
			e.state, e.holds = string(rf.Record.State), holdsOf(rf.Record)
		}
	}
	return f.do("rename", newpath, e, func() error { return f.inner.Rename(oldpath, newpath) })
}

func (f *faultFS) Unlink(name string) error {
	return f.do("unlink", name, event{}, func() error { return f.inner.Unlink(name) })
}

func (f *faultFS) SyncDir(name string) error {
	return f.do("syncdir", name, event{}, func() error { return f.inner.SyncDir(name) })
}

func (f *faultFS) ReadDir(name string) ([]os.DirEntry, error) {
	var out []os.DirEntry
	err := f.do("readdir", name, event{}, func() error {
		var err error
		out, err = f.inner.ReadDir(name)
		return err
	})
	return out, err
}

func (f *faultFS) Lstat(name string) (os.FileInfo, error) {
	var out os.FileInfo
	err := f.do("lstat", name, event{}, func() error {
		var err error
		out, err = f.inner.Lstat(name)
		return err
	})
	return out, err
}

func (f *faultFS) ReadFile(name string, max int64) ([]byte, error) {
	var out []byte
	err := f.do("readfile", name, event{}, func() error {
		var err error
		out, err = f.inner.ReadFile(name, max)
		return err
	})
	return out, err
}

type faultFile struct {
	inner writableFile
	path  string
	fs    *faultFS
}

func (w *faultFile) Write(p []byte) (int, error) {
	x := w.fs.match("write", w.path)
	if x != nil && x.block != nil {
		x.block()
	}
	e := event{kind: "fs", op: "write", id: idOf(w.path)}
	switch {
	case x != nil && x.mode == "before":
		e.inject, e.err = "before", x.err.Error()
		w.fs.tr.add(e)
		return 0, x.err
	case x != nil && x.mode == "short":
		n, err := w.inner.Write(p[:len(p)/2])
		w.fs.counted("write")
		e.inject, e.err = "short", fmt.Sprintf("real %d/%d bytes (%v); injected: %v", n, len(p), err, x.err)
		w.fs.tr.add(e)
		return n, x.err
	}
	n, err := w.inner.Write(p)
	w.fs.counted("write")
	if x != nil && x.mode == "after" {
		e.inject, e.err = "after", fmt.Sprintf("real: %v; injected: %v", err, x.err)
		w.fs.tr.add(e)
		return n, x.err
	}
	e.err = errString(err)
	w.fs.tr.add(e)
	return n, err
}

func (w *faultFile) Sync() error {
	return w.fs.do("sync", w.path, event{}, w.inner.Sync)
}

func (w *faultFile) Close() error {
	return w.fs.do("close", w.path, event{}, w.inner.Close)
}

func (w *faultFile) Truncate(size int64) error {
	return w.fs.do("truncate", w.path, event{}, func() error { return w.inner.Truncate(size) })
}

func (w *faultFile) Stat() (os.FileInfo, error) { return w.inner.Stat() }

func holdsOf(r Record) []string {
	var h []string
	if r.HasDisk {
		h = append(h, "disk")
	}
	if r.HasCgroup {
		h = append(h, "cgroup")
	}
	if r.HasNetwork {
		h = append(h, "network")
	}
	if r.HasVMM {
		h = append(h, "vmm")
	}
	return h
}

// ---- modelHost -----------------------------------------------------------

type hostFault struct {
	mode  string // before (no effect, plain error) | noeffect (no effect, ErrNoEffect) | after (acted, then error)
	err   error
	times int
	hits  int
}

// modelHost keeps its "resources" as marker files under dir:
// jail/<id>/disk.ext4, cgroup/<id>, network/<id> (the index), vmm/<id>
// (the pid). It executes nothing.
type modelHost struct {
	dir    string
	tr     *tracer
	mu     sync.Mutex
	faults map[string]*hostFault // by op: prepare cgroup network start kill connect rmnet rmcgroup rmdisk rmjail
	points map[string]func(id string)
	calls  map[string]int
	hang   map[string]bool // a VMM that survives TERM and KILL
	// unknown: ids whose VMM's state the model cannot verify (stage 01,
	// independent review 01: Liveness answers Unknown for them)
	unknown map[string]bool
	nextPID int
}

func newModelHost(t *testing.T, tr *tracer) *modelHost {
	t.Helper()
	return openModelHost(t.TempDir(), tr)
}

func openModelHost(dir string, tr *tracer) *modelHost {
	for _, k := range []string{"jail", "cgroup", "network", "vmm"} {
		if err := os.MkdirAll(filepath.Join(dir, k), 0o700); err != nil {
			panic(err)
		}
	}
	return &modelHost{dir: dir, tr: tr, faults: map[string]*hostFault{}, points: map[string]func(string){}, calls: map[string]int{}, hang: map[string]bool{}, unknown: map[string]bool{}, nextPID: 40000}
}

func (h *modelHost) fail(op, mode string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.faults[op] = &hostFault{mode: mode, err: err}
}

func (h *modelHost) heal(op string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.faults, op)
}

func (h *modelHost) at(point string, fn func(id string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points[point] = fn
}

func (h *modelHost) count(op string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[op]
}

func (h *modelHost) point(p, id string) {
	h.mu.Lock()
	fn := h.points[p]
	h.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

// enter counts the call, runs the "before" barrier and any fault of the
// before phase.
func (h *modelHost) enter(op, id string) error {
	h.mu.Lock()
	h.calls[op]++
	h.mu.Unlock()
	h.point(op+"/before", id)
	return h.injected(op, id, false)
}

// leave runs the "after" barrier and any fault of the after phase.
func (h *modelHost) leave(op, id string) error {
	h.point(op+"/after", id)
	return h.injected(op, id, true)
}

func (h *modelHost) injected(op, id string, after bool) error {
	h.mu.Lock()
	x := h.faults[op]
	var err error
	if x != nil && (x.times == 0 || x.hits < x.times) {
		switch {
		case !after && x.mode == "before":
			x.hits++
			err = x.err
		case !after && x.mode == "noeffect":
			x.hits++
			err = fmt.Errorf("%w: %w", x.err, ErrNoEffect)
		case after && x.mode == "after":
			x.hits++
			err = x.err
		}
	}
	h.mu.Unlock()
	if err != nil {
		phase := "before"
		if after {
			phase = "after"
		}
		h.tr.add(event{kind: "host", op: op, id: id, inject: phase, err: err.Error()})
	}
	return err
}

func (h *modelHost) marker(kind, id string) string { return filepath.Join(h.dir, kind, id) }

func (h *modelHost) exists(kind, id string) bool {
	_, err := os.Lstat(h.marker(kind, id))
	return err == nil
}

// leaks lists every marker id still has.
func (h *modelHost) leaks(id string) []string {
	var out []string
	for _, k := range []string{"jail", "cgroup", "network", "vmm"} {
		if h.exists(k, id) {
			out = append(out, k)
		}
	}
	return out
}

func (h *modelHost) did(op, id string) {
	h.tr.add(event{kind: "host", op: op, id: id})
}

func (h *modelHost) PrepareDisk(id string, img Image, totalMiB int) (string, error) {
	if err := h.enter("prepare", id); err != nil {
		return "", err
	}
	root := h.marker("jail", id)
	if h.exists("jail", id) {
		return "", fmt.Errorf("jail root %s already exists", root)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	disk := filepath.Join(root, "disk.ext4")
	if err := os.WriteFile(disk, []byte(strconv.Itoa(totalMiB)), 0o600); err != nil {
		return "", err
	}
	h.did("prepare", id)
	if err := h.leave("prepare", id); err != nil {
		return "", err
	}
	return disk, nil
}

func (h *modelHost) CreateCgroup(id string, cpus, memMiB int) error {
	if err := h.enter("cgroup", id); err != nil {
		return err
	}
	if err := os.WriteFile(h.marker("cgroup", id), []byte(fmt.Sprintf("%d %d", cpus, memMiB)), 0o600); err != nil {
		return err
	}
	h.did("cgroup", id)
	return h.leave("cgroup", id)
}

func (h *modelHost) CreateNetwork(id string, index int, allow []string) (NetInfo, error) {
	if err := h.enter("network", id); err != nil {
		return NetInfo{}, err
	}
	if err := os.WriteFile(h.marker("network", id), []byte(strconv.Itoa(index)), 0o600); err != nil {
		return NetInfo{}, err
	}
	h.did("network", id)
	if err := h.leave("network", id); err != nil {
		return NetInfo{}, err
	}
	return NetInfo{TAP: "tap0", GuestIP: "172.16.0.2/30", Gateway: "172.16.0.1", Index: index}, nil
}

func (h *modelHost) StartVM(id string, spec VMSpec) (int, error) {
	if err := h.enter("start", id); err != nil {
		return 0, err
	}
	h.mu.Lock()
	h.nextPID++
	pid := h.nextPID
	h.mu.Unlock()
	if err := os.WriteFile(h.marker("vmm", id), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return 0, err
	}
	h.did("start", id)
	if err := h.leave("start", id); err != nil {
		return 0, err // a VMM runs; its pid is lost with this error
	}
	return pid, nil
}

func (h *modelHost) Connect(id string, port uint32) (*os.File, error) {
	if err := h.enter("connect", id); err != nil {
		return nil, err
	}
	if !h.exists("vmm", id) {
		return nil, errors.New("no vmm")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	w.Close()
	h.did("connect", id)
	return r, h.leave("connect", id)
}

// Liveness is the model's answer (stage 01, independent review 01: the
// three-valued Host.Liveness replaced Alive): running while the VMM marker
// names pid, gone otherwise — unknown for an id the test marks so.
func (h *modelHost) Liveness(id string, pid int) (Liveness, error) {
	h.mu.Lock()
	unknown := h.unknown[id]
	h.mu.Unlock()
	if unknown {
		return Unknown, errors.New("the model cannot verify it")
	}
	data, err := os.ReadFile(h.marker("vmm", id))
	if err == nil && string(data) == strconv.Itoa(pid) {
		return Running, nil
	}
	return Gone, nil
}

func (h *modelHost) Kill(id string, pid int, sig string) error {
	if err := h.enter("kill", id); err != nil {
		return err
	}
	h.mu.Lock()
	hang := h.hang[id]
	h.mu.Unlock()
	if l, _ := h.Liveness(id, pid); !hang && l == Running {
		if err := os.Remove(h.marker("vmm", id)); err != nil {
			return err
		}
	}
	h.did("kill-"+sig, id)
	return h.leave("kill", id)
}

// RemoveNetwork insists on the index the network was created with: a
// cleanup that lost it would leave the per-VM policy behind.
func (h *modelHost) RemoveNetwork(id string, index int) error {
	if err := h.enter("rmnet", id); err != nil {
		return err
	}
	data, err := os.ReadFile(h.marker("network", id))
	if err == nil {
		if string(data) != strconv.Itoa(index) {
			return fmt.Errorf("network of %s has index %s, not %d", id, data, index)
		}
		if err := os.Remove(h.marker("network", id)); err != nil {
			return err
		}
	}
	h.did("rmnet", id)
	return h.leave("rmnet", id)
}

func (h *modelHost) RemoveCgroup(id string) error {
	if err := h.enter("rmcgroup", id); err != nil {
		return err
	}
	if h.exists("vmm", id) {
		return fmt.Errorf("cgroup of %s still has processes", id)
	}
	if err := os.Remove(h.marker("cgroup", id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	h.did("rmcgroup", id)
	return h.leave("rmcgroup", id)
}

func (h *modelHost) RemoveDisk(id string) error {
	if err := h.enter("rmdisk", id); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(h.marker("jail", id), "disk.ext4")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	h.did("rmdisk", id)
	return h.leave("rmdisk", id)
}

func (h *modelHost) RemoveJail(id string) error {
	if err := h.enter("rmjail", id); err != nil {
		return err
	}
	if err := os.RemoveAll(h.marker("jail", id)); err != nil {
		return err
	}
	h.did("rmjail", id)
	return h.leave("rmjail", id)
}

// ---- service helpers -----------------------------------------------------

// testConfig is a small launcher: two guests, 8 cpus, 18432 MiB.
func testConfig(state string) Config {
	return Config{
		StateDir: state, IDPrefix: "t",
		Images:     map[string]Image{"img": {Manifest: "img", Kernel: "/model/vmlinux", Rootfs: "/model/rootfs", BaselineMiB: 100, BootArgs: "console=ttyS0"}},
		BudgetCPUs: 8, BudgetMemoryMiB: 18432, MaxGuests: 2, CleanupBound: 400 * time.Millisecond,
		Ceiling: []string{"tcp:192.0.2.1:8472"},
	}
}

// open opens a service on cfg's state directory through fsys (nil: the
// production adapter) and closes it at the end of the test.
func open(t *testing.T, cfg Config, h Host, fsys fileSystem) *Service {
	t.Helper()
	if fsys == nil {
		fsys = osFS{}
	}
	s, err := openService(cfg, h, fsys)
	if err != nil {
		t.Fatalf("open %s: %v", cfg.StateDir, err)
	}
	closeAtEnd(t, s)
	return s
}

// closeAtEnd closes s when the test ends; an operation token never given
// back is a failure of the test, not a hang of the binary.
func closeAtEnd(t *testing.T, s *Service) {
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() {
			s.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("Close did not return: an operation still holds a record's token")
		}
	})
}

// reopen closes s (a process stop) and opens the state again.
func reopen(t *testing.T, s *Service, h Host) *Service {
	t.Helper()
	cfg := s.cfg
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return open(t, cfg, h, nil)
}

// The server names a connection's owner by the peer's uid, so the fixture
// owners carry this process's own uid: a test that reserves directly and
// lists over the wire then finds the same owner on any CI account.
var owner1 = Owner{UID: uint32(os.Getuid()), Daemon: "0vd1"}
var owner2 = Owner{UID: uint32(os.Getuid()), Daemon: "0vd2"}

func lockedReq(attempt string, cpus, mem int) ReserveRequest {
	return ReserveRequest{Attempt: attempt, Image: "img", CPUs: cpus, MemoryMiB: mem, DiskMiB: 10, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}
}

// testClock is a service's clock in a test: the wall clock moved forward
// by what the test advanced, so a deadline passes the way it does in
// production — after the work began — without anything started past it.
type testClock struct{ off atomic.Int64 }

func (c *testClock) now() time.Time          { return time.Now().Add(time.Duration(c.off.Load())) }
func (c *testClock) advance(d time.Duration) { c.off.Add(int64(d)) }

// withClock gives cfg a clock the test moves (reopen keeps it: it is part
// of the service's Config).
func withClock(cfg *Config) *testClock {
	c := &testClock{}
	cfg.now = c.now
	return c
}

// expiring is lockedReq with its deadline a minute away; clock.advance(
// expire) lets it pass while a fresh lockedReq stays an hour away.
func expiring(attempt string, cpus, mem int) ReserveRequest {
	r := lockedReq(attempt, cpus, mem)
	r.DeadlineUnix = time.Now().Add(time.Minute).Unix()
	return r
}

const expire = 2 * time.Minute

func mustReserve(t *testing.T, s *Service, o Owner, req ReserveRequest) ReserveReply {
	t.Helper()
	r, err := s.Reserve(o, req)
	if err != nil {
		t.Fatalf("reserve %s: %v", req.Attempt, err)
	}
	return r
}

// used is the charge the service holds: cpus, MiB, guests.
func used(s *Service) [3]int {
	_, _, _, c, m, g := s.Budget()
	return [3]int{c, m, g}
}

func held(s *Service, id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.vms[id]
	if !ok {
		return Record{}, false
	}
	return e.rec, true
}

// durable reads the authoritative record file of id, if it is one.
func durable(t *testing.T, state, id string) (Record, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "attempts", id+".json"))
	if err != nil {
		return Record{}, false
	}
	var rf recordFile
	if err := json.Unmarshal(data, &rf); err != nil {
		t.Fatalf("record %s unparseable: %v", id, err)
	}
	return rf.Record, true
}

// stateEntries lists the attempts directory: name -> kind.
func stateEntries(t *testing.T, state string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(state, "attempts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		fi, err := os.Lstat(filepath.Join(state, "attempts", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = kindOf(fi.Mode())
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// waitFor polls cond for up to d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// gate is a one-shot barrier: reached is closed when a goroutine arrives,
// which then waits for release.
type gate struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{reached: make(chan struct{}), release: make(chan struct{})} }

func (g *gate) wait(string) {
	g.once.Do(func() { close(g.reached) })
	<-g.release
}

func (g *gate) open() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}
