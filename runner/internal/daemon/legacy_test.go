package daemon

// Legacy-recovery UI ruling 01 (QUESTIONS-SOURCE-01 §11; runner/launcher/
// INTEGRATION.md §11.12): a retention a runner before settled admission
// recorded without its reserve request is released only from Urgit — a
// command the ship signs for this daemon, carried on the daemon's own poll —
// and only on its evidence, taken again when the command runs: its legacy
// mark, the launcher's protocol, an authoritative inventory without any
// record of its attempt. The daemon, the owner of its state file, carries it
// out and answers; the slot returns only once the release is durable.
//
// Through the real Microvm backend and the launcher's real core and wire
// over private files; the ship is an in-process handler modelling the
// recovery contract (specs/ci-execution-contract.md §8b), its commands
// signed here with the key the state file pins.

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

const legacyAttempt = "0v4.legacy"

// legacyShip is the ship of these tests: vmShip's routes, and the recovery
// contract — the retention reports the daemon posts, the commands the
// operator confirmed (handed out on a poll that says it carries them out),
// and the daemon's answers.
type legacyShip struct {
	*vmShip
	mu       sync.Mutex
	reports  []map[string]any
	pending  []map[string]any // commands not handed out yet, in order
	again    map[string]any   // a command handed out again on every capable poll (its answer taken for lost)
	answers  []map[string]any
	loseNext int // the next answers are recorded, and their replies lost on the way back
	capable  int // polls that said they carry commands out
}

func (s *legacyShip) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/apps/urgit/api/ci")
	if r.Header.Get("x-ci-bearer") == "0v1.bearer" {
		switch {
		case r.Method == http.MethodPost && path == "/daemon/"+vmDaemon+"/retentions":
			var report map[string]any
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &report); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			s.reports = append(s.reports, report)
			s.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		case r.Method == http.MethodPost && path == "/daemon/"+vmDaemon+"/recovery":
			var answer map[string]any
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &answer); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			s.answers = append(s.answers, answer)
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"status": answer["status"]})
			return
		case r.Method == http.MethodGet && path == "/daemon/"+vmDaemon+"/assignment" && r.Header.Get("x-ci-recovery") != "":
			s.mu.Lock()
			s.capable++
			var cmd map[string]any
			switch {
			case len(s.pending) > 0:
				cmd, s.pending = s.pending[0], s.pending[1:]
			case s.again != nil:
				cmd = s.again
			}
			s.mu.Unlock()
			if cmd != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"recovery": cmd})
				return
			}
		}
	}
	s.vmShip.ServeHTTP(w, r)
}

// lossy serves every request in process, and loses the reply to the ship's
// answers route while the ship says so: the ship has recorded the answer,
// the daemon's request fails.
type lossy struct {
	inproc
	ship *legacyShip
}

func (l lossy) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := l.inproc.RoundTrip(r)
	if err != nil || !strings.HasSuffix(r.URL.Path, "/daemon/"+vmDaemon+"/recovery") {
		return resp, err
	}
	l.ship.mu.Lock()
	defer l.ship.mu.Unlock()
	if l.ship.loseNext > 0 {
		l.ship.loseNext--
		return nil, errors.New("connection reset by peer (the answer's reply was lost)")
	}
	return resp, err
}

func (s *legacyShip) enqueue(cmd map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, cmd)
}

// answersFor is every answer to command id: by its atom, as the ship's
// @uv parse identifies it — the daemon answers under the atom's one
// spelling (INTEGRATION.md §11.12, "One command, one identity"), and these
// fixtures name commands under other spellings too.
func (s *legacyShip) answersFor(id string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, a := range s.answers {
		if a["command"] == id || sameAtom(a["command"], id) {
			out = append(out, a)
		}
	}
	return out
}

// entry is the latest report's retention of handle, and that report.
func (s *legacyShip) entry(handle string) (map[string]any, map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reports) == 0 {
		return nil, nil
	}
	report := s.reports[len(s.reports)-1]
	list, _ := report["retentions"].([]any)
	for _, item := range list {
		if e, ok := item.(map[string]any); ok && e["handle"] == handle {
			return e, report
		}
	}
	return nil, report
}

func (s *legacyShip) reported() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reports)
}

// legacyFixture is a runner host whose state file a runner before settled
// admission wrote: no format stamp, and its retentions as that runner
// recorded them.
type legacyFixture struct {
	*vmFixture
	ship *legacyShip
}

func newLegacyFixture(t *testing.T, capacity int, entries ...map[string]any) *legacyFixture {
	t.Helper()
	return newStateFixture(t, capacity, 0, entries...)
}

