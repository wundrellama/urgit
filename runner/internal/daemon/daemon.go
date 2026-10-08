// Package daemon is the runner loop (D7): enroll once, reconcile orphans,
// then long-poll the ship and carry out each assignment in a sandbox.
// Every branch that chooses between outcomes here traces to a field the
// ship sent or a line act emitted; the daemon decides nothing itself.
//
// P4: the sandbox is the microvm backend by default (Firecracker +
// jailer through the administrator's launcher) or the explicit
// docker-rootless compatibility mode; every assignment carries a signed
// execution manifest the daemon verifies whole; the sandbox requirement
// and the network profile in it are honoured or refused, never
// downgraded; the bundle the guest receives is assembled and verified on
// the host (bundle.go); a failed teardown is quarantined in the state
// file so the slot stays withheld across restarts.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"urgit/runner/internal/act"
	"urgit/runner/internal/config"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// ExitEnrollmentLost is the exit status after the ship answers 401.
const ExitEnrollmentLost = 3

// ExitNoCapacity is the exit status once every slot is quarantined.
const ExitNoCapacity = 4

// ExitRevoked is the exit status after the ship answers that the operator
// revoked this daemon (P3 D2): nothing short of a new token brings it back.
const ExitRevoked = 5

type Daemon struct {
	cfg      *config.Config
	box      sandbox.Sandbox
	client   *ship.Client
	daemonID string
	log      *log.Logger
	// the CI public key every assignment and grant must verify against
	// (D5): the config's pin when set, else enrollment's
	ciKey string

	// capacity accounting (runner/launcher/INTEGRATION.md §4), under mu:
	// capacity is what is advertised, the configured capacity less the
	// retentions; a slot is free when the configured capacity exceeds the
	// retentions, the held orphans and the attempts running together
	mu          sync.Mutex
	slotFreed   *sync.Cond
	capacity    int
	quarantined []string           // the retained handles, for the log
	retained    []state.Quarantine // what the state file holds, or will once saved
	held        map[string]sandbox.Handle
	running     int
	unsaved     error           // the state file does not hold what retained does
	inFlight    map[string]bool // attempts this process is running now
	stateFile   string
	st          *state.State
	lock        *state.Lock
	// reconcileEvery is how often Run reconciles the backend's records
	// again (held orphans, anything left); tests shorten it
	reconcileEvery time.Duration
	// jobTiming is injectable in tests; zero selects the bounded C9 defaults.
	jobTiming jobTiming
	// saveState writes the state file (state.Save when nil); tests make it
	// fail after or before its effect
	saveState func(path string, st *state.State) error
	// covering: the reserve requests whose admissions (retentions with a
	// Request and no VM) a Prepare running in this process stands for — its
	// running slot is theirs, so they withhold none of their own until it
	// ends (runner/launcher/INTEGRATION.md §11.10)
	covering map[string]bool

	// the execution ledger (replay.go; INTEGRATION.md §11.14): every attempt
	// this runner takes for execution, recorded before it runs and never
	// run again. take and finish write its records (the ledger's when nil;
	// tests make them fail before or after their effect). Memory keeps, for
	// this life, the attempts given back to the ship because their record
	// could not be written, and those whose finish could not be — under mu
	ledger       *state.Ledger
	take         func(e state.Execution) error
	finish       func(attempt string, at int64) error
	givenBack    map[string]bool
	finishedHere map[string]bool
	// began is the ledger's history as it was opened: a runner whose
	// history is not complete runs nothing until its transition is recorded
	// (history.go; INTEGRATION.md §11.15)
	began state.History

	// the operator's recovery commands from Urgit (recovery.go;
	// INTEGRATION.md §11.12): carried out one at a time (recoveryMu). A
	// final decision is the state file's alone (released, or st.Refused);
	// memory keeps only what is not final: the commands in progress, by id;
	// the releases whose save was uncertain; the refusals not recorded yet —
	// both recorded, and answered, by the next save that succeeds; the
	// answers the ship has not taken yet; and the last report's error,
	// logged on change — all but recoveryMu under mu
	recoveryMu sync.Mutex
	recovering map[string]bool
	doubtful   map[string]ship.RecoveryAnswer
	unrecorded map[string]state.Refusal
	outbox     map[string]ship.RecoveryAnswer
	reportErr  string

	// checkout materializes the candidate on the host; tests replace it
	checkout func(ctx context.Context, a *ship.Assignment, dir string) error
	// mirror materializes one locked action into the bundle; tests replace it
	mirror func(ctx context.Context, a *ship.Assignment, node ship.LockNode, dir string) error
	// fetchDownload stages one inventoried download; tests replace it
	fetchDownload func(ctx context.Context, a *ship.Assignment, dl ship.Download, dir string) error
}

