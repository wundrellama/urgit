package launcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var errInjected = errors.New("injected EIO")

// A reservation is acknowledged only after its record was written,
// fsynced, renamed into place and the directory fsynced — in that order,
// each through the real adapter.
func TestPublicationIsWriteSyncRenameSyncDir(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, fsys)
	opened := len(tr.all()) // the open's own calls (it created attempts/ and certified the links)
	syncsAtOpen := fsys.realCalls("syncdir")
	var certified []string
	for _, e := range tr.all()[:opened] {
		if e.op == "syncdir" {
			certified = append(certified, e.id)
		}
	}
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	var got []string
	for _, e := range tr.all()[opened:] {
		if e.kind == "fs" && (e.id == r.ID || e.op == "syncdir") {
			if e.err != "" || e.inject != "" {
				t.Fatalf("unexpected failure in the trace: %s", e)
			}
			got = append(got, e.op)
		}
	}
	tr.dump(t)
	want := []string{"open", "truncate", "write", "sync", "close", "rename", "syncdir"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("publication order %v, want %v", got, want)
	}
	// the open certified the state directory's link (its parent), then
	// the records directory's (the state directory), then — stage 01,
	// triage 08's restart proof — the records' own presence and absence
	// (the records directory), once each
	if want := []string{filepath.Base(filepath.Dir(cfg.StateDir)), filepath.Base(cfg.StateDir), "attempts"}; syncsAtOpen != 3 || strings.Join(certified, " ") != strings.Join(want, " ") {
		t.Fatalf("the open synced %v (%d), want %v", certified, syncsAtOpen, want)
	}
	for op, want := range map[string]int{"sync": 1, "syncdir": 4, "rename": 1} {
		if fsys.realCalls(op) != want {
			t.Fatalf("%d real %s calls, want %d", fsys.realCalls(op), op, want)
		}
	}
	rec, ok := durable(t, cfg.StateDir, r.ID)
	if !ok || rec.State != StatePreparing || rec.CID != r.CID {
		t.Fatalf("durable record %+v %v", rec, ok)
	}
	if e := stateEntries(t, cfg.StateDir); len(e) != 1 {
		t.Fatalf("state directory %v: a temp was left", e)
	}
}