// newStateFixture is a runner host whose state file has format (0: none, as
// a runner before this version wrote it) and entries.
func newStateFixture(t *testing.T, capacity, format int, entries ...map[string]any) *legacyFixture {
	t.Helper()
	f := newVMHost(t, capacity) // no ledger: an upgraded runner's (INTEGRATION.md §11.15)
	pub := f.priv.Public().(ed25519.PublicKey)
	file := map[string]any{"daemon_id": vmDaemon, "bearer": "0v1.bearer", "ship_url": shipURL, "ci_public_key": hex.EncodeToString(pub), "quarantined": entries}
	if format != 0 {
		file["format"] = format
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cfg.StateFile, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return &legacyFixture{vmFixture: f, ship: &legacyShip{vmShip: f.ship}}
}

// preRuling is a microvm retention as a runner before settled admission
// recorded a reserve whose answer was lost while the launcher could not be
// asked: its attempt and job, no launcher identity, no request.
func preRuling(attempt string) map[string]any {
	return map[string]any{"handle": "ci-" + attempt, "reason": "sandbox prepare failed and could not be rolled back: reserve: launcher: connection reset by peer; and the reservation could not be released: the launcher's identity of this reservation is unknown",
		"at": time.Now().Add(-48 * time.Hour).Unix(), "backend": "microvm", "attempt": attempt, "label": "owner/repo · ci.yml · build"}
}

// start is the daemon of f with the recovery ship in process.
func (f *legacyFixture) start(t *testing.T) *Daemon {
	t.Helper()
	d := f.vmFixture.start(t)
	d.client.HTTP = &http.Client{Transport: lossy{inproc: inproc{"ship.test": f.ship}, ship: f.ship}}
	d.reconcileEvery = 50 * time.Millisecond
	return d
}

// running runs d until the returned stop, which says what Run returned.
func running(t *testing.T, d *Daemon) (stop func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan int, 1)
	go func() { ran <- d.Run(ctx) }()
	var once sync.Once
	var code int
	stop = func() int {
		once.Do(func() {
			cancel()
			select {
			case code = <-ran:
			case <-time.After(30 * time.Second):
				t.Fatal("Run did not return")
			}
		})
		return code
	}
	t.Cleanup(func() { stop() })
	return stop
}

// cmdSpec is a recovery command as the ship would sign it for this daemon;
// each zero field takes its default, and the rest make a bad one.
type cmdSpec struct {
	id, recipient, operation, selection, evidence, nonce string
	revision                                             uint64
	expiry                                               int64
	version                                              int
	key                                                  ed25519.PrivateKey
	unsigned                                             bool
	tamper                                               func(map[string]any)
}

// recoveryMessage is the noun the ship's CI key signs for a recovery
// command (desk/lib/ci-recovery.hoon): ['recovery' 1 recipient command
// operation expiry nonce selection revision evidence].
func recoveryMessage(t *testing.T, recipient, command, operation string, expiry int64, nonce, selection string, revision uint64, evidence string) []byte {
	t.Helper()
	uv := func(s string) *sig.Noun {
		v, err := sig.ParseUV(s)
		if err != nil {
			t.Fatal(err)
		}
		return sig.Atom(v)
	}
	n := sig.Tuple(sig.Cord("recovery"), sig.Atom(big.NewInt(1)), uv(recipient), uv(command), sig.Cord(operation), sig.Atom(big.NewInt(expiry)),
		uv(nonce), sig.Cord(selection), sig.Atom(new(big.Int).SetUint64(revision)), sig.Cord(evidence))
	return sig.LittleEndian(sig.Jam(n))
}

func (f *legacyFixture) command(t *testing.T, sp cmdSpec) map[string]any {
	t.Helper()
	if sp.id == "" {
		sp.id = "0v5.cmd"
	}
	if sp.recipient == "" {
		sp.recipient = vmDaemon
	}
	if sp.operation == "" {
		sp.operation = "release-legacy"
	}
	if sp.nonce == "" {
		sp.nonce = "0v6.nonce"
	}
	if sp.expiry == 0 {
		sp.expiry = time.Now().Add(10 * time.Minute).Unix()
	}
	if sp.version == 0 {
		sp.version = 1
	}
	if sp.key == nil {
		sp.key = f.priv
	}
	cmd := map[string]any{"version": sp.version, "id": sp.id, "recipient": sp.recipient, "operation": sp.operation, "selection": sp.selection,
		"revision": sp.revision, "evidence": sp.evidence, "label": "owner/repo · ci.yml · build", "expiry": sp.expiry, "nonce": sp.nonce, "sig": ""}
	if !sp.unsigned {
		msg := recoveryMessage(t, sp.recipient, sp.id, sp.operation, sp.expiry, sp.nonce, sp.selection, sp.revision, sp.evidence)
		cmd["sig"] = hex.EncodeToString(ed25519.Sign(sp.key, msg))
	}
	if sp.tamper != nil {
		sp.tamper(cmd)
	}
	return cmd
}

