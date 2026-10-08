package launcher

// Review 02's mechanism classes (runner/launcher/LIFECYCLE.md §2.1, §2.2,
// §3.1, §8): the namespace a record lives in is certified at every open; no
// host effect begins at or after the selected deadline; every record the
// launcher acknowledges or publishes reloads as exactly itself, and every
// identifier it hands out stays inside the domain the loader and the host
// accept. The reviewer's probes are ported here with their scenarios and
// assertions (kept byte-identical in
// .scratch/repair/reference/reviews/opus-lifecycle-review02/ and replayed
// there through a Go overlay); the tests after each port cover the class.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

// certFS is the production adapter with a per-directory fsync switch: a
// failing directory's fsync is not made (EIO), and the real ones are
// counted, per directory.
type certFS struct {
	fileSystem
	mu     sync.Mutex
	fail   map[string]bool
	synced map[string]int
	failed map[string]int
}

func newCertFS() *certFS {
	return &certFS{fileSystem: osFS{}, fail: map[string]bool{}, synced: map[string]int{}, failed: map[string]int{}}
}

func (f *certFS) SyncDir(path string) error {
	f.mu.Lock()
	failing := f.fail[path]
	if failing {
		f.failed[path]++
	}
	f.mu.Unlock()
	if failing {
		return syscall.EIO
	}
	if err := f.fileSystem.SyncDir(path); err != nil {
		return err
	}
	f.mu.Lock()
	f.synced[path]++
	f.mu.Unlock()
	return nil
}

func (f *certFS) set(path string, failing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[path] = failing
}

func (f *certFS) counts(path string) (synced, failed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.synced[path], f.failed[path]
}

// ---- A: the namespace, certified at every open ----

// Review 02's TestIndependentR2RetryCertifiesCreatedDirectory, ported: the
// open that created attempts/ fails at its parent's fsync, which is not
// made; the directory is visible; the next open must still certify it
// before a reservation is acknowledged.
func TestRetryCertifiesCreatedDirectoryPort(t *testing.T) {
	for _, manuallyCertify := range []bool{true, false} {
		name := "retry-without-parent-certification"
		if manuallyCertify {
			name = "explicit-certification-control"
		}
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t.TempDir())
			fsys := newCertFS()
			fsys.set(cfg.StateDir, true)
			h := newFakeHost()
			s, err := openService(cfg, h, fsys)
			if _, failed := fsys.counts(cfg.StateDir); err == nil || s != nil || failed != 1 {
				t.Fatalf("initial parent fsync fault not reached: service=%v err=%v calls=%d", s, err, failed)
			}
			info, err := os.Stat(filepath.Join(cfg.StateDir, "attempts"))
			if err != nil || !info.IsDir() {
				t.Fatalf("created directory not visible: %v", err)
			}
			fsys.set(cfg.StateDir, false)
			if manuallyCertify {
				if err = fsys.SyncDir(cfg.StateDir); err != nil {
					t.Fatal(err)
				}
			}
			s, err = openService(cfg, h, fsys)
			if err != nil {
				t.Fatal(err)
			}
			closeAtEnd(t, s)
			reply, err := s.Reserve(owner1, lockedReq("review-parent-sync", 1, 128))
			if err != nil {
				t.Fatal(err)
			}
			if reply.ID == "" {
				t.Fatal("control reservation missing")
			}
			if synced, _ := fsys.counts(cfg.StateDir); synced == 0 {
				t.Fatal("PARENT DURABILITY NOT RECOVERED: reservation acknowledged after a failed directory-creation barrier, without ever syncing its parent")
			}
		})
	}
}

