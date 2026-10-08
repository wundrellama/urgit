package launcher

// The release evidence's namespace (INTEGRATION.md §11.9; independent
// review 03, R3-1). Every link from the state directory down to a release's
// evidence E is certified by the process that relies on it — each by a
// barrier issued after it saw what it certifies — before E authorizes any
// withdrawal: released/'s name by an fsync of the state directory at every
// keeping, E's name by an fsync of released/, E's bytes by their fsync.
// mkdir's EEXIST, an earlier attempt, an earlier life's visible file and a
// symlink are observations, never certificates. The files are private, the
// host the nonexecuting model, each fault injected before its syscall, and a
// restart a fresh service over the same files: not a power loss.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// stateSync fails the state directory's own fsync — released/'s link — and
// nothing else: the records' and the evidence's directories end in their
// own names.
func stateSync(cfg Config) *fsFault {
	return &fsFault{op: "syncdir", path: cfg.StateDir, mode: "before", err: errInjected}
}

// pendingRelease fails the test unless r's release is still pending: held
// and charged, not an incident, its record durable and never unlinked, and
// answered not durable.
func pendingRelease(t *testing.T, what string, s *Service, cfg Config, tr *tracer, r ReserveReply) {
	t.Helper()
	rec, ok := held(s, r.ID)
	_, rerr := s.Released(owner1, r.Ref())
	d, dok := durable(t, cfg.StateDir, r.ID)
	if !ok || rec.State == StateQuarantined || used(s)[2] != 1 || !dok || d.State != StateStopping || unlinkedRecord(tr, r.ID) || !errors.Is(rerr, ErrNotDurable) {
		tr.dump(t)
		t.Fatalf("A RELEASE WAS ACKNOWLEDGED WITHOUT CERTIFIED EVIDENCE (%s): held %v %s, used %v, record %v %s, released %v", what, ok, rec.State, used(s), dok, d.State, rerr)
	}
}

// stateWatch records, at each fsync of the state directory, whether
// released/ was a real directory at that moment: what that fsync certified.
// It is a barrier only, injected while no fault of the same call is (the
// first matching fault acts).
type stateWatch struct {
	mu  sync.Mutex
	saw []bool
}

func watchStateSync(fsys *faultFS, cfg Config) *stateWatch {
	w := &stateWatch{}
	dir := filepath.Join(cfg.StateDir, "released")
	fsys.inject(&fsFault{op: "syncdir", path: cfg.StateDir, block: func() {
		fi, err := os.Lstat(dir)
		w.mu.Lock()
		w.saw = append(w.saw, err == nil && fi.IsDir())
		w.mu.Unlock()
	}})
	return w
}

// certified says whether events — from the watch's injection on — hold,
// in this order: an fsync of the state directory that succeeded while
// released/ was a real directory; then E kept — published (its rename) or,
// found there already, its bytes' fsync — then released/'s fsync; and only
// then r's record unlinked.
func (w *stateWatch) certified(events []event, cfg Config, r ReserveReply, existing bool) bool {
	w.mu.Lock()
	saw := append([]bool(nil), w.saw...)
	w.mu.Unlock()
	ev := r.ID + "." + r.Incarnation
	kept := func(e event) bool { return e.op == "rename" && e.id == ev }
	if existing {
		kept = func(e event) bool { return e.op == "sync" && e.id == ev }
	}
	step, k := 0, 0
	for _, e := range events {
		if e.kind != "fs" {
			continue
		}
		if e.op == "syncdir" && e.id == filepath.Base(cfg.StateDir) {
			if step == 0 && e.err == "" && k < len(saw) && saw[k] {
				step = 1
			}
			k++
			continue
		}
		switch {
		case e.err != "":
		case step == 1 && kept(e):
			step = 2
		case step == 2 && e.op == "syncdir" && e.id == "released":
			step = 3
		case step == 3 && e.op == "unlink" && e.id == r.ID:
			step = 4
		}
	}
	return step == 4
}