// inspected waits for a report that lists handle, and returns its entry.
func (f *legacyFixture) inspected(t *testing.T, handle string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if e, _ := f.ship.entry(handle); e != nil {
			return e
		}
		if time.Now().After(deadline) {
			t.Fatalf("NO RETENTION REPORT REACHED THE SHIP: %d report(s), none listing %s; log:\n%s", f.ship.reported(), handle, f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// answered waits for the daemon's answers to command id until one is
// final (completed or refused), and returns them all.
func (f *legacyFixture) answered(t *testing.T, id string) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		got := f.ship.answersFor(id)
		for _, a := range got {
			if a["status"] == "completed" || a["status"] == "refused" {
				return got
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("THE DAEMON NEVER ANSWERED THE RECOVERY COMMAND %s: answers %v; log:\n%s", id, got, f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// firstAnswer waits for the daemon's first answer to command id, final or
// not, and returns it.
func (f *legacyFixture) firstAnswer(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if got := f.ship.answersFor(id); len(got) > 0 {
			return got[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("THE DAEMON NEVER ANSWERED THE RECOVERY COMMAND %s; log:\n%s", id, f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func final(answers []map[string]any) map[string]any {
	for i := len(answers) - 1; i >= 0; i-- {
		if s := answers[i]["status"]; s == "completed" || s == "refused" {
			return answers[i]
		}
	}
	return nil
}

// released is the state file's released entries, decoded generically.
func released(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Released []map[string]any `json:"released"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Released
}

func handlesOf(entries []map[string]any) []string {
	var out []string
	for _, e := range entries {
		out = append(out, fmt.Sprint(e["handle"]))
	}
	return out
}

func conditionsOf(e map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := e["conditions"].([]any)
	for _, item := range list {
		if c, ok := item.(map[string]any); ok {
			met, _ := c["met"].(bool)
			out[fmt.Sprint(c["name"])] = met
		}
	}
	return out
}

func revisionOf(e map[string]any) uint64 {
	n, _ := e["revision"].(float64)
	return uint64(n)
}

// The whole path: the pre-ruling entry is marked legacy at the first load
// and reported with its evidence, every condition met; the command bound to
// it is carried out by the daemon — the entry released in the state file,
// stamped with the command and the evidence, before the slot returns — and
// answered completed with the capacity now. The launcher is asked, never
// acted on. A retention of another kind in the same file is reported with
// how its slot returns, and offered no release.
func TestALegacyRetentionIsReleasedFromUrgitOnItsEvidence(t *testing.T) {
	older := map[string]any{"handle": "ci-0v3.older", "reason": "teardown failed", "at": time.Now().Add(-72 * time.Hour).Unix()}
	f := newLegacyFixture(t, 3, preRuling(legacyAttempt), older)
	d := f.start(t)
	defer closeDaemon(d)
	if got := d.remainingCapacity(); got != 1 {
		t.Fatalf("fixture: two retentions withhold two slots of three: advertised %d", got)
	}
	// the earlier runner's file is saved at the start in this version's
	// format, the mark with it: no entry of it can be marked again
	data, err := os.ReadFile(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Format      int              `json:"format"`
		Quarantined []map[string]any `json:"quarantined"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file.Format != 1 || len(file.Quarantined) != 2 || file.Quarantined[0]["legacy"] == nil {
		t.Fatalf("THE DAEMON DID NOT SAVE AN EARLIER RUNNER'S FILE IN ITS FORMAT AT START: format %d, %v", file.Format, file.Quarantined)
	}
	stop := running(t, d)
	e := f.inspected(t, "ci-"+legacyAttempt)
	conds := conditionsOf(e)
	if e["kind"] != "legacy" || e["eligible"] != true || len(conds) == 0 {
		t.Fatalf("THE LEGACY RETENTION WAS NOT REPORTED RELEASABLE: %v", e)
	}
	for name, met := range conds {
		if !met {
			t.Fatalf("THE LEGACY RETENTION WAS REPORTED WITH AN UNMET CONDITION %q: %v", name, e)
		}
	}
	evidence, _ := e["evidence"].(string)
	if len(evidence) != 64 {
		t.Fatalf("the report carries no evidence digest: %v", e)
	}
	if o, _ := f.ship.entry("ci-0v3.older"); o == nil || o["kind"] != "unknown-backend" || o["eligible"] == true || o["release"] != "none" {
		t.Fatalf("AN ENTRY WITHOUT ITS BACKEND WAS REPORTED RELEASABLE: %v", o)
	}
	sel, _ := e["selection"].(string)
	f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
	answers := f.answered(t, "0v5.cmd")
	a := final(answers)
	if a["status"] != "completed" {
		stop()
		t.Fatalf("THE LEGACY RETENTION WAS NOT RELEASED FROM URGIT'S COMMAND: %v; log:\n%s", answers, f.logs.String())
	}
	stop()
	kept, gone := retentions(t, f.cfg.StateFile), released(t, f.cfg.StateFile)
	if len(kept) != 1 || kept[0]["handle"] != "ci-0v3.older" || len(gone) != 1 || gone[0]["handle"] != "ci-"+legacyAttempt ||
		!sameAtom(gone[0]["command"], "0v5.cmd") || gone[0]["evidence"] != evidence || gone[0]["released_at"] == nil {
		t.Fatalf("THE RELEASE IS NOT DURABLE IN THE STATE FILE: kept %v, released %v", kept, gone)
	}
	if got := d.remainingCapacity(); got != 2 {
		t.Fatalf("THE SLOT DID NOT RETURN: advertised %d (want 2 of 3)", got)
	}
	if ops := f.host.Ops(); len(ops) != 0 {
		t.Fatalf("THE RELEASE TOUCHED THE LAUNCHER'S HOST: %v", ops)
	}
}

// Every refusal keeps the charge, says why, and releases nothing: the
// launcher holds a record of the attempt (reported, and refused when a
// command comes anyway), cannot be asked, speaks another protocol, or its
// inventory is not authoritative; the entry is not legacy — this version
// wrote its shape, or it names no backend, or it is an admission; its
// attempt runs here.
func TestALegacyReleaseIsRefusedWithoutItsProof(t *testing.T) {
	type setup func(t *testing.T, f *legacyFixture, d *Daemon) (restore func())
	none := func(*testing.T, *legacyFixture, *Daemon) func() { return func() {} }
	cases := []struct {
		name    string
		entry   func() map[string]any
		current bool // the state file as this version writes it (format stamped)
		before  setup
		cond    string // the condition the report shows unmet ("" none: refused only at the command)
		kind    string
	}{
		{name: "the launcher holds a record of its attempt", entry: func() map[string]any { return preRuling(legacyAttempt) },
			before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
				f.quarantineAtLauncher(t, legacyAttempt)
				return func() {}
			}, cond: "attempt", kind: "legacy"},
		{name: "the launcher cannot be asked", entry: func() map[string]any { return preRuling(legacyAttempt) },
			before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
				away := f.srv.Socket + ".away"
				if err := os.Rename(f.srv.Socket, away); err != nil {
					t.Fatal(err)
				}
				return func() { _ = os.Rename(away, f.srv.Socket) }
			}, cond: "inventory", kind: "legacy"},
		{name: "the launcher speaks another protocol", entry: func() map[string]any { return preRuling(legacyAttempt) },
			before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
				away := f.srv.Socket + ".away"
				if err := os.Rename(f.srv.Socket, away); err != nil {
					t.Fatal(err)
				}
				stop := scriptedLauncher(t, f.srv.Socket, 3)
				return func() { stop(); _ = os.Remove(f.srv.Socket); _ = os.Rename(away, f.srv.Socket) }
			}, cond: "inventory", kind: "legacy"},
		{name: "the launcher's inventory is not authoritative", entry: func() map[string]any { return preRuling(legacyAttempt) },
			before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
				p := filepath.Join(f.srv.StateDir(), "attempts", launcher.IDFor("t", "0v9.other")+".json")
				if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
				f.srv.Restart(t)
				if len(f.srv.Service.Problems()) == 0 {
					t.Fatal("fixture: the spoiled record is unaccounted for")
				}
				return func() {}
			}, cond: "inventory", kind: "legacy"},
		{name: "this version wrote its shape", entry: func() map[string]any { return preRuling(legacyAttempt) }, current: true, before: none, kind: "unmarked"},
		{name: "it names no backend", entry: func() map[string]any {
			return map[string]any{"handle": "ci-" + legacyAttempt, "reason": "teardown failed", "at": time.Now().Add(-72 * time.Hour).Unix()}
		}, before: none, kind: "unknown-backend"},
		{name: "it is an admission", entry: func() map[string]any {
			e := preRuling(legacyAttempt)
			e["request"] = strings.Repeat("c", 32)
			return e
		}, before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
			// its settlement is refused, so it stays an admission
			away := f.srv.Socket + ".away"
			if err := os.Rename(f.srv.Socket, away); err != nil {
				t.Fatal(err)
			}
			return func() { _ = os.Rename(away, f.srv.Socket) }
		}, kind: "admission"},
		{name: "its attempt runs here", entry: func() map[string]any { return preRuling(legacyAttempt) },
			before: func(t *testing.T, f *legacyFixture, d *Daemon) func() {
				d.claim(legacyAttempt)
				return func() { d.release(legacyAttempt) }
			}, cond: "idle", kind: "legacy"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			format := 0
			if c.current {
				format = 1 // the same entry in a file this version wrote
			}
			f := newStateFixture(t, 2, format, c.entry())
			d := f.start(t)
			defer closeDaemon(d)
			d.reconcileEvery = time.Hour // the entry as loaded: no reconcile completes it meanwhile
			restore := c.before(t, f, d)
			defer restore()
			before := d.remainingCapacity()
			stop := running(t, d)
			e := f.inspected(t, "ci-"+legacyAttempt)
			if e["kind"] != c.kind {
				stop()
				t.Fatalf("A RETENTION WAS REPORTED AS ANOTHER KIND: %v, want %s: %v", e["kind"], c.kind, e)
			}
			if e["eligible"] == true {
				stop()
				t.Fatalf("A RETENTION WITHOUT ITS PROOF WAS REPORTED RELEASABLE: %v", e)
			}
			if c.cond != "" && conditionsOf(e)[c.cond] {
				stop()
				t.Fatalf("the report shows %q met: %v", c.cond, e)
			}
			// a command comes anyway (a ship that did not check, or a forged
			// report): bound to the entry as reported
			evidence, _ := e["evidence"].(string)
			if evidence == "" {
				evidence = strings.Repeat("0", 64)
			}
			sel, _ := e["selection"].(string)
			f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
			a := final(f.answered(t, "0v5.cmd"))
			stop()
			if a["status"] != "refused" || a["detail"] == "" {
				t.Fatalf("A LEGACY RELEASE WITHOUT ITS PROOF WAS NOT REFUSED: %v", a)
			}
			if len(released(t, f.cfg.StateFile)) != 0 || len(retentions(t, f.cfg.StateFile)) != 1 || d.remainingCapacity() != before {
				t.Fatalf("A RETENTION WITHOUT ITS PROOF WAS RELEASED: kept %v, released %v, advertised %d (was %d)",
					handlesOf(retentions(t, f.cfg.StateFile)), handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity(), before)
			}
		})
	}
}

// scriptedLauncher answers hello at path with protocol, and refuses every
// other request; stop closes it.
func scriptedLauncher(t *testing.T, path string, protocol int) (stop func()) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req launcher.Request
					if json.Unmarshal(line, &req) != nil {
						return
					}
					reply := launcher.Reply{Error: "scripted launcher"}
					if req.Op == "hello" {
						reply = launcher.Reply{OK: true, Launcher: "scripted", Protocol: protocol}
					}
					data, _ := json.Marshal(reply)
					if _, err := c.Write(append(data, '\n')); err != nil {
						return
					}
				}
			}(c)
		}
	}()
	return func() { l.Close() }
}

