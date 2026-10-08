package daemon

// Stage 01 (runner/launcher/INTEGRATION.md §§3–6): whatever a failed
// Prepare or a failed teardown leaves charged is withheld by the daemon,
// durably, under its exact launcher identity, and charged once however
// often the daemon restarts. The launcher here is the real core and wire
// server on a private unix socket over a nonexecuting host (launchertest),
// the backend is the real Microvm, the ship an in-process handler.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

const vmDaemon = "0v1.daemon"

// vmShip is the ship of these tests: fakeShip's routes, the attempt status
// reconciliation reads, and an assignment queue for Run.
type vmShip struct {
	*fakeShip
	t      *testing.T
	mu     sync.Mutex
	status map[string]string // attempt -> status ("" = unknown to the ship: 404)
	queue  []queued
}

// queued is an assignment Run is handed on a poll once its gate (if any)
// says so.
type queued struct {
	a    *ship.Assignment
	gate func() bool
}

func (s *vmShip) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/apps/urgit/api/ci")
	if r.Header.Get("x-ci-bearer") == "0v1.bearer" && r.Method == http.MethodGet {
		switch {
		case strings.HasPrefix(path, "/attempt/"):
			s.mu.Lock()
			st := s.statusOf(strings.TrimPrefix(path, "/attempt/"))
			s.mu.Unlock()
			switch st {
			case "":
				w.WriteHeader(http.StatusNotFound)
			case "!unreadable": // the ship cannot say
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"attempt status unavailable"}`))
			case "!not-ours": // the ship calls it another daemon's
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"attempt authentication required"}`))
			default:
				_ = json.NewEncoder(w).Encode(map[string]string{"status": st})
			}
			return
		case path == "/daemon/"+vmDaemon+"/assignment":
			s.mu.Lock()
			var next *ship.Assignment
			if len(s.queue) > 0 && (s.queue[0].gate == nil || s.queue[0].gate()) {
				next, s.queue = s.queue[0].a, s.queue[1:]
			}
			s.mu.Unlock()
			if next == nil {
				time.Sleep(20 * time.Millisecond) // a short long-poll window
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"assignment": next})
			return
		}
	}
	s.fakeShip.handler(s.t).ServeHTTP(w, r)
}

// statusOf is the ship's status of attempt (under s.mu), found by its atom
// as the ship finds an attempt (`slaw %uv`), whatever spelling the test
// keyed it under: the daemon asks in the ship's own spelling (stage 01
// fixture adaptation, assignment-identity ruling 01).
func (s *vmShip) statusOf(attempt string) string {
	if st, ok := s.status[attempt]; ok {
		return st
	}
	for k, st := range s.status {
		if sameAtom(k, attempt) {
			return st
		}
	}
	return ""
}

// abandoned says whether the ship has heard at least n abandons.
func (s *vmShip) abandoned(n int) func() bool {
	return func() bool {
		s.fakeShip.mu.Lock()
		defer s.fakeShip.mu.Unlock()
		return len(s.abandons) >= n
	}
}

func (s *vmShip) abandon(i int) string {
	s.fakeShip.mu.Lock()
	defer s.fakeShip.mu.Unlock()
	if i >= len(s.abandons) {
		return ""
	}
	return s.abandons[i]
}

type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// vmFixture is one runner host: a launcher on a private socket, a verified
// guest image, the daemon's configuration and enrolled state file.
type vmFixture struct {
	host   *launchertest.Host
	srv    *launchertest.Server
	digest string
	cfg    *config.Config
	ship   *vmShip
	logs   *logBuffer
	priv   ed25519.PrivateKey
}

// newVMFixture is a runner enrolled with this version: its execution
// ledger's history began with its enrollment — complete — and its state file
// names the ledger (legacy-replay-upgrade ruling 01: a runner whose history
// is incomplete runs nothing until its transition; stage 01 fixture
// adaptation, runner/launcher/INTEGRATION.md §11.15). newVMHost is the same
// host with no ledger yet: a runner upgraded from a version without one.
func newVMFixture(t *testing.T, capacity int) *vmFixture {
	t.Helper()
	f := newVMHost(t, capacity)
	if _, err := state.LedgerFor(f.cfg.StateFile).Open(state.History{Since: time.Now().Unix(), Enrolled: true, StateFormat: state.CurrentFormat}, false); err != nil {
		t.Fatal(err)
	}
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	st.Ledger = true
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	return f
}

func newVMHost(t *testing.T, capacity int) *vmFixture {
	t.Helper()
	h := launchertest.NewHost()
	dir, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	work := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, ImagePath: dir, ActImage: "img", ActBinary: "/bin/sh",
		Capacity: capacity, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, WorkDir: filepath.Join(work, "work"),
		StateFile: filepath.Join(work, "state", "state.json"), ShipURL: shipURL}
	st := &state.State{DaemonID: vmDaemon, Bearer: "0v1.bearer", ShipURL: shipURL, CIPublicKey: hex.EncodeToString(pub)}
	if err := state.Save(cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	return &vmFixture{host: h, srv: srv, digest: digest, cfg: cfg, ship: &vmShip{fakeShip: &fakeShip{}, t: t, status: map[string]string{}}, logs: &logBuffer{}, priv: priv}
}