// Every failure of a publication step refuses the acknowledgement. What
// is left behind matches the step: before the rename nothing durable
// changed and the charge is dropped; at or after the rename the outcome
// is uncertain, so the record is withdrawn durably before it is
// forgotten — and if that fails too the charge stays.
func TestReservationIsNeverAcknowledgedWithoutDurability(t *testing.T) {
	cases := []struct {
		name     string
		faults   []*fsFault
		charged  bool // still charged in the live service
		reopened bool // held after a process stop and reopen
		leftover bool // an explained temp remains
	}{
		{name: "open fails before", faults: []*fsFault{{op: "open", path: ".tmp", mode: "before", err: errInjected}}},
		{name: "write fails before", faults: []*fsFault{{op: "write", mode: "before", err: errInjected}}},
		{name: "write is short", faults: []*fsFault{{op: "write", mode: "short", err: io.ErrShortWrite}}},
		{name: "fsync fails before", faults: []*fsFault{{op: "sync", mode: "before", err: errInjected}}},
		{name: "fsync fails after syncing", faults: []*fsFault{{op: "sync", mode: "after", err: errInjected}}},
		{name: "close fails after closing", faults: []*fsFault{{op: "close", mode: "after", err: errInjected}}},
		{name: "temp cleanup fails too", faults: []*fsFault{{op: "sync", mode: "before", err: errInjected}, {op: "unlink", path: ".tmp", mode: "before", err: errInjected}}, leftover: true},
		{name: "rename fails before", faults: []*fsFault{{op: "rename", mode: "before", err: errInjected}}},
		{name: "rename renames then fails", faults: []*fsFault{{op: "rename", mode: "after", err: errInjected}}},
		{name: "directory fsync fails before", faults: []*fsFault{{op: "syncdir", mode: "before", err: errInjected, times: 1}}},
		{name: "directory fsync fails after syncing", faults: []*fsFault{{op: "syncdir", mode: "after", err: errInjected, times: 1}}},
		{name: "rename renames then fails and the withdrawal fails", faults: []*fsFault{{op: "rename", mode: "after", err: errInjected}, {op: "unlink", path: ".json", mode: "before", err: errInjected}}, charged: true, reopened: true},
		// the withdrawal's unlink happened, only its fsync failed: a process
		// stop finds no record (no power was lost), the live service keeps
		// the charge because the withdrawal's durability is unproven
		{name: "directory fsync fails and so does the withdrawal's", faults: []*fsFault{{op: "syncdir", mode: "before", err: errInjected}}, charged: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			for _, f := range c.faults {
				fsys.inject(f)
			}
			req := lockedReq("0v1", 1, 128)
			id := IDFor(cfg.IDPrefix, req.Attempt)
			reply, err := s.Reserve(owner1, req)
			for _, f := range c.faults {
				if f.hits == 0 {
					tr.dump(t)
					t.Fatalf("fault %s/%s was never reached", f.op, f.mode)
				}
			}
			tr.dump(t)
			if err == nil || reply.ID != "" {
				t.Fatalf("acknowledged without a durable record: %+v %v", reply, err)
			}
			if !errors.Is(err, ErrNotDurable) || !errors.Is(err, c.faults[0].err) {
				t.Fatalf("refusal does not carry the fault: %v", err)
			}
			rec, isHeld := held(s, id)
			if isHeld != c.charged || (used(s)[2] == 1) != c.charged {
				t.Fatalf("charged %v (held %v %+v), want %v", used(s), isHeld, rec, c.charged)
			}
			// an unresolved publication stays charged as an unacknowledged
			// reservation — not a quarantine, which no durable record could
			// hold here — until its owner destroys it or its deadline passes
			if c.charged && (rec.State != StatePreparing || errors.Is(err, ErrQuarantined) || !strings.Contains(rec.Reason, "not acknowledged")) {
				t.Fatalf("an unresolved publication must stay charged as an unacknowledged reservation: %+v %v", rec, err)
			}
			fsys.clear()
			names := stateEntries(t, cfg.StateDir)
			if _, ok := names[id+".json.tmp"]; ok != c.leftover {
				t.Fatalf("state %v: leftover temp %v, want %v", names, ok, c.leftover)
			}
			s2 := reopen(t, s, h)
			if _, ok := held(s2, id); ok != c.reopened {
				t.Fatalf("after reopen held %v, want %v (state %v)", ok, c.reopened, names)
			}
			if p := s2.Problems(); len(p) != 0 {
				t.Fatalf("a publication fault left unexplained state: %+v", p)
			}
			if c.leftover && len(s2.Leftovers()) != 1 {
				t.Fatalf("leftover not reported: %v", s2.Leftovers())
			}
			// an unacknowledged record that did become durable is withdrawn
			// by its owner like any reservation; otherwise the attempt can
			// be reserved again
			if c.reopened {
				if err := s2.Destroy(owner1, id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s2.Reserve(owner1, req); err != nil {
				t.Fatalf("reserve after the fault cleared: %v", err)
			}
		})
	}
}

// A release is capacity given back: it waits for the durable withdrawal
// of the record. A failed unlink leaves the record file: its release is
// authorized and evidenced, its accounting pending — charged, not an
// incident — and its owner's retry confirms it. Policy-dependent
// (late-accounting ruling 01; INTEGRATION.md §11.8): before the ruling it
// was quarantined, its owner's retry refused and only the operator's clear
// released it. An unlink that acted despite its error is looked up and
// confirmed by the directory's fsync: a durable release. A completed
// unlink whose directory fsync fails is not a quarantine: the capacity
// stays charged until a retry confirms it.
func TestReleaseWaitsForADurableWithdrawal(t *testing.T) {
	cases := []struct {
		name       string
		fault      *fsFault
		outcome    string // quarantined | released | unconfirmed
		durableRec bool   // the record file is there right after the destroy
	}{
		{name: "unlink fails before", fault: &fsFault{op: "unlink", path: ".json", mode: "before", err: errInjected, times: 1}, outcome: "pending", durableRec: true},
		{name: "unlink unlinks then fails", fault: &fsFault{op: "unlink", path: ".json", mode: "after", err: errInjected, times: 1}, outcome: "released"},
		{name: "directory fsync fails", fault: &fsFault{op: "syncdir", mode: "before", err: errInjected, times: 1}, outcome: "unconfirmed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			// the stopping publication succeeds; the withdrawal is what fails
			x := fsys.inject(&fsFault{op: c.fault.op, path: c.fault.path, mode: c.fault.mode, err: c.fault.err, times: 1})
			if c.fault.op == "syncdir" {
				// skip the stopping publication's own directory fsync
				fsys.clear()
				fsys.inject(&fsFault{op: "unlink", path: ".json", block: func() {
					fsys.inject(x)
				}, times: 1})
			}
			err := s.Destroy(owner1, r.ID)
			tr.dump(t)
			if x.hits != 1 {
				t.Fatalf("fault not reached (%d)", x.hits)
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 {
				t.Fatalf("host cleanup incomplete: %v", leaks)
			}
			if _, ok := durable(t, cfg.StateDir, r.ID); ok != c.durableRec {
				t.Fatalf("record file present %v, want %v", ok, c.durableRec)
			}
			rec, ok := held(s, r.ID)
			switch c.outcome {
			case "pending":
				if !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) || !ok || rec.State != StateStopping || used(s)[2] != 1 {
					t.Fatalf("a failed withdrawal: %v; %+v %v used %v", err, rec, ok, used(s))
				}
				fsys.clear()
				if err := s.Destroy(owner1, r.ID); err != nil {
					t.Fatalf("the owner's retry did not confirm the release: %v", err)
				}
			case "released":
				if err != nil || ok || used(s)[2] != 0 {
					t.Fatalf("a confirmed removal was not a release: %v; held %v used %v", err, ok, used(s))
				}
			case "unconfirmed":
				if !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) || !ok || rec.State != StateStopping || used(s)[2] != 1 {
					t.Fatalf("an unconfirmed withdrawal: %v; %+v %v used %v", err, rec, ok, used(s))
				}
				fsys.clear()
				if err := s.Destroy(owner1, r.ID); err != nil {
					t.Fatalf("the retry did not confirm the release: %v", err)
				}
			}
			if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
				t.Fatalf("not released at the end: %v", used(s))
			}
			if _, ok := durable(t, cfg.StateDir, r.ID); ok {
				t.Fatal("the record file survived the release")
			}
		})
	}
}