// New takes the state file's lock (held for the daemon's life: the
// operator's retry and release, `urgit-runner -recover`, are refused
// meanwhile), loads or performs enrollment and constructs the sandbox
// backend.
func New(ctx context.Context, cfg *config.Config, logger *log.Logger) (d *Daemon, err error) {
	lock, err := state.Acquire(cfg.StateFile)
	if err != nil {
		return nil, fmt.Errorf("state file: %w (another urgit-runner, or the operator's -recover retry or release, has it)", err)
	}
	defer func() {
		if err != nil {
			lock.Release()
		}
	}()
	box, err := sandbox.New(cfg)
	if err != nil {
		return nil, err
	}
	d = &Daemon{cfg: cfg, box: box, log: logger, capacity: cfg.Capacity, inFlight: map[string]bool{}, stateFile: cfg.StateFile, lock: lock}
	d.checkout = d.gitCheckout
	d.mirror = d.gitMirror
	d.fetchDownload = d.storeDownload
	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	// the execution ledger (INTEGRATION.md §11.14): no daemon runs, or
	// enrolls, on one that cannot record, nor on one that lost its records
	// — the state file names the ledger its runner keeps. Its history
	// begins with this runner's enrollment, or with a state file kept
	// before it — of an earlier version's format (0), or this one's: what
	// ran then left no record (QUESTIONS-SOURCE-01 §15)
	began := state.History{Since: time.Now().Unix(), Enrolled: st == nil, StateFormat: state.CurrentFormat}
	if st != nil && st.Upgraded() {
		began.StateFormat = 0
	}
	d.ledger = state.LedgerFor(cfg.StateFile)
	if began, err = d.ledger.Open(began, st != nil && st.Ledger); err != nil {
		return nil, fmt.Errorf("execution ledger: %w", err)
	}
	named := st == nil || st.Ledger // the file names it once saved
	d.began = began
	switch {
	case began.Since == 0:
		d.log.Printf("execution ledger %s: when its history began is not recorded (its HISTORY cannot be read); what ran before it is not known (QUESTIONS-SOURCE-01 §15)", d.ledger.Dir)
	case began.Enrolled:
		d.log.Printf("execution ledger %s: every attempt since this runner enrolled (%s)", d.ledger.Dir, time.Unix(began.Since, 0).UTC().Format(time.RFC3339))
	default:
		d.log.Printf("execution ledger %s: the attempts taken since %s; what ran before it is not known (QUESTIONS-SOURCE-01 §15)", d.ledger.Dir, time.Unix(began.Since, 0).UTC().Format(time.RFC3339))
	}
	if st == nil {
		if cfg.EnrollToken == "" {
			return nil, errors.New("no state file and no enroll_token: mint one in the ship's web interface (Settings → Runners → Mint token) and put it in the config")
		}
		d.log.Printf("enrolling with the ship at %s (sandbox %s, labels %s, profiles %s, resolver %v)", cfg.ShipURL, cfg.Sandbox, strings.Join(cfg.Labels, ","), strings.Join(cfg.ProfileNames(), ","), cfg.Resolver)
		client := ship.New(cfg.ShipURL, "")
		enrolled, err := client.Enroll(ctx, cfg.EnrollToken, cfg.Capacity, cfg.Sandbox, cfg.Labels, cfg.ProfileNames(), cfg.Resolver)
		if err != nil {
			return nil, err
		}
		st = &state.State{DaemonID: enrolled.DaemonID, Bearer: enrolled.Bearer, ShipURL: cfg.ShipURL, CIPublicKey: enrolled.CIPublicKey, Ledger: true}
		if err := state.Save(cfg.StateFile, st); err != nil {
			return nil, fmt.Errorf("state file: %w", err)
		}
		d.log.Printf("enrolled as daemon %s; state written to %s (mode 0600); CI public key pinned: %s", enrolled.DaemonID, cfg.StateFile, enrolled.CIPublicKey)
	} else {
		d.log.Printf("state file %s present: daemon %s, no re-enrollment", cfg.StateFile, st.DaemonID)
	}
	d.st = st
	// the sandbox objects carry this daemon's id from here on (P3 D6 g;
	// P4: the launcher's ownership records), so a restart reconciles its
	// own leftovers and no other runner's
	if err := box.SetOwner(st.DaemonID); err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}
	// a quarantine recorded before a crash still withholds its slot
	// (rider 04): capacity is what the state file leaves, never reset —
	// one slot per retention, however often an older daemon recorded it
	merged := st.Dedupe()
	if merged > 0 {
		d.log.Printf("state file %s held %d duplicate quarantine entr(ies) for retentions it also holds; each is withheld once", cfg.StateFile, merged)
	}
	for _, q := range st.Quarantined {
		d.retained = append(d.retained, q)
		d.recountLocked()
		if q.Admission() {
			// a reserve request an earlier life sent, or may have sent, and
			// never settled: withheld until it is, before any capacity is
			// advertised for it (INTEGRATION.md §11.10)
			d.log.Printf("ADMISSION NOT SETTLED %s carried over from the state file (%s; %s): its slot is withheld until the launcher settles it; advertised capacity now %d", q.Handle, q.Reason, describeRetention(q), d.capacity)
			continue
		}
		if q.IsLegacy() {
			// recorded without its request by a runner before settled
			// admission: withheld until the operator releases it from Urgit,
			// on its evidence (INTEGRATION.md §11.12)
			d.log.Printf("LEGACY retention %s carried over from the state file (%s; %s): its slot is withheld until the operator releases it from Urgit (Settings → Runners), on its evidence; advertised capacity now %d",
				q.Handle, q.Reason, legacyMark(q), d.capacity)
			continue
		}
		d.quarantined = append(d.quarantined, q.Handle)
		d.log.Printf("QUARANTINED slot %s carried over from the state file (%s at %s; %s); advertised capacity now %d", q.Handle, q.Reason, time.Unix(q.At, 0).UTC().Format(time.RFC3339), describeRetention(q), d.capacity)
	}
	d.ciKey = st.CIPublicKey
	if cfg.CIPublicKey != "" {
		d.ciKey = cfg.CIPublicKey
		d.log.Printf("CI public key pinned by the config: %s", cfg.CIPublicKey)
	}
	if d.ciKey == "" {
		d.log.Printf("no CI public key pinned: every assignment will be refused until one is (re-enroll, or set ci_public_key)")
	}
	// the token is consumed: forget it so it is never logged or written
	cfg.EnrollToken = ""
	d.client = ship.New(cfg.ShipURL, st.Bearer)
	d.client.Capacity = d.capacity
	d.client.Labels = cfg.Labels
	d.client.Profiles = cfg.ProfileNames()
	d.client.Resolver = cfg.Resolver
	// this daemon carries out the operator's recovery commands from Urgit
	// (INTEGRATION.md §11.12): the ship hands them over on its poll
	d.client.Recovery = true
	d.daemonID = st.DaemonID
	// a file an earlier runner wrote is saved at once in this version's
	// format, its legacy marks with it (state.Load): from here no entry of
	// it can be marked
	if st.Upgraded() {
		marked := 0
		for _, q := range st.Quarantined {
			if q.IsLegacy() {
				marked++
			}
		}
		d.log.Printf("state file %s was last written by an earlier runner: saved now in this version's format (%d), %d legacy retention(s) marked", cfg.StateFile, state.CurrentFormat, marked)
	}
	// the state file names the execution ledger from here: a start that
	// finds it gone is refused; until the file is saved, no new work (§5)
	if !named {
		st.Ledger = true
		d.log.Printf("state file %s: names its execution ledger %s from now on", cfg.StateFile, d.ledger.Dir)
	}
	// a runner whose history is incomplete runs nothing until its
	// transition (INTEGRATION.md §11.15): it says so, and why
	d.mu.Lock()
	paused, epoch := d.pausedLocked(), d.epochLocked()
	_, notTransition := d.transitionRecordLocked()
	d.mu.Unlock()
	if notTransition != "" {
		// a record that is no transition: kept, never taken for one
		// (INTEGRATION.md §11.16)
		d.log.Printf("TRANSITION RECORD IS NO TRANSITION: %s; it is kept as the state file holds it, and not taken for a transition", notTransition)
	}
	switch {
	case paused:
		d.log.Printf("EXECUTION PAUSED: what this runner ran before %s is not known; it advertises no capacity and runs nothing until its transition is confirmed in Urgit (Settings → Runners)", sinceText(began.Since))
	case epoch > 0:
		d.log.Printf("execution history: transition to authorization epoch %d recorded; nothing its ship signed before it runs here", epoch)
	}
	if merged > 0 || st.Upgraded() || !named {
		d.mu.Lock()
		d.persistLocked()
		d.mu.Unlock()
	}
	return d, nil
}