// Each evidence keeping certifies released/'s link — an fsync of the state
// directory issued after released/ was seen — before E is kept and before
// anything is withdrawn: at its first creation; at every retry while the
// barrier still fails, EEXIST notwithstanding, until it is repaired; when
// released/ was replaced after an earlier certification (a name is not a
// directory); and for the operator's release, refused at every attempt.
// E found already there has its own name certified again, by released/'s
// fsync, at every retry: the reaper's, its owner's and the stop's.
func TestEveryEvidenceKeepingCertifiesItsDirectorysLink(t *testing.T) {
	setup := func(t *testing.T, attempt string) (*tracer, *faultFS, *modelHost, Config, *Service, ReserveReply) {
		t.Helper()
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		r := mustReserve(t, s, owner1, lockedReq(attempt, 1, 128))
		if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
		return tr, fsys, h, cfg, s, r
	}
	t.Run("first creation: certified before its evidence and its withdrawal", func(t *testing.T) {
		tr, fsys, _, cfg, s, r := setup(t, "0vfirst")
		w := watchStateSync(fsys, cfg)
		mark := len(tr.all())
		if err := s.DestroyOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
		if !w.certified(tr.all()[mark:], cfg, r, false) {
			tr.dump(t)
			t.Fatal("RELEASED/'S NEW LINK WAS NOT CERTIFIED BEFORE ITS EVIDENCE AND ITS WITHDRAWAL")
		}
	})
	t.Run("retried while the barrier still fails, then repaired", func(t *testing.T) {
		tr, fsys, _, cfg, s, r := setup(t, "0vretry")
		x := fsys.inject(stateSync(cfg))
		if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) || x.hits != 1 {
			t.Fatalf("fixture: the first keeping's barrier: %v (%d hits)", err, x.hits)
		}
		pendingRelease(t, "the first keeping", s, cfg, tr, r)
		for pass := 1; pass <= 3; pass++ {
			s.ReapOnce() // released/ exists now: its mkdir meets EEXIST
			pendingRelease(t, fmt.Sprintf("the reaper's retry %d", pass), s, cfg, tr, r)
			if x.hits != 1+pass {
				t.Fatalf("the barrier was not issued again at the reaper's retry %d: %d hits", pass, x.hits)
			}
		}
		err := s.DestroyOf(owner1, r.Ref())
		pendingRelease(t, "its owner's retry", s, cfg, tr, r)
		if !errors.Is(err, ErrNotDurable) || x.hits != 5 {
			t.Fatalf("its owner's retry: %v (%d hits)", err, x.hits)
		}
		fsys.clear() // repaired
		w := watchStateSync(fsys, cfg)
		mark := len(tr.all())
		s.ReapOnce()
		if _, ok := held(s, r.ID); ok || used(s)[2] != 0 || !w.certified(tr.all()[mark:], cfg, r, false) {
			tr.dump(t)
			t.Fatalf("the retry after the repair: held %v, used %v", ok, used(s))
		}
		if rel, err := s.Released(owner1, r.Ref()); err != nil || rel.By != ByTimelyCleanup {
			t.Fatalf("its evidence after the repair: %+v %v", rel, err)
		}
	})
	t.Run("its evidence published, the evidence directory's fsync failing at every retry", func(t *testing.T) {
		tr, fsys, _, cfg, s, r := setup(t, "0vname")
		x := fsys.inject(&fsFault{op: "syncdir", path: "released", mode: "before", err: errInjected})
		if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) || x.hits != 1 {
			t.Fatalf("fixture: its evidence renamed, released/'s fsync failed: %v (%d hits)", err, x.hits)
		}
		if _, found, err := ReadEvidence(cfg.StateDir, r.Ref()); !found || err != nil {
			t.Fatalf("fixture: its evidence visible: %v %v", found, err)
		}
		for pass := 1; pass <= 2; pass++ {
			s.ReapOnce() // its evidence found already there
			pendingRelease(t, fmt.Sprintf("its evidence's name uncertified, the reaper's retry %d", pass), s, cfg, tr, r)
		}
		err := s.DestroyOf(owner1, r.Ref())
		pendingRelease(t, "its evidence's name uncertified, its owner's retry", s, cfg, tr, r)
		if !errors.Is(err, ErrNotDurable) || x.hits != 4 {
			t.Fatalf("its owner's retry: %v (%d hits)", err, x.hits)
		}
		err = s.Shutdown(context.Background())
		if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateStopping || unlinkedRecord(tr, r.ID) || !errors.Is(err, ErrNotDurable) {
			tr.dump(t)
			t.Fatalf("A RELEASE WAS ACKNOWLEDGED WITHOUT CERTIFIED EVIDENCE (its evidence's name uncertified, the stop's retry): %v; record %v %s", err, ok, d.State)
		}
	})
	t.Run("the evidence directory replaced after an earlier certification", func(t *testing.T) {
		tr, fsys, _, cfg, s, r := setup(t, "0vafter")
		first := mustReserve(t, s, owner1, lockedReq("0vbefore", 1, 128))
		if err := s.DestroyOf(owner1, first.Ref()); err != nil {
			t.Fatal(err) // released/ made, certified, its evidence kept
		}
		// a directory this service never certified now has the name, as an
		// operator might leave it while serve runs (against §8.4); the
		// earlier evidence is moved aside, not deleted
		dir := filepath.Join(cfg.StateDir, "released")
		if err := os.Rename(dir, filepath.Join(t.TempDir(), "aside")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		x := fsys.inject(stateSync(cfg))
		err := s.DestroyOf(owner1, r.Ref())
		pendingRelease(t, "a replaced evidence directory", s, cfg, tr, r)
		if !errors.Is(err, ErrNotDurable) || x.hits != 1 {
			t.Fatalf("fixture: %v (%d hits)", err, x.hits)
		}
	})
	t.Run("the operator's release, refused at every attempt while the barrier fails", func(t *testing.T) {
		tr, fsys, h, cfg, s, r := setup(t, "0vop")
		h.fail("rmjail", "before", errInjected)
		if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("fixture: quarantined: %v", err)
		}
		h.heal("rmjail")
		rec, _ := held(s, r.ID)
		in, err := s.RetryCleanup(rec.Selection())
		if err != nil {
			t.Fatal(err)
		}
		x := fsys.inject(stateSync(cfg))
		for attempt := 1; attempt <= 2; attempt++ {
			err := s.Release(in.Selection)
			got, ok := held(s, r.ID)
			if err == nil || !ok || got.State != StateQuarantined || used(s)[2] != 1 || unlinkedRecord(tr, r.ID) {
				tr.dump(t)
				t.Fatalf("AN OPERATOR'S RELEASE WAS DONE WITHOUT CERTIFIED EVIDENCE (attempt %d): %v; held %v %s, used %v", attempt, err, ok, got.State, used(s))
			}
			if x.hits != attempt {
				t.Fatalf("the barrier at attempt %d: %d hits", attempt, x.hits)
			}
		}
		fsys.clear()
		if err := s.Release(in.Selection); err != nil {
			t.Fatalf("the operator's release after the repair: %v", err)
		}
		if rel, err := s.Released(owner1, r.Ref()); err != nil || rel.By != ByOperator {
			t.Fatalf("its evidence after the repair: %+v %v", rel, err)
		}
	})
}