// A release withdraws the record after the host cleanup: unlink, then the
// directory's fsync, and nothing else.
func TestReleaseIsUnlinkThenSyncDir(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	mark := len(tr.all())
	if err := s.Destroy(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	events := tr.all()[mark:]
	last := -1
	for i, e := range events {
		if e.kind == "host" {
			last = i
		}
	}
	// policy-dependent (late-accounting ruling 01; INTEGRATION.md §11.8):
	// before the ruling the tail was the withdrawal alone, unlink then
	// syncdir. The release's evidence is durable first (released/, created
	// by this first release, its link certified), then the record is
	// withdrawn, then the evidence records the confirmation.
	var tail []string
	for _, e := range events[last+1:] {
		free := e.op == "lstat" && strings.Contains(e.err, "no such file") // the evidence path is free
		if e.err != "" && !free || e.inject != "" || (e.op == "unlink" && e.id != r.ID) {
			t.Fatalf("unexpected withdrawal event %s", e)
		}
		op := e.op
		if op == "rename" {
			op += ":" + e.state
		}
		tail = append(tail, op)
	}
	// the tail's three parts: the evidence first, the withdrawal (as the
	// accepted test checked it, word for word), the evidence's update.
	// Correction-dependent (independent review 03; INTEGRATION.md §11.9):
	// the evidence began "syncdir lstat" — the state directory fsynced only
	// when this release had made released/ — and now begins "lstat syncdir
	// lstat": released/ seen as a real directory, then the state directory
	// fsynced (its link certified) at every keeping, then the evidence path
	const evidence = "open truncate write sync close rename:released syncdir"
	const keeping = "lstat syncdir lstat "
	cut := func(from int, op string) int {
		for i := from; i < len(tail); i++ {
			if tail[i] == op {
				return i
			}
		}
		return len(tail)
	}
	un := cut(0, "unlink")
	rd := cut(un, "readfile")
	if last < 0 || strings.Join(tail[un:rd], " ") != "unlink syncdir" {
		tr.dump(t)
		t.Fatalf("withdrawal order %v after the last host operation (%d), want [unlink syncdir]", tail[un:rd], last)
	}
	if first, update := strings.Join(tail[:un], " "), strings.Join(tail[rd:], " "); first != keeping+evidence || update != "readfile "+evidence {
		tr.dump(t)
		t.Fatalf("release order: the evidence %q before the withdrawal and %q after it, want %q and %q", first, update, keeping+evidence, "readfile "+evidence)
	}
}

// A directory that appears at a record's path is never removed by the
// launcher: the kernel refuses the rename over it (EISDIR), the outcome
// counts as uncertain, and the withdrawal's unlink(2) refuses a directory
// too — so the reservation is not acknowledged, stays charged as an
// unacknowledged reservation, and a restart fences on the directory.
func TestForeignDirectoryAtTheRecordPathIsNeverRemoved(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	id := IDFor(cfg.IDPrefix, "0v1")
	dir := filepath.Join(cfg.StateDir, "attempts", id+".json")
	if err := os.Mkdir(dir, 0o700); err != nil { // after the open: the loader never saw it
		t.Fatal(err)
	}
	_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
	if fi, lerr := os.Lstat(dir); lerr != nil || !fi.IsDir() {
		t.Fatalf("the foreign directory was removed: %v (reserve: %v)", lerr, err)
	}
	if !errors.Is(err, syscall.EISDIR) || !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrQuarantined) {
		t.Fatalf("reserve over a foreign directory: %v", err)
	}
	if rec, ok := held(s, id); !ok || rec.State != StatePreparing || used(s)[2] != 1 {
		t.Fatalf("an uncertain publication was forgotten: %+v %v", rec, ok)
	}
	s2 := reopen(t, s, nil)
	if p := s2.Problems(); len(p) != 1 || p[0].Path != dir || p[0].Kind != "not-regular" {
		t.Fatalf("restart problems %+v", p)
	}
}

