package daemon

// Settled admission at the daemon (runner/launcher/INTEGRATION.md §11.10;
// settled-admission ruling 01). Before a reserve is sent, its request is
// recorded durably as an admission: a retention whose Request names it and
// that names no VM. It withholds a slot until the launcher settles that
// request — except while the Prepare that sent it runs in this process,
// whose running slot stands for it. Settlement, not an empty list, a
// timeout or an elapsed wait, returns the slot: automatically, once the
// admission's removal is durable (no new work while it is not, §5).

import (
	"context"
	"errors"
	"slices"
	"time"

	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/state"
)

// withheldLocked is how many slots the retentions withhold: every one but
// an admission a running Prepare covers (under mu).
func (d *Daemon) withheldLocked() int {
	n := 0
	for _, q := range d.retained {
		if q.Admission() && d.covering[q.Request] {
			continue
		}
		n++
	}
	return n
}

// recountLocked sets the advertised capacity: the configured capacity less
// what the retentions withhold (under mu).
func (d *Daemon) recountLocked() {
	d.capacity = max(0, d.limit()-d.withheldLocked())
}

// exhausted says whether every slot is withheld by a retention only the
// operator's release, the daemon stopped, returns: an unsettled admission is
// not one — its settlement returns it, at a reconcile the daemon keeps
// running for — and neither is a legacy retention, released from Urgit
// through the poll the daemon keeps running for (INTEGRATION.md §11.12).
func (d *Daemon) exhausted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	bound := 0
	for _, q := range d.retained {
		if !q.Admission() && !q.IsLegacy() {
			bound++
		}
	}
	return d.limit()-bound <= 0
}

// newAdmission is the admission of a reserve request for spec's attempt:
// a fresh request token, recorded before it is sent.
func (d *Daemon) newAdmission(spec sandbox.Spec) (state.Quarantine, error) {
	request, err := sandbox.NewRequest()
	if err != nil {
		return state.Quarantine{}, err
	}
	return state.Quarantine{Handle: "ci-" + spec.Attempt, Reason: "its reserve request was sent; its outcome is not settled yet", At: time.Now().Unix(),
		Backend: d.box.Kind(), Attempt: spec.Attempt, Request: request, Label: spec.Label}, nil
}

// admit records q, an admission, before its reserve is sent — in memory,
// covered by its attempt's running slot, then in the state file — and says
// whether that record is durable: the reserve may be sent only then. One
// that is not is withdrawn at once (its reserve is never sent), and the
// save's failure is the error.
func (d *Daemon) admit(q state.Quarantine) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.covering == nil {
		d.covering = map[string]bool{}
	}
	d.retained = append(d.retained, q)
	d.covering[q.Request] = true
	d.persistLocked()
	err := d.unsaved
	if err == nil {
		return nil
	}
	d.removeAdmissionLocked(q.Request)
	d.persistLocked()
	return err
}

// removeAdmissionLocked forgets the admission of request, covered or not
// (under mu; the caller saves).
func (d *Daemon) removeAdmissionLocked(request string) {
	delete(d.covering, request)
	d.retained = slices.DeleteFunc(slices.Clone(d.retained), func(q state.Quarantine) bool { return q.Admission() && q.Request == request })
	d.recountLocked()
	d.signalLocked()
}

// resolveAdmission settles q, the admission of a Prepare that has returned
// with err, as far as that Prepare could, and says whether the attempt's
// slot may be reused (INTEGRATION.md §11.10):
//   - prepared (err nil): it goes; the reservation is the running
//     attempt's, and a crash from here leaves an orphan the first reconcile
//     finds;
//   - retained under the reservation's identity: that retention takes its
//     place, the slot counted once;
//   - retained by its request alone (the lost answer could not be
//     settled): it stays, withholding the slot from now on, until a
//     reconcile settles it;
//   - any other failure: nothing it requested is left — refused by the
//     launcher, never sent, or settled (closed, released, or admitted and
//     rolled back) — so it goes.
func (d *Daemon) resolveAdmission(q state.Quarantine, err error) bool {
	var kept *sandbox.RetainedError
	retained := errors.As(err, &kept)
	d.mu.Lock()
	defer d.mu.Unlock()
	if retained && kept.Handle.VM == "" {
		delete(d.covering, q.Request)
		for i, have := range d.retained {
			if have.Admission() && have.Request == q.Request {
				d.retained[i].Reason = "its reserve's answer was lost and its request could not be settled: " + err.Error()
				d.retained[i].Rev++
			}
		}
		d.recountLocked()
		d.log.Printf("ADMISSION NOT SETTLED %s (reserve request %s): %v; its slot is withheld until the launcher settles it; advertised capacity now %d", q.Handle, q.Request, err, d.capacity)
		d.persistLocked()
		return false
	}
	d.removeAdmissionLocked(q.Request)
	if retained {
		return !d.retainLocked(kept.Handle, "sandbox prepare failed and could not be rolled back: "+err.Error())
	}
	d.persistLocked()
	return true
}

// settleAdmissions asks the backend to settle every admission no running
// Prepare covers, each by its request (INTEGRATION.md §11.10):
//   - closed or released: it goes, and its slot returns once that is saved
//     (no new work before, §5);
//   - admitted: the reservation it made is held in its place, as an
//     orphan, its slot still withheld; the orphan logic of this reconcile —
//     or the next, if the list did not show it yet — resolves it by its
//     actual disposition (the ship's status; destroy or hold; a quarantine
//     stays a retention);
//   - no answer: it stays, and the next pass asks again.
func (d *Daemon) settleAdmissions(ctx context.Context) {
	settler, ok := d.box.(sandbox.Settler)
	if !ok {
		return
	}
	d.mu.Lock()
	var todo []state.Quarantine
	for _, q := range d.retained {
		if q.Admission() && !d.covering[q.Request] {
			todo = append(todo, q)
		}
	}
	d.mu.Unlock()
	for _, q := range todo {
		h := sandbox.Handle{ID: q.Handle, Attempt: q.Attempt, Request: q.Request, Label: q.Label}
		s, err := settler.Settle(ctx, h)
		if err != nil {
			d.log.Printf("reconcile %s: its reserve request %s is not settled (%v); its slot stays withheld", q.Handle, q.Request, err)
			continue
		}
		d.mu.Lock()
		if s.Outcome == sandbox.SettledAdmitted {
			if d.held == nil {
				d.held = map[string]sandbox.Handle{}
			}
			d.held[s.Handle.ID] = s.Handle
		}
		d.removeAdmissionLocked(q.Request)
		d.persistLocked()
		capacity := d.capacity
		d.mu.Unlock()
		switch s.Outcome {
		case sandbox.SettledAdmitted:
			d.log.Printf("reconcile %s: its reserve request %s is settled: %s; held while the launcher holds it (%s)", q.Handle, q.Request, s.Detail, sandbox.Describe(s.Handle))
		default:
			d.log.Printf("reconcile %s: its reserve request %s is settled: %s (%s); its slot returns; advertised capacity now %d", q.Handle, q.Request, s.Outcome, s.Detail, capacity)
		}
	}
}