// Close gives the state file's lock back (the process's exit does too).
func (d *Daemon) Close() error { return d.lock.Release() }

func describeRetention(q state.Quarantine) string {
	switch {
	case q.Admission():
		return fmt.Sprintf("%s reserve request %s, its outcome not settled (attempt %s)", q.Backend, q.Request, q.Attempt)
	case q.VM != "" && q.Incarnation != "":
		return fmt.Sprintf("%s vm %s incarnation %s (cid %d created %d)", q.Backend, q.VM, q.Incarnation, q.CID, q.Created)
	case q.VM != "":
		return fmt.Sprintf("%s vm %s cid %d created %d (no incarnation token)", q.Backend, q.VM, q.CID, q.Created)
	case q.Network != "" || q.Container != "":
		return fmt.Sprintf("%s network %s volume %s container %s", q.Backend, q.Network, q.Volume, q.Container)
	case q.Backend == "":
		return "recorded before its identity was kept"
	}
	return q.Backend + " identity unknown (attempt " + q.Attempt + ")"
}

// Banner is the startup disclosure (CI-SANDBOX-1-B; P4 D2).
func (d *Daemon) Banner() string {
	limits := ""
	if d.cfg.Sandbox == "microvm" {
		limits = fmt.Sprintf(", guest %d vcpu / %d MiB (+%d MiB host overhead) / %d MiB writable disk, budget %d cpus / %d MiB", d.cfg.CPUs, d.cfg.MemoryMiB, config.OverheadMiB, d.cfg.DiskMiB, d.cfg.BudgetCPUs, d.cfg.BudgetMemoryMiB)
	}
	return fmt.Sprintf("urgit-runner: daemon %s, ship %s, capacity %d, labels [%s], profiles [%s], act %s, sandbox: %s%s",
		d.daemonID, d.cfg.ShipURL, d.capacity, strings.Join(d.cfg.Labels, " "), strings.Join(d.cfg.ProfileNames(), " "), act.Version, d.box.Name(), limits)
}

