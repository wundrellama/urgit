package launcher

// Incidents: cleanup and release are separate (recovery ruling A;
// runner/launcher/INTEGRATION.md §8). A quarantined record is an incident:
// cleanup — automatic, or the operator's retry — mitigates its hazard and
// never releases it; release is a distinct operator action, after
// resolution, on the exact incarnation and revision the operator inspected.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	// ErrStale: a reference names another incarnation of the id, or — an
	// operator's selection — a revision the record has moved past: nothing
	// is done; inspect again.
	ErrStale = errors.New("launcher: stale: it names another incarnation or revision")
	// ErrUnresolved: the incident still holds something; release is
	// never proof that it is gone.
	ErrUnresolved = errors.New("launcher: the incident is not resolved")
)

// Incident is a quarantined record's history: the obligation it was
// quarantined under and every cleanup attempt since.
type Incident struct {
	Trigger  string           `json:"trigger"`  // what asked for the cleanup
	Due      int64            `json:"due_unix"` // the obligation's deadline
	Missed   bool             `json:"missed"`   // the cleanup began after Due
	Attempts []CleanupAttempt `json:"attempts,omitempty"`
	Count    int              `json:"count"` // every attempt, Attempts keeps the latest
}

// CleanupAttempt is one cleanup of an incident — when, by whom, and what it
// left: Result "resolved" (nothing held) or "unresolved" (Left, Detail) —
// or its release ("released").
type CleanupAttempt struct {
	At     int64    `json:"at"`
	By     string   `json:"by"` // teardown, late recovery, recovery, operator retry, operator release
	Result string   `json:"result"`
	Left   []string `json:"left,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// VMMFinder is a Host that can look for the VMM processes of an id,
// whatever their pid (the adapter scans /proc for the exact `--id <id>`
// argument): how the operator's retry resolves a VMM of unknown pid.
type VMMFinder interface {
	FindVMMs(id string) ([]int, error)
}

// Selection names one incident exactly: its record, its incarnation (its
// token; INTEGRATION.md §11.1) and the revision an inspection showed. Its
// text form is what scripts pass (ParseSelection); every use checks it
// against the live record.
type Selection struct {
	ID          string
	Incarnation string // "" only for a record written before tokens
	CID         uint32
	Created     int64
	Rev         uint64
}

// Selection is the selection an inspection of r shows: its exact
// incarnation at its current revision.
func (r Record) Selection() Selection {
	return Selection{ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, Rev: r.Rev}
}

// Ref is the incarnation sel names.
func (sel Selection) Ref() Ref {
	return Ref{ID: sel.ID, Incarnation: sel.Incarnation, CID: sel.CID, Created: sel.Created}
}

// String is <id>/<incarnation>/<cid>/<created>/<rev>, the incarnation "-"
// for a record written before tokens.
func (sel Selection) String() string {
	inc := sel.Incarnation
	if inc == "" {
		inc = "-"
	}
	return fmt.Sprintf("%s/%s/%d/%d/%d", sel.ID, inc, sel.CID, sel.Created, sel.Rev)
}

// ParseSelection reads <id>/<incarnation>/<cid>/<created>/<rev>: the
// incarnation is a token, or "-" for a record written before tokens.
func ParseSelection(text string) (Selection, error) {
	const form = "a selection is <id>/<incarnation>/<cid>/<created>/<rev>"
	f := strings.Split(strings.TrimSpace(text), "/")
	if len(f) != 5 {
		return Selection{}, fmt.Errorf("%w: %s", ErrInvalid, form)
	}
	inc := f[1]
	if inc == "-" {
		inc = ""
	}
	cid, err1 := strconv.ParseUint(f[2], 10, 32)
	created, err2 := strconv.ParseInt(f[3], 10, 64)
	rev, err3 := strconv.ParseUint(f[4], 10, 64)
	if f[0] == "" || (inc != "" && !validIncarnation(inc)) || errors.Join(err1, err2, err3) != nil {
		return Selection{}, fmt.Errorf("%w: %s: %q", ErrInvalid, form, text)
	}
	return Selection{ID: f[0], Incarnation: inc, CID: uint32(cid), Created: created, Rev: rev}, nil
}

// Inspection is an incident as the operator sees it.
type Inspection struct {
	Record    Record
	Selection Selection
	Busy      string   // an operation holding the record now ("" none)
	Remaining []string // what it may still hold
	// the charge it withholds: cpus, MiB (guest + overhead), one guest
	CPUs, MemoryMiB int
	Releasable      bool
	Why             string // why release is allowed or refused
}

// maxAttempts is how many cleanup attempts an incident keeps (Count says
// how many there were); each attempt's text is bounded, so the record's
// growth stays within recordGrowthBytes.
const (
	maxAttempts    = 8
	maxDetailBytes = 512
	maxLeftEntries = 8
)

// withAttempt is inc with a appended, keeping the latest maxAttempts.
func withAttempt(inc *Incident, a CleanupAttempt) *Incident {
	out := *inc
	out.Attempts = append(append([]CleanupAttempt(nil), inc.Attempts...), a)
	if len(out.Attempts) > maxAttempts {
		out.Attempts = out.Attempts[len(out.Attempts)-maxAttempts:]
	}
	out.Count++
	return &out
}

// remaining is what r may still hold, as the operator reads it; the jail
// goes with the disk.
func remaining(r Record) []string {
	var out []string
	if r.HasVMM {
		if r.PID != 0 {
			out = append(out, fmt.Sprintf("vmm pid %d", r.PID))
		} else {
			out = append(out, "vmm of unknown pid")
		}
	}
	if r.HasNetwork {
		out = append(out, fmt.Sprintf("network index %d", r.NetIndex))
	}
	if r.HasCgroup {
		out = append(out, "cgroup")
	}
	if r.HasDisk || r.DiskPath != "" {
		out = append(out, "disk")
	}
	return out
}

// InspectRecord is a record as the operator sees it from its durable form
// alone — what it holds, its charge, and whether a release would be allowed
// now — for a reader that does not own the state directory (serve may run
// and act on it): the release itself decides again, under the lock.
func InspectRecord(r Record) Inspection {
	in := Inspection{Record: r, Selection: r.Selection(),
		Remaining: remaining(r), CPUs: r.CPUs, MemoryMiB: r.MemoryMiB + OverheadMiB}
	switch {
	case r.State != StateQuarantined:
		in.Why = fmt.Sprintf("it is %s, not an incident", r.State)
	case len(in.Remaining) > 0:
		in.Why = "it still holds " + strings.Join(in.Remaining, ", ") + ": retry its cleanup; release is never proof that anything is gone"
	default:
		in.Releasable = true
		in.Why = "nothing is held any more: release returns its charge"
	}
	return in
}

// inspectLocked is e as the operator sees it (under s.mu).
func (s *Service) inspectLocked(e *entry) Inspection {
	in := InspectRecord(e.rec)
	in.Busy = e.busy
	switch {
	case e.acct != nil:
		in.Releasable = true
		in.Why = "its release is authorized; its accounting is not confirmed yet (" + e.rec.Reason + "): a retried release, or the reaper's next pass, confirms it"
	case e.busy != "":
		in.Releasable = false
		in.Why = fmt.Sprintf("a %s holds it now: inspect again once it has ended", e.busy)
	}
	return in
}

// selectLocked is the entry sel names — that incarnation, and when rev is
// set that revision — or why none is (under s.mu).
func (s *Service) selectLocked(sel Selection, rev bool) (*entry, error) {
	e, ok := s.vms[sel.ID]
	switch {
	case !ok:
		// not loaded: unknown only when the inventory is authoritative
		// (INTEGRATION.md §11.7)
		if err := s.absenceLocked(sel.ID + "'s absence"); err != nil {
			return nil, err
		}
		return nil, ErrUnknown
	case !sel.Ref().Names(e.rec):
		return nil, fmt.Errorf("%w: %s is another incarnation now (%s); the selection names %s", ErrStale, sel.ID, e.rec.Ref(), sel.Ref())
	case rev && e.rec.Rev != sel.Rev:
		return nil, fmt.Errorf("%w: %s changed since it was inspected (revision %d, now %d): inspect it again", ErrStale, sel.ID, sel.Rev, e.rec.Rev)
	}
	return e, nil
}

// Incidents is every quarantined record, in creation order, as the
// operator sees it.
func (s *Service) Incidents() []Inspection {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Inspection
	for _, e := range s.candidatesLocked() {
		if e.rec.State == StateQuarantined || e.acct != nil {
			out = append(out, s.inspectLocked(e))
		}
	}
	return out
}

// InspectIncident is the incident sel names (its incarnation; any
// revision: the inspection shows the current one).
func (s *Service) InspectIncident(sel Selection) (Inspection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.selectLocked(sel, false)
	if err != nil {
		return Inspection{}, err
	}
	return s.inspectLocked(e), nil
}

// RetryCleanup is the operator's cleanup retry of the incident sel names
// (its incarnation): one bounded effort, from this request, that stops what
// is verified the record's, looks for a VMM of unknown pid where the host
// can, removes what the record holds once nothing may run, records the
// attempt, and never releases (INTEGRATION.md §8.2). It is refused while
// any operation holds the record. It answers the inspection after the
// attempt, and an error when something is left unresolved.
func (s *Service) RetryCleanup(sel Selection) (Inspection, error) {
	s.mu.Lock()
	e, err := s.selectLocked(sel, false)
	if err == nil {
		switch {
		case s.closing:
			err = ErrClosed
		case e.busy != "":
			err = fmt.Errorf("%w: %s has a %s in progress; retry once it has ended", ErrState, sel.ID, e.busy)
		case e.acct != nil:
			err = fmt.Errorf("%w: %s's release is being confirmed; a retried release finishes it", ErrState, sel.ID)
		case e.rec.State != StateQuarantined:
			err = fmt.Errorf("%w: %s is %s, not an incident", ErrState, sel.ID, e.rec.State)
		}
	}
	if err != nil {
		s.mu.Unlock()
		return Inspection{}, err
	}
	s.take(e, "teardown")
	// the operator's request bounds this effort; the incident keeps the
	// obligation's own trigger and deadline
	by := s.cfg.now().Add(s.cfg.CleanupBound)
	s.mu.Unlock()
	terr := s.teardown(e, StateQuarantined, "cleanup retried by the operator", retrying, by)
	s.mu.Lock()
	in := s.inspectLocked(e)
	s.mu.Unlock()
	return in, terr
}

// Release is the operator's release of the incident sel names — that
// incarnation, at the revision inspected — once it holds nothing: its
// evidence, its disposition an operator's release, is kept durably first
// (released/<id>.<token>.json), then its record is withdrawn durably, and
// only then is it released and its charge returned (INTEGRATION.md §§8.3,
// 11.8). It never cleans up and is never proof that anything is gone: a
// stale selection, a cleanup in progress, or anything still held refuses
// it. Any failure keeps the charge: evidence that could not be kept
// refuses it, still an incident; a withdrawal not confirmed leaves it
// pending, finished by a retried release or the reaper's next pass.
func (s *Service) Release(sel Selection) error {
	s.mu.Lock()
	e, err := s.selectLocked(sel, true)
	if err == nil {
		switch {
		case s.closing:
			err = ErrClosed
		case e.busy != "":
			err = fmt.Errorf("%w: %s has a %s in progress; inspect it again once it has ended", ErrState, sel.ID, e.busy)
		case e.acct != nil:
			// authorized already: its accounting is owed — its evidence kept
			// (certified) first, if this process has not yet (§11.9)
			s.take(e, "release")
			s.mu.Unlock()
			return s.finishAccounting(e)
		case e.rec.State != StateQuarantined:
			err = fmt.Errorf("%w: %s is %s, not an incident", ErrState, sel.ID, e.rec.State)
		default:
			if left := remaining(e.rec); len(left) > 0 {
				err = fmt.Errorf("%w: %s still holds %s; release is never proof that anything is gone: retry its cleanup", ErrUnresolved, sel.ID, strings.Join(left, ", "))
			}
		}
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.take(e, "release")
	now := s.cfg.now()
	s.authorizeLocked(e, Release{By: ByOperator, Final: StateReleased, DecidedUnixNano: now.UnixNano()})
	a := e.acct
	if a.ev.Incident == nil {
		a.ev.Incident = &Incident{Trigger: "quarantined before incidents were recorded"}
	}
	a.ev.Incident = withAttempt(a.ev.Incident, CleanupAttempt{At: now.Unix(), By: "operator release", Result: "released"})
	s.mu.Unlock()
	if err := s.st.archive(a.ev); err != nil {
		// not authorized durably: still the incident it was
		s.mu.Lock()
		e.acct = nil
		s.put(e)
		s.mu.Unlock()
		return fmt.Errorf("release of %s not done: its evidence could not be kept: %w; it stays charged", sel.ID, err)
	}
	s.mu.Lock()
	a.evidenced = true
	s.mu.Unlock()
	return s.finishAccounting(e)
}