// start is one daemon process's start: New (the state file, the backend,
// the owner) with the in-process ship; nothing is ever checked out.
func (f *vmFixture) start(t *testing.T) *Daemon {
	t.Helper()
	d, err := New(context.Background(), f.cfg, log.New(f.logs, "", 0))
	if err != nil {
		t.Fatalf("daemon: %v", err)
	}
	d.client.HTTP = &http.Client{Transport: inproc{"ship.test": f.ship}}
	d.checkout = func(context.Context, *ship.Assignment, string) error { return errors.New("no checkout in this test") }
	return d
}

// closeDaemon ends a daemon's life the way its process exit does: its
// state lock, where it has one, is given back.
func closeDaemon(d *Daemon) {
	if c, ok := any(d).(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// quarantineAtLauncher leaves attempt quarantined in the launcher under
// this daemon's ownership: reserved, created, and a destroy whose jail
// removal failed.
func (f *vmFixture) quarantineAtLauncher(t *testing.T, attempt string) launcher.Record {
	t.Helper()
	cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	r, err := cl.Reserve(launcher.ReserveRequest{Attempt: attempt, Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Create(r.Ref()); err != nil {
		t.Fatal(err)
	}
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	if q, err := cl.DestroyOf(r.Ref()); err == nil || !q {
		t.Fatalf("fixture: the destroy should quarantine: %v %v", err, q)
	}
	f.host.SetFail("rmjail", nil)
	for _, rec := range f.srv.Held(t, vmDaemon) {
		if rec.ID == r.ID {
			return rec
		}
	}
	t.Fatalf("fixture: %s is not held", r.ID)
	return launcher.Record{}
}

// retainAsBefore records a slot quarantine the way the daemon did before
// this stage: the handle and the reason, nothing else.
func (f *vmFixture) retainAsBefore(t *testing.T, handle, reason string) {
	t.Helper()
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	st.Quarantined = append(st.Quarantined, state.Quarantine{Handle: handle, Reason: reason, At: time.Now().Unix()})
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
}

// retentions is the state file's quarantine entries, decoded generically:
// every field an entry carries, as the file holds it.
func retentions(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Quarantined []map[string]any `json:"quarantined"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Quarantined
}

// names says whether a state file entry carries rec's exact identity: its
// incarnation token too (INTEGRATION.md §11.1).
func names(entry map[string]any, rec launcher.Record) bool {
	return entry["backend"] == "microvm" && entry["handle"] == "ci-"+rec.Attempt && entry["attempt"] == rec.Attempt &&
		entry["vm"] == rec.ID && rec.Incarnation != "" && entry["incarnation"] == rec.Incarnation &&
		entry["cid"] == float64(rec.CID) && entry["created"] == float64(rec.Created)
}

func vmAssignment(attempt string) *ship.Assignment {
	m := jobManifest
	m.Sandbox = "vm"
	a := *jobAssignment
	a.ID, a.Attempt, a.Manifest = "0v1.asg."+attempt, attempt, &m
	return &a
}

// signed is vmAssignment as the ship hands it to Run: signed for this
// daemon with the key its state file pins.
func (f *vmFixture) signed(t *testing.T, attempt string) *ship.Assignment {
	t.Helper()
	a := vmAssignment(attempt)
	expiry := time.Now().Add(10 * time.Minute).Unix()
	msg := sig.ManifestMessage{Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: "0v7." + attempt, Manifest: *a.Manifest}
	b, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	a.Sig = &ship.Signature{Version: sig.ManifestVersion, Recipient: vmDaemon, Attempt: attempt, Operation: "assign", Expiry: expiry, Nonce: msg.Nonce, Sig: hex.EncodeToString(ed25519.Sign(f.priv, b))}
	return a
}

func waitUntil(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// §2: a create that fails, and a rollback the launcher cannot finish, leave
// a charged reservation. The daemon withholds its slot at once and in its
// state file, under the exact launcher identity (record, cid, created) —
// never an empty or a container-shaped handle — and tells the ship why.
func TestPrepareRetainedReservationWithholdsItsSlot(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	// the disk step fails after it may have acted, and the rollback cannot
	// remove the jail: the launcher quarantines the reservation
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	keep := d.handle(context.Background(), vmAssignment("0v1.att"))
	recs := f.srv.Held(t, vmDaemon)
	if len(recs) != 1 || recs[0].State != launcher.StateQuarantined || recs[0].Attempt != "0v1.att" {
		t.Fatalf("fixture: the launcher should hold the quarantined reservation: %+v", recs)
	}
	kept := retentions(t, f.cfg.StateFile)
	if keep || d.remainingCapacity() != 1 || len(kept) != 1 || !names(kept[0], recs[0]) {
		t.Fatalf("PREPARE RETENTION LOST: keep=%v, advertised capacity %d (want 1 of 2), state file entries %v; the launcher charges %s (cid %d, created %d)",
			keep, d.remainingCapacity(), kept, recs[0].ID, recs[0].CID, recs[0].Created)
	}
	if a := f.ship.abandon(0); !strings.Contains(a, "sandbox prepare") || !strings.Contains(a, recs[0].ID) {
		t.Fatalf("the ship was not told which reservation stays held: %q", a)
	}
}

// §2b: every start reconciles the launcher's records against the state
// file, and a quarantine the daemon already withholds — here recorded the
// way the daemon recorded it before this stage — is charged once, however
// often the daemon restarts; the entry gains the exact identity.
func TestRestartsChargeALauncherQuarantineOnce(t *testing.T) {
	f := newVMFixture(t, 3)
	rec := f.quarantineAtLauncher(t, "0v9.att")
	f.retainAsBefore(t, "ci-0v9.att", "teardown failed")
	f.ship.status["0v9.att"] = "failed"
	for i := 1; i <= 3; i++ {
		d := f.start(t)
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		capacity, kept := d.remainingCapacity(), retentions(t, f.cfg.StateFile)
		closeDaemon(d)
		if capacity != 2 || len(kept) != 1 {
			t.Fatalf("start %d: THE SAME LAUNCHER QUARANTINE CHARGED AGAIN: advertised capacity %d (want 2 of 3), %d state entries (want 1): %v", i, capacity, len(kept), kept)
		}
		if !names(kept[0], rec) {
			t.Fatalf("start %d: the entry does not name the launcher's record %s (cid %d, created %d): %v", i, rec.ID, rec.CID, rec.Created, kept[0])
		}
	}
	if recs := f.srv.Held(t, vmDaemon); len(recs) != 1 || recs[0].State != launcher.StateQuarantined {
		t.Fatalf("the launcher's quarantine is the operator's to clear: %+v", recs)
	}
}

// A retention the state file cannot hold yet is still withheld, and no new
// work starts on accounting a crash could lose (INTEGRATION.md §5): each
// assignment offered meanwhile is abandoned with the reason, before any
// reservation. Once a save succeeds the file names the retention and work
// resumes.
func TestUnsavedRetentionStopsNewWorkUntilDurable(t *testing.T) {
	f := newVMFixture(t, 2)
	d := f.start(t)
	defer closeDaemon(d)
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	dir := filepath.Dir(f.cfg.StateFile)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	// ruling-dependent fixture (settled-admission ruling 01; INTEGRATION.md
	// §11.10): the admission is saved before its reserve, and a daemon that
	// cannot save it sends none, so the state file turns unwritable once the
	// first attempt's reservation exists — at its disk step — where it did
	// before the run
	release := f.host.Hold(t, "disk")
	f.ship.mu.Lock()
	// the second only once the first was answered
	f.ship.queue = []queued{{a: f.signed(t, "0v1.att")}, {a: f.signed(t, "0v2.att"), gate: f.ship.abandoned(1)}}
	f.ship.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan int, 1)
	go func() { ran <- d.Run(ctx) }()
	select {
	case <-f.host.Reached():
	case <-time.After(20 * time.Second):
		t.Fatal("the first attempt did not reach its disk step")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	release()
	stop := func() {
		cancel()
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Fatal("Run did not return")
		}
	}
	// a wait that times out says what the daemon and the ship saw
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				stop()
				f.ship.fakeShip.mu.Lock()
				abandons := append([]string(nil), f.ship.abandons...)
				f.ship.fakeShip.mu.Unlock()
				t.Fatalf("timed out waiting for %s; the ship's abandons %q; launcher ops %q; log:\n%s", what, abandons, f.host.Ops(), f.logs.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("the second assignment's answer", f.ship.abandoned(2))
	id2 := f.at("0v2.att")
	if n, second := f.host.Count("disk", id2), f.ship.abandon(1); n != 0 || !strings.Contains(second, "not durable") {
		stop()
		t.Fatalf("NEW WORK STARTED ON UNSAVED ACCOUNTING: the second attempt reached the launcher %d time(s); its abandon: %q; log:\n%s", n, second, f.logs.String())
	}
	// healed: the daemon's retry makes the retention durable, and work resumes
	f.host.SetFail("disk", nil)
	f.host.SetFail("rmjail", nil)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	wait("the retention saved", func() bool { return len(retentions(t, f.cfg.StateFile)) == 1 })
	f.ship.mu.Lock()
	f.ship.queue = append(f.ship.queue, queued{a: f.signed(t, "0v3.att")})
	f.ship.mu.Unlock()
	id3 := f.at("0v3.att")
	wait("the third attempt at the launcher", func() bool { return f.host.Count("disk", id3) > 0 })
	stop()
	recs := f.srv.Held(t, vmDaemon)
	kept := retentions(t, f.cfg.StateFile)
	if len(recs) != 1 || len(kept) != 1 || !names(kept[0], recs[0]) {
		t.Fatalf("after the heal: the launcher holds %+v, the state file %v", recs, kept)
	}
}