// A symlink planted in a publication path is not followed: the open is
// refused by the kernel (ELOOP) and its target is not written.
func TestSymlinkAtThePublicationPathIsNotFollowed(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	id := IDFor(cfg.IDPrefix, "0v1")
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cfg.StateDir, "attempts", id+".json.tmp")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
	if data, _ := os.ReadFile(victim); string(data) != "untouched" {
		t.Fatalf("the publication followed a symlink and wrote %q", data)
	}
	if !errors.Is(err, syscall.ELOOP) || used(s)[2] != 0 {
		t.Fatalf("reserve through a symlink: %v, used %v", err, used(s))
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was moved or removed: %v", err)
	}
}

// A file with another name planted in a publication path is not this
// launcher's: nothing is truncated or written through it, and a restart
// fences on it.
func TestHardLinkAtThePublicationPathIsNotOverwritten(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	id := IDFor(cfg.IDPrefix, "0v1")
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cfg.StateDir, "attempts", id+".json.tmp")
	if err := os.Link(victim, link); err != nil {
		t.Skipf("no hard link across the fixture directories: %v", err)
	}
	_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
	if data, _ := os.ReadFile(victim); string(data) != "untouched" {
		t.Fatalf("the publication overwrote a file with another name: %q", data)
	}
	if !errors.Is(err, ErrNotDurable) || used(s)[2] != 0 {
		t.Fatalf("reserve through a hard link: %v, used %v", err, used(s))
	}
	s2 := reopen(t, s, nil)
	if p := s2.Problems(); len(p) != 1 || p[0].Path != link || p[0].Kind != "foreign" {
		t.Fatalf("restart problems %+v", p)
	}
}

