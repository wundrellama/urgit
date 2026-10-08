package daemon

// A runner with an incomplete execution history runs nothing until its
// transition (legacy-replay-upgrade ruling 01, Q15; runner/launcher/
// INTEGRATION.md §11.15). Its ledger's history began with a state file kept
// before it, or cannot be read: what it ran before is not known, and a copy
// of an assignment its ship signed then could run once more. Until the
// owner's transition is recorded, it polls advertising no capacity and gives
// back every assignment it receives; once it is, it refuses every
// assignment whose signed nonce is below its epoch's floor — every
// assignment its ship signed before — and runs the rest. A runner enrolled
// with this version, its history complete, never waits.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"
)

// historyOperation is the recovery command's operation that confirms a
// runner's transition.
const historyOperation = "confirm-history"

// epochFloor is the smallest nonce an assignment of epoch e is signed with:
// e above bit 128. Every nonce the ship ever signed before a transition is
// below 2^128 (fresh-nonce is sixteen bytes).
func epochFloor(e uint64) *big.Int {
	return new(big.Int).Lsh(new(big.Int).SetUint64(e), 128)
}

// historySelection names a runner's history as a recovery command does a
// retention: by what it is and its revision, the unix time it began (0
// when that is not known).
func historySelection(since int64) string {
	return "history/" + strconv.FormatInt(since, 10)
}