// Every link the constructor creates is certified by every open — the
// state directory's own (its parent is fsynced) and the records
// directory's (the state directory is) — whichever open created it, so a
// failed certification is completed by the next open before anything is
// acknowledged. The constructor creates nothing above the state directory.
func TestEveryOpenCertifiesTheStateNamespace(t *testing.T) {
	for _, failing := range []string{"the state directory's parent", "the state directory"} {
		t.Run("a failed certification of "+failing+" is completed by the next open", func(t *testing.T) {
			root := t.TempDir()
			cfg := testConfig(filepath.Join(root, "state")) // this open creates it
			target := root
			if failing == "the state directory" {
				target = cfg.StateDir
			}
			fsys := newCertFS()
			fsys.set(target, true)
			s, err := openService(cfg, newFakeHost(), fsys)
			if _, failed := fsys.counts(target); s != nil || !errors.Is(err, ErrNotDurable) || failed != 1 {
				t.Fatalf("the open with an uncertified link: %v %v (failed fsyncs %d)", s, err, failed)
			}
			if _, err := os.Stat(cfg.StateDir); err != nil {
				t.Fatalf("the state directory should be visible, only uncertified: %v", err)
			}
			fsys.set(target, false)
			rootBefore, _ := fsys.counts(root)
			stateBefore, _ := fsys.counts(cfg.StateDir)
			s, err = openService(cfg, newFakeHost(), fsys)
			if err != nil {
				t.Fatal(err)
			}
			closeAtEnd(t, s)
			rootAfter, _ := fsys.counts(root)
			stateAfter, _ := fsys.counts(cfg.StateDir)
			if rootAfter != rootBefore+1 || stateAfter != stateBefore+1 {
				t.Fatalf("the next open certified the parent %d and the state directory %d times, want 1 and 1", rootAfter-rootBefore, stateAfter-stateBefore)
			}
			if _, err := s.Reserve(owner1, lockedReq("0v1", 1, 128)); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("an ordinary reopen certifies both again", func(t *testing.T) {
		root := t.TempDir()
		cfg := testConfig(filepath.Join(root, "state"))
		fsys := newCertFS()
		for i := 1; i <= 2; i++ {
			s, err := openService(cfg, newFakeHost(), fsys)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			r, _ := fsys.counts(root)
			st, _ := fsys.counts(cfg.StateDir)
			if r != i || st != i {
				t.Fatalf("after open %d: parent certified %d, state directory %d times", i, r, st)
			}
		}
	})
	t.Run("nothing is created above the state directory", func(t *testing.T) {
		root := t.TempDir()
		cfg := testConfig(filepath.Join(root, "missing", "state"))
		if s, err := openService(cfg, newFakeHost(), newCertFS()); s != nil || err == nil {
			t.Fatalf("opened below a missing parent: %v %v", s, err)
		}
		if _, err := os.Lstat(filepath.Join(root, "missing")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the open created a directory above the state directory: %v", err)
		}
	})
}

// ---- B: the selected absolute deadline ----

// Review 02's TestIndependentR2ExpiredCreate, ported: neither an expired
// request nor an expired reservation recovered from a valid private
// record may reach StartVM.
func TestExpiredCreatePort(t *testing.T) {
	for _, mode := range []string{"future-control", "expired-request", "expired-recovered-reservation"} {
		t.Run(mode, func(t *testing.T) {
			h := newFakeHost()
			s := newService(t, h, Config{})
			req := ReserveRequest{Attempt: "review-expired", Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"}
			if mode == "expired-request" {
				req.DeadlineUnix = time.Now().Add(-time.Second).Unix()
			}
			reply, err := s.Reserve(ownerA, req)
			if err != nil {
				if mode == "expired-request" && !h.has("start ") {
					return
				}
				t.Fatal(err)
			}
			if mode == "expired-recovered-reservation" {
				cfg := s.cfg
				if err = s.Close(); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(cfg.StateDir, "attempts", reply.ID+".json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var rec recordFile
				if err = json.Unmarshal(data, &rec); err != nil {
					t.Fatal(err)
				}
				rec.Record.DeadlineUnix = time.Now().Add(-time.Second).Unix()
				data, err = json.Marshal(rec)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				s = newService(t, h, cfg)
			}
			_, err = s.Create(ownerA, reply.ID)
			started := h.has("start " + reply.ID)
			if mode == "future-control" {
				if err != nil || !started {
					t.Fatalf("future control: %v start=%v", err, started)
				}
				return
			}
			if err == nil || started {
				t.Fatalf("EXPIRED EXECUTION STARTED: mode=%s create=%v start_called=%v", mode, err, started)
			}
		})
	}
}

// Admission takes only a deadline after now; it sets no maximum.
func TestReserveTakesOnlyADeadlineAhead(t *testing.T) {
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, nil, nil)
	now := clock.now().Unix()
	for _, d := range []int64{now - 3600, now - 1, now} {
		req := lockedReq("0vpast", 1, 128)
		req.DeadlineUnix = d
		if _, err := s.Reserve(owner1, req); !errors.Is(err, ErrExpired) {
			t.Fatalf("a deadline not after now was admitted (%d at %d): %v", d, now, err)
		}
	}
	if used(s)[2] != 0 || len(stateEntries(t, cfg.StateDir)) != 0 {
		t.Fatalf("a refused deadline left a charge or a record: %v %v", used(s), stateEntries(t, cfg.StateDir))
	}
	for i, d := range []int64{now + 60, now + 10*365*24*3600} { // no launcher maximum
		req := lockedReq("0vahead"+string(rune('a'+i)), 1, 128)
		req.DeadlineUnix = d
		if _, err := s.Reserve(owner1, req); err != nil {
			t.Fatalf("a deadline ahead was refused (%d at %d): %v", d, now, err)
		}
	}
}

// No host effect of a create begins at or after the selected deadline:
// the create checks it before every step and again just before every
// effect, whatever the reaper has done yet. What was already made is
// rolled back and released; a VMM whose start was never attempted is not
// taken for one of unknown pid.
func TestNoEffectBeginsAtOrAfterTheDeadline(t *testing.T) {
	cases := []struct {
		name, passes, never string
		networked           bool
	}{
		{"it passes during the disk preparation", "prepare/after", "cgroup", false},
		{"it passes during the cgroup creation", "cgroup/after", "start", false},
		{"it passes during the network creation", "network/after", "start", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			clock := withClock(&cfg)
			s := open(t, cfg, h, nil)
			req := expiring("0v1", 2, 1024)
			if c.networked {
				req.Network, req.Destinations = "egress", []string{"tcp:192.0.2.1:8472"}
			}
			r := mustReserve(t, s, owner1, req)
			h.at(c.passes, func(string) { clock.pass(t, s, r.ID) })
			_, err := s.Create(owner1, r.ID)
			if !errors.Is(err, ErrExpired) || errors.Is(err, ErrQuarantined) || !strings.Contains(err.Error(), "rolled back") {
				t.Fatalf("create: %v", err)
			}
			if h.count(c.never) != 0 || h.count("start") != 0 {
				t.Fatalf("an effect began past the deadline: %s %d, start %d", c.never, h.count(c.never), h.count("start"))
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 {
				t.Fatalf("not rolled back and released: leaks %v used %v", leaks, used(s))
			}
		})
	}
	t.Run("it passes while the vmm intent is published", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		s := open(t, cfg, h, fsys)
		r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
		// the create publishes the disk intent, the cgroup intent, then the
		// vmm intent: the deadline passes during the third
		n := 0
		fsys.inject(&fsFault{op: "rename", path: r.ID + ".json", block: func() {
			if n++; n == 3 {
				clock.pass(t, s, r.ID)
			}
		}})
		_, err := s.Create(owner1, r.ID)
		tr.dump(t)
		if n < 3 || !errors.Is(err, ErrExpired) || errors.Is(err, ErrQuarantined) {
			t.Fatalf("create: %v (publications %d)", err, n)
		}
		if h.count("start") != 0 {
			t.Fatal("StartVM ran past the deadline")
		}
		if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 {
			t.Fatalf("a start never attempted was kept as a vmm: leaks %v used %v", leaks, used(s))
		}
	})
}

// A create asked at or after the deadline is refused before it takes the
// record: nothing is published, nothing reaches the host, and the
// reservation stays charged for the reaper — it is not rolled back by
// the create itself.
func TestAnExpiredCreateChangesNothing(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
	clock.pass(t, s, r.ID)
	mark := len(tr.all())
	if _, err := s.Create(owner1, r.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("create past the deadline: %v", err)
	}
	if after := tr.all()[mark:]; len(after) != 0 {
		t.Fatalf("an expired create acted: %v", after)
	}
	if rec, ok := held(s, r.ID); !ok || rec.State != StatePreparing || used(s)[2] != 1 {
		t.Fatalf("an expired create changed the reservation: %+v %v", rec, ok)
	}
	s.ReapOnce()
	if _, ok := held(s, r.ID); ok {
		t.Fatal("the reaper did not release the expired reservation")
	}
}

// A connection is how work reaches a guest: none past its deadline.
func TestConnectIsRefusedPastTheDeadline(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	clock := withClock(&cfg)
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, expiring("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	f, err := s.Connect(owner1, r.ID, 5000)
	if err != nil {
		t.Fatalf("connect within the deadline: %v", err)
	}
	f.Close()
	clock.pass(t, s, r.ID)
	if _, err := s.Connect(owner1, r.ID, 5000); !errors.Is(err, ErrExpired) || h.count("connect") != 1 {
		t.Fatalf("connect past the deadline: %v (host connects %d)", err, h.count("connect"))
	}
	s.ReapOnce()
	if _, ok := held(s, r.ID); ok || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("not reaped: %v", h.leaks(r.ID))
	}
}

// ---- C: identifiers and records the loader accepts ----

// Review 02's TestIndependentR2RecoveredCIDCannotWrap, ported: a record
// recovered with the highest cid must not make the next acknowledged cid
// wrap to 0 (or alias).
func TestRecoveredCIDCannotWrapPort(t *testing.T) {
	for _, extreme := range []bool{false, true} {
		name := "control"
		if extreme {
			name = "recovered-maximum"
		}
		t.Run(name, func(t *testing.T) {
			h := newFakeHost()
			s := newService(t, h, Config{})
			reply := reserve(t, s, ownerA, "review-existing-cid", 1, 128, time.Now().Add(time.Hour))
			cfg := s.cfg
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cfg.StateDir, "attempts", reply.ID+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var rec recordFile
			if err = json.Unmarshal(data, &rec); err != nil {
				t.Fatal(err)
			}
			if extreme {
				rec.Record.CID = ^uint32(0)
			}
			data, err = json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			s, err = NewService(cfg, h)
			if err != nil {
				if extreme {
					return
				}
				t.Fatal(err)
			}
			closeAtEnd(t, s)
			next, err := s.Reserve(ownerA, ReserveRequest{Attempt: "review-next-cid", Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
			if err != nil {
				if extreme {
					return
				}
				t.Fatal(err)
			}
			if next.CID < 3 || next.CID == rec.Record.CID {
				t.Fatalf("CID ALLOCATION WRAPPED: acknowledged cid=%d after recovered cid=%d", next.CID, rec.Record.CID)
			}
			if _, err = s.Inspect(ownerA, next.ID); err != nil && !errors.Is(err, ErrUnknown) {
				t.Fatal(err)
			}
		})
	}
}

// Review 02's TestIndependentR2AcknowledgedRecordIsReloadable, ported: an
// acknowledged reservation must reload, charged, with no loader problem.
func TestAcknowledgedRecordIsReloadablePort(t *testing.T) {
	for _, large := range []bool{false, true} {
		name := "control"
		if large {
			name = "oversize-attempt"
		}
		t.Run(name, func(t *testing.T) {
			host := newFakeHost()
			s := newService(t, host, Config{})
			attempt := "review-record-size"
			if large {
				attempt = strings.Repeat("a", maxRecordBytes)
			}
			reply, err := s.Reserve(ownerA, ReserveRequest{Attempt: attempt, Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
			if err != nil {
				if large {
					return
				}
				t.Fatal(err)
			}
			cfg := s.cfg
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewService(cfg, host)
			if err != nil {
				t.Fatal(err)
			}
			closeAtEnd(t, reopened)
			_, err = reopened.Inspect(ownerA, reply.ID)
			_, _, _, _, _, used := reopened.Budget()
			if err != nil || used != 1 || len(reopened.Problems()) != 0 {
				t.Fatalf("ACKNOWLEDGED RECORD NOT RELOADABLE: inspect=%v used=%d loader_problems=%d", err, used, len(reopened.Problems()))
			}
		})
	}
}

// The allocators hand out only what the loader and the host accept —
// cids in [3, 0xFFFFFFFE], network indexes in [MinNetIndex, MaxNetIndex]
// — skip every value a held record has, start again at the bottom past
// the top, and refuse when the whole domain is held: never a wrap, never
// an alias.
func TestAllocatorsStayInTheirDomains(t *testing.T) {
	t.Run("the cycle", func(t *testing.T) {
		held := map[uint32]bool{5: true, 6: true}
		inUse := func(v uint32) bool { return held[v] }
		for _, c := range []struct{ cursor, want uint32 }{{3, 3}, {5, 7}, {7, 7}, {9, 3}, {2, 3}} {
			if v, ok := firstFree(c.cursor, 3, 7, inUse); !ok || v != c.want {
				t.Fatalf("from %d: %d %v, want %d", c.cursor, v, ok, c.want)
			}
		}
		if v, ok := firstFree(7, 3, 7, func(v uint32) bool { return v == 7 }); !ok || v != 3 {
			t.Fatalf("past the top: %d %v, want the bottom", v, ok)
		}
		if v, ok := firstFree(4, 3, 7, func(uint32) bool { return true }); ok {
			t.Fatalf("a full domain gave %d", v)
		}
		if cycleNext(maxCID, minCID, maxCID) != minCID || cycleNext(MaxNetIndex, MinNetIndex, MaxNetIndex) != MinNetIndex {
			t.Fatal("a cursor left its domain at the top")
		}
	})
	t.Run("recovered cids at the top", func(t *testing.T) {
		cfg := testConfig(t.TempDir())
		writeRecord(t, cfg.StateDir, goodRecord(cfg, "0vtop", maxCID))
		writeRecord(t, cfg.StateDir, goodRecord(cfg, "0vlow", minCID))
		cfg.MaxGuests = 3
		s := open(t, cfg, nil, nil)
		if len(s.Problems()) != 0 {
			t.Fatalf("problems %v", s.Problems())
		}
		a := mustReserve(t, s, owner1, lockedReq("0va", 1, 128))
		if a.CID != minCID+1 {
			t.Fatalf("after cids %d and %d: %d, want %d", uint32(maxCID), minCID, a.CID, minCID+1)
		}
		s2 := reopen(t, s, nil)
		if len(s2.Problems()) != 0 || len(s2.All()) != 3 {
			t.Fatalf("the allocated cid does not reload: %v %d", s2.Problems(), len(s2.All()))
		}
	})
	t.Run("recovered network indexes at the top", func(t *testing.T) {
		cfg := testConfig(t.TempDir())
		for _, x := range []struct {
			attempt string
			cid     uint32
			index   int
		}{{"0vtop", 3, MaxNetIndex}, {"0vlow", 4, MinNetIndex}} {
			r := goodRecord(cfg, x.attempt, x.cid)
			r.Network, r.Destinations, r.HasNetwork, r.NetIndex = "egress", []string{"tcp:192.0.2.1:8472"}, true, x.index
			writeRecord(t, cfg.StateDir, r)
		}
		cfg.MaxGuests = 3
		h := newModelHost(t, &tracer{})
		s := open(t, cfg, h, nil)
		req := lockedReq("0va", 1, 128)
		req.Network, req.Destinations = "egress", []string{"tcp:192.0.2.1:8472"}
		a := mustReserve(t, s, owner1, req)
		if _, err := s.Create(owner1, a.ID); err != nil {
			t.Fatal(err)
		}
		if rec, _ := held(s, a.ID); rec.NetIndex != MinNetIndex+1 {
			t.Fatalf("after indexes %d and %d: %d, want %d", MaxNetIndex, MinNetIndex, rec.NetIndex, MinNetIndex+1)
		}
	})
	t.Run("every network index held", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		req := lockedReq("0va", 1, 128)
		req.Network, req.Destinations = "egress", []string{"tcp:192.0.2.1:8472"}
		a := mustReserve(t, s, owner1, req)
		s.mu.Lock()
		for i := MinNetIndex; i <= MaxNetIndex; i++ {
			s.vms[string(rune(i))+"filler"] = &entry{rec: Record{NetIndex: i, HasNetwork: true}}
		}
		s.mu.Unlock()
		if _, err := s.Create(owner1, a.ID); !errors.Is(err, ErrOverBudget) || !strings.Contains(err.Error(), "network index") {
			t.Fatalf("create with every index held: %v", err)
		}
		if h.count("prepare") != 0 {
			t.Fatal("a create without an index reached the host")
		}
		s.mu.Lock()
		for k := range s.vms {
			if strings.HasSuffix(k, "filler") {
				delete(s.vms, k)
			}
		}
		s.mu.Unlock()
	})
	t.Run("a create's index is its own until its release", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		networked := func(a string) ReserveRequest {
			r := lockedReq(a, 1, 128)
			r.Network, r.Destinations = "egress", []string{"tcp:192.0.2.1:8472"}
			return r
		}
		a := mustReserve(t, s, owner1, networked("0va"))
		b := mustReserve(t, s, owner1, networked("0vb"))
		g := newGate()
		t.Cleanup(g.open)
		h.at("prepare/before", func(id string) {
			if id == a.ID {
				g.wait(id)
			}
		})
		created := make(chan error, 1)
		go func() { _, err := s.Create(owner1, a.ID); created <- err }()
		<-g.reached // a holds its index; its network intent is not recorded yet
		s.mu.Lock()
		s.nextNet = s.vms[a.ID].rec.NetIndex // the cursor has come round to it
		s.mu.Unlock()
		if _, err := s.Create(owner1, b.ID); err != nil {
			t.Fatal(err)
		}
		g.open()
		if err := <-created; err != nil {
			t.Fatal(err)
		}
		ra, _ := held(s, a.ID)
		rb, _ := held(s, b.ID)
		if ra.NetIndex == rb.NetIndex || ra.NetIndex < MinNetIndex || rb.NetIndex < MinNetIndex {
			t.Fatalf("two creates shared or left the domain: %d %d", ra.NetIndex, rb.NetIndex)
		}
	})
}

// hosts that report what a record cannot hold
type pidlessHost struct {
	*modelHost
	pid int
}

func (h pidlessHost) StartVM(id string, spec VMSpec) (int, error) {
	if _, err := h.modelHost.StartVM(id, spec); err != nil {
		return 0, err
	}
	return h.pid, nil // a VMM runs; the host names no usable pid
}

type pathHost struct {
	*modelHost
	path string
}

func (h pathHost) PrepareDisk(id string, img Image, totalMiB int) (string, error) {
	if _, err := h.modelHost.PrepareDisk(id, img, totalMiB); err != nil {
		return "", err
	}
	return h.path, nil
}

type messyHost struct {
	*modelHost
	msg string
}

func (h messyHost) RemoveCgroup(id string) error {
	if err := h.modelHost.RemoveCgroup(id); err != nil {
		return err
	}
	return errors.New(h.msg) // removed, then an error: the holding stays
}

// Every record the launcher acknowledges or publishes reloads as exactly
// itself: the encoder refuses what the loader would refuse or read
// differently, admission keeps the room a record's lifecycle needs, and
// what a host reports is recorded only if the record still reloads.
func TestEveryPublishedRecordReloadsAsWritten(t *testing.T) {
	t.Run("a request that does not survive encoding", func(t *testing.T) {
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, nil, nil)
		if _, err := s.Reserve(owner1, lockedReq("0v\xff", 1, 128)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("an attempt that is not UTF-8: %v", err)
		}
		if _, err := s.Reserve(Owner{UID: 1, Daemon: "0vd\xff"}, lockedReq("0v1", 1, 128)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("an owner that is not UTF-8: %v", err)
		}
		if used(s)[2] != 0 || len(stateEntries(t, cfg.StateDir)) != 0 {
			t.Fatalf("a refused request left a charge or a record: %v %v", used(s), stateEntries(t, cfg.StateDir))
		}
	})
	t.Run("the room an admitted record keeps", func(t *testing.T) {
		tr := &tracer{}
		cfg := testConfig(t.TempDir())
		h := messyHost{newModelHost(t, tr), strings.Repeat("\xffcgroup busy ", 16<<10)}
		s := open(t, cfg, h, nil)
		// the encoding grows one byte per byte of an ASCII attempt: size an
		// attempt that encodes to exactly the admission limit
		now := time.Now().Unix()
		// stage 01 (independent review 01): every admitted record carries its
		// incarnation token, so the probe that sizes the boundary does too
		probe := Record{ID: IDFor("t", "a"), Incarnation: strings.Repeat("0", 32), Attempt: "a", Owner: owner1, Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 10,
			DeadlineUnix: now + 3600, Network: "locked", CID: minCID, State: StatePreparing, Created: now, Updated: now}
		data, _, err := encodeRecord("t", probe)
		if err != nil {
			t.Fatal(err)
		}
		room := maxRecordBytes - recordGrowthBytes
		fits := strings.Repeat("a", room-len(data)+1)
		if _, err := s.Reserve(owner1, lockedReq(fits+"b", 1, 128)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a record one byte over the room: %v", err)
		}
		r := mustReserve(t, s, owner1, lockedReq(fits, 1, 128))
		if d, _ := os.ReadFile(filepath.Join(cfg.StateDir, "attempts", r.ID+".json")); len(d) != room {
			t.Fatalf("the record at the limit is %d bytes, want %d", len(d), room)
		}
		// its lifecycle adds a disk path, holdings, a pid and a long reason
		// that is not UTF-8: every publication still reloads
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("destroy: %v", err)
		}
		if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateQuarantined {
			t.Fatalf("the quarantine with its long reason was not published: %+v %v", d.State, ok)
		}
		s2 := reopen(t, s, h)
		got, ok := held(s2, r.ID)
		if !ok || len(s2.Problems()) != 0 || got.State != StateQuarantined || len(got.Reason) > maxReasonBytes || !utf8.ValidString(got.Reason) {
			t.Fatalf("the grown record does not reload: %v %+v problems %v", ok, len(got.Reason), s2.Problems())
		}
	})
	for _, pid := range []int{0, -5} {
		t.Run(fmt.Sprintf("a vmm pid the host did not give (%d)", pid), func(t *testing.T) {
			cfg := testConfig(t.TempDir())
			h := pidlessHost{newModelHost(t, &tracer{}), pid}
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("create: %v", err)
			}
			rec, _ := held(s, r.ID)
			if rec.State != StateQuarantined || !rec.HasVMM || rec.PID != 0 || !h.exists("jail", r.ID) || !h.exists("cgroup", r.ID) {
				t.Fatalf("a start without a usable pid must stay a vmm of unknown pid, nothing removed from under it: %+v leaks %v", rec, h.leaks(r.ID))
			}
			s2 := reopen(t, s, h)
			if got, ok := held(s2, r.ID); !ok || got.State != StateQuarantined || len(s2.Problems()) != 0 {
				t.Fatalf("reloaded %+v %v %v", got, ok, s2.Problems())
			}
		})
	}
	for name, path := range map[string]string{
		"a disk path that is not UTF-8":    "/jail/disk\xff",
		"a disk path longer than PATH_MAX": "/" + strings.Repeat("d", maxPathBytes),
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t.TempDir())
			h := pathHost{newModelHost(t, &tracer{}), path}
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err == nil || !strings.Contains(err.Error(), "unusable disk path") || !strings.Contains(err.Error(), "rolled back") {
				t.Fatalf("create: %v", err)
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 || h.count("cgroup") != 0 {
				t.Fatalf("not rolled back: leaks %v used %v", leaks, used(s))
			}
		})
	}
	t.Run("the encoder's own refusal", func(t *testing.T) {
		r := goodRecord(testConfig(""), "0v1", minCID)
		for name, mutate := range map[string]func(*Record){
			"an identity mismatch": func(r *Record) { r.Attempt = "0v2" },
			"larger than the loader reads": func(r *Record) {
				r.HasDisk, r.DiskPath = true, strings.Repeat("d", maxRecordBytes)
			},
			"a cid outside":       func(r *Record) { r.CID = math.MaxUint32 },
			"running with no pid": func(r *Record) { r.State, r.HasVMM = StateRunning, true },
		} {
			bad := r
			mutate(&bad)
			if _, _, err := encodeRecord("t", bad); err == nil {
				t.Fatalf("%s: encoded", name)
			}
		}
		if _, _, err := encodeRecord("t", r); err != nil {
			t.Fatal(err)
		}
	})
}