// firstLifeKeptUncertified leaves r's release pending with its evidence
// visible but never certified in its life — released/'s fsync failing after
// E's rename — and ends that life without a stop, as a crash would: nothing
// retries it.
func firstLifeKeptUncertified(t *testing.T, cfg Config, h Host, fsys *faultFS, attempt string) ReserveReply {
	t.Helper()
	s, err := openService(cfg, h, fsys)
	if err != nil {
		t.Fatal(err)
	}
	r := mustReserve(t, s, owner1, lockedReq(attempt, 1, 128))
	if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
		t.Fatal(err)
	}
	fsys.inject(&fsFault{op: "syncdir", path: "released", mode: "before", err: errInjected})
	if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) {
		t.Fatalf("fixture: pending: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	fsys.clear()
	if _, found, err := ReadEvidence(cfg.StateDir, r.Ref()); !found || err != nil {
		t.Fatalf("fixture: its evidence visible: %v %v", found, err)
	}
	if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateStopping {
		t.Fatalf("fixture: its record stopping: %+v %v", d, ok)
	}
	return r
}

// A reopen finds a release whose evidence an earlier life published but
// never certified. It is loaded as a pending release, charged, and its
// evidence is not taken as durable: while any barrier down to E fails —
// released/'s, the state directory's, E's own — the reaper's retry
// withdraws nothing; with every barrier holding, the retry certifies E
// before the record's unlink. A keeping that failed before E was written
// leaves the record `stopping` alone, and a reopen quarantines it (A).
func TestAReopenCertifiesItsEvidenceBeforeAnyWithdrawal(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault func(cfg Config, r ReserveReply) *fsFault // the second life's, after its open
	}{
		{"the evidence directory's fsync still failing", func(Config, ReserveReply) *fsFault {
			return &fsFault{op: "syncdir", path: "released", mode: "before", err: errInjected}
		}},
		{"the state directory's fsync failing", func(cfg Config, _ ReserveReply) *fsFault { return stateSync(cfg) }},
		{"its evidence's own fsync failing", func(_ Config, r ReserveReply) *fsFault {
			return &fsFault{op: "sync", path: r.Incarnation + ".json", mode: "before", err: errInjected}
		}},
		{"every barrier holding, the control", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			r := firstLifeKeptUncertified(t, cfg, h, fsys, "0vreopen")
			s := open(t, cfg, h, fsys)
			if rec, ok := held(s, r.ID); !ok || rec.State != StateStopping {
				t.Fatalf("fixture: loaded as a pending release: %+v %v", rec, ok)
			}
			if c.fault != nil {
				x := fsys.inject(c.fault(cfg, r))
				s.ReapOnce()
				pendingRelease(t, "after a reopen, "+c.name, s, cfg, tr, r)
				if x.hits == 0 {
					t.Fatal("fixture: the fault was not reached")
				}
				fsys.clear() // repaired
			}
			w := watchStateSync(fsys, cfg)
			mark := len(tr.all())
			s.ReapOnce()
			_, ok := held(s, r.ID)
			if !w.certified(tr.all()[mark:], cfg, r, true) {
				tr.dump(t)
				t.Fatalf("A REOPENED RELEASE WAS WITHDRAWN WITHOUT CERTIFYING ITS EVIDENCE: held %v, used %v", ok, used(s))
			}
			if ok || used(s)[2] != 0 {
				t.Fatalf("its certified release was not finished: used %v", used(s))
			}
		})
	}
	t.Run("its keeping failed before its evidence was written", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s, err := openService(cfg, h, fsys)
		if err != nil {
			t.Fatal(err)
		}
		r := mustReserve(t, s, owner1, lockedReq("0vnone", 1, 128))
		if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
		fsys.inject(stateSync(cfg)) // at every keeping of this life, the stop's included
		if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrNotDurable) {
			t.Fatalf("fixture: pending: %v", err)
		}
		_ = s.Shutdown(context.Background()) // its retry: pending still, or (the defect) released
		fsys.clear()
		s2 := open(t, cfg, h, fsys)
		rec, ok := held(s2, r.ID)
		_, rerr := s2.Released(owner1, r.Ref())
		if !ok || rec.State != StateQuarantined || used(s2)[2] != 1 || !errors.Is(rerr, ErrState) {
			tr.dump(t)
			t.Fatalf("A RELEASE WHOSE EVIDENCE WAS NEVER CERTIFIED CAME BACK RELEASED: held %v %s, used %v, released %v", ok, rec.State, used(s2), rerr)
		}
		if _, found, err := ReadEvidence(cfg.StateDir, r.Ref()); found || err != nil {
			t.Fatalf("fixture: no evidence was written: %v %v", found, err)
		}
	})
}