// historyEvidenceOf is the digest a transition is bound to: the runner's id
// and its ledger's history as HISTORY holds it, or that it cannot be read.
// A change in either is another digest.
func historyEvidenceOf(daemonID string, h state.History) string {
	var b strings.Builder
	b.WriteString("urgit runner history evidence 1\n")
	fmt.Fprintf(&b, "daemon %s\n", daemonID)
	if h.Since == 0 {
		b.WriteString("history unknown\n")
	} else {
		fmt.Fprintf(&b, "history since %d enrolled %t state_format %d\n", h.Since, h.Enrolled, h.StateFormat)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// parseTransitionEvidence reads a transition's evidence: "epoch <n> history
// <digest>".
func parseTransitionEvidence(evidence string) (epoch uint64, digest string, ok bool) {
	f := strings.Fields(evidence)
	if len(f) != 4 || f[0] != "epoch" || f[2] != "history" {
		return 0, "", false
	}
	epoch, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return epoch, f[3], true
}

// completeLocked says whether this runner's history is complete (under mu):
// its ledger began with its enrollment, and says so.
func (d *Daemon) completeLocked() bool {
	return d.began.Enrolled && d.began.Since != 0
}

// recordedTransitionLocked is this runner's recorded transition, if its
// state file holds one it can stand on (under mu): a record this version
// writes (Transition.Problem), bound to this runner's history as it is now.
// Anything else is none, and the runner waits (INTEGRATION.md §11.16).
func (d *Daemon) recordedTransitionLocked() *state.Transition {
	t, _ := d.transitionRecordLocked()
	return t
}

// transitionRecordLocked is recordedTransitionLocked's transition, and why
// a record the state file holds is none: "" when it holds none, or one that
// is a transition (under mu). A record that is none is read, never trusted,
// and kept as the file holds it (INTEGRATION.md §11.16).
func (d *Daemon) transitionRecordLocked() (*state.Transition, string) {
	if d.st == nil || d.st.Transition == nil {
		return nil, ""
	}
	t := d.st.Transition
	if why := t.Problem(); why != "" {
		return nil, why
	}
	if t.Evidence != historyEvidenceOf(d.daemonID, d.began) {
		return nil, "it is bound to another history than this runner's as it is now"
	}
	return t, ""
}

// pausedLocked says whether this runner waits for its transition (under
// mu): its history is not complete, and no transition is recorded.
func (d *Daemon) pausedLocked() bool {
	return !d.completeLocked() && d.recordedTransitionLocked() == nil
}

// epochLocked is the authorization epoch this runner is at (under mu): its
// transition's, or 0.
func (d *Daemon) epochLocked() uint64 {
	if t := d.recordedTransitionLocked(); t != nil {
		return t.Epoch
	}
	return 0
}

// advertisedCapacity is what a poll advertises, and whether this runner
// waits for its transition: then nothing, explicitly — its ship offers it no
// work — and its slots otherwise. What the retentions withhold stays
// withheld either way.
func (d *Daemon) advertisedCapacity() (capacity int, paused bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pausedLocked() {
		return 0, true
	}
	return d.capacity, false
}

func sinceText(since int64) string {
	if since == 0 {
		return "its ledger began (when is not recorded)"
	}
	return "its ledger began (" + time.Unix(since, 0).UTC().Format(time.RFC3339) + ")"
}

// fenced is why a verified assignment may not run here, or "": this runner
// waits for its transition; or the assignment's nonce is below its epoch's
// floor, so its ship signed it before the transition.
func (d *Daemon) fenced(a *ship.Assignment) string {
	d.mu.Lock()
	paused, epoch, since := d.pausedLocked(), d.epochLocked(), d.began.Since
	d.mu.Unlock()
	if paused {
		return fmt.Sprintf("not started: what this runner ran before %s is not known, and it runs nothing until its transition is confirmed in Urgit (Settings → Runners)", sinceText(since))
	}
	if epoch == 0 {
		return ""
	}
	if n, err := sig.ParseUV(a.Sig.Nonce); err != nil || n.Cmp(epochFloor(epoch)) < 0 {
		return fmt.Sprintf("not run: signed before this runner's history transition (authorization epoch %d); nothing its ship authorized before the transition runs here", epoch)
	}
	return ""
}

// historyReport is this runner's execution history, for its report.
func (d *Daemon) historyReport() *ship.HistoryReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := &ship.HistoryReport{Selection: historySelection(d.began.Since), Revision: uint64(max(d.began.Since, 0)), Since: d.began.Since,
		Known: d.began.Since != 0, Complete: d.completeLocked(), StateFormat: d.began.StateFormat, Paused: d.pausedLocked(),
		Epoch: d.epochLocked(), Evidence: historyEvidenceOf(d.daemonID, d.began)}
	t, why := d.transitionRecordLocked()
	if t != nil {
		h.Transition = &ship.TransitionReport{Epoch: t.Epoch, Command: commandKey(t.Command), At: t.At}
	}
	if why != "" {
		// a record that is no transition, reported as the file holds it
		// (INTEGRATION.md §11.16)
		h.InvalidTransition = &ship.InvalidTransitionReport{Problem: why, Record: clip(string(d.st.Transition.Record()))}
	}
	switch {
	case h.Complete:
		h.Explanation = "complete: its ledger began with this runner's enrollment, and every attempt it took is recorded"
	case t != nil:
		h.Explanation = fmt.Sprintf("what it ran before %s is not known; its transition to authorization epoch %d is recorded: it refuses every assignment its ship signed before, and runs new work", sinceText(d.began.Since), t.Epoch)
	case why != "":
		h.Explanation = fmt.Sprintf("its state file holds a transition record that is no transition (%s): it is kept as the file holds it; what it ran before %s is not known, and it runs nothing until its transition is confirmed", why, sinceText(d.began.Since))
	case !h.Known:
		h.Explanation = "its history cannot be read: it is not taken for a fresh runner, and it runs nothing until its transition is confirmed"
	default:
		h.Explanation = fmt.Sprintf("what it ran before %s is not known (a state file kept before its ledger): it runs nothing until its transition is confirmed", sinceText(d.began.Since))
	}
	return h
}

// confirmHistory carries out the owner's transition (under recoveryMu; the
// command authenticated, not decided before, and in time): refused unless it
// names this runner's history as it is now — its selection, and its
// evidence digest — with an epoch of 1 or more, while the runner waits;
// then recorded in the state file, and only once that record is durable
// does the runner run again.
func (d *Daemon) confirmHistory(cmd *ship.RecoveryCommand) ship.RecoveryAnswer {
	d.mu.Lock()
	defer d.mu.Unlock()
	evidence := historyEvidenceOf(d.daemonID, d.began)
	refuse := func(format string, args ...any) ship.RecoveryAnswer {
		return d.refuseLocked(cmd, evidence, fmt.Sprintf(format, args...))
	}
	if current := historySelection(d.began.Since); cmd.Selection != current {
		return refuse("it names history %s; this runner's is %s: inspect it again", cmd.Selection, current)
	}
	epoch, digest, ok := parseTransitionEvidence(cmd.Evidence)
	if !ok {
		return refuse("its evidence %q names no authorization epoch and history evidence (\"epoch <n> history <digest>\")", clip(cmd.Evidence))
	}
	if digest != evidence {
		return refuse("the history evidence changed since it was inspected (now %s, confirmed %s): inspect it again", short(evidence), short(digest))
	}
	if epoch == 0 {
		return refuse("it names authorization epoch 0: a transition takes a runner to epoch 1 or more")
	}
	if !d.pausedLocked() {
		if t := d.recordedTransitionLocked(); t != nil {
			return refuse("this runner is not waiting for a transition: its transition to authorization epoch %d is recorded (command %s)", t.Epoch, commandKey(t.Command))
		}
		return refuse("this runner is not waiting for a transition: its history is complete")
	}
	if d.st == nil || d.stateFile == "" {
		return refuse("this daemon keeps no state file")
	}
	return d.recordTransitionLocked(cmd, epoch, evidence)
}

// recordTransitionLocked records the transition cmd confirms (under mu): the
// state file saved with it — and with every refusal due — and only once
// that save is durable does it take effect. A save that failed before its
// rename changes nothing (refused, the refusal recorded first); one whose
// outcome is uncertain answers uncertain, and the runner keeps waiting until
// a save of memory's view succeeds, which records the command refused, not
// applied (settleLocked).
func (d *Daemon) recordTransitionLocked(cmd *ship.RecoveryCommand, epoch uint64, evidence string) ship.RecoveryAnswer {
	answer := ship.RecoveryAnswer{Command: cmd.ID, Selection: cmd.Selection, Revision: cmd.Revision, Evidence: cmd.Evidence}
	next := *d.st
	next.Quarantined = slices.Clone(d.retained)
	next.Transition = &state.Transition{Epoch: epoch, Command: cmd.ID, Evidence: evidence, At: time.Now().Unix()}
	if old := d.st.Transition; old != nil {
		// a record that was no transition is superseded, and kept as the
		// file held it (INTEGRATION.md §11.16)
		_, why := d.transitionRecordLocked()
		next.Superseded = append(slices.Clone(d.st.Superseded), state.SupersededTransition{Record: old.Record(), Problem: why, By: cmd.ID, At: next.Transition.At})
	}
	next.Refused = d.refusalsDueLocked()
	save := d.saveState
	if save == nil {
		save = state.Save
	}
	if err := save(d.stateFile, &next); err != nil {
		var se *state.SaveError
		if errors.As(err, &se) && se.Uncertain {
			// the file may hold the transition; memory holds none, and is
			// written back before anything else counts (§5)
			d.unsaved = err
			answer.Status = ship.AnswerUncertain
			answer.Detail = fmt.Sprintf("its transition may or may not be recorded (%v): this runner keeps waiting, runs nothing, and the answer follows once the state file is durable again", err)
			if d.doubtful == nil {
				d.doubtful = map[string]ship.RecoveryAnswer{}
			}
			d.doubtful[cmd.ID] = answer
			d.log.Printf("RECOVERY %s UNCERTAIN: %s", cmd.ID, answer.Detail)
			return answer
		}
		return d.refuseLocked(cmd, evidence, fmt.Sprintf("its transition could not be saved (%v)", err))
	}
	// durable: the transition takes effect
	d.st.Format, d.st.Quarantined, d.st.Transition, d.st.Superseded = next.Format, next.Quarantined, next.Transition, next.Superseded
	if d.unsaved != nil {
		d.log.Printf("state file durable again: %d retention(s) recorded", len(d.retained))
		d.unsaved = nil
	}
	d.settleLocked(next.Refused)
	capacity := d.capacityLocked()
	answer.Status, answer.Capacity = ship.AnswerCompleted, &capacity
	answer.Detail = fmt.Sprintf("recorded the transition to authorization epoch %d: this runner refuses every assignment its ship signed before it, and runs new work again; its retentions stay withheld (capacity %d)", epoch, d.capacity)
	d.log.Printf("RECOVERY %s: %s", cmd.ID, answer.Detail)
	return answer
}