// A FIFO planted in a publication path does not block the publication
// (which would hold the record's token for ever): the kernel refuses the
// non-blocking open (ENXIO) and the FIFO is left in place.
func TestFIFOAtThePublicationPathDoesNotBlock(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	id := IDFor(cfg.IDPrefix, "0v1")
	fifo := filepath.Join(cfg.StateDir, "attempts", id+".json.tmp")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Reserve(owner1, lockedReq("0v1", 1, 128))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, syscall.ENXIO) || used(s)[2] != 0 {
			t.Fatalf("reserve through a fifo: %v, used %v", err, used(s))
		}
	case <-time.After(5 * time.Second):
		// release the blocked open so the test binary can end
		if r, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			defer r.Close()
		}
		t.Fatal("the publication blocked on a fifo")
	}
	if fi, err := os.Lstat(fifo); err != nil || fi.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("the fifo was moved or removed: %v", err)
	}
}

// The same durability refusal with a real kernel error: the records
// directory is made read-only for this ordinary user, so the teardown's
// `stopping` record is refused with EACCES by the kernel itself. Without
// it the teardown stops the VMM, removes nothing and decides no
// quarantine: the record keeps its state and charge for a retry.
func TestReleaseRefusedByTheKernelKeepsTheCharge(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.StateDir, "attempts")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		t.Skip("the directory stayed writable (running as root?): no kernel refusal to test")
	}
	err := s.Destroy(owner1, r.ID)
	if !errors.Is(err, ErrNotDurable) || !errors.Is(err, syscall.EACCES) || errors.Is(err, ErrQuarantined) {
		t.Fatalf("destroy on a read-only records directory: %v", err)
	}
	rec, ok := held(s, r.ID)
	if !ok || rec.State != StateRunning || used(s)[2] != 1 || !strings.Contains(rec.Reason, "teardown halted before any removal") {
		t.Fatalf("a halted teardown changed the record: %+v %v", rec, ok)
	}
	if h.exists("vmm", r.ID) {
		t.Fatal("the halted teardown did not stop the VMM")
	}
	if !h.exists("jail", r.ID) || !h.exists("cgroup", r.ID) {
		t.Fatalf("a holding was removed without a durable stopping record: %v", h.leaks(r.ID))
	}
	// a publication is refused by the same kernel error, unacknowledged
	if _, err := s.Reserve(owner1, lockedReq("0v2", 1, 128)); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("reserve on a read-only records directory: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// the durable record still says running: a process stop and reopen
	// charges it, and the owner's retry releases it
	s2 := reopen(t, s, h)
	if rec, ok := held(s2, r.ID); !ok || rec.State != StateRunning {
		t.Fatalf("reopened %+v %v", rec, ok)
	}
	if err := s2.Destroy(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	if used(s2)[2] != 0 || len(stateEntries(t, cfg.StateDir)) != 0 || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("not released: %v %v %v", used(s2), stateEntries(t, cfg.StateDir), h.leaks(r.ID))
	}
}

// A regular temp in a record's publication path is this launcher's own
// unfinished publication: reported, never authoritative, and reused by
// the next publication of that id.
func TestOwnLeftoverTempIsReportedAndReused(t *testing.T) {
	cfg := testConfig(t.TempDir())
	id := IDFor(cfg.IDPrefix, "0v1")
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "attempts"), 0o700); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(cfg.StateDir, "attempts", id+".json.tmp")
	if err := os.WriteFile(tmp, []byte(`{"format":1,"record":{"id":"half`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, cfg, nil, nil)
	if len(s.Problems()) != 0 || len(s.Leftovers()) != 1 || s.Leftovers()[0] != tmp || used(s)[2] != 0 {
		t.Fatalf("problems %v leftovers %v used %v", s.Problems(), s.Leftovers(), used(s))
	}
	mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if names := stateEntries(t, cfg.StateDir); len(names) != 1 || names[id+".json"] != "regular file" {
		t.Fatalf("state after reuse: %v", names)
	}
}