// released/ must be a real directory. A symlink there is never followed:
// nothing is kept through it, the release stays pending and the link is
// left as found; a regular file there keeps the release pending too. And
// nothing is read through it: released, a replayed destroy, the bare
// destroy, the operator's views and the loader prove nothing through a
// symlink — even to a directory holding that incarnation's own evidence.
func TestEvidenceIsKeptAndReadOnlyInARealDirectory(t *testing.T) {
	for _, kind := range []string{"symlink", "regular file"} {
		t.Run("keeping: a "+kind+" at the evidence directory's name", func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			elsewhere := t.TempDir()
			path := filepath.Join(cfg.StateDir, "released")
			if kind == "symlink" {
				if err := os.Symlink(elsewhere, path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("not a directory\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			r := mustReserve(t, s, owner1, lockedReq("0vnotdir", 1, 128))
			if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
			err := s.DestroyOf(owner1, r.Ref())
			pendingRelease(t, "a "+kind+" at the evidence directory's name", s, cfg, tr, r)
			through, _ := os.ReadDir(elsewhere)
			fi, lerr := os.Lstat(path)
			if len(through) != 0 || lerr != nil || (kind == "symlink") != (fi.Mode()&os.ModeSymlink != 0) || !errors.Is(err, ErrNotDurable) {
				t.Fatalf("SOMETHING WAS KEPT THROUGH, OR DONE TO, A %s AT THE EVIDENCE DIRECTORY'S NAME: kept %d, %v %v; destroy %v", strings.ToUpper(kind), len(through), fi, lerr, err)
			}
		})
	}
	t.Run("reading: no release is proven through a symlink", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		r := mustReserve(t, s, owner1, lockedReq("0vread", 1, 128))
		if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
		if err := s.DestroyOf(owner1, r.Ref()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Released(owner1, r.Ref()); err != nil {
			t.Fatalf("fixture: its evidence proves it: %v", err)
		}
		// its evidence moved out of the state directory (not deleted), and
		// reachable from released/ only through a symlink
		path := filepath.Join(cfg.StateDir, "released")
		moved := filepath.Join(t.TempDir(), "released")
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, path); err != nil {
			t.Fatal(err)
		}
		if rel, err := s.Released(owner1, r.Ref()); !errors.Is(err, ErrUnproven) {
			t.Fatalf("A RELEASE WAS PROVEN THROUGH A SYMLINK: released %+v %v", rel, err)
		}
		if err := s.DestroyOf(owner1, r.Ref()); !errors.Is(err, ErrUnproven) {
			t.Fatalf("A RELEASE WAS PROVEN THROUGH A SYMLINK: its owner's replay %v", err)
		}
		if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrUnproven) {
			t.Fatalf("A RELEASE WAS PROVEN THROUGH A SYMLINK: the bare destroy %v", err)
		}
		if ev, found, err := ReadEvidence(cfg.StateDir, r.Ref()); found || err == nil {
			t.Fatalf("THE OPERATOR'S VIEW READ EVIDENCE THROUGH A SYMLINK: %+v %v %v", ev.Release, found, err)
		}
		if evs, bad, err := Evidence(cfg.StateDir); len(evs) != 0 || len(bad) != 1 || bad[0].Path != path || err != nil {
			t.Fatalf("THE OPERATOR'S LIST READ EVIDENCE THROUGH A SYMLINK: %d kept, problems %+v, %v", len(evs), bad, err)
		}
	})
	t.Run("the loader: evidence reachable only through a symlink backs nothing", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		r := firstLifeKeptUncertified(t, cfg, h, fsys, "0vloader")
		path := filepath.Join(cfg.StateDir, "released")
		moved := filepath.Join(t.TempDir(), "released")
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, path); err != nil {
			t.Fatal(err)
		}
		s := open(t, cfg, h, fsys)
		if rec, ok := held(s, r.ID); !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
			t.Fatalf("A RECORD WAS BACKED BY EVIDENCE REACHABLE ONLY THROUGH A SYMLINK: %+v %v, used %v", rec.State, ok, used(s))
		}
	})
}