// Reconcile (D7 c): every sandbox left from a previous life is destroyed
// when the ship says its attempt is terminal or unknown; a running one is
// left to its deadline, never resumed — and, while the backend holds it,
// its slot is not offered (held). One this daemon already withholds is
// neither destroyed again nor charged again (§2b). Reconcile may run any
// number of times (Run repeats it) with the same outcome for the same
// records (runner/launcher/INTEGRATION.md §4).
func (d *Daemon) Reconcile(ctx context.Context) error {
	// every reserve request whose outcome is not settled first: an admitted
	// one's reservation is then held, and resolved below with the orphans
	d.settleAdmissions(ctx)
	orphans, err := d.box.Orphans(ctx)
	if err != nil {
		return err
	}
	listed := map[string]bool{}
	for _, id := range orphans {
		listed[id] = true
		attempt := strings.TrimPrefix(id, "ci-")
		// this process's own sandbox is named after its claimed attempt in
		// the ship's spelling; one named in another spelling of it is an
		// orphan like any other (INTEGRATION.md §11.13)
		if uvKey(attempt) == attempt && d.claimed(attempt) {
			continue // this process's own attempt, not an orphan
		}
		h := d.box.HandleFor(id)
		if q, ok := d.retainedAs(h); ok {
			d.unhold(id)
			d.log.Printf("reconcile %s: already withheld (%s); not destroyed or charged again: only the operator's release (urgit-runner -recover) returns it", id, describeRetention(q))
			continue
		}
		status, found, err := d.client.AttemptStatus(ctx, uvKey(attempt))
		if errors.Is(err, ship.ErrNotOurs) {
			if d.box.Kind() == "microvm" {
				// the launcher lists this daemon's records only: whatever the
				// ship says, it is charged to this runner while it exists
				d.hold(h, "the ship calls its attempt another daemon's; the launcher charges it to this one")
				continue
			}
			// another daemon's attempt on a shared Docker daemon (an
			// unlabelled leftover the owner filter let through): not
			// this runner's to destroy, and not enrollment loss
			d.log.Printf("reconcile %s: attempt belongs to another daemon; left alone", id)
			continue
		}
		if errors.Is(err, ship.ErrUnauthorized) {
			return err
		}
		if err != nil {
			d.hold(h, fmt.Sprintf("ship unreachable: %v (kept)", err))
			continue
		}
		if found && status == "running" {
			d.hold(h, "attempt still running on the ship; left for its deadline, not resumed")
			continue
		}
		reason := "unknown to the ship"
		if found {
			reason = "attempt " + status
		}
		if err := d.box.Destroy(ctx, h); err != nil {
			d.log.Printf("reconcile %s (%s): destroy failed: %v", id, reason, err)
			d.unhold(id)
			d.retain(h, fmt.Sprintf("reconcile (%s): teardown failed: %v", reason, err))
			continue
		}
		d.unhold(id)
		d.log.Printf("reconcile %s (%s): destroyed", id, reason)
	}
	// a held orphan the backend no longer lists is gone, and its slot free
	// — for a backend that proves releases, only once it proves this one's:
	// its absence from the list is no authority (runner/launcher/
	// INTEGRATION.md §11.8)
	d.mu.Lock()
	var unlisted []sandbox.Handle
	for id, h := range d.held {
		if !listed[id] {
			unlisted = append(unlisted, h)
		}
	}
	d.mu.Unlock()
	prover, proves := d.box.(sandbox.ReleaseProver)
	for _, h := range unlisted {
		if proves {
			if err := prover.ProveReleased(ctx, h); err != nil {
				d.log.Printf("reconcile %s: no longer listed by the %s backend, but its release is not proven (%v); its slot stays withheld", h.ID, d.box.Kind(), err)
				continue
			}
		}
		d.mu.Lock()
		if cur, ok := d.held[h.ID]; ok && cur == h {
			delete(d.held, h.ID)
			d.log.Printf("reconcile %s: no longer held by the %s backend; its slot is free", h.ID, d.box.Kind())
			d.signalLocked()
		}
		d.mu.Unlock()
	}
	return nil
}

