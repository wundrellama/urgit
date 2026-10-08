package daemon

// The operator's release of a legacy retention from Urgit (legacy-recovery
// UI ruling 01; QUESTIONS-SOURCE-01 §11; runner/launcher/INTEGRATION.md
// §11.12). The daemon — the owner of its state file — reports every
// retention to the ship, with a legacy retention's evidence; receives the
// operator's command on its own poll; checks the command and everything the
// release stands on again; releases the entry durably, and only then
// returns its slot; and answers. One operation, three bound parameters,
// nothing else: it never stops, destroys, retries or reserves anything.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// recoveryOperation is the one operation a recovery command may name.
const recoveryOperation = "release-legacy"

// recoveryBound bounds each launcher inventory and each post to the ship,
// as the daemon's abandon and the operator's launcher asks are bounded.
const recoveryBound = 30 * time.Second

// A report lists at most maxReported retentions and maxReportedReleased
// released ones, each text at most maxReportedText bytes.
const (
	maxReported         = 64
	maxReportedReleased = 16
	maxReportedText     = 512
)

// legacyJudgement is what a legacy release stands on: each condition, met
// or not, and why; the facts the launcher answered; and the digest of the
// evidence, which a command must be bound to.
type legacyJudgement struct {
	conditions []ship.Condition
	facts      *ship.Facts
	eligible   bool
	evidence   string
}

