// Package launchertest is the in-process launcher the sandbox and daemon
// tests drive: the real launcher core and wire server on a private unix
// socket, over a nonexecuting Host whose VMs are entries in memory and
// whose vsock connection is one end of a socketpair answered by a minimal
// guest-helper responder (HELLO → READY, then SHUTDOWN or EOF). Nothing
// here runs a command, a VM, a guest helper service or a network
// operation; it is imported by tests only.
package launchertest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"urgit/runner/internal/guest"
	"urgit/runner/internal/launcher"
)

// Host is a nonexecuting launcher.Host. Fail makes an operation return an
// error (op names: disk cgroup network start connect rmnet rmcgroup rmdisk
// rmjail); a VM "runs" from StartVM until Kill.
type Host struct {
	mu     sync.Mutex
	ops    []string
	fail   map[string]error
	hold   map[string]chan struct{} // op -> closed when the held op may go on
	held   chan string              // "op id" of each op that reached a hold
	alive  map[string]int
	next   int
	Bridge string // what the fake helper's READY names
	// Pins is what CreateNetwork answers as pinned (NetInfo.Pinned): a
	// test sets it to model the launcher's pinned names
	Pins []launcher.Pin
}

func NewHost() *Host {
	return &Host{fail: map[string]error{}, hold: map[string]chan struct{}{}, held: make(chan string, 64), alive: map[string]int{}, next: 7000, Bridge: "10.0.2.1"}
}

// Hold makes every later op of that name wait, once it has been recorded
// and announced on Reached, until release is called (or the test ends).
func (h *Host) Hold(t testing.TB, op string) (release func()) {
	gate := make(chan struct{})
	h.mu.Lock()
	h.hold[op] = gate
	h.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.hold, op)
			h.mu.Unlock()
			close(gate)
		})
	}
	t.Cleanup(release)
	return release
}

// Reached announces "op id" for each op that reached a Hold.
func (h *Host) Reached() <-chan string { return h.held }

// SetFail makes op fail with err from now on (nil heals it).
func (h *Host) SetFail(op string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		delete(h.fail, op)
		return
	}
	h.fail[op] = err
}

// Ops is every operation so far, as "op id".
func (h *Host) Ops() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ops...)
}

// Count is how many times op ran for id ("" = any id).
func (h *Host) Count(op, id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, o := range h.ops {
		if o == op+" "+id || (id == "" && strings.HasPrefix(o, op+" ")) {
			n++
		}
	}
	return n
}