// Run polls until the context ends, the enrollment is lost or revoked, or
// every slot is quarantined. The exit status is the caller's to use.
//
// The poll is also the heartbeat (P3 D6 f): it runs at capacity too, so
// the ship's `last-seen` is always liveness and a busy daemon never reads
// stale. The ship's scheduler hands a full daemon nothing; an operator's
// %assign can, and such an assignment waits here for a slot.
func (d *Daemon) Run(ctx context.Context) int {
	var wg sync.WaitGroup
	// running work is cancelled when the ship revokes this daemon: the
	// ship has re-offered every attempt it held, and its bearer is gone
	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	every := d.reconcileEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	reconciled := time.Now()
	// the ship hears what this runner withholds at once, and after every
	// reconcile and recovery command (INTEGRATION.md §11.12)
	d.report(ctx)
	for {
		// only retentions the operator releases with the daemon stopped stop
		// the daemon: an unsettled admission returns its slot once settled,
		// which a reconcile does, and a legacy retention is released from
		// Urgit through this poll
		if d.exhausted() {
			d.log.Printf("capacity 0: every slot is quarantined (%s); stopping", strings.Join(d.quarantinedNames(), ", "))
			wg.Wait()
			return ExitNoCapacity
		}
		if ctx.Err() != nil {
			wg.Wait()
			return 0
		}
		// an unsaved retention is retried at every turn (§5)
		_ = d.retrySave()
		// a recovery command's answer the ship has not taken yet
		d.flushAnswers(ctx)
		// the backend's records again: held orphans released, anything
		// left by this process's life settled
		if time.Since(reconciled) >= every {
			reconciled = time.Now()
			if err := d.Reconcile(ctx); err != nil {
				if errors.Is(err, ship.ErrUnauthorized) {
					d.log.Printf("enrollment lost; re-enroll with a fresh token")
					wg.Wait()
					return ExitEnrollmentLost
				}
				d.log.Printf("reconcile: %v", err)
			}
			d.report(ctx)
		}
		// the poll reports the capacity as it stands now (x-ci-capacity is
		// read from the poll only): a copy of the client carries it, so no
		// request in flight on the shared client sees a field change
		poll := *d.client
		poll.Capacity, poll.Paused = d.advertisedCapacity()
		assignment, command, err := poll.PollWork(ctx, d.daemonID)
		if errors.Is(err, ship.ErrRevoked) {
			// the operator revoked this daemon (P3 D2): whatever it was
			// running has been offered to another daemon; it stops here
			d.log.Printf("revoked by the ship; stopping (a new enrollment token is the only way back)")
			cancelWork()
			wg.Wait()
			return ExitRevoked
		}
		if errors.Is(err, ship.ErrUnauthorized) {
			d.log.Printf("enrollment lost; re-enroll with a fresh token")
			wg.Wait()
			return ExitEnrollmentLost
		}
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return 0
			}
			d.log.Printf("poll: %v; retrying in 5 s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		// the operator's recovery command from Urgit: carried out by this
		// daemon, the owner of its state file, and answered (recovery.go)
		if command != nil {
			d.startRecovery(ctx, command, &wg)
			continue
		}
		if assignment == nil {
			continue
		}
		// a delivery of an attempt this process has claimed — running, or
		// waiting for a slot — under any spelling of its atom, is that
		// attempt delivered again, verified or not: nothing is said to the
		// ship about it, since an abandon would hand the attempt to another
		// runner while it runs here (INTEGRATION.md §11.13)
		if d.claimed(assignment.Attempt) {
			d.log.Printf("assignment %s for attempt %s is already running here; ignored", assignment.ID, assignment.Attempt)
			continue
		}
		// an attempt taken here is never run again (INTEGRATION.md §11.14):
		// one finished here, or given back to the ship, is ignored, verified
		// or not — its answer was given; the rest is decided once verified
		hist, herr := d.historyOf(assignment.Attempt)
		if herr == nil && hist == takenAnswered {
			d.log.Printf("assignment %s for attempt %s: the attempt was taken here already, and answered; ignored (an assignment runs at most once here)", assignment.ID, assignment.Attempt)
			continue
		}
		// no new work on accounting a crash could lose (§5): while a
		// retention is not in the state file, whatever is offered goes
		// back to the ship with the reason
		if err := d.retrySave(); err != nil {
			d.log.Printf("[%s %s] assignment %s not started: STATE NOT DURABLE: %v", assignment.Kind, assignment.Attempt, assignment.ID, err)
			d.abandon(assignment, "runner state not durable: "+err.Error()+"; no new work until its state file can be written")
			continue
		}
		// an assignment the ship did not sign, or signed with a key other
		// than the pinned one, or whose manifest this runner cannot honour
		// (a VM-required attempt on a container runner, a network scope
		// beyond its profile) is refused before any work (D5, M10): the
		// reason is logged and the attempt abandoned with it
		if err := d.verifyAssignment(assignment); err != nil {
			d.log.Printf("[%s %s] assignment %s refused: %v; no work", assignment.Kind, assignment.Attempt, assignment.ID, err)
			d.refuse(assignment, "assignment refused: "+err.Error())
			continue
		}
		if err := d.honourable(assignment); err != nil {
			d.log.Printf("[%s %s] assignment %s cannot run here: %v; abandoned", assignment.Kind, assignment.Attempt, assignment.ID, err)
			d.abandon(assignment, err.Error())
			continue
		}
		// a runner whose history is incomplete runs nothing until its
		// transition; one transitioned runs nothing its ship signed before
		// it (INTEGRATION.md §11.15)
		if reason := d.fenced(assignment); reason != "" {
			d.log.Printf("[%s %s] assignment %s: %s", assignment.Kind, assignment.Attempt, assignment.ID, reason)
			d.abandon(assignment, reason)
			continue
		}
		// what the execution ledger cannot answer, it does not guess; an
		// attempt an earlier life took, and a stop interrupted, is given
		// back, not run (INTEGRATION.md §11.14)
		if herr != nil {
			d.log.Printf("[%s %s] assignment %s not started: its execution ledger cannot be read: %v", assignment.Kind, assignment.Attempt, assignment.ID, herr)
			d.abandon(assignment, "not started: this runner cannot read its record of the attempts it has taken ("+herr.Error()+"); nothing that may have run here runs again")
			continue
		}
		if hist == takenInterrupted {
			d.log.Printf("[%s %s] assignment %s: the attempt was taken here before, and not finished; given back, not run again", assignment.Kind, assignment.Attempt, assignment.ID)
			d.abandon(assignment, interruptedReason(assignment.Attempt))
			continue
		}
		// the ship offers a delivered assignment again when its attempt
		// shows no activity; one this process is already running (or
		// waiting for a slot) is ignored
		if !d.claim(assignment.Attempt) {
			d.log.Printf("assignment %s for attempt %s is already running here; ignored", assignment.ID, assignment.Attempt)
			continue
		}
		wg.Add(1)
		go func(a *ship.Assignment) {
			defer wg.Done()
			if !d.acquire(workCtx) {
				d.release(a.Attempt)
				return
			}
			// an assignment that waited for its slot faces the same rule as
			// one just polled: no new work on unsaved accounting (§5)
			if err := d.retrySave(); err != nil {
				d.releaseSlot()
				d.log.Printf("[%s %s] assignment %s not started after its wait: STATE NOT DURABLE: %v", a.Kind, a.Attempt, a.ID, err)
				d.abandon(a, "runner state not durable: "+err.Error()+"; no new work until its state file can be written")
				d.release(a.Attempt)
				return
			}
			// taken, durably, before it crosses into execution; a record that
			// cannot be written leaves it unstarted, given back to the ship,
			// and not run in this life (INTEGRATION.md §11.14)
			if err := d.takeExecution(a); err != nil {
				d.releaseSlot()
				d.mu.Lock()
				d.givenBackLocked(a.Attempt)
				d.mu.Unlock()
				if errors.Is(err, state.ErrTaken) {
					// its record appeared after its delivery was checked: an
					// attempt taken before, never run again
					d.log.Printf("[%s %s] assignment %s: the attempt is in the execution ledger already; given back, not run again", a.Kind, a.Attempt, a.ID)
					d.abandon(a, interruptedReason(a.Attempt))
				} else {
					d.log.Printf("[%s %s] assignment %s not started: its execution record cannot be written: %v", a.Kind, a.Attempt, a.ID, err)
					d.abandon(a, "not started: this runner cannot record that it takes the attempt ("+err.Error()+"); it runs nothing it could not keep from running again")
				}
				d.release(a.Attempt)
				return
			}
			// a retention handle records passes the slot on to it; the
			// accounting is the retention's from here (§4)
			d.handle(workCtx, a)
			d.finishExecution(a)
			d.releaseSlot()
			d.release(a.Attempt)
		}(assignment)
	}
}