// writeRecord writes a record file as a previous process would have.
func writeRecord(t *testing.T, state string, r Record) string {
	t.Helper()
	data, err := json.MarshalIndent(recordFile{Format: recordFormat, Record: r}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(state, "attempts", r.ID+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// goodRecord is a valid preparing record for attempt, as Reserve writes it.
func goodRecord(cfg Config, attempt string, cid uint32) Record {
	now := time.Now().Unix()
	return Record{ID: IDFor(cfg.IDPrefix, attempt), Attempt: attempt, Owner: owner1, Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 10,
		DeadlineUnix: now + 3600, Network: "locked", CID: cid, State: StatePreparing, Created: now, Updated: now}
}

func fileDigest(t *testing.T, p string) string {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		return "absent"
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, _ := os.Readlink(p)
		return "symlink:" + target
	case !fi.Mode().IsRegular():
		return kindOf(fi.Mode())
	}
	os.Chmod(p, 0o600)
	defer os.Chmod(p, fi.Mode().Perm())
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// Anything in the records directory the loader cannot account for fences
// the service: its file is left byte-for-byte, no new reservation or boot
// is admitted, and what IS accounted for stays charged and cleanable.
func TestOpenFencesOnStateItCannotAccountFor(t *testing.T) {
	type fixture func(t *testing.T, cfg Config, dir string) (path string)
	record := func(mutate func(*Record)) fixture {
		return func(t *testing.T, cfg Config, dir string) string {
			r := goodRecord(cfg, "0vbad", 9)
			r.Created += 10 // after the good record: a duplicate is reported on the later one
			mutate(&r)
			return writeRecord(t, cfg.StateDir, r)
		}
	}
	raw := func(name string, data string) fixture {
		return func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	badID := IDFor("t", "0vbad")
	cases := []struct {
		name string
		kind string
		make fixture
	}{
		{"malformed json", "malformed", raw(badID+".json", `{"format":1,"record":`)},
		{"partial record", "malformed", func(t *testing.T, cfg Config, dir string) string {
			data, _ := json.Marshal(recordFile{Format: recordFormat, Record: goodRecord(cfg, "0vbad", 9)})
			return raw(badID+".json", string(data[:len(data)/2]))(t, cfg, dir)
		}},
		{"trailing data", "malformed", func(t *testing.T, cfg Config, dir string) string {
			data, _ := json.Marshal(recordFile{Format: recordFormat, Record: goodRecord(cfg, "0vbad", 9)})
			return raw(badID+".json", string(data)+"{}")(t, cfg, dir)
		}},
		{"unknown field", "malformed", func(t *testing.T, cfg Config, dir string) string {
			data, _ := json.Marshal(recordFile{Format: recordFormat, Record: goodRecord(cfg, "0vbad", 9)})
			return raw(badID+".json", strings.Replace(string(data), `"id"`, `"paused":true,"id"`, 1))(t, cfg, dir)
		}},
		{"pre-repair flat record", "unsupported", func(t *testing.T, cfg Config, dir string) string {
			data, _ := json.Marshal(goodRecord(cfg, "0vbad", 9))
			return raw(badID+".json", string(data))(t, cfg, dir)
		}},
		{"future format", "unsupported", func(t *testing.T, cfg Config, dir string) string {
			data, _ := json.Marshal(recordFile{Format: 2, Record: goodRecord(cfg, "0vbad", 9)})
			return raw(badID+".json", string(data))(t, cfg, dir)
		}},
		{"unreadable record", "unreadable", func(t *testing.T, cfg Config, dir string) string {
			p := record(func(*Record) {})(t, cfg, dir)
			if err := os.Chmod(p, 0o000); err != nil {
				t.Fatal(err)
			}
			if f, err := os.Open(p); err == nil {
				f.Close()
				t.Skip("mode 000 is readable here (root?)")
			}
			return p
		}},
		{"directory at the record path", "not-regular", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, badID+".json")
			return p + mustDo(t, os.Mkdir(p, 0o700))
		}},
		{"symlink at the record path", "not-regular", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, badID+".json")
			return p + mustDo(t, os.Symlink("elsewhere", p))
		}},
		{"fifo at the record path", "not-regular", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, badID+".json")
			return p + mustDo(t, syscall.Mkfifo(p, 0o600))
		}},
		{"directory at a publication path", "not-regular", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, badID+".json.tmp")
			return p + mustDo(t, os.Mkdir(p, 0o700))
		}},
		{"symlink at a publication path", "not-regular", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, badID+".json.tmp")
			return p + mustDo(t, os.Symlink("elsewhere", p))
		}},
		{"id differs from its file", "identity", func(t *testing.T, cfg Config, dir string) string {
			r := goodRecord(cfg, "0vother", 9)
			data, _ := json.Marshal(recordFile{Format: recordFormat, Record: r})
			return raw(badID+".json", string(data))(t, cfg, dir)
		}},
		{"attempt is not the id's", "identity", record(func(r *Record) { r.Attempt = "0vother" })},
		{"another launcher's record", "foreign", func(t *testing.T, cfg Config, dir string) string {
			r := goodRecord(Config{IDPrefix: "other"}, "0vbad", 9)
			return writeRecord(t, cfg.StateDir, r)
		}},
		{"another launcher's temp", "foreign", raw(IDFor("other", "0vbad")+".json.tmp", "x")},
		{"a stray file", "foreign", raw("notes.txt", "hello")},
		{"a stray directory", "foreign", func(t *testing.T, cfg Config, dir string) string {
			p := filepath.Join(dir, "sub")
			return p + mustDo(t, os.Mkdir(p, 0o700))
		}},
		{"unknown state", "invalid", record(func(r *Record) { r.State = "paused" })},
		{"a terminal state on disk", "invalid", record(func(r *Record) { r.State = StateDestroyed })},
		{"running without a pid", "invalid", record(func(r *Record) { r.State, r.HasVMM = StateRunning, true })},
		{"a pid without a vmm holding", "invalid", record(func(r *Record) { r.PID = 77 })},
		{"no charge", "invalid", record(func(r *Record) { r.CPUs = 0 })},
		{"an overflowing charge", "invalid", record(func(r *Record) { r.MemoryMiB = maxUnit + 1 })},
		{"no owner", "invalid", record(func(r *Record) { r.Owner.Daemon = "" })},
		{"a locked record with a network", "invalid", record(func(r *Record) { r.HasNetwork = true })},
		{"the vsock cid VMADDR_CID_ANY", "invalid", record(func(r *Record) { r.CID = math.MaxUint32 })},
		{"a host's cid", "invalid", record(func(r *Record) { r.CID = 2 })},
		{"a network index the host cannot address", "invalid", record(func(r *Record) {
			r.Network, r.Destinations, r.HasNetwork, r.NetIndex = "egress", []string{"tcp:192.0.2.1:8472"}, true, MaxNetIndex+1
		})},
		{"a duplicate cid", "duplicate", record(func(r *Record) { r.CID = 5 })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			// stage 01 (recovery ruling A): the good record's deadline passed
			// a second or two before the reaper's pass; an allowance that
			// covers that keeps the pass in time, as meant — a pass after the
			// allowance would make it a missed obligation, an incident
			cfg.CleanupBound = 5 * time.Second
			dir := filepath.Join(cfg.StateDir, "attempts")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			// one good record, past its deadline, that cleanup must still reach
			good := goodRecord(cfg, "0vgood", 5)
			good.DeadlineUnix = time.Now().Add(-time.Second).Unix()
			writeRecord(t, cfg.StateDir, good)
			bad := c.make(t, cfg, dir)
			before := fileDigest(t, bad)
			t.Cleanup(func() {
				if fi, err := os.Lstat(bad); err == nil && fi.Mode().IsRegular() {
					os.Chmod(bad, 0o600)
				}
			})

			s := open(t, cfg, h, nil)
			probs := s.Problems()
			if len(probs) != 1 || probs[0].Kind != c.kind || probs[0].Path != bad {
				t.Fatalf("problems %+v, want one %s at %s", probs, c.kind, bad)
			}
			t.Logf("diagnosis: %s", probs[0])
			if _, err := s.Reserve(owner1, lockedReq("0vnew", 1, 128)); !errors.Is(err, ErrUnsafeState) || !strings.Contains(err.Error(), bad) {
				t.Fatalf("a fenced service admitted, or without the diagnosis: %v", err)
			}
			charged := 1
			if c.kind == "duplicate" {
				charged = 2 // both records are kept: charged and cleanable
			}
			if used(s)[2] != charged {
				t.Fatalf("charge %v, want %d guests", used(s), charged)
			}
			// what is accounted for is still cleaned: the reaper releases the
			// good record past its deadline
			s.ReapOnce()
			if _, ok := held(s, good.ID); ok {
				t.Fatalf("a fenced service stopped cleaning up: %+v", s.All())
			}
			if after := fileDigest(t, bad); after != before {
				t.Fatalf("the evidence changed: %s -> %s", before, after)
			}
			if kind := stateEntries(t, cfg.StateDir)[filepath.Base(bad)]; kind == "" {
				t.Fatalf("the evidence is gone")
			}
			// still fenced after a restart: nothing resolved itself (a
			// duplicate does resolve when its partner is released)
			s2 := reopen(t, s, h)
			want := 1
			if c.kind == "duplicate" {
				want = 0
			}
			if len(s2.Problems()) != want {
				t.Fatalf("after a restart %d problems, want %d: %+v", len(s2.Problems()), want, s2.Problems())
			}
		})
	}
}