// A command is carried out only when it is what the ship signed for this
// daemon, for this operation, in time, for exactly the entry, revision and
// evidence inspected. One the daemon can authenticate is otherwise refused,
// the refusal recorded before it is answered; a message it cannot
// authenticate decides nothing — answered uncertain, recorded nowhere
// (INTEGRATION.md §11.12, "Every final answer is recorded first"). Neither
// releases anything.
func TestARecoveryCommandIsAuthenticatedAndBound(t *testing.T) {
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		authentic bool // a command this daemon can authenticate
		spec      func(sel string, rev uint64, evidence string) cmdSpec
	}{
		{"unsigned", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, unsigned: true}
		}},
		{"signed by another key", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, key: otherKey}
		}},
		{"signed for another daemon", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, recipient: "0v2.other"}
		}},
		{"another operation", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, operation: "release"}
		}},
		{"another message version", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, version: 2}
		}},
		{"expired", true, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, expiry: time.Now().Add(-time.Minute).Unix()}
		}},
		{"a field changed after signing", false, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: ev, tamper: func(m map[string]any) { m["revision"] = rev + 1 }}
		}},
		{"a stale revision", true, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel[:strings.LastIndex(sel, "/")+1] + fmt.Sprint(rev-1), revision: rev - 1, evidence: ev}
		}},
		{"a replaced entry", true, func(sel string, rev uint64, ev string) cmdSpec {
			parts := strings.Split(sel, "/")
			parts[7] = fmt.Sprint(time.Now().Unix())
			return cmdSpec{selection: strings.Join(parts, "/"), revision: rev, evidence: ev}
		}},
		{"evidence it was not inspected on", true, func(sel string, rev uint64, ev string) cmdSpec {
			return cmdSpec{selection: sel, revision: rev, evidence: strings.Repeat("e", 64)}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
			d := f.start(t)
			defer closeDaemon(d)
			stop := running(t, d)
			e := f.inspected(t, "ci-"+legacyAttempt)
			if e["eligible"] != true {
				stop()
				t.Fatalf("fixture: the entry is releasable: %v", e)
			}
			sel, _ := e["selection"].(string)
			evidence, _ := e["evidence"].(string)
			f.ship.enqueue(f.command(t, c.spec(sel, revisionOf(e), evidence)))
			if !c.authentic {
				a := f.firstAnswer(t, "0v5.cmd")
				stop()
				if a["status"] != "uncertain" || a["detail"] == "" {
					t.Fatalf("A MESSAGE THE DAEMON COULD NOT AUTHENTICATE WAS ANSWERED AS A FINAL OUTCOME (%s): %v", c.name, a)
				}
				if got := refusalsOf(t, f.cfg.StateFile); len(got) != 0 {
					t.Fatalf("UNAUTHENTICATED INPUT WAS RECORDED OR ACTED ON (%s): refused %v", c.name, got)
				}
			} else {
				a := final(f.answered(t, "0v5.cmd"))
				stop()
				if a["status"] != "refused" || a["detail"] == "" {
					t.Fatalf("A COMMAND THAT IS NOT WHAT THE SHIP SIGNED FOR THIS ENTRY WAS NOT REFUSED (%s): %v", c.name, a)
				}
				if r := recordedRefusal(t, f.cfg.StateFile, "0v5.cmd"); r == nil || r["reason"] != a["detail"] {
					t.Fatalf("AN AUTHENTICATED REFUSAL WAS NOT RECORDED IN THE STATE FILE BEFORE IT WAS ANSWERED (%s): answered %v, recorded %v", c.name, a, refusalsOf(t, f.cfg.StateFile))
				}
			}
			if len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
				t.Fatalf("A COMMAND THAT IS NOT WHAT THE SHIP SIGNED FOR THIS ENTRY RELEASED IT (%s): released %v, advertised %d",
					c.name, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
			}
		})
	}
}