func (j legacyJudgement) unmet() string {
	var out []string
	for _, c := range j.conditions {
		if !c.Met {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return strings.Join(out, "; ")
}

// attemptOf is the attempt a retention withholds a slot for.
func attemptOf(q state.Quarantine) string {
	if q.Attempt != "" {
		return q.Attempt
	}
	return strings.TrimPrefix(q.Handle, "ci-")
}

// legacyMark is a retention's legacy mark as the operator reads it, and
// says so when it has none.
func legacyMark(q state.Quarantine) string {
	if q.Legacy == nil {
		return "no legacy mark"
	}
	return fmt.Sprintf("found in a state file an earlier runner wrote last (format %d), first loaded by this version at %s", q.Legacy.Format, when(q.Legacy.Found))
}

func when(unix int64) string {
	if unix == 0 {
		return "(not recorded)"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// judgeLegacy says whether q may be released as a legacy retention on inv
// (or invErr: no inventory), its attempt claimed by this process or not
// (INTEGRATION.md §11.12). The report and a command's check both ask it,
// each with an inventory of its own.
func judgeLegacy(q state.Quarantine, sandboxKind string, inv sandbox.Inventory, invErr error, claimed bool) legacyJudgement {
	var j legacyJudgement
	add := func(name string, met bool, detail string) {
		j.conditions = append(j.conditions, ship.Condition{Name: name, Met: met, Detail: detail})
	}
	switch {
	case q.IsLegacy():
		add("provenance", true, legacyMark(q)+"; it names neither a reserve request nor a launcher identity, which only a runner before settled admission recorded")
	case q.LegacyShape():
		add("provenance", false, "it names neither a reserve request nor a launcher identity, but it was not in a state file an earlier runner wrote: it is not a legacy entry, and a missing request proves nothing")
	default:
		add("provenance", false, "it is not a legacy entry: "+describeRetention(q))
	}
	add("backend", sandboxKind == "microvm", fmt.Sprintf("a launcher reservation of this runner, whose sandbox is %s", sandboxKind))
	attempt := attemptOf(q)
	j.facts = &ship.Facts{Socket: inv.Socket, Launcher: inv.Launcher, Protocol: inv.Protocol, Records: len(inv.Records), Held: []string{}}
	var held []string
	if invErr != nil {
		j.facts.Error = invErr.Error()
		add("protocol", false, "not known: the launcher answered no inventory")
		add("inventory", false, "the launcher answered no authoritative inventory: "+invErr.Error())
		add("attempt", false, "not known: the launcher answered no inventory")
	} else {
		add("protocol", inv.Protocol == launcher.WireProtocol, fmt.Sprintf("the launcher at %s (%s) speaks wire protocol %d; protocol %d refuses every reserve that names no request token — every copy of this entry's request", inv.Socket, inv.Launcher, inv.Protocol, launcher.WireProtocol))
		add("inventory", true, fmt.Sprintf("the launcher answered its list, which it answers only when its records directory is certified and every entry is accounted for: %d record(s) of this runner", len(inv.Records)))
		for _, r := range inv.Records {
			// a record of the attempt under any spelling of its atom is its
			// record (INTEGRATION.md §11.13)
			if uvKey(r.Attempt) == uvKey(attempt) {
				held = append(held, fmt.Sprintf("%s (%s)", r.Ref(), r.State))
			}
		}
		sort.Strings(held)
		j.facts.Held = append(j.facts.Held, held...)
		if len(held) == 0 {
			add("attempt", true, "the launcher holds no record of attempt "+attempt+", in any state")
		} else {
			add("attempt", false, "the launcher holds "+strings.Join(held, ", ")+" of attempt "+attempt+": that reservation follows its own disposition — the daemon's reconcile, or the launcher operator's recover for an incident")
		}
	}
	if claimed {
		add("idle", false, "attempt "+attempt+" is running, or waiting for a slot, in this runner now")
	} else {
		add("idle", true, "attempt "+attempt+" is not running in this runner")
	}
	j.eligible = true
	for _, c := range j.conditions {
		j.eligible = j.eligible && c.Met
	}
	if invErr == nil {
		j.evidence = legacyDigest(q, attempt, inv, held)
	}
	return j
}

// legacyDigest is the digest of the evidence a legacy release stands on:
// the entry exactly (its selection, revision included), its mark, the
// launcher asked and its protocol, the inventory's authority, and what it
// holds of the attempt. A change in any of them is another digest.
func legacyDigest(q state.Quarantine, attempt string, inv sandbox.Inventory, held []string) string {
	var b strings.Builder
	b.WriteString("urgit legacy release evidence 1\n")
	fmt.Fprintf(&b, "selection %s\n", q.Selection())
	if q.Legacy != nil {
		fmt.Fprintf(&b, "legacy %d %d\n", q.Legacy.Format, q.Legacy.Found)
	} else {
		b.WriteString("legacy none\n")
	}
	fmt.Fprintf(&b, "launcher %s protocol %d\n", inv.Socket, inv.Protocol)
	b.WriteString("inventory authoritative\n")
	holds := "none"
	if len(held) > 0 {
		holds = strings.Join(held, ";")
	}
	fmt.Fprintf(&b, "attempt %s held %s\n", attempt, holds)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// kindOf is what a retention is to the operator, how its slot returns, and
// why.
func kindOf(q state.Quarantine) (kind, release, explanation string) {
	switch {
	case q.IsLegacy():
		return "legacy", "urgit", "recorded without its reserve request by a runner before settled admission: released from Urgit, by this daemon, on its evidence taken again when the command runs (QUESTIONS-SOURCE-01 §11)"
	case q.Admission():
		return "admission", "settlement", "its reserve request " + q.Request + " is settled by the launcher at every reconcile; its slot returns by itself once it is, and nothing here releases it"
	case q.LegacyShape():
		return "unmarked", "none", "it names neither a reserve request nor a launcher identity, but it was not in a state file an earlier runner wrote: not a legacy entry, so nothing can settle it, and it stays withheld"
	case q.Backend == "microvm":
		return "vm", "cli", "released with urgit-runner -recover, the daemon stopped, once the launcher proves this incarnation's release; an incident's cleanup and release are the launcher operator's (urgit-vm-launcher recover)"
	case q.Backend == "docker-rootless":
		return "docker", "cli", "its cleanup retried, and its release made, with urgit-runner -recover, the daemon stopped, once none of its objects remain"
	}
	return "unknown-backend", "none", "recorded before retentions named their backend (the handle only): it may be a Docker sandbox of an earlier configuration, and no launcher inventory can prove it released; it stays withheld (QUESTIONS-SOURCE-01 §11)"
}

func clip(s string) string {
	if len(s) <= maxReportedText {
		return s
	}
	return s[:maxReportedText] + "…"
}

// capacityLocked is the runner's slots now (under mu).
func (d *Daemon) capacityLocked() ship.ReportCapacity {
	return ship.ReportCapacity{Configured: d.limit(), Withheld: d.withheldLocked(), Held: len(d.held), Running: d.running, Advertised: d.capacity}
}

// inventory is the launcher's answer for a legacy release, bounded.
func (d *Daemon) inventory(ctx context.Context) (sandbox.Inventory, error) {
	inv, ok := d.box.(sandbox.Inventorier)
	if !ok {
		return sandbox.Inventory{}, fmt.Errorf("this runner's %s sandbox keeps no launcher inventory", d.box.Kind())
	}
	ictx, cancel := context.WithTimeout(ctx, recoveryBound)
	defer cancel()
	return inv.Inventory(ictx)
}

// retentionReport is every retention this daemon withholds, how its slot
// returns, and a legacy retention's evidence, taken now.
func (d *Daemon) retentionReport(ctx context.Context) ship.RetentionReport {
	d.mu.Lock()
	qs := slices.Clone(d.retained)
	var gone []state.Quarantine
	if d.st != nil {
		gone = slices.Clone(d.st.Released)
	}
	claimed := map[string]bool{}
	for a := range d.inFlight {
		claimed[a] = true
	}
	r := ship.RetentionReport{Version: 1, At: time.Now().Unix(), Sandbox: d.box.Kind(), Capacity: d.capacityLocked(), Retentions: []ship.RetentionEntry{}, Released: []ship.ReleasedEntry{}}
	d.mu.Unlock()
	var inv sandbox.Inventory
	var invErr error
	asked := false
	for i, q := range qs {
		if i == maxReported {
			r.Truncated = len(qs) - maxReported
			break
		}
		kind, release, why := kindOf(q)
		// its attempt as the ship writes it, whatever spelling the record was
		// written under (§11.13); the record, and its evidence, as written
		e := ship.RetentionEntry{Selection: q.Selection(), Revision: q.Rev, Handle: q.Handle, Attempt: uvKey(attemptOf(q)), Label: clip(q.Label), Backend: q.Backend,
			Kind: kind, Identity: clip(describeRetention(q)), Reason: clip(q.Reason), Retained: q.At, Release: release, Explanation: why}
		if q.Legacy != nil {
			e.Legacy = &ship.LegacyMark{Format: q.Legacy.Format, Found: q.Legacy.Found}
		}
		if q.IsLegacy() {
			if !asked {
				inv, invErr = d.inventory(ctx)
				asked = true
			}
			j := judgeLegacy(q, d.box.Kind(), inv, invErr, claimed[uvKey(attemptOf(q))])
			e.Eligible, e.Evidence, e.Conditions, e.Facts = j.eligible, j.evidence, j.conditions, j.facts
			for k := range e.Conditions {
				e.Conditions[k].Detail = clip(e.Conditions[k].Detail)
			}
		}
		r.Retentions = append(r.Retentions, e)
	}
	if len(gone) > maxReportedReleased {
		gone = gone[len(gone)-maxReportedReleased:]
	}
	// the runner's execution history, and whether it waits for its
	// transition (INTEGRATION.md §11.15)
	r.History = d.historyReport()
	for _, q := range gone {
		// its command as the ship writes it, whatever spelling the record was
		// written under (§11.12, "One command, one identity"); none stays none
		r.Released = append(r.Released, ship.ReleasedEntry{Selection: q.Selection(), Handle: q.Handle, Attempt: uvKey(attemptOf(q)), Label: clip(q.Label), ReleasedAt: q.ReleasedAt, Command: commandKey(q.Command), Evidence: q.Evidence})
	}
	return r
}

// report posts the retention report. A ship without the recovery channel,
// or one that cannot be reached, is logged once per change; nothing waits
// for it.
func (d *Daemon) report(ctx context.Context) {
	r := d.retentionReport(ctx)
	pctx, cancel := context.WithTimeout(ctx, recoveryBound)
	defer cancel()
	err := d.client.PostRetentions(pctx, d.daemonID, r)
	d.mu.Lock()
	defer d.mu.Unlock()
	text := ""
	if err != nil {
		text = err.Error()
	}
	if text != d.reportErr {
		switch {
		case errors.Is(err, ship.ErrNoRecovery):
			d.log.Printf("retention report: the ship has no recovery channel: a legacy retention cannot be released from Urgit until it has one")
		case err != nil:
			d.log.Printf("retention report: %v; sent again at the next reconcile", err)
		default:
			d.log.Printf("retention report: the ship has this runner's %d retention(s)", len(r.Retentions))
		}
	}
	d.reportErr = text
}

// commandKey is a recovery command's identity: the canonical spelling of
// the atom its id names (sig.FormatUV), whatever spelling it came in — the
// signature binds the atom (INTEGRATION.md §11.12, "One command, one
// identity"). An id that is no @uv is its own text; no such command
// verifies.
func commandKey(id string) string {
	if key, err := sig.CanonicalUV(id); err == nil {
		return key
	}
	return id
}

// startRecovery carries cmd out on a goroutine of its own, and answers it;
// a second delivery of a command in progress, under any spelling, is
// ignored (its answer comes).
func (d *Daemon) startRecovery(ctx context.Context, cmd *ship.RecoveryCommand, wg *sync.WaitGroup) {
	key := commandKey(cmd.ID)
	d.mu.Lock()
	if d.recovering == nil {
		d.recovering = map[string]bool{}
	}
	if d.recovering[key] {
		d.mu.Unlock()
		d.log.Printf("recovery command %s is being carried out already; this delivery is ignored", key)
		return
	}
	d.recovering[key] = true
	d.mu.Unlock()
	wg.Add(1)
	go func() {
		defer wg.Done()
		answer := d.recover(ctx, cmd)
		d.mu.Lock()
		delete(d.recovering, key)
		d.queueAnswerLocked(answer)
		d.mu.Unlock()
		d.flushAnswers(ctx)
		d.report(ctx)
	}()
}

// verifyRecovery checks the ship's signature over cmd against the pinned CI
// public key, for this daemon and the operations it carries out: a legacy
// release, and a history transition (history.go).
func (d *Daemon) verifyRecovery(cmd *ship.RecoveryCommand) error {
	if d.ciKey == "" {
		return errors.New("no CI public key pinned")
	}
	if cmd.Sig == "" {
		return errors.New("the command is unsigned")
	}
	if cmd.Version != sig.RecoveryVersion {
		return fmt.Errorf("recovery message version %d is not the version this runner speaks (%d)", cmd.Version, sig.RecoveryVersion)
	}
	if cmd.Recipient != d.daemonID {
		return fmt.Errorf("signed for daemon %s, not this one", cmd.Recipient)
	}
	if cmd.Operation != recoveryOperation && cmd.Operation != historyOperation {
		return fmt.Errorf("operation %q is not one this runner carries out", cmd.Operation)
	}
	if i := strings.LastIndex(cmd.Selection, "/"); i < 0 || cmd.Selection[i+1:] != strconv.FormatUint(cmd.Revision, 10) {
		return fmt.Errorf("its selection %q does not name its revision %d", cmd.Selection, cmd.Revision)
	}
	m := sig.RecoveryMessage{Recipient: cmd.Recipient, Command: cmd.ID, Operation: cmd.Operation, Expiry: cmd.Expiry, Nonce: cmd.Nonce,
		Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence}
	return sig.VerifyRecovery(d.ciKey, m, cmd.Sig)
}

// recover carries a recovery command out, one at a time, and says its
// answer (INTEGRATION.md §11.12): refused unless it is what the ship signed
// for this daemon, in time, for exactly a legacy retention as inspected,
// its attempt idle here, and the launcher's evidence, taken now, the
// evidence confirmed; then released in the state file, stamped with the
// command and the evidence, and only once that is durable is the slot
// returned. Every final answer is the state file's ("Every final answer is
// recorded first"): a command it records a decision of is answered from
// that record — never judged again, in this life or after a restart — and
// a refusal is recorded before it is answered. A message this daemon
// cannot authenticate decides nothing and is recorded nowhere.
func (d *Daemon) recover(ctx context.Context, cmd *ship.RecoveryCommand) ship.RecoveryAnswer {
	d.recoveryMu.Lock()
	defer d.recoveryMu.Unlock()
	if err := d.verifyRecovery(cmd); err != nil {
		// no command of this daemon's: nothing of it is recorded — no
		// unauthenticated input fills the state file — and nothing final is
		// said of its id; the genuine command is decided on its own
		d.log.Printf("RECOVERY %s not authenticated: %v; nothing done, decided or recorded", cmd.ID, err)
		return ship.RecoveryAnswer{Command: commandKey(cmd.ID), Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence, Status: ship.AnswerUncertain,
			Detail: fmt.Sprintf("not carried out, and nothing decided: this runner cannot authenticate it (%v); nothing is released or recorded, and the slot stays withheld", err)}
	}
	// verified: from here the command is its atom, under the one spelling
	// the ship writes it in — every key, record and answer (§11.12, "One
	// command, one identity")
	canon := *cmd
	canon.ID = commandKey(cmd.ID)
	cmd = &canon
	d.mu.Lock()
	if a, ok := d.decisionLocked(cmd); ok {
		if _, pending := d.unrecorded[cmd.ID]; pending {
			// a refusal not recorded yet: record it now if the state file
			// takes it, never judge the command again
			d.persistLocked()
			a, _ = d.decisionLocked(cmd)
		}
		d.mu.Unlock()
		return a
	}
	d.mu.Unlock()
	refuse := func(found, format string, args ...any) ship.RecoveryAnswer {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.refuseLocked(cmd, found, fmt.Sprintf(format, args...))
	}
	if cmd.Expiry <= time.Now().Unix() {
		return refuse("", "it expired at %s before it was carried out", when(cmd.Expiry))
	}
	// the owner's transition of this runner's history (INTEGRATION.md
	// §11.15)
	if cmd.Operation == historyOperation {
		return d.confirmHistory(cmd)
	}
	q, why := d.inspected(cmd)
	if why != "" {
		return refuse("", "%s", why)
	}
	inv, invErr := d.inventory(ctx)
	j := judgeLegacy(q, d.box.Kind(), inv, invErr, d.claimed(attemptOf(q)))
	if !j.eligible {
		return refuse(j.evidence, "its release is not proven now: %s", j.unmet())
	}
	if j.evidence != cmd.Evidence {
		return refuse(j.evidence, "the evidence changed since it was inspected (now %s, confirmed %s): inspect it again", short(j.evidence), short(cmd.Evidence))
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.releaseLocked(cmd, j)
}

// decisionLocked is what this daemon holds of cmd, if anything (under mu):
// a released entry naming it, completed; its recorded refusal, refused with
// the recorded reason; a refusal not recorded yet, or a release whose save
// was uncertain, uncertain — neither of them final.
func (d *Daemon) decisionLocked(cmd *ship.RecoveryCommand) (ship.RecoveryAnswer, bool) {
	answer := ship.RecoveryAnswer{Command: cmd.ID, Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence}
	if d.st != nil {
		// the records are found by the command's atom, whatever spelling
		// they were written under
		// only a record that is a transition answers the command it names
		// (INTEGRATION.md §11.16)
		if tr := d.recordedTransitionLocked(); tr != nil && commandKey(tr.Command) == cmd.ID {
			answer.Status = ship.AnswerCompleted
			capacity := d.capacityLocked()
			answer.Capacity = &capacity
			answer.Detail = fmt.Sprintf("recorded at %s by this command (its state file says so): the transition to authorization epoch %d", when(tr.At), tr.Epoch)
			return answer, true
		}
		if q, ok := d.st.ReleaseOf(cmd.ID); ok {
			answer.Status, answer.ReleasedAt = ship.AnswerCompleted, q.ReleasedAt
			capacity := d.capacityLocked()
			answer.Capacity = &capacity
			answer.Detail = fmt.Sprintf("released at %s by this command (its state file says so); advertised capacity now %d", when(q.ReleasedAt), d.capacity)
			return answer, true
		}
		if r, ok := d.st.RefusalOf(cmd.ID); ok {
			answer.Status, answer.Detail = ship.AnswerRefused, r.Reason
			if r.Selection != cmd.Selection || r.Revision != cmd.Revision || r.Evidence != cmd.Evidence {
				// one id, signed twice for different bindings: its recorded
				// refusal stands for the id, whatever it names now
				answer.Detail += fmt.Sprintf(" (recorded for %s revision %d; this delivery names %s revision %d: a new confirmation is a new command)", r.Selection, r.Revision, cmd.Selection, cmd.Revision)
			}
			return answer, true
		}
	}
	if r, ok := d.unrecorded[cmd.ID]; ok {
		why := "this daemon keeps no state file"
		if d.unsaved != nil {
			why = d.unsaved.Error()
		}
		answer.Status = ship.AnswerUncertain
		answer.Detail = fmt.Sprintf("refused (%s), but the refusal is not recorded durably yet (%s): nothing is released, the slot stays withheld, and the answer follows once the state file is durable again", r.Reason, why)
		return answer, true
	}
	if doubt, ok := d.doubtful[cmd.ID]; ok {
		return doubt, true
	}
	return ship.RecoveryAnswer{}, false
}

// refuseLocked refuses cmd for reason (under mu), found the evidence taken
// now if the launcher answered: the refusal is recorded in the state file
// first, with memory's view, and answered refused only once that save is
// durable. Until then it is answered uncertain and kept, and the next save
// that succeeds records it (persistLocked; Run saves again at every turn
// while the state is not durable).
func (d *Daemon) refuseLocked(cmd *ship.RecoveryCommand, found, reason string) ship.RecoveryAnswer {
	d.log.Printf("RECOVERY %s refused: %s; nothing released, the slot stays withheld", cmd.ID, reason)
	if d.unrecorded == nil {
		d.unrecorded = map[string]state.Refusal{}
	}
	d.unrecorded[cmd.ID] = state.Refusal{Command: cmd.ID, Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence, Found: found, Reason: reason, At: time.Now().Unix()}
	d.persistLocked()
	a, _ := d.decisionLocked(cmd)
	return a
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// inspected is the retention cmd names exactly — its selection, the
// revision the operator inspected included — when it is a legacy
// retention; else why not.
func (d *Daemon) inspected(cmd *ship.RecoveryCommand) (state.Quarantine, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, why := d.findLocked(cmd.Selection)
	if why != "" {
		return state.Quarantine{}, why
	}
	q := d.retained[i]
	if !q.IsLegacy() {
		_, _, explanation := kindOf(q)
		return state.Quarantine{}, "it is not a legacy retention: " + explanation
	}
	return q, ""
}

// findLocked is the index of the retention sel names exactly, or why none
// does (under mu): changed since it was inspected, or gone.
func (d *Daemon) findLocked(sel string) (int, string) {
	for i, q := range d.retained {
		if q.Selection() == sel {
			return i, ""
		}
	}
	id := sel
	if i := strings.LastIndex(sel, "/"); i >= 0 {
		id = sel[:i+1]
	}
	for _, q := range d.retained {
		if cur := q.Selection(); strings.HasPrefix(cur, id) {
			return -1, fmt.Sprintf("it changed since it was inspected (now %s): inspect it again", cur)
		}
	}
	return -1, "no retention " + sel + " is withheld now: released, or replaced by another"
}

// releaseLocked releases the retention cmd names, whose evidence j is
// (under mu): the entry is checked again, the state file saved with it
// released — stamped with the command and the evidence, and with every
// refusal due — and only once that save is durable does the entry leave
// memory and its slot return. A save that failed before its rename changes
// nothing (refused, the refusal recorded first); one whose outcome is
// uncertain answers uncertain, returns no slot, and leaves the state not
// durable until a save of memory's view succeeds, which records the
// command refused, not applied, and only then answers it (settleLocked).
func (d *Daemon) releaseLocked(cmd *ship.RecoveryCommand, j legacyJudgement) ship.RecoveryAnswer {
	answer := ship.RecoveryAnswer{Command: cmd.ID, Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence}
	refuse := func(format string, args ...any) ship.RecoveryAnswer {
		return d.refuseLocked(cmd, j.evidence, fmt.Sprintf(format, args...))
	}
	i, why := d.findLocked(cmd.Selection)
	if why != "" {
		return refuse("while its evidence was taken, %s", why)
	}
	q := d.retained[i]
	if !q.IsLegacy() {
		return refuse("while its evidence was taken, it stopped being a legacy retention")
	}
	if d.inFlight[uvKey(attemptOf(q))] {
		return refuse("while its evidence was taken, attempt %s began running here", attemptOf(q))
	}
	if d.st == nil || d.stateFile == "" {
		return refuse("this daemon keeps no state file")
	}
	now := time.Now().Unix()
	rel := q
	rel.ReleasedAt, rel.Command, rel.Evidence = now, cmd.ID, j.evidence
	next := *d.st
	next.Quarantined = slices.Delete(slices.Clone(d.retained), i, i+1)
	next.Released = append(slices.Clone(d.st.Released), rel)
	next.Refused = d.refusalsDueLocked()
	save := d.saveState
	if save == nil {
		save = state.Save
	}
	if err := save(d.stateFile, &next); err != nil {
		var se *state.SaveError
		if errors.As(err, &se) && se.Uncertain {
			// the file may hold the release; memory holds the entry, and is
			// written back before anything else counts (§5)
			d.unsaved = err
			answer.Status = ship.AnswerUncertain
			answer.Detail = fmt.Sprintf("its release may or may not be recorded (%v): the slot stays withheld, no new work starts, and the answer follows once the state file is durable again", err)
			if d.doubtful == nil {
				d.doubtful = map[string]ship.RecoveryAnswer{}
			}
			d.doubtful[cmd.ID] = answer
			d.log.Printf("RECOVERY %s UNCERTAIN: %s", cmd.ID, answer.Detail)
			return answer
		}
		return refuse("its release could not be saved (%v)", err)
	}
	// durable: the release takes effect
	d.st.Format, d.st.Quarantined, d.st.Released = next.Format, next.Quarantined, next.Released
	d.retained = slices.Clone(next.Quarantined)
	if k := slices.Index(d.quarantined, q.Handle); k >= 0 {
		d.quarantined = slices.Delete(slices.Clone(d.quarantined), k, k+1)
	}
	d.recountLocked()
	d.signalLocked()
	if d.unsaved != nil {
		d.log.Printf("state file durable again: %d retention(s) recorded", len(d.retained))
		d.unsaved = nil
	}
	d.settleLocked(next.Refused)
	capacity := d.capacityLocked()
	answer.Status, answer.ReleasedAt, answer.Capacity = ship.AnswerCompleted, now, &capacity
	answer.Detail = fmt.Sprintf("released %s (%s) from Urgit's command, on evidence %s: its slot returns; advertised capacity now %d", q.Handle, labelText(q), short(j.evidence), d.capacity)
	d.log.Printf("RECOVERY %s: %s", cmd.ID, answer.Detail)
	return answer
}

func labelText(q state.Quarantine) string {
	if q.Label != "" {
		return q.Label
	}
	return "no job label"
}

// notApplied is the recorded reason of a release whose save was uncertain,
// once a save of memory's view — the entry still withheld — has succeeded;
// notTransitioned, of a transition's (history.go).
const (
	notApplied      = "not applied: its release's save was uncertain, and the state file now holds the retention again, withheld; confirm the release again to retry it"
	notTransitioned = "not applied: its transition's save was uncertain, and the state file now holds no transition: this runner keeps waiting; confirm its transition again to retry it"
)

// refusalsDueLocked is what the next save records of the refusals (under
// mu): those recorded; those decided and not recorded yet; and one for each
// release whose save was uncertain — the save of memory's view that records
// it is the one proving the release absent, so not applied.
func (d *Daemon) refusalsDueLocked() []state.Refusal {
	var out []state.Refusal
	if d.st != nil {
		out = slices.Clone(d.st.Refused)
	}
	pending := make([]string, 0, len(d.unrecorded))
	for id := range d.unrecorded {
		pending = append(pending, id)
	}
	sort.Strings(pending)
	for _, id := range pending {
		out = append(out, d.unrecorded[id])
	}
	doubts := make([]string, 0, len(d.doubtful))
	for id := range d.doubtful {
		doubts = append(doubts, id)
	}
	sort.Strings(doubts)
	now := time.Now().Unix()
	for _, id := range doubts {
		a := d.doubtful[id]
		reason := notApplied
		if strings.HasPrefix(a.Selection, "history/") {
			reason = notTransitioned
		}
		out = append(out, state.Refusal{Command: id, Selection: a.Selection, Revision: a.Revision, Evidence: a.Evidence, Found: a.Evidence, Reason: reason, At: now})
	}
	return out
}

// settleLocked takes the refusals a save that succeeded recorded (under
// mu): each one due — a refusal not recorded before, a release whose save
// was uncertain — is durable now, and answered refused from its record.
func (d *Daemon) settleLocked(saved []state.Refusal) {
	if d.st == nil {
		return
	}
	d.st.Refused = saved
	for _, r := range saved {
		key := commandKey(r.Command)
		_, pending := d.unrecorded[key]
		_, doubt := d.doubtful[key]
		if !pending && !doubt {
			continue
		}
		delete(d.unrecorded, key)
		delete(d.doubtful, key)
		d.queueAnswerLocked(ship.RecoveryAnswer{Command: key, Selection: r.Selection, Revision: r.Revision, Evidence: r.Evidence, Status: ship.AnswerRefused, Detail: r.Reason})
		d.log.Printf("RECOVERY %s refused, and recorded: %s", key, r.Reason)
	}
}

// isFinal says whether an answer is a final outcome: completed or refused.
func isFinal(status string) bool {
	return status == ship.AnswerCompleted || status == ship.AnswerRefused
}

// queueAnswerLocked keeps a command's latest answer until the ship takes
// it (under mu), keyed and named by the command's atom's one spelling: one
// command, one answer, whatever spellings its answers were made under. A
// final answer waiting for the ship is never replaced by a non-final one
// for the same command.
func (d *Daemon) queueAnswerLocked(a ship.RecoveryAnswer) {
	if d.outbox == nil {
		d.outbox = map[string]ship.RecoveryAnswer{}
	}
	a.Command = commandKey(a.Command)
	if cur, ok := d.outbox[a.Command]; ok && isFinal(cur.Status) && !isFinal(a.Status) {
		return
	}
	d.outbox[a.Command] = a
}

// flushAnswers posts every answer the ship has not taken yet; one it cannot
// take (no such command of this daemon) is dropped, one not delivered is
// posted again at the next turn.
func (d *Daemon) flushAnswers(ctx context.Context) {
	d.mu.Lock()
	pending := make([]ship.RecoveryAnswer, 0, len(d.outbox))
	for _, a := range d.outbox {
		pending = append(pending, a)
	}
	d.mu.Unlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].Command < pending[j].Command })
	for _, a := range pending {
		pctx, cancel := context.WithTimeout(ctx, recoveryBound)
		found, err := d.client.PostRecoveryAnswer(pctx, d.daemonID, a)
		cancel()
		if err != nil {
			d.log.Printf("recovery command %s: its answer (%s) is not delivered: %v; posted again at the next turn", a.Command, a.Status, err)
			continue
		}
		if !found {
			d.log.Printf("recovery command %s: the ship holds no such command of this runner; its answer (%s) is dropped", a.Command, a.Status)
		}
		d.mu.Lock()
		if cur, ok := d.outbox[a.Command]; ok && cur.Status == a.Status && cur.Detail == a.Detail {
			delete(d.outbox, a.Command)
		}
		d.mu.Unlock()
	}
}