// verifyAssignment checks the ship's signature over the assignment
// against the pinned CI public key: version 2, the recipient this
// daemon, the attempt the assignment's, the operation "assign", the
// expiry in the future, and the whole execution manifest bound.
func (d *Daemon) verifyAssignment(a *ship.Assignment) error {
	if d.ciKey == "" {
		return errors.New("no CI public key pinned")
	}
	if a.Sig == nil || a.Sig.Sig == "" {
		return errors.New("assignment is unsigned")
	}
	if a.Sig.Version != sig.ManifestVersion {
		return fmt.Errorf("manifest version %d is not the version this runner speaks (%d)", a.Sig.Version, sig.ManifestVersion)
	}
	if a.Manifest == nil {
		return errors.New("assignment carries no execution manifest")
	}
	if a.Sig.Recipient != d.daemonID {
		return fmt.Errorf("signed for daemon %s, not this one", a.Sig.Recipient)
	}
	if uvKey(a.Sig.Attempt) != uvKey(a.Attempt) {
		return fmt.Errorf("signed for attempt %s, not %s", a.Sig.Attempt, a.Attempt)
	}
	if a.Sig.Operation != "assign" {
		return fmt.Errorf("signed for operation %q", a.Sig.Operation)
	}
	if a.Sig.Expiry <= time.Now().Unix() {
		return fmt.Errorf("authorization expired at %s", time.Unix(a.Sig.Expiry, 0).UTC().Format(time.RFC3339))
	}
	// the manifest's own fields must agree with the assignment's plain
	// ones: the signature covers the manifest, so a plain field that
	// differs is a tampered assignment
	if a.Manifest.Repo != a.Repo || uvKey(a.Manifest.Candidate) != uvKey(a.Candidate) || a.Manifest.OID != a.OID || a.Manifest.Workflow != a.Workflow || a.Manifest.Job != a.Job {
		return errors.New("assignment fields disagree with the signed manifest")
	}
	m := sig.ManifestMessage{Recipient: a.Sig.Recipient, Attempt: a.Sig.Attempt, Operation: a.Sig.Operation, Expiry: a.Sig.Expiry, Nonce: a.Sig.Nonce, Manifest: *a.Manifest}
	if err := sig.VerifyManifest(d.ciKey, m, a.Sig.Sig); err != nil {
		return err
	}
	// verified: from here the assignment is its atoms, in the one spelling
	// the ship writes — the claim, the sandbox, the launcher's record and
	// every message about it (§11.13). The signature and the manifest are
	// replaced by copies, never written through.
	sg, mf := *a.Sig, *a.Manifest
	a.Attempt = uvKey(a.Attempt)
	sg.Attempt = a.Attempt
	a.Candidate = uvKey(a.Candidate)
	mf.Candidate, mf.Incarnation = a.Candidate, uvKey(mf.Incarnation)
	a.Sig, a.Manifest = &sg, &mf
	d.log.Printf("[%s %s] assignment signature verified (manifest v%d, mode %s, sandbox %s, network %s, generation %d, nonce %s, expires %s)",
		a.Kind, a.Attempt, a.Sig.Version, a.Manifest.Mode, a.Manifest.Sandbox, a.Manifest.Network, a.Manifest.Generation, a.Sig.Nonce, time.Unix(a.Sig.Expiry, 0).UTC().Format(time.RFC3339))
	return nil
}

