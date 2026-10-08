package launcher

// The records directory is certified at every open, before anything reads
// an absence from it (INTEGRATION.md §11.6; orchestrator triage 08's
// restart proof). A withdrawal whose directory fsync failed leaves a
// visible absence that is not durable. A reopen whose own barrier still
// fails is fenced — nothing admitted or booted, no absence answered as a
// release, the owners' list refused — while the records it can read are
// still enforced; a retry of the barrier (the reaper's pass) lifts the
// fence. The files are real and private; only the records directory's
// fsync fails, on demand, before its syscall; the host is the nonexecuting
// model. A reopen here is a fresh service over the same visible files: not
// a power loss, nor a real launcher process's restart.

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

// barrierFS fails the records directory's fsync from the moment the armed
// record is unlinked, until repaired; with acted, that unlink also reports
// an error after it acted (an uncertain unlink).
type barrierFS struct {
	osFS
	mu      sync.Mutex
	records string
	armed   string
	acted   bool
	fail    bool
}

func (f *barrierFS) arm(id string) {
	f.mu.Lock()
	f.armed = id
	f.mu.Unlock()
}

func (f *barrierFS) repair() {
	f.mu.Lock()
	f.armed, f.fail = "", false
	f.mu.Unlock()
}

func (f *barrierFS) Unlink(p string) error {
	err := f.osFS.Unlink(p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil && f.armed != "" && p == filepath.Join(f.records, f.armed+".json") {
		f.fail = true
		if f.acted {
			return syscall.EIO // it acted, then failed
		}
	}
	return err
}

func (f *barrierFS) SyncDir(p string) error {
	f.mu.Lock()
	failing := f.fail && p == f.records
	f.mu.Unlock()
	if failing {
		return syscall.EIO
	}
	return f.osFS.SyncDir(p)
}

// withdrawnUncertainly leaves gone's record unlinked with its directory's
// fsync failed — the first life keeps the charge (ErrNotDurable) — beside
// a known guest, running, that the reopen must still enforce; then it
// closes the first life, the barrier still failing.
func withdrawnUncertainly(t *testing.T, cfg Config, h *modelHost, fsys *barrierFS) (gone, known ReserveReply) {
	t.Helper()
	s := open(t, cfg, h, fsys)
	gone = mustReserve(t, s, owner1, lockedReq("0vgone", 1, 128))
	known = mustReserve(t, s, owner1, lockedReq("0vknown", 1, 128))
	for _, r := range []ReserveReply{gone, known} {
		if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
	}
	fsys.arm(gone.ID)
	if err := s.DestroyOf(owner1, gone.Ref()); !errors.Is(err, ErrNotDurable) {
		t.Fatalf("fixture: an uncertain withdrawal: %v", err)
	}
	if _, ok := held(s, gone.ID); !ok || used(s)[2] != 2 {
		t.Fatalf("fixture: the first life keeps the charge: used %v", used(s))
	}
	if _, ok := durable(t, cfg.StateDir, gone.ID); ok {
		t.Fatal("fixture: the unlink is visible")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return gone, known
}

// listOver serves s on a fresh private unix socket and asks it for the
// owners' list, as its consumers — the daemon's reconcile, the runner's
// release — do.
func listOver(t *testing.T, s *Service) ([]Record, error) {
	t.Helper()
	t.Chdir(t.TempDir()) // a short, private socket path
	l, err := net.Listen("unix", "c.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	go sv.Serve(l)
	cl, err := Dial(context.Background(), "c.sock", owner1.Daemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	return cl.List()
}

func uncertified(s *Service) bool {
	for _, p := range s.Problems() {
		if p.Kind == "uncertified" {
			return true
		}
	}
	return false
}

func TestReopenCertifiesTheRecordsDirectory(t *testing.T) {
	t.Run("the barrier still fails", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts")}
		gone, known := withdrawnUncertainly(t, cfg, h, fsys)
		s, err := openService(cfg, h, fsys)
		if err != nil {
			t.Fatalf("A STORAGE FAILURE AT REOPEN LEFT THE KNOWN GUESTS UNENFORCED: the open failed: %v", err)
		}
		closeAtEnd(t, s)
		if !uncertified(s) {
			t.Fatalf("A REOPEN TOOK AN UNCERTIFIED ABSENCE FOR A HEALTHY INVENTORY: problems %+v", s.Problems())
		}
		if err := s.DestroyOf(owner1, gone.Ref()); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("A REPLAYED DESTROY OF AN UNCERTIFIED ABSENCE WAS ANSWERED DONE: %v", err)
		}
		if _, err := s.Inspect(owner1, gone.ID); errors.Is(err, ErrUnknown) || !errors.Is(err, ErrNotDurable) {
			t.Fatalf("AN UNCERTIFIED ABSENCE WAS ANSWERED AS NO SUCH VM: %v", err)
		}
		// refused before anything is charged — not admitted, and not left
		// retained by a publication the broken barrier could not finish
		if _, err := s.Reserve(owner1, lockedReq("0vnew", 1, 128)); !errors.Is(err, ErrNotDurable) || used(s)[2] != 1 {
			t.Fatalf("A RESERVATION WAS ADMITTED ON AN UNCERTIFIED ABSENCE: %v; used %v", err, used(s))
		}
		if recs, err := listOver(t, s); err == nil {
			t.Fatalf("THE OWNERS' LIST SERVED AN UNCERTIFIED ABSENCE: %+v", recs)
		}
		s.ReapOnce() // its retry of the barrier fails too
		if !uncertified(s) {
			t.Fatal("A FAILED RETRY OF THE BARRIER LIFTED THE FENCE")
		}
		// the known guest is loaded, charged and still enforced: its
		// owner's destroy stops its VMM, though its stopping record cannot
		// be made durable (a halt: nothing removed, still charged)
		if _, ok := held(s, known.ID); !ok || used(s)[2] != 1 {
			t.Fatalf("the known guest was not loaded: used %v", used(s))
		}
		kills := h.count("kill")
		err = s.DestroyOf(owner1, known.Ref())
		if err == nil || h.exists("vmm", known.ID) || h.count("kill") == kills {
			t.Fatalf("A STORAGE FAILURE AT REOPEN LEFT THE KNOWN GUESTS UNENFORCED: %v; vmm %v; kills %d then %d", err, h.exists("vmm", known.ID), kills, h.count("kill"))
		}
		if _, ok := held(s, known.ID); !ok || used(s)[2] != 1 {
			t.Fatalf("the halted teardown released the known guest: used %v", used(s))
		}
	})
	t.Run("an unlink that acted and then failed", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts"), acted: true}
		gone, _ := withdrawnUncertainly(t, cfg, h, fsys)
		s, err := openService(cfg, h, fsys)
		if err != nil {
			t.Fatalf("A STORAGE FAILURE AT REOPEN LEFT THE KNOWN GUESTS UNENFORCED: the open failed: %v", err)
		}
		closeAtEnd(t, s)
		if !uncertified(s) {
			t.Fatalf("A REOPEN TOOK AN UNCERTIFIED ABSENCE FOR A HEALTHY INVENTORY: problems %+v", s.Problems())
		}
		if err := s.DestroyOf(owner1, gone.Ref()); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("A REPLAYED DESTROY OF AN UNCERTIFIED ABSENCE WAS ANSWERED DONE: %v", err)
		}
	})
	t.Run("the barrier works again at the reopen (control)", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts")}
		gone, known := withdrawnUncertainly(t, cfg, h, fsys)
		fsys.repair()
		s := open(t, cfg, h, fsys)
		if len(s.Problems()) != 0 {
			t.Fatalf("a certified reopen is fenced: %+v", s.Problems())
		}
		if err := s.DestroyOf(owner1, gone.Ref()); err != nil {
			t.Fatalf("a certified absence: %v", err)
		}
		if _, ok := held(s, known.ID); !ok || used(s)[2] != 1 {
			t.Fatalf("the known guest: used %v", used(s))
		}
		if _, err := listOver(t, s); err != nil {
			t.Fatalf("the owners' list of a certified reopen: %v", err)
		}
	})
	t.Run("the barrier works again later", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts")}
		gone, _ := withdrawnUncertainly(t, cfg, h, fsys)
		s, err := openService(cfg, h, fsys)
		if err != nil || !uncertified(s) {
			t.Fatalf("fixture: a fenced reopen: %v", err)
		}
		closeAtEnd(t, s)
		fsys.repair()
		s.ReapOnce() // its retry of the barrier succeeds
		if len(s.Problems()) != 0 {
			t.Fatalf("THE FENCE OUTLIVED A CERTIFIED BARRIER: %+v", s.Problems())
		}
		if err := s.DestroyOf(owner1, gone.Ref()); err != nil {
			t.Fatalf("a certified absence: %v", err)
		}
		if _, err := listOver(t, s); err != nil {
			t.Fatalf("the owners' list once certified: %v", err)
		}
		if _, err := s.Reserve(owner1, lockedReq("0vnew", 1, 128)); err != nil {
			t.Fatalf("an admission once certified: %v", err)
		}
	})
	t.Run("a stale destroy of an earlier incarnation", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts")}
		s := open(t, cfg, h, fsys)
		first := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
		if err := s.DestroyOf(owner1, first.Ref()); err != nil {
			t.Fatal(err)
		}
		second := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
		other := mustReserve(t, s, owner1, lockedReq("0vother", 1, 128))
		fsys.arm(other.ID)
		if err := s.DestroyOf(owner1, other.Ref()); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("fixture: an uncertain withdrawal: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s2, err := openService(cfg, h, fsys)
		if err != nil || !uncertified(s2) {
			t.Fatalf("fixture: a fenced reopen: %v", err)
		}
		closeAtEnd(t, s2)
		if err := s2.DestroyOf(owner1, first.Ref()); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("A STALE DESTROY WAS ANSWERED DONE ON AN UNCERTIFIED ABSENCE: %v", err)
		}
		if r, ok := held(s2, second.ID); !ok || r.Incarnation != second.Incarnation {
			t.Fatalf("the incarnation it holds: %+v %v", r, ok)
		}
	})
}