// The wire's consumers of release evidence — released, and the destroy a
// daemon replays after a lost reply — are refused at every retry while the
// state directory's fsync fails, and answered released once it is repaired:
// the daemon's held orphan and the runner's retention take nothing less
// (§11.8: any refusal is no proof). A real core, wire and client over a
// fresh private unix socket.
func TestTheWiresConsumersWaitForCertifiedEvidence(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, newModelHost(t, tr), fsys)
	t.Chdir(t.TempDir()) // a short, private socket path
	l, err := net.Listen("unix", "e.sock")
	if err != nil {
		t.Fatal(err)
	}
	sv := &Server{Service: s, AllowedUIDs: map[uint32]bool{uint32(os.Getuid()): true}, Version: "test"}
	go sv.Serve(l)
	t.Cleanup(func() { l.Close() })
	cl, err := Dial(context.Background(), "e.sock", owner1.Daemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r, err := cl.Reserve(lockedReq("0vwire", 1, 128))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	x := fsys.inject(stateSync(cfg))
	if _, err := cl.DestroyOfRelease(r.Ref()); err == nil || !strings.Contains(err.Error(), ErrNotDurable.Error()) {
		t.Fatalf("fixture: pending: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if rel, err := cl.DestroyOfRelease(r.Ref()); err == nil || !strings.Contains(err.Error(), ErrNotDurable.Error()) {
			t.Fatalf("A REPLAYED DESTROY WAS ANSWERED DONE WITHOUT CERTIFIED EVIDENCE (attempt %d): %+v %v", attempt, rel, err)
		}
		if rel, err := cl.Released(r.Ref()); err == nil || !strings.Contains(err.Error(), ErrNotDurable.Error()) {
			t.Fatalf("RELEASED WAS ANSWERED WITHOUT CERTIFIED EVIDENCE (attempt %d): %+v %v", attempt, rel, err)
		}
	}
	if vms, err := cl.List(); err != nil || len(vms) != 1 || !r.Ref().Names(vms[0]) || used(s)[2] != 1 || x.hits != 3 {
		t.Fatalf("the pending release, listed and charged: %+v %v, used %v, %d hits", vms, err, used(s), x.hits)
	}
	fsys.clear() // repaired
	s.ReapOnce()
	if rel, err := cl.Released(r.Ref()); err != nil || rel.By != ByTimelyCleanup {
		t.Fatalf("released after the repair: %+v %v", rel, err)
	}
	if vms, err := cl.List(); err != nil || len(vms) != 0 || used(s)[2] != 0 {
		t.Fatalf("after the repair: %+v %v, used %v", vms, err, used(s))
	}
}