// honourable checks the verified manifest against what this runner is:
// a VM-required attempt never runs on the container backend (M10), a
// resolve only on a resolver, and a network profile only when the runner
// declares it and the ship's scope is inside the runner's ceiling.
func (d *Daemon) honourable(a *ship.Assignment) error {
	if a.Manifest == nil {
		return errors.New("no manifest")
	}
	if a.Kind == "resolve" {
		// host-side parsing and fetching: no sandbox is involved
		if !d.cfg.Resolver {
			return errors.New("resolve assignments need a resolver-capable runner (resolver = true)")
		}
		return nil
	}
	if a.Manifest.Sandbox == "vm" && d.box.Kind() != "microvm" {
		return fmt.Errorf("sandbox requirement vm; this runner is %s", d.box.Kind())
	}
	if _, err := d.effectiveScope(a); err != nil {
		return err
	}
	return nil
}

// effectiveScope is the ship-authorized scope intersected with this
// runner's declared ceiling for the profile. A ship destination the
// runner cannot offer is a refusal, never a silently narrower run.
func (d *Daemon) effectiveScope(a *ship.Assignment) ([]string, error) {
	profile := a.Manifest.Network
	if profile == "" || profile == "locked" {
		if a.Manifest.NetworkScope != "" {
			return nil, errors.New("a locked manifest names destinations")
		}
		return nil, nil
	}
	p, ok := d.cfg.Profile(profile)
	if !ok {
		return nil, fmt.Errorf("network profile %s is not declared by this runner", profile)
	}
	if a.Manifest.NetworkScope == "" {
		return nil, fmt.Errorf("network profile %s authorized with no destinations", profile)
	}
	ceiling := map[string]bool{}
	for _, dst := range p.Destinations {
		ceiling[dst] = true
	}
	var out []string
	for _, dst := range strings.Split(a.Manifest.NetworkScope, ",") {
		if !ceiling[dst] {
			return nil, fmt.Errorf("network profile %s: destination %s is outside this runner's ceiling", profile, dst)
		}
		out = append(out, dst)
	}
	return out, nil
}

// claim takes attempt for this process, or says it is taken already: one
// claim per atom, whatever spelling attempt comes in (INTEGRATION.md
// §11.13, "One assignment, one identity").
func (d *Daemon) claim(attempt string) bool {
	key := uvKey(attempt)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inFlight[key] {
		return false
	}
	d.inFlight[key] = true
	return true
}

func (d *Daemon) release(attempt string) {
	key := uvKey(attempt)
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inFlight, key)
}

// claimed says whether this process has claimed attempt (it is running
// it, or waiting for a slot for it), under any spelling of its atom.
func (d *Daemon) claimed(attempt string) bool {
	key := uvKey(attempt)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inFlight[key]
}

// uvKey is the key of the atom a @uv text names: its spelling as the ship
// writes it (sig.FormatUV), whatever spelling it came in. A text that is no
// @uv is its own key (§11.13; commandKey is a recovery command's, §11.12).
func uvKey(text string) string {
	if key, err := sig.CanonicalUV(text); err == nil {
		return key
	}
	return text
}

// remainingCapacity is the advertised capacity: the configured capacity
// less the retentions (INTEGRATION.md §4).
func (d *Daemon) remainingCapacity() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.capacity
}

func (d *Daemon) quarantinedNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.quarantined...)
}

// limit is the configured capacity.
func (d *Daemon) limit() int {
	if d.cfg == nil {
		return 0
	}
	return d.cfg.Capacity
}

// freeLocked is how many slots no retention, held orphan or running
// attempt occupies (under mu): an admission a running Prepare covers is
// that attempt's slot, counted once (withheldLocked).
func (d *Daemon) freeLocked() int {
	return d.limit() - d.withheldLocked() - len(d.held) - d.running
}

func (d *Daemon) signalLocked() {
	if d.slotFreed == nil {
		d.slotFreed = sync.NewCond(&d.mu)
	}
	d.slotFreed.Broadcast()
}

// acquire waits for a free slot, or ctx's end, and takes it.
func (d *Daemon) acquire(ctx context.Context) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.slotFreed == nil {
		d.slotFreed = sync.NewCond(&d.mu)
	}
	stop := context.AfterFunc(ctx, func() {
		d.mu.Lock()
		d.slotFreed.Broadcast()
		d.mu.Unlock()
	})
	defer stop()
	for d.freeLocked() <= 0 {
		if ctx.Err() != nil {
			return false
		}
		d.slotFreed.Wait()
	}
	d.running++
	return true
}

// releaseSlot gives an attempt's slot back: to the free slots, or — when
// its sandbox was retained — to the retention, which counts it from then.
func (d *Daemon) releaseSlot() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.running--
	d.signalLocked()
}

// hold withholds an orphan's slot while the backend holds it (§4); the
// advertised capacity is unchanged: the ship counts the attempts it
// believes run here, and it can have no other ones in mind.
func (d *Daemon) hold(h sandbox.Handle, why string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held == nil {
		d.held = map[string]sandbox.Handle{}
	}
	if _, ok := d.held[h.ID]; !ok {
		d.log.Printf("reconcile %s: %s; its slot is withheld while the %s backend holds it (%s)", h.ID, why, d.box.Kind(), sandbox.Describe(h))
	}
	d.held[h.ID] = h
}

