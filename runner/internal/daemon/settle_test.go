package daemon

// Settled admission at the daemon (runner/launcher/INTEGRATION.md §11.10;
// settled-admission ruling 01, QUESTIONS-SOURCE-01 §9): a reserve whose
// answer is lost withholds its slot until the launcher settles exactly its
// request — never on an empty list, a timeout or an elapsed wait — and
// once settled the slot returns by itself, no operator and no cleanup
// evidence for a VM that never was. Through the real Microvm, the
// launcher's real core, wire and client, a proxy on the wire that loses,
// holds or delivers late exactly one request, the in-process ship, and the
// daemon's real state file; a restart is a new daemon (or launcher) over
// the same files, not a real process's.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
	"urgit/runner/internal/state"
)

// viaProxy routes the daemon f starts through a proxy on the wire to its
// launcher (before f.start).
func (f *vmFixture) viaProxy(t *testing.T) *launchertest.Proxy {
	t.Helper()
	p := launchertest.NewProxy(t, f.srv.Socket)
	f.cfg.LauncherSocket = p.Socket
	return p
}

// admissions is the state file's unsettled admissions.
func admissions(t *testing.T, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range retentions(t, path) {
		if e["request"] != nil && e["vm"] == nil {
			out = append(out, e)
		}
	}
	return out
}

// ledgerOf is the launcher's request ledger: request -> outcome.
func ledgerOf(t *testing.T, f *vmFixture) map[string]string {
	t.Helper()
	dir := filepath.Join(f.srv.StateDir(), "requests")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var file struct {
			Request struct {
				Request string `json:"request"`
				Outcome string `json:"outcome"`
			} `json:"request"`
		}
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatal(err)
		}
		out[file.Request.Request] = file.Request.Outcome
	}
	return out
}

// count is how many of the proxy's requests were op.
func count(ops []string, op string) int {
	n := 0
	for _, o := range ops {
		if o == op {
			n++
		}
	}
	return n
}