func (h *Host) do(op, id string) error {
	h.mu.Lock()
	h.ops = append(h.ops, op+" "+id)
	gate := h.hold[op]
	h.mu.Unlock()
	if gate != nil {
		h.held <- op + " " + id
		<-gate
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fail[op]
}

func (h *Host) PrepareDisk(id string, img launcher.Image, totalMiB int) (string, error) {
	return "/model/jail/" + id + "/disk.ext4", h.do("disk", id)
}
func (h *Host) CreateCgroup(id string, cpus, memMiB int) error { return h.do("cgroup", id) }
func (h *Host) CreateNetwork(id string, index int, allow []string) (launcher.NetInfo, error) {
	h.mu.Lock()
	pins := h.Pins
	h.mu.Unlock()
	return launcher.NetInfo{TAP: "tap0", GuestIP: "172.16.0.2/30", Gateway: "172.16.0.1", Index: index, Pinned: pins}, h.do("network", id)
}
func (h *Host) StartVM(id string, spec launcher.VMSpec) (int, error) {
	if err := h.do("start", id); err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	h.alive[id] = h.next
	return h.next, nil
}
func (h *Host) Liveness(id string, pid int) (launcher.Liveness, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pid != 0 && h.alive[id] == pid {
		return launcher.Running, nil
	}
	return launcher.Gone, nil
}
func (h *Host) Kill(id string, pid int, sig string) error {
	if err := h.do("kill", id); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.alive[id] == pid {
		delete(h.alive, id)
	}
	return nil
}
func (h *Host) RemoveNetwork(id string, index int) error { return h.do("rmnet", id) }
func (h *Host) RemoveCgroup(id string) error             { return h.do("rmcgroup", id) }
func (h *Host) RemoveDisk(id string) error               { return h.do("rmdisk", id) }
func (h *Host) RemoveJail(id string) error               { return h.do("rmjail", id) }

// Connect hands back one end of a socketpair; a goroutine on the other end
// answers the guest helper's HELLO with READY (naming h.Bridge) and then
// reads until SHUTDOWN or EOF.
func (h *Host) Connect(id string, port uint32) (*os.File, error) {
	if err := h.do("connect", id); err != nil {
		return nil, err
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	mine, theirs := os.NewFile(uintptr(fds[0]), "vsock-host"), os.NewFile(uintptr(fds[1]), "vsock-guest")
	h.mu.Lock()
	bridge := h.Bridge
	h.mu.Unlock()
	go answer(theirs, bridge)
	return mine, nil
}

func answer(conn *os.File, bridge string) {
	defer conn.Close()
	for {
		typ, _, err := guest.ReadFrame(conn, guest.MaxPayload)
		if err != nil {
			return
		}
		switch typ {
		case guest.TypeHello:
			ready, _ := json.Marshal(guest.Ready{Helper: guest.ProtocolVersion, Bridge: bridge})
			if guest.WriteFrame(conn, guest.TypeReady, ready) != nil {
				return
			}
		case guest.TypeShutdown:
			return
		}
	}
}

// Server is a launcher core served on a private unix socket.
type Server struct {
	Socket  string // relative to the test's working directory (t.Chdir)
	Service *launcher.Service
	cfg     launcher.Config
	host    launcher.Host
	stop    func()
}

// Serve opens a launcher core (cfg.StateDir, else a fresh private one)
// over host and serves it on a fresh private unix socket for uid. The
// test's working directory becomes a fresh private directory, so the
// socket path stays short.
func Serve(t testing.TB, host launcher.Host, cfg launcher.Config) *Server {
	t.Helper()
	if cfg.StateDir == "" {
		cfg.StateDir = t.TempDir()
	}
	t.Chdir(t.TempDir())
	s := &Server{cfg: cfg, host: host}
	s.start(t)
	return s
}

func (s *Server) start(t testing.TB) {
	t.Helper()
	svc, err := launcher.NewService(s.cfg, s.host)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	if s.Socket == "" {
		s.Socket = fmt.Sprintf("l%d.sock", time.Now().UnixNano()%1000000)
	}
	l, err := net.Listen("unix", s.Socket)
	if err != nil {
		svc.Close()
		t.Fatalf("listen: %v", err)
	}
	sv := &launcher.Server{Service: svc, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	done := make(chan struct{})
	go func() { _ = sv.Serve(l); close(done) }()
	s.Service = svc
	var once sync.Once
	s.stop = func() {
		once.Do(func() {
			l.Close()
			<-done
			svc.Close()
		})
	}
	t.Cleanup(s.stop)
}

// Restart stops the launcher (its lock released, its VMs left as they
// are) and serves a new one over the same state directory and host at the
// same socket path — a launcher process restart. A connection still open
// across it is answered by the stopped launcher (ErrClosed), never by the
// new one.
func (s *Server) Restart(t testing.TB) {
	t.Helper()
	s.stop()
	s.start(t)
}

// StateDir is the state directory the launcher serves: a test's private
// fixture, whose records a test may spoil between lives.
func (s *Server) StateDir() string { return s.cfg.StateDir }

// Image writes a minimal guest image directory whose manifest digests
// match its kernel and rootfs, for actImage; it returns the directory and
// the manifest's sha256 (the image identity the launcher boots).
func Image(t testing.TB, actImage string) (dir, digest string) {
	t.Helper()
	dir = t.TempDir()
	write := func(name, data string) string {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(data))
		return hex.EncodeToString(sum[:])
	}
	kernel, rootfs := write("vmlinux", "kernel"), write("rootfs.ext4", "rootfs")
	m := map[string]any{
		"version":   1,
		"kernel":    map[string]any{"name": "vmlinux", "sha256": kernel},
		"rootfs":    map[string]any{"name": "rootfs.ext4", "sha256": rootfs, "baseline_mib": 1},
		"act_image": map[string]any{"reference": actImage},
		"helper":    map[string]any{"version": guest.ProtocolVersion},
		"boot_args": "console=ttyS0",
	}
	data, _ := json.Marshal(m)
	write("manifest.json", string(data))
	sum := sha256.Sum256(data)
	return dir, hex.EncodeToString(sum[:])
}

// Config is a launcher configuration for image digest, with room for
// guests guests of 1 cpu / 128 MiB.
func Config(digest string, guests int) launcher.Config {
	return launcher.Config{
		IDPrefix: "t", Images: map[string]launcher.Image{digest: {Manifest: digest, Kernel: "/model/vmlinux", Rootfs: "/model/rootfs", BaselineMiB: 1}},
		BudgetCPUs: 2 * guests, BudgetMemoryMiB: guests * (512 + launcher.OverheadMiB), MaxGuests: guests, CleanupBound: 400 * time.Millisecond,
	}
}

// Held lists the launcher's records for daemon, via a fresh client.
func (s *Server) Held(t testing.TB, daemon string) []launcher.Record {
	t.Helper()
	cl, err := launcher.Dial(context.Background(), s.Socket, daemon)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cl.Close()
	recs, err := cl.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return recs
}

// ErrInjected is the fault the tests inject.
var ErrInjected = errors.New("injected host failure")