func (d *Daemon) unhold(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.held[id]; ok {
		delete(d.held, id)
		d.signalLocked()
	}
}

// retention is the state file's record of h, retained for why.
func (d *Daemon) retention(h sandbox.Handle, why string) state.Quarantine {
	q := state.Quarantine{Handle: h.ID, Reason: why, At: time.Now().Unix(), Backend: d.box.Kind(), Attempt: h.Attempt, Label: h.Label}
	if q.Attempt == "" {
		q.Attempt = strings.TrimPrefix(h.ID, "ci-")
	}
	if q.Backend == "microvm" {
		q.VM, q.Incarnation, q.CID, q.Created = h.VM, h.Incarnation, h.CID, h.Created
	} else {
		q.Network, q.Volume, q.Container = h.Network, h.Volume, h.Container
	}
	return q
}

// retainedAs is the retention recorded for h's identity, if any; one that
// lacked h's identity is completed with it (and saved).
func (d *Daemon) retainedAs(h sandbox.Handle) (state.Quarantine, bool) {
	q := d.retention(h, "")
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, have := range d.retained {
		if have.Same(q) {
			if done := have.CompletedBy(q); !reflect.DeepEqual(done, have) {
				d.retained[i] = done
				d.persistLocked()
			}
			return d.retained[i], true
		}
	}
	return state.Quarantine{}, false
}

// retain withholds a slot for h because the backend may still hold what h
// names and its release is unproven — in memory at once, then in the state
// file (§§4–5). A retention already recorded for h's identity is not
// counted again. It reports whether the retention is new.
func (d *Daemon) retain(h sandbox.Handle, why string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.retainLocked(h, why)
}

// retainLocked is retain under mu.
func (d *Daemon) retainLocked(h sandbox.Handle, why string) bool {
	q := d.retention(h, why)
	for i, have := range d.retained {
		if have.Same(q) {
			d.retained[i] = have.CompletedBy(q)
			d.log.Printf("QUARANTINED slot %s is withheld already (%s); not charged again: %s", h.ID, describeRetention(d.retained[i]), why)
			d.persistLocked()
			return false
		}
	}
	d.retained = append(d.retained, q)
	d.quarantined = append(d.quarantined, h.ID)
	d.recountLocked()
	// the ship hears it on the next poll (Run polls with the capacity as
	// it stands then; the shared client is never written after New)
	d.log.Printf("QUARANTINED slot %s (%s): %s; advertised capacity now %d", h.ID, describeRetention(q), why, d.capacity)
	d.persistLocked()
	return true
}

// persistLocked saves the retentions memory holds (under mu), with every
// recovery refusal due: those recorded, those decided and not recorded yet,
// and a release whose save was uncertain — not applied, once this save of
// memory's view succeeds (INTEGRATION.md §11.12). A failure is surfaced —
// logged, and kept in unsaved until a retry succeeds, while no new work
// starts (§5); the retentions stay withheld either way.
func (d *Daemon) persistLocked() {
	if d.st == nil || d.stateFile == "" {
		return
	}
	d.st.Quarantined = append([]state.Quarantine(nil), d.retained...)
	next := *d.st
	next.Refused = d.refusalsDueLocked()
	save := d.saveState
	if save == nil {
		save = state.Save
	}
	// any error is not durable, whatever the file may hold now (a save that
	// renamed and then failed is proven by the next successful one only)
	err := save(d.stateFile, &next)
	switch {
	case err != nil && d.unsaved == nil:
		d.log.Printf("STATE NOT DURABLE: %v; %d retention(s) held in memory; no new work until the state file can be written", err, len(d.retained))
	case err == nil && d.unsaved != nil:
		d.log.Printf("state file durable again: %d retention(s) recorded", len(d.retained))
	}
	d.unsaved = err
	if err == nil {
		// the file holds memory's view and every refusal due: each is
		// answered now, from its record
		d.st.Format = next.Format
		d.settleLocked(next.Refused)
	}
}

// retrySave saves again when the state file is behind memory, and says
// whether it still is.
func (d *Daemon) retrySave() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.unsaved != nil {
		d.persistLocked()
	}
	return d.unsaved
}

// refuse gives a refused assignment back to the ship whatever its kind
// (CI-DELIVERY-1.1 b): a refusal over the pinned key is an abandon, never
// a plan error — the ship de-lists this daemon and re-offers the attempt.
func (d *Daemon) refuse(a *ship.Assignment, reason string) {
	d.abandon(a, reason)
}

// abandon tells the ship no result is coming for an assignment this
// daemon did not start (a manifest it cannot honour); the ship re-offers
// it on another runner or fails it with the reason.
func (d *Daemon) abandon(a *ship.Assignment, reason string) {
	d.log.Printf("[%s %s] no result: %s", a.Kind, a.Attempt, reason)
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := d.client.Abandon(rctx, a.Attempt, reason)
	if err != nil {
		d.log.Printf("[%s %s] abandon POST failed: %v", a.Kind, a.Attempt, err)
		return
	}
	d.log.Printf("[%s %s] abandon POST -> %s", a.Kind, a.Attempt, resp.Error())
}
