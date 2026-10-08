package launcher

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func serve(t *testing.T, h Host, allowed map[uint32]bool) (string, *Service) {
	t.Helper()
	s := newService(t, h, Config{Ceiling: []string{"tcp:192.168.1.229:8472"}})
	// a unix socket path is 108 bytes at most: the harness's TMPDIR plus
	// a long test name overflows it, so the socket is made relative to a
	// temporary working directory
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(".", fmt.Sprintf("l%d.sock", time.Now().UnixNano()%100000))
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: allowed, Version: "test"}
	go sv.Serve(l)
	t.Cleanup(func() { l.Close() })
	return path, s
}

// Stage 01 (independent review 01): every create, connect, stop and destroy
// on the wire names the incarnation it means (its token: r.Ref()); the
// scenarios of these tests are otherwise unchanged.
func TestWireLifecycleWithDescriptorPassing(t *testing.T) {
	h := newFakeHost()
	path, _ := serve(t, h, map[uint32]bool{uint32(os.Getuid()): true})
	cl, err := Dial(context.Background(), path, "0va")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if cl.Hello.Budget == nil || cl.Hello.Budget.CPUs != 8 || len(cl.Hello.Ceiling) != 1 {
		t.Fatalf("hello: %+v", cl.Hello)
	}
	r, err := cl.Reserve(ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 2, MemoryMiB: 4096, DiskMiB: 20480, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if pid, err := cl.Create(r.Ref()); err != nil || pid == 0 {
		t.Fatalf("create: %v %d", err, pid)
	}
	fd, err := cl.Connect(r.Ref(), 5000)
	if err != nil {
		t.Fatal(err)
	}
	// the fake host hands a pipe whose writer is closed: reading it hits EOF
	if _, err := io.ReadAll(fd); err != nil {
		t.Fatalf("passed descriptor unusable: %v", err)
	}
	fd.Close()
	rec, err := cl.Inspect(r.ID)
	if err != nil || rec.State != StateRunning || !rec.Alive {
		t.Fatalf("inspect: %v %+v", err, rec)
	}
	vms, _ := cl.List()
	if len(vms) != 1 {
		t.Fatalf("list: %+v", vms)
	}
	if q, err := cl.DestroyOf(r.Ref()); err != nil || q {
		t.Fatalf("destroy: %v %v", err, q)
	}
	if _, err := cl.Inspect(r.ID); err == nil || !strings.Contains(err.Error(), "no such vm") {
		t.Fatalf("after destroy: %v", err)
	}
}

func TestWireRefusesForeignUIDAndForeignDaemon(t *testing.T) {
	h := newFakeHost()
	// a server that allows only some other uid refuses this process
	path, _ := serve(t, h, map[uint32]bool{uint32(os.Getuid()) + 1: true})
	if _, err := Dial(context.Background(), path, "0va"); err == nil || !strings.Contains(err.Error(), "not an allowed runner uid") {
		t.Fatalf("foreign uid accepted: %v", err)
	}
	path2, _ := serve(t, h, map[uint32]bool{uint32(os.Getuid()): true})
	a, _ := Dial(context.Background(), path2, "0va")
	b, _ := Dial(context.Background(), path2, "0vb")
	r, err := a.Reserve(ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Inspect(r.ID); err == nil || !strings.Contains(err.Error(), "not the owner") {
		t.Fatalf("daemon b inspected a's vm: %v", err)
	}
	if _, err := b.DestroyOf(r.Ref()); err == nil {
		t.Fatal("daemon b destroyed a's vm")
	}
	if vms, _ := b.List(); len(vms) != 0 {
		t.Fatalf("b lists a's vm: %+v", vms)
	}
}

// A hello without a daemon id is refused. With logging on, the server
// used to dereference the owner that refusal never set: the handler
// panicked and took the whole launcher down, whatever it was doing.
func TestHelloWithoutDaemonIsRefusedWithoutCrashing(t *testing.T) {
	s := newService(t, newFakeHost(), Config{})
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "h.sock")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test", Log: log.New(io.Discard, "", 0)}
	go sv.Serve(l)
	t.Cleanup(func() { l.Close() })
	c, err := net.Dial("unix", "h.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(`{"op":"hello"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.Contains(line, "hello needs a daemon id") {
		t.Fatalf("reply %q %v", line, err)
	}
	cl, err := Dial(context.Background(), "h.sock", "0va")
	if err != nil {
		t.Fatalf("the launcher did not survive: %v", err)
	}
	cl.Close()
}

// Over the wire, a quarantined record's destroy is refused and reported
// quarantined — also once the failed removal would succeed — and the
// record stays listed; only the operator's clear (the core's, here)
// releases it.
func TestWireRefusesAQuarantinedDestroy(t *testing.T) {
	h := newFakeHost()
	path, s := serve(t, h, map[uint32]bool{uint32(os.Getuid()): true})
	cl, err := Dial(context.Background(), path, "0va")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r, err := cl.Reserve(ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.fail["rmcgroup"] = os.ErrPermission
	h.mu.Unlock()
	if q, err := cl.DestroyOf(r.Ref()); err == nil || !q {
		t.Fatalf("first destroy: %v %v", err, q)
	}
	h.mu.Lock()
	delete(h.fail, "rmcgroup")
	h.mu.Unlock()
	q, err := cl.DestroyOf(r.Ref())
	if err == nil || !q || !strings.Contains(err.Error(), "only the operator's clear releases it") {
		t.Fatalf("the owner's retry over the wire: %v %v", err, q)
	}
	if vms, _ := cl.List(); len(vms) != 1 || vms[0].State != StateQuarantined {
		t.Fatalf("after the refused retry: %+v", vms)
	}
	if err := s.ClearQuarantine(r.ID); err != nil {
		t.Fatal(err)
	}
	if vms, _ := cl.List(); len(vms) != 0 {
		t.Fatalf("after the operator's clear: %+v", vms)
	}
}

// A launcher refuses a peer by answering and closing before it reads any
// request. A client whose request write then fails (EPIPE) still reports
// the launcher's answer, not the broken pipe — a race the foreign-uid test
// used to lose now and then.
func TestClientReportsARefusalDespiteItsFailedWrite(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	mine, theirs := os.NewFile(uintptr(fds[0]), "client"), os.NewFile(uintptr(fds[1]), "launcher")
	c, err := net.FileConn(mine)
	mine.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := theirs.Write([]byte(`{"ok":false,"error":"peer uid 4242 is not an allowed runner uid"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	theirs.Close() // before any request was read
	cl := &Client{conn: c.(*net.UnixConn), reader: bufio.NewReaderSize(c, 1<<20)}
	_, _, err = cl.call(Request{Op: "hello", Daemon: "0va"}, false)
	if err == nil || !strings.Contains(err.Error(), "not an allowed runner uid") {
		t.Fatalf("the refusal after a failed write: %v", err)
	}
}

func TestWireDestroyFailureReportsQuarantine(t *testing.T) {
	h := newFakeHost()
	path, _ := serve(t, h, map[uint32]bool{uint32(os.Getuid()): true})
	cl, _ := Dial(context.Background(), path, "0va")
	r, _ := cl.Reserve(ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 512, DiskMiB: 1024, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	cl.Create(r.Ref())
	h.fail["rmcgroup"] = os.ErrPermission
	q, err := cl.DestroyOf(r.Ref())
	if err == nil || !q {
		t.Fatalf("expected a quarantined failure: %v %v", err, q)
	}
	rec, _ := cl.Inspect(r.ID)
	if rec.State != StateQuarantined {
		t.Fatalf("state %s", rec.State)
	}
}
