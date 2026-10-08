package daemon

// An assignment runs at most once here (assignment-replay ruling 01, Q14;
// runner/launcher/INTEGRATION.md §11.14): every attempt this runner takes
// for execution is recorded in its execution ledger, durably, before it
// crosses into execution, and a delivery of an attempt taken here is never
// run again — whatever its spelling, nonce, signature or expiry.

import (
	"fmt"
	"strings"
	"time"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

// ledgerState is what this runner holds of a delivered attempt.
type ledgerState int

const (
	notTaken         ledgerState = iota // never taken here: it may run
	takenAnswered                       // finished here, or given back to the ship: ignored
	takenInterrupted                    // taken by an earlier life and not finished, or named by an earlier state file: given back, once verified
)

// historyOf is what this runner holds of attempt, under any spelling of its
// atom. An error is no answer: the caller does not run the attempt.
func (d *Daemon) historyOf(attempt string) (ledgerState, error) {
	key := uvKey(attempt)
	d.mu.Lock()
	if d.finishedHere[key] || d.givenBack[key] {
		d.mu.Unlock()
		return takenAnswered, nil
	}
	named := d.namedLocked(key)
	d.mu.Unlock()
	taken, finished, err := d.ledger.Has(key)
	switch {
	case err != nil:
		return takenInterrupted, err
	case finished:
		return takenAnswered, nil
	case taken, named:
		return takenInterrupted, nil
	}
	return notTaken, nil
}

// namedLocked says whether what this runner's state holds names attempt
// key (under mu): a retention, a released entry, a held orphan. Each was
// taken by an earlier life — a state file from before the ledger says no
// more of what it ran (QUESTIONS-SOURCE-01 §15).
func (d *Daemon) namedLocked(key string) bool {
	for _, q := range d.retained {
		if uvKey(attemptOf(q)) == key {
			return true
		}
	}
	if d.st != nil {
		for _, q := range d.st.Released {
			if uvKey(attemptOf(q)) == key {
				return true
			}
		}
	}
	for id, h := range d.held {
		attempt := h.Attempt
		if attempt == "" {
			attempt = strings.TrimPrefix(id, "ci-")
		}
		if uvKey(attempt) == key {
			return true
		}
	}
	return false
}

// takeExecution records a — verified, claimed, with its slot — as taken for
// execution, durably, before it crosses into execution. An error leaves it
// unstarted; the caller gives it back to the ship.
func (d *Daemon) takeExecution(a *ship.Assignment) error {
	e := state.Execution{Attempt: uvKey(a.Attempt), At: time.Now().Unix(), ID: a.ID, Kind: a.Kind, Repo: a.Repo, Workflow: a.Workflow, Job: a.Job}
	if a.Sig != nil {
		e.Expiry, e.Nonce = a.Sig.Expiry, a.Sig.Nonce
	}
	take := d.take
	if take == nil {
		take = d.ledger.Take
	}
	return take(e)
}

// givenBackLocked marks attempt as given back to the ship without running
// here (under mu): its record could not be written, so it is not run in
// this life either.
func (d *Daemon) givenBackLocked(attempt string) {
	if d.givenBack == nil {
		d.givenBack = map[string]bool{}
	}
	d.givenBack[uvKey(attempt)] = true
}

// finishExecution records a as finished once its run returned: in the
// ledger, durably, and — when that write fails — in memory for this life;
// after a restart it then counts as interrupted: given back, never run.
func (d *Daemon) finishExecution(a *ship.Assignment) {
	key := uvKey(a.Attempt)
	finish := d.finish
	if finish == nil {
		finish = d.ledger.Finish
	}
	if err := finish(key, time.Now().Unix()); err != nil {
		d.mu.Lock()
		if d.finishedHere == nil {
			d.finishedHere = map[string]bool{}
		}
		d.finishedHere[key] = true
		d.mu.Unlock()
		d.log.Printf("[%s %s] its finish is not in the execution ledger (%v): not run again in this life; after a restart it counts as interrupted, and is given back, never run", a.Kind, key, err)
	}
}

// interruptedReason is what the ship hears of a delivery of an attempt an
// earlier life took.
func interruptedReason(attempt string) string {
	return fmt.Sprintf("not run again: this runner took attempt %s for execution before, and a stop interrupted it (or an earlier version's state names it); an assignment runs at most once on a runner", attempt)
}