func mustDo(t *testing.T, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return ""
}

// A booked reservation cannot boot in a fenced service either: new host
// resources could collide with what the fence is about.
func TestFencedServiceRefusesToBoot(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "attempts", "stray"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := reopen(t, s, h)
	if _, err := s2.Create(owner1, r.ID); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("fenced create: %v", err)
	}
	if h.count("prepare") != 0 {
		t.Fatal("a fenced create reached the host")
	}
	if err := s2.Destroy(owner1, r.ID); err != nil {
		t.Fatalf("a fenced service must still release: %v", err)
	}
}

// State that cannot even be listed is not opened.
func TestOpenRefusesStateItCannotList(t *testing.T) {
	t.Run("records path is a file", func(t *testing.T) {
		state := t.TempDir()
		if err := os.WriteFile(filepath.Join(state, "attempts"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(testConfig(state), nil); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("open: %v", err)
		}
	})
	t.Run("records path is a symlink", func(t *testing.T) {
		state := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(state, "attempts")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(testConfig(state), nil); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("open: %v", err)
		}
	})
	t.Run("records directory unreadable", func(t *testing.T) {
		state := t.TempDir()
		dir := filepath.Join(state, "attempts")
		if err := os.Mkdir(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		if _, err := os.ReadDir(dir); err == nil {
			t.Skip("mode 000 is listable here (root?)")
		}
		_, err := NewService(testConfig(state), nil)
		if !errors.Is(err, syscall.EACCES) {
			t.Fatalf("open: %v", err)
		}
		// and the lock was given back
		os.Chmod(dir, 0o700)
		s, err := NewService(testConfig(state), nil)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	})
}

// ReadState is the list subcommand's view: no lock, no host, the same
// classification as an open.
func TestReadStateIsReadOnlyAndClassifies(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	stray := filepath.Join(cfg.StateDir, "attempts", "stray")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := ReadState(cfg.StateDir, cfg.IDPrefix) // while s holds the lock
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Records) != 1 || len(snap.Problems) != 1 || snap.Problems[0].Path != stray {
		t.Fatalf("snapshot %+v", snap)
	}
	empty, err := ReadState(t.TempDir(), "")
	if err != nil || len(empty.Records)+len(empty.Problems) != 0 {
		t.Fatalf("a fresh install: %+v %v", empty, err)
	}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(snap.Problems[0])
	if !strings.Contains(buf.String(), `"kind":"foreign"`) {
		t.Fatalf("problem json: %s", buf.String())
	}
}