// A reserve delayed in flight crosses an empty authoritative list. Settled
// first, it is closed for good: the slot returns, and the delayed request,
// arriving later, is refused — no late admission. Its settlement
// unanswered, the slot stays withheld whatever the list says; the delayed
// request then arriving is admitted, charged once on both sides, and the
// next reconcile settles it — admitted, held, destroyed by the orphan logic
// — before the slot returns.
func TestADelayedAdmissionNeverOutlivesItsSettlement(t *testing.T) {
	t.Run("settled before the delayed request arrives", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.Hold("reserve") // delayed in flight; its caller gives up
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		if !keep || d.free() != 2 || d.remainingCapacity() != 2 || len(admissions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("a settled request's slot: keep %v, free %d, advertised %d, admissions %v", keep, d.free(), d.remainingCapacity(), admissions(t, f.cfg.StateFile))
		}
		replies, err := p.Deliver()
		if err != nil || len(replies) != 1 || replies[0].OK || !strings.Contains(replies[0].Error, launcher.ErrSettled.Error()) || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("A LATE ADMISSION FOLLOWED A CERTIFIED CLOSURE: %+v %v; the launcher holds %+v", replies, err, f.srv.Held(t, vmDaemon))
		}
	})
	t.Run("its settlement unanswered", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.Hold("reserve")
		p.Lose("settle") // the settle never reaches the launcher
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		if keep || d.free() != 1 || d.remainingCapacity() != 1 || len(admissions(t, f.cfg.StateFile)) != 1 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("A SLOT RETURNED BEFORE ITS REQUEST WAS SETTLED: keep %v, free %d, advertised %d, admissions %v; the list is empty: %+v", keep, d.free(), d.remainingCapacity(), admissions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
		replies, err := p.Deliver() // the delayed request arrives: admitted
		if err != nil || len(replies) != 1 || !replies[0].OK || len(f.srv.Held(t, vmDaemon)) != 1 || d.free() != 1 {
			t.Fatalf("the delayed admission, charged once on both sides: %+v %v; launcher %+v; free %d", replies, err, f.srv.Held(t, vmDaemon), d.free())
		}
		f.ship.mu.Lock()
		f.ship.status["0v1.att"] = "failed"
		f.ship.mu.Unlock()
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 2 || len(admissions(t, f.cfg.StateFile)) != 0 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("AN UNSETTLED ADMISSION WAS NOT SETTLED AT THE RECONCILE: free %d, state %v, launcher %+v", d.free(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
	})
	t.Run("its settlement unanswered at a reconcile too", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.Hold("reserve")
		p.Lose("settle") // the Prepare's
		p.Lose("settle") // the reconcile's
		d.handle(context.Background(), vmAssignment("0v1.att"))
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 1 || d.remainingCapacity() != 1 || len(admissions(t, f.cfg.StateFile)) != 1 {
			t.Fatalf("AN UNSETTLED ADMISSION'S SLOT RETURNED AT A RECONCILE: free %d, advertised %d, admissions %v", d.free(), d.remainingCapacity(), admissions(t, f.cfg.StateFile))
		}
		if replies, err := p.Deliver(); err != nil || len(replies) != 1 || !replies[0].OK {
			t.Fatalf("the delayed admission: %+v %v", replies, err)
		}
		f.ship.mu.Lock()
		f.ship.status["0v1.att"] = "failed"
		f.ship.mu.Unlock()
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("settled at the next reconcile: free %d, state %v, launcher %+v", d.free(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
	})
	t.Run("settled admitted, the list then refused", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.Hold("reserve")
		p.Lose("settle")
		d.handle(context.Background(), vmAssignment("0v1.att"))
		if replies, err := p.Deliver(); err != nil || len(replies) != 1 || !replies[0].OK {
			t.Fatalf("fixture: the delayed admission: %+v %v", replies, err)
		}
		// this reconcile settles it — admitted — and then its list is lost:
		// the reservation it found is held, never freed with its admission
		p.Lose("list")
		if err := d.Reconcile(context.Background()); err == nil {
			t.Fatal("fixture: the list should be lost")
		}
		if d.free() != 1 || len(admissions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 1 {
			t.Fatalf("AN ADMITTED REQUEST'S RESERVATION WAS NOT HELD IN ITS ADMISSION'S PLACE: free %d, state %v, launcher %+v", d.free(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
		f.ship.mu.Lock()
		f.ship.status["0v1.att"] = "failed"
		f.ship.mu.Unlock()
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 2 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("resolved by the next reconcile: free %d, launcher %+v", d.free(), f.srv.Held(t, vmDaemon))
		}
	})
}

// A reserve the launcher admitted whose answer was lost is found by its
// settlement — the exact reservation, made once — and resolved by its
// identity: rolled back, the slot returning; or, the rollback's own answer
// lost, retained under that identity in the admission's place, counted
// once, never released early.
func TestAnAdmittedRequestWhoseAnswerWasLostIsResolvedByItsIdentity(t *testing.T) {
	t.Run("rolled back", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.DropReply("reserve")
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		ledger := ledgerOf(t, f)
		if !keep || d.free() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("AN ADMITTED REQUEST WAS NOT RESOLVED BY ITS IDENTITY: keep %v, free %d, state %v, launcher %+v", keep, d.free(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
		if n := count(p.Seen(), "reserve"); n != 1 || len(ledger) != 1 {
			t.Fatalf("A DUPLICATE RESERVATION WAS MADE: %d reserve(s); ledger %v", n, ledger)
		}
		for _, outcome := range ledger {
			if outcome != launcher.SettledAdmitted {
				t.Fatalf("its ledger entry: %v", ledger)
			}
		}
	})
	t.Run("its rollback's answer lost too", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		p.DropReply("reserve")
		p.DropReply("destroy")
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		kept := retentions(t, f.cfg.StateFile)
		if keep || d.free() != 1 || len(kept) != 1 || kept[0]["vm"] == nil || kept[0]["incarnation"] == nil || len(admissions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("AN ADMITTED REQUEST'S RESERVATION WAS NOT RETAINED EXACTLY: keep %v, free %d, state %v", keep, d.free(), kept)
		}
	})
}

// A request the launcher never admitted is closed by its settlement, and
// its slot returns by itself: no operator, and no cleanup evidence for a
// VM that never existed.
func TestANeverAdmittedRequestReturnsItsSlotByItself(t *testing.T) {
	f := newVMFixture(t, 2)
	p := f.viaProxy(t)
	d := f.start(t)
	defer closeDaemon(d)
	p.Lose("reserve")
	keep := d.handle(context.Background(), vmAssignment("0v1.att"))
	ledger := ledgerOf(t, f)
	if !keep || d.free() != 2 || d.remainingCapacity() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 {
		t.Fatalf("A NEVER-ADMITTED REQUEST KEPT ITS SLOT: keep %v, free %d, advertised %d, state %v", keep, d.free(), d.remainingCapacity(), retentions(t, f.cfg.StateFile))
	}
	if len(ledger) != 1 {
		t.Fatalf("its closure: ledger %v", ledger)
	}
	for _, outcome := range ledger {
		if outcome != launcher.SettledClosed {
			t.Fatalf("its closure: ledger %v", ledger)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(f.srv.StateDir(), "released")); len(entries) != 0 {
		t.Fatalf("A CLEANUP CERTIFICATE WAS MADE FOR A VM THAT NEVER EXISTED: %d evidence file(s)", len(entries))
	}
	if !strings.Contains(f.ship.abandon(0), "never admitted") {
		t.Fatalf("the ship's reason: %q", f.ship.abandon(0))
	}
}

// A reserve the launcher refuses reserved nothing on its delivery, but the
// refusal is kept nowhere: the request is settled — closed — before its
// slot returns, so a replay of it is refused however much the launcher has
// freed meanwhile. Its settlement unanswered, the slot stays withheld; a
// replay admitted meanwhile is charged once on both sides, and the next
// reconcile settles it — admitted, held, destroyed by the orphan logic.
func TestARefusedRequestIsSettledBeforeItsSlotReturns(t *testing.T) {
	// full takes every guest the launcher allows, for another owner, and
	// returns a way to give one back
	full := func(t *testing.T, f *vmFixture) (giveBack func()) {
		t.Helper()
		cl, err := launcher.Dial(context.Background(), f.srv.Socket, "0v2.other")
		if err != nil {
			t.Fatal(err)
		}
		defer cl.Close()
		var refs []launcher.Ref
		for i := 0; ; i++ {
			request, err := launcher.NewRequest()
			if err != nil {
				t.Fatal(err)
			}
			r, err := cl.Reserve(launcher.ReserveRequest{Attempt: fmt.Sprintf("0v%d.fill", i), Image: f.digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked", Request: request})
			if err != nil {
				if !strings.Contains(err.Error(), launcher.ErrOverBudget.Error()) || len(refs) == 0 {
					t.Fatalf("fixture: filling the launcher: %v", err)
				}
				break
			}
			refs = append(refs, r.Ref())
		}
		return func() {
			t.Helper()
			cl, err := launcher.Dial(context.Background(), f.srv.Socket, "0v2.other")
			if err != nil {
				t.Fatal(err)
			}
			defer cl.Close()
			if _, err := cl.DestroyOf(refs[0]); err != nil {
				t.Fatalf("fixture: giving a guest back: %v", err)
			}
		}
	}
	// replay sends req, the daemon's reserve as the wire carried it, again
	replay := func(t *testing.T, f *vmFixture, req launcher.Request) error {
		t.Helper()
		cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
		if err != nil {
			t.Fatal(err)
		}
		defer cl.Close()
		_, err = cl.Reserve(launcher.ReserveRequest{Attempt: req.Attempt, Image: req.Image, CPUs: req.CPUs, MemoryMiB: req.MemoryMiB, DiskMiB: req.DiskMiB,
			DeadlineUnix: req.DeadlineUnix, Network: req.Network, Destinations: req.Destinations, Label: req.Label, Request: req.Request})
		return err
	}
	t.Run("settled", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		giveBack := full(t, f)
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		reserves := p.Requests("reserve")
		if len(reserves) != 1 || !strings.Contains(f.ship.abandon(0), launcher.ErrOverBudget.Error()) {
			t.Fatalf("fixture: the reserve is refused: %d reserve(s); the ship's reason %q", len(reserves), f.ship.abandon(0))
		}
		if !keep || d.free() != 2 || len(admissions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("a refused request, settled: keep %v, free %d, admissions %v", keep, d.free(), admissions(t, f.cfg.StateFile))
		}
		giveBack()
		if err := replay(t, f, reserves[0]); err == nil || !strings.Contains(err.Error(), launcher.ErrSettled.Error()) || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("A REFUSED REQUEST WAS ADMITTED ON ITS REPLAY: %v; the launcher holds %+v", err, f.srv.Held(t, vmDaemon))
		}
		if ledger := ledgerOf(t, f); ledger[reserves[0].Request] != launcher.SettledClosed {
			t.Fatalf("its closure: ledger %v", ledger)
		}
	})
	t.Run("its settlement unanswered", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		defer closeDaemon(d)
		giveBack := full(t, f)
		p.Lose("settle")
		keep := d.handle(context.Background(), vmAssignment("0v1.att"))
		if keep || d.free() != 1 || d.remainingCapacity() != 1 || len(admissions(t, f.cfg.StateFile)) != 1 {
			t.Fatalf("A REFUSED REQUEST'S SLOT RETURNED BEFORE IT WAS SETTLED: keep %v, free %d, advertised %d, admissions %v", keep, d.free(), d.remainingCapacity(), admissions(t, f.cfg.StateFile))
		}
		giveBack()
		if err := replay(t, f, p.Requests("reserve")[0]); err != nil || len(f.srv.Held(t, vmDaemon)) != 1 || d.free() != 1 {
			t.Fatalf("a replay before its settlement, charged once on both sides: %v; launcher %+v; free %d", err, f.srv.Held(t, vmDaemon), d.free())
		}
		f.ship.mu.Lock()
		f.ship.status["0v1.att"] = "failed"
		f.ship.mu.Unlock()
		if err := d.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if d.free() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("settled at the reconcile: free %d, state %v, launcher %+v", d.free(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
	})
}

// An unsettled admission withholds its slot, but it never stops the daemon
// as a retention only the operator releases does: with every slot withheld
// by one, Run keeps reconciling, settles it, and offers the slot again — no
// operator, no restart.
func TestAnUnsettledAdmissionNeverStopsTheDaemon(t *testing.T) {
	f := newVMFixture(t, 1)
	p := f.viaProxy(t)
	d := f.start(t)
	defer closeDaemon(d)
	p.Lose("reserve")
	p.Lose("settle")
	if keep := d.handle(context.Background(), vmAssignment("0v1.att")); keep || d.free() != 0 || len(admissions(t, f.cfg.StateFile)) != 1 {
		t.Fatalf("fixture: every slot withheld by an unsettled admission: keep %v, free %d, state %v", keep, d.free(), retentions(t, f.cfg.StateFile))
	}
	d.reconcileEvery = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan int, 1)
	go func() { ran <- d.Run(ctx) }()
	deadline := time.After(20 * time.Second)
	for d.free() != 1 {
		select {
		case code := <-ran:
			t.Fatalf("AN UNSETTLED ADMISSION STOPPED THE DAEMON: Run returned %d, its slot withheld", code)
		case <-deadline:
			t.Fatal("the admission was not settled by Run's reconcile")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	<-ran
	if len(admissions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
		t.Fatalf("settled by Run: state %v, launcher %+v", retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
	}
}

// A settlement whose answer is lost leaves the slot withheld; the daemon
// and the launcher both restart; the delayed request is refused; the new
// daemon withholds the slot from its start, before any capacity is
// advertised, and settles the request again at its reconcile — closed,
// once — and repeated reconciles change nothing.
func TestSettlementSurvivesALostAnswerAndARestart(t *testing.T) {
	f := newVMFixture(t, 2)
	p := f.viaProxy(t)
	d := f.start(t)
	p.Hold("reserve")
	p.DropReply("settle") // the launcher closes the request; its answer is lost
	keep := d.handle(context.Background(), vmAssignment("0v1.att"))
	if keep || d.free() != 1 || len(admissions(t, f.cfg.StateFile)) != 1 {
		t.Fatalf("A SLOT RETURNED ON A LOST SETTLEMENT: keep %v, free %d, admissions %v", keep, d.free(), admissions(t, f.cfg.StateFile))
	}
	if replies, err := p.Deliver(); err != nil || len(replies) != 1 || replies[0].OK {
		t.Fatalf("A LATE ADMISSION FOLLOWED A CLOSURE WHOSE ANSWER WAS LOST: %+v %v", replies, err)
	}
	closeDaemon(d)
	f.srv.Restart(t)
	again := f.start(t)
	defer closeDaemon(again)
	if again.free() != 1 || again.remainingCapacity() != 1 {
		t.Fatalf("AN UNSETTLED ADMISSION WAS NOT WITHHELD AT A RESTART: free %d, advertised %d", again.free(), again.remainingCapacity())
	}
	for i := 0; i < 3; i++ {
		if err := again.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if again.free() != 2 || again.remainingCapacity() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 || len(f.srv.Held(t, vmDaemon)) != 0 {
			t.Fatalf("AN UNSETTLED ADMISSION WAS NOT SETTLED AFTER A RESTART (reconcile %d): free %d, advertised %d, state %v, launcher %+v", i+1, again.free(), again.remainingCapacity(), retentions(t, f.cfg.StateFile), f.srv.Held(t, vmDaemon))
		}
	}
}

// The admission is durable before its reserve is sent: a save that fails —
// before its effect, or after it — sends none. A settled admission whose
// removal cannot be saved is withheld again after a restart, and settled
// again: charged once.
func TestAnAdmissionIsDurableBeforeItsReserve(t *testing.T) {
	for _, c := range []struct {
		name   string
		effect bool // the failing save renames the file before failing
	}{{"its save fails", false}, {"its save acts, then fails", true}} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			p := f.viaProxy(t)
			d := f.start(t)
			defer closeDaemon(d)
			saves := 0
			d.saveState = func(path string, st *state.State) error {
				saves++
				if saves == 1 && c.effect {
					if err := state.Save(path, st); err != nil {
						return err
					}
					return errors.New("injected: the directory's fsync failed after the rename")
				}
				if saves == 1 {
					return errors.New("injected: the state file cannot be written")
				}
				return state.Save(path, st)
			}
			keep := d.handle(context.Background(), vmAssignment("0v1.att"))
			if n := count(p.Seen(), "reserve"); n != 0 || len(f.srv.Held(t, vmDaemon)) != 0 || len(ledgerOf(t, f)) != 0 {
				t.Fatalf("A RESERVE WAS SENT WITHOUT ITS DURABLE ADMISSION: %d reserve(s), launcher %+v", n, f.srv.Held(t, vmDaemon))
			}
			if !keep || d.free() != 2 || len(admissions(t, f.cfg.StateFile)) != 0 || !strings.Contains(f.ship.abandon(0), "none was sent") {
				t.Fatalf("an admission not durable: keep %v, free %d, state %v, abandon %q", keep, d.free(), retentions(t, f.cfg.StateFile), f.ship.abandon(0))
			}
		})
	}
	t.Run("while its Prepare runs, it is that attempt's slot", func(t *testing.T) {
		f := newVMFixture(t, 2)
		d := f.start(t)
		defer closeDaemon(d)
		release := f.host.Hold(t, "connect")
		handled := make(chan struct{})
		go func() {
			defer close(handled)
			d.handle(context.Background(), vmAssignment("0v1.att"))
		}()
		<-f.host.Reached()
		d.mu.Lock()
		d.running++ // the slot Run's acquire takes for the attempt
		d.mu.Unlock()
		free, advertised, recorded := d.free(), d.remainingCapacity(), len(admissions(t, f.cfg.StateFile))
		d.mu.Lock()
		d.running--
		d.mu.Unlock()
		release()
		<-handled
		if free != 1 || advertised != 2 || recorded != 1 {
			t.Fatalf("A RUNNING PREPARE'S ADMISSION WAS COUNTED TWICE: free %d (want 1), advertised %d (want 2), admissions recorded %d (want 1)", free, advertised, recorded)
		}
	})
	t.Run("its removal cannot be saved", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		saves := 0
		d.saveState = func(path string, st *state.State) error {
			saves++
			if saves >= 2 {
				return errors.New("injected: the state file cannot be written")
			}
			return state.Save(path, st)
		}
		p.Lose("reserve")
		d.handle(context.Background(), vmAssignment("0v1.att"))
		if d.retrySave() == nil || len(admissions(t, f.cfg.StateFile)) != 1 {
			t.Fatalf("fixture: its removal is in memory only: state %v", retentions(t, f.cfg.StateFile))
		}
		closeDaemon(d) // the process dies before a save succeeds
		again := f.start(t)
		defer closeDaemon(again)
		if again.free() != 1 {
			t.Fatalf("A SETTLED ADMISSION WAS COUNTED %d TIME(S) AFTER A RESTART: free %d", 2-again.free(), again.free())
		}
		if err := again.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if again.free() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("settled again after the restart: free %d, state %v", again.free(), retentions(t, f.cfg.StateFile))
		}
	})
}

// Settlement changes no disposition: a request whose reservation became an
// incident while its admission was unsettled is admitted, held, and — its
// destroy refused — retained under its identity: the operator's (A); one
// whose reservation was released by a timely cleanup, on its evidence, is
// settled released, and its slot returns (Q8).
func TestSettlementLeavesADispositionAsItIs(t *testing.T) {
	// unsettled leaves attempt's admission unsettled, the launcher holding
	// the reservation its request made, and returns that reservation
	unsettled := func(t *testing.T, f *vmFixture, p *launchertest.Proxy, d *Daemon) launcher.Record {
		t.Helper()
		p.DropReply("reserve")
		p.Lose("settle")
		if keep := d.handle(context.Background(), vmAssignment("0v1.att")); keep || len(admissions(t, f.cfg.StateFile)) != 1 {
			t.Fatalf("fixture: unsettled: keep %v, state %v", keep, retentions(t, f.cfg.StateFile))
		}
		recs := f.srv.Held(t, vmDaemon)
		if len(recs) != 1 {
			t.Fatalf("fixture: the launcher holds %+v", recs)
		}
		return recs[0]
	}
	t.Run("an incident", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		rec := unsettled(t, f, p, d)
		closeDaemon(d)
		cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cl.Create(rec.Ref()); err != nil {
			t.Fatal(err)
		}
		f.host.SetFail("rmjail", launchertest.ErrInjected)
		if q, err := cl.DestroyOf(rec.Ref()); err == nil || !q {
			t.Fatalf("fixture: quarantined: %v %v", err, q)
		}
		cl.Close()
		f.ship.mu.Lock()
		f.ship.status["0v1.att"] = "failed"
		f.ship.mu.Unlock()
		again := f.start(t)
		defer closeDaemon(again)
		if err := again.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		recs := f.srv.Held(t, vmDaemon)
		kept := retentions(t, f.cfg.StateFile)
		if len(recs) != 1 || recs[0].State != launcher.StateQuarantined || len(kept) != 1 || !names(kept[0], recs[0]) || again.free() != 1 {
			t.Fatalf("AN INCIDENT WAS SETTLED AWAY: launcher %+v, state %v, free %d", recs, kept, again.free())
		}
	})
	t.Run("a timely cleanup's release", func(t *testing.T) {
		f := newVMFixture(t, 2)
		p := f.viaProxy(t)
		d := f.start(t)
		rec := unsettled(t, f, p, d)
		closeDaemon(d)
		cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cl.DestroyOf(rec.Ref()); err != nil {
			t.Fatal(err)
		}
		cl.Close()
		again := f.start(t)
		defer closeDaemon(again)
		if err := again.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if again.free() != 2 || len(retentions(t, f.cfg.StateFile)) != 0 {
			t.Fatalf("a released request's slot: free %d, state %v", again.free(), retentions(t, f.cfg.StateFile))
		}
	})
}