// Once, across lost replies, repeats and restarts: the answer's reply lost
// (the daemon answers again until the ship takes it); the command handed
// out again after it was carried out (answered completed from the state
// file); a restart before the ship took the answer (the new daemon answers
// from the file); a second command for the released entry (refused: no
// such retention now). The entry is released once, the slot returned once.
func TestARecoveryIsAppliedOnceAcrossLostRepliesAndRestarts(t *testing.T) {
	f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
	d := f.start(t)
	stop := running(t, d)
	e := f.inspected(t, "ci-"+legacyAttempt)
	sel, _ := e["selection"].(string)
	evidence, _ := e["evidence"].(string)
	cmd := f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence})
	f.ship.mu.Lock()
	f.ship.loseNext = 2 // the first two answers reach the ship; their replies are lost
	f.ship.again = cmd  // and the command is handed out on every poll
	f.ship.mu.Unlock()
	deadline := time.Now().Add(15 * time.Second)
	for {
		completed := 0
		for _, a := range f.ship.answersFor("0v5.cmd") {
			if a["status"] == "completed" {
				completed++
			}
		}
		f.ship.mu.Lock()
		lost := f.ship.loseNext
		f.ship.mu.Unlock()
		if completed >= 3 && lost == 0 {
			break
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatalf("THE ANSWER WAS NOT GIVEN AGAIN AFTER ITS REPLY WAS LOST: answers %v; log:\n%s", f.ship.answersFor("0v5.cmd"), f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, a := range f.ship.answersFor("0v5.cmd") {
		if a["status"] != "completed" {
			stop()
			t.Fatalf("A REPEATED COMMAND WAS ANSWERED OTHERWISE THAN ITS DURABLE RELEASE: %v", a)
		}
	}
	stop()
	closeDaemon(d)
	if gone := released(t, f.cfg.StateFile); len(gone) != 1 {
		t.Fatalf("THE RELEASE WAS APPLIED %d TIMES: %v", len(gone), gone)
	}
	// a restart: the ship hands the command out again; the new daemon
	// answers it from the state file, and releases nothing else
	f.ship.mu.Lock()
	f.ship.answers = nil
	f.ship.mu.Unlock()
	d2 := f.start(t)
	defer closeDaemon(d2)
	stop2 := running(t, d2)
	if a := final(f.answered(t, "0v5.cmd")); a["status"] != "completed" {
		stop2()
		t.Fatalf("AFTER A RESTART THE CARRIED-OUT COMMAND WAS NOT ANSWERED FROM THE STATE FILE: %v", a)
	}
	// a second command for the released entry: nothing left to release
	f.ship.mu.Lock()
	f.ship.again = nil
	f.ship.mu.Unlock()
	f.ship.enqueue(f.command(t, cmdSpec{id: "0v7.cmd", selection: sel, revision: revisionOf(e), evidence: evidence}))
	a := final(f.answered(t, "0v7.cmd"))
	stop2()
	if a["status"] != "refused" {
		t.Fatalf("A SECOND COMMAND FOR THE RELEASED ENTRY WAS NOT REFUSED: %v", a)
	}
	if gone := released(t, f.cfg.StateFile); len(gone) != 1 || d2.remainingCapacity() != 2 {
		t.Fatalf("THE RELEASE WAS APPLIED AGAIN AFTER A RESTART: released %v, advertised %d", handlesOf(gone), d2.remainingCapacity())
	}
}

var errInjectedSave = errors.New("injected save failure")

// A release is reported, and its slot returned, only once durable: a save
// that fails before its rename refuses the command and changes nothing; a
// save whose outcome is uncertain answers uncertain, returns no slot and
// starts no new work, and once a save of the daemon's view succeeds, the
// command is answered refused — not applied. A restart while the file holds
// the uncertain release answers the command from the file.
func TestAnUncertainReleaseReturnsNoSlot(t *testing.T) {
	isRelease := func(st *state.State) bool {
		for _, q := range st.Released {
			if q.Handle == "ci-"+legacyAttempt {
				return true
			}
		}
		return false
	}
	t.Run("a save that fails before its rename", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		defer closeDaemon(d)
		d.saveState = func(path string, st *state.State) error {
			if isRelease(st) {
				return &state.SaveError{Step: "write", Path: path, Err: errInjectedSave}
			}
			return state.Save(path, st)
		}
		stop := running(t, d)
		e := f.inspected(t, "ci-"+legacyAttempt)
		sel, _ := e["selection"].(string)
		evidence, _ := e["evidence"].(string)
		f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
		a := final(f.answered(t, "0v5.cmd"))
		stop()
		if a["status"] != "refused" || len(released(t, f.cfg.StateFile)) != 0 || d.remainingCapacity() != 1 {
			t.Fatalf("A RELEASE THAT WAS NOT SAVED WAS REPORTED OR RETURNED ITS SLOT: %v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
		}
	})
	t.Run("a save whose outcome is uncertain", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		defer closeDaemon(d)
		var mu sync.Mutex
		failing := true // until the test lets saves succeed again
		d.saveState = func(path string, st *state.State) error {
			mu.Lock()
			defer mu.Unlock()
			if isRelease(st) {
				if err := state.Save(path, st); err != nil {
					return err
				}
				return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
			}
			if failing {
				return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
			}
			return state.Save(path, st)
		}
		stop := running(t, d)
		e := f.inspected(t, "ci-"+legacyAttempt)
		sel, _ := e["selection"].(string)
		evidence, _ := e["evidence"].(string)
		f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
		deadline := time.Now().Add(10 * time.Second)
		for len(f.ship.answersFor("0v5.cmd")) == 0 {
			if time.Now().After(deadline) {
				stop()
				t.Fatalf("THE DAEMON NEVER ANSWERED THE RECOVERY COMMAND 0v5.cmd; log:\n%s", f.logs.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		first := f.ship.answersFor("0v5.cmd")[0]
		if first["status"] != "uncertain" || d.remainingCapacity() != 1 || d.retrySave() == nil {
			stop()
			t.Fatalf("AN UNCERTAIN RELEASE RETURNED THE SLOT OR WAS NOT SAID: %v, advertised %d, state durable %v", first, d.remainingCapacity(), d.retrySave() == nil)
		}
		// no new work meanwhile: the file may hold either version
		f.ship.vmShip.mu.Lock()
		f.ship.vmShip.queue = append(f.ship.vmShip.queue, queued{a: f.signed(t, "0v8.att")})
		f.ship.vmShip.mu.Unlock()
		waitUntil(t, 10*time.Second, "the assignment's answer", f.ship.abandoned(1))
		if ab := f.ship.abandon(0); !strings.Contains(ab, "not durable") || f.host.Count("disk", f.at("0v8.att")) != 0 {
			stop()
			t.Fatalf("NEW WORK STARTED WHILE A RELEASE WAS UNCERTAIN: %q", ab)
		}
		// saves succeed again: the daemon's view (the entry withheld) is
		// written, and the command is answered: not applied
		mu.Lock()
		failing = false
		mu.Unlock()
		a := final(f.answered(t, "0v5.cmd"))
		stop()
		if a["status"] != "refused" || len(released(t, f.cfg.StateFile)) != 0 || len(retentions(t, f.cfg.StateFile)) != 1 || d.remainingCapacity() != 1 {
			t.Fatalf("AN UNCERTAIN RELEASE WAS NEVER RESOLVED AS NOT APPLIED: %v, kept %v, released %v, advertised %d",
				a, handlesOf(retentions(t, f.cfg.StateFile)), handlesOf(released(t, f.cfg.StateFile)), d.remainingCapacity())
		}
	})
	t.Run("a restart while the file holds the uncertain release", func(t *testing.T) {
		f := newLegacyFixture(t, 2, preRuling(legacyAttempt))
		d := f.start(t)
		d.saveState = func(path string, st *state.State) error {
			if isRelease(st) {
				if err := state.Save(path, st); err != nil {
					return err
				}
				return &state.SaveError{Step: "sync-dir", Path: path, Uncertain: true, Err: errInjectedSave}
			}
			// nothing else is written before the crash
			return &state.SaveError{Step: "create", Path: path, Err: errInjectedSave}
		}
		stop := running(t, d)
		e := f.inspected(t, "ci-"+legacyAttempt)
		sel, _ := e["selection"].(string)
		evidence, _ := e["evidence"].(string)
		cmd := f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence})
		f.ship.enqueue(cmd)
		deadline := time.Now().Add(10 * time.Second)
		for len(f.ship.answersFor("0v5.cmd")) == 0 {
			if time.Now().After(deadline) {
				stop()
				t.Fatalf("THE DAEMON NEVER ANSWERED THE RECOVERY COMMAND 0v5.cmd; log:\n%s", f.logs.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		stop()
		closeDaemon(d) // the crash: the file holds the release
		f.ship.mu.Lock()
		f.ship.answers = nil
		f.ship.again = cmd
		f.ship.mu.Unlock()
		d2 := f.start(t)
		defer closeDaemon(d2)
		stop2 := running(t, d2)
		a := final(f.answered(t, "0v5.cmd"))
		stop2()
		if a["status"] != "completed" || len(released(t, f.cfg.StateFile)) != 1 || d2.remainingCapacity() != 2 {
			t.Fatalf("AFTER A RESTART THE DURABLE RELEASE WAS NOT ANSWERED FROM THE FILE: %v, released %v, advertised %d", a, handlesOf(released(t, f.cfg.StateFile)), d2.remainingCapacity())
		}
	})
}

// The daemon whose only free slot a legacy retention withholds keeps
// polling — its release comes through that poll — and once released, the
// slot is back.
func TestADaemonWhoseSlotsAreAllLegacyStaysUpForUrgit(t *testing.T) {
	f := newLegacyFixture(t, 1, preRuling(legacyAttempt))
	d := f.start(t)
	defer closeDaemon(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan int, 1)
	go func() { ran <- d.Run(ctx) }()
	select {
	case code := <-ran:
		t.Fatalf("THE DAEMON STOPPED WHILE ONLY A LEGACY RETENTION WITHHELD ITS SLOT: Run returned %d; log:\n%s", code, f.logs.String())
	case <-time.After(300 * time.Millisecond):
	}
	e := f.inspected(t, "ci-"+legacyAttempt)
	sel, _ := e["selection"].(string)
	evidence, _ := e["evidence"].(string)
	f.ship.enqueue(f.command(t, cmdSpec{selection: sel, revision: revisionOf(e), evidence: evidence}))
	a := final(f.answered(t, "0v5.cmd"))
	cancel()
	select {
	case <-ran:
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return")
	}
	if a["status"] != "completed" || d.remainingCapacity() != 1 {
		t.Fatalf("THE ONLY SLOT DID NOT RETURN: %v, advertised %d", a, d.remainingCapacity())
	}
}

// What this version writes is never legacy: every save stamps the state
// file's format, and no retention this source records of a microvm slot —
// a Prepare it could not roll back, a lost answer it could not settle, a
// failed reconcile destroy — lacks both its launcher identity and its
// request, or carries the mark. So the mark, set only on a file an earlier
// runner wrote, is the provenance.
func TestNoRetentionThisSourceWritesIsLegacyShaped(t *testing.T) {
	f := newVMFixture(t, 4)
	p := f.viaProxy(t)
	d := f.start(t)
	defer closeDaemon(d)
	check := func(stage string, want func(map[string]any) bool) {
		t.Helper()
		data, err := os.ReadFile(f.cfg.StateFile)
		if err != nil {
			t.Fatal(err)
		}
		var file map[string]any
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatal(err)
		}
		if file["format"] != float64(1) {
			t.Fatalf("%s: THIS VERSION'S STATE FILE CARRIES NO FORMAT STAMP: format %v", stage, file["format"])
		}
		found := false
		for _, e := range retentions(t, f.cfg.StateFile) {
			if e["backend"] == "microvm" && e["vm"] == nil && e["request"] == nil {
				t.Fatalf("%s: THIS SOURCE WROTE A LEGACY-SHAPED RETENTION: %v", stage, e)
			}
			if e["legacy"] != nil {
				t.Fatalf("%s: THIS SOURCE MARKED ITS OWN RETENTION LEGACY: %v", stage, e)
			}
			found = found || want(e)
		}
		if !found {
			t.Fatalf("%s: fixture: the retention was not recorded: %v", stage, retentions(t, f.cfg.StateFile))
		}
	}
	// a Prepare the launcher could not roll back: retained by identity
	f.host.SetFail("disk", launchertest.ErrInjected)
	f.host.SetFail("rmjail", launchertest.ErrInjected)
	d.handle(context.Background(), vmAssignment("0v1.att"))
	f.host.SetFail("disk", nil)
	f.host.SetFail("rmjail", nil)
	check("a Prepare not rolled back", func(e map[string]any) bool { return e["attempt"] == "0v1.att" && e["vm"] != nil })
	// a reserve whose answer is lost and whose settlement cannot be had:
	// retained by its request
	p.DropReply("reserve")
	p.Lose("settle")
	d.handle(context.Background(), vmAssignment("0v2.att"))
	check("a lost answer not settled", func(e map[string]any) bool { return e["attempt"] == "0v2.att" && e["request"] != nil })
	// a reconcile whose destroy fails: retained by identity (the admission's
	// settlement lost again, so it stays one)
	p.Lose("settle")
	f.quarantineAtLauncher(t, "0v3.att")
	f.ship.status["0v3.att"] = "failed"
	if err := d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	check("a reconcile destroy that failed", func(e map[string]any) bool { return e["attempt"] == "0v3.att" && e["vm"] != nil })
}
