package launcher

// Late-accounting ruling 01 (runner/launcher/INTEGRATION.md §11.8): a
// release is authorized by durable evidence — its exact incarnation, its
// obligation, its physical cleanup's verified end and its disposition —
// kept in released/ before its record is withdrawn. Its capacity returns
// only once that withdrawal is durably confirmed, even late, and a late
// confirmation is kept in its evidence and reported apart. Nothing else
// answers a release: not a missing record, not an incomplete inventory,
// not a directory barrier alone, not a timing held only in memory.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrUnproven: no durable evidence proves the release of the incarnation
// named; its absence is no authority (INTEGRATION.md §11.8).
var ErrUnproven = errors.New("launcher: no durable evidence of its release")

// The dispositions a release's evidence records.
const (
	// ByTimelyCleanup: a teardown's own release, its cleanup verified
	// complete before its obligation's deadline
	ByTimelyCleanup = "timely cleanup"
	// ByOperator: the operator's explicit release of an incident (§8.3)
	ByOperator = "operator release"
)

// Release is a released record's disposition, kept with its evidence in
// released/ (§§8.3, 11.8).
type Release struct {
	// By is who authorized it: ByTimelyCleanup or ByOperator
	By string `json:"by"`
	// Final is the state the released record ends in: destroyed or reaped
	// (a timely cleanup's), released (the operator's)
	Final State `json:"final,omitempty"`
	// Trigger and DueUnixNano are the obligation a timely cleanup met: what
	// asked for it and its deadline D
	Trigger     string `json:"trigger,omitempty"`
	DueUnixNano int64  `json:"due_unix_nano,omitempty"`
	// CleanedUnixNano is T_c: when the physical cleanup was verified
	// complete, nothing left — before D, or it is no timely cleanup
	CleanedUnixNano int64 `json:"cleaned_unix_nano,omitempty"`
	// DecidedUnixNano is when the release was authorized
	DecidedUnixNano int64 `json:"decided_unix_nano"`
	// ConfirmedUnixNano is T_a: when its record's withdrawal was durably
	// confirmed. 0 while that is pending — and when it was never recorded
	// (the evidence's update failed after the confirmation, or a crash came
	// between them): then it is read as late, never as on time
	ConfirmedUnixNano int64 `json:"confirmed_unix_nano,omitempty"`
	// LateAccounting: its accounting was confirmed at or after D (late
	// bookkeeping, reported apart)
	LateAccounting bool `json:"late_accounting,omitempty"`
}

// Late says whether r's accounting may not be claimed on time: confirmed
// at or after its obligation's deadline, or at an instant never recorded.
func (r Release) Late() bool {
	return r.LateAccounting || r.DueUnixNano != 0 && r.ConfirmedUnixNano == 0
}

// timely says whether ev is a timely cleanup's evidence of exactly r's
// incarnation: what backs a `stopping` record found at open (§11.8).
func timely(ev, r Record) bool {
	rel := ev.Release
	return ev.State == StateReleased && rel != nil && rel.By == ByTimelyCleanup &&
		rel.DueUnixNano != 0 && rel.CleanedUnixNano != 0 && rel.CleanedUnixNano < rel.DueUnixNano &&
		r.Ref().Names(ev) && ev.CID == r.CID && ev.Created == r.Created && ev.Owner == r.Owner
}

// accounting is a release authorized and not yet confirmed (under s.mu; its
// progress with the token held): its evidence, then its record's
// withdrawal, must be durable before its capacity returns.
type accounting struct {
	ev        Record    // the evidence: the record as released, Release set
	evidenced bool      // this process kept ev: every link down to it certified (§11.9); a release loaded at open has not
	unlinked  bool      // the record file is gone; its absence not confirmed yet
	due       time.Time // D (zero: the operator's release has no obligation)
	reason    string    // the record's reason when it was authorized
}

// authorizeLocked makes e an authorized release whose evidence is e's
// record as released with rel (under s.mu, token held).
func (s *Service) authorizeLocked(e *entry, rel Release) {
	ev := e.rec
	ev.State, ev.Updated = StateReleased, s.cfg.now().Unix()
	ev.Rev++
	ev.Release = &rel
	e.acct = &accounting{ev: ev, reason: e.rec.Reason}
	if rel.DueUnixNano != 0 {
		e.acct.due = time.Unix(0, rel.DueUnixNano)
	}
}

// finishAccounting completes e's authorized release (token held, given
// back): its evidence made durable, then its record's withdrawal, then —
// only then — its capacity returned. The evidence then records when the
// accounting was confirmed, and whether that was at or after the
// obligation's deadline: late bookkeeping, reported apart, never claimed
// on time. A failure leaves it pending — charged, listed, retried by the
// owner's destroy, the reaper's next pass, the stop, or a retried operator
// release — never a quarantine, never a release.
func (s *Service) finishAccounting(e *entry) error {
	s.mu.Lock()
	id, a := e.rec.ID, e.acct
	s.mu.Unlock()
	if !a.evidenced {
		if err := s.st.archive(a.ev); err != nil {
			return s.accountingPending(e, "its release evidence is not durable yet", err)
		}
		s.mu.Lock()
		a.evidenced = true
		s.mu.Unlock()
	}
	unlinked, err := s.st.remove(id)
	if err != nil {
		s.mu.Lock()
		a.unlinked = a.unlinked || unlinked
		s.mu.Unlock()
		what := "its record could not be withdrawn"
		if unlinked {
			what = "its record's withdrawal is not confirmed yet (its directory's fsync failed)"
		}
		return s.accountingPending(e, what, err)
	}
	confirmed := s.cfg.now()
	ev := a.ev
	rel := *ev.Release
	rel.ConfirmedUnixNano = confirmed.UnixNano()
	rel.LateAccounting = !a.due.IsZero() && !confirmed.Before(a.due)
	ev.Release = &rel
	ev.Rev++
	// the confirmation, kept in the evidence: the late bookkeeping's audit.
	// The capacity returns whatever this does — the accounting is confirmed;
	// evidence left reading pending is read as late (Release.Late)
	werr := s.st.rearchive(ev)
	s.mu.Lock()
	defer s.mu.Unlock()
	e.acct = nil
	e.rec = ev
	e.rec.State = rel.Final
	e.rec.Reason = a.reason
	if rel.LateAccounting {
		e.rec.Reason += fmt.Sprintf("; LATE BOOKKEEPING: its accounting (its record's withdrawal) was confirmed at %s, not before its obligation's deadline %s; its cleanup had finished in time, at %s", instant(confirmed), instant(a.due), instant(time.Unix(0, rel.CleanedUnixNano)))
	}
	if werr != nil {
		e.rec.Reason += "; its evidence does not record that confirmation (it still reads pending, which counts as late): " + werr.Error()
		rel.ConfirmedUnixNano = 0
	}
	e.disposition, e.released = rel, true
	s.drop(e)
	s.hist = append(s.hist, e.rec)
	s.put(e)
	return nil
}

// accountingPending leaves e an authorized release whose accounting is
// pending (token given back): charged, listed, its reason saying what is
// pending (§11.8).
func (s *Service) accountingPending(e *entry, what string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := e.rec.ID
	e.rec.Reason = e.acct.reason + "; its release is authorized, but " + what + ": it stays charged until a retry confirms it: " + err.Error()
	s.put(e)
	return fmt.Errorf("%w: the release of %s is authorized, but %s; it stays charged until a retry confirms it: %v", ErrNotDurable, id, what, err)
}

// Released is the evidence of the release of exactly the incarnation ref
// names, the owner's (§11.8): its disposition, when a durable release
// proves it — the incarnation is not held, the inventory is authoritative
// (§§11.6, 11.7) and its evidence is kept. Otherwise it says why not:
// ErrNotDurable while its accounting is pending; ErrState while it is held;
// ErrNotOwner for another owner's; ErrUnproven when no evidence proves it;
// the fence's refusal while the inventory is not authoritative.
func (s *Service) Released(owner Owner, ref Ref) (Release, error) {
	s.mu.Lock()
	if e, ok := s.vms[ref.ID]; ok && ref.Names(e.rec) {
		defer s.mu.Unlock()
		switch {
		case e.rec.Owner != owner:
			return Release{}, ErrNotOwner
		case e.acct != nil:
			return Release{}, fmt.Errorf("%w: the release of %s is authorized, but its accounting is not confirmed yet", ErrNotDurable, ref)
		}
		return Release{}, fmt.Errorf("%w: %s is held (%s), not released", ErrState, ref, e.rec.State)
	}
	if err := s.absenceLocked("the absence of " + ref.String()); err != nil {
		s.mu.Unlock()
		return Release{}, err
	}
	s.mu.Unlock()
	ev, found, err := s.st.evidence(ref)
	switch {
	case err != nil:
		return Release{}, fmt.Errorf("%w: the evidence of %s could not be read: %v", ErrUnproven, ref, err)
	case !found || ev.Owner != owner:
		return Release{}, fmt.Errorf("%w: %s is not held, and no evidence of its release is kept: its absence is no authority", ErrUnproven, ref)
	}
	return dispositionOf(ev), nil
}

// dispositionOf is ev's disposition; evidence kept before dispositions were
// recorded is an operator's release (§8.3 kept only those).
func dispositionOf(ev Record) Release {
	if ev.Release != nil {
		return *ev.Release
	}
	return Release{By: ByOperator, Final: StateReleased}
}

// releasedAny is Released for the in-process bare Destroy(owner, id), which
// names no incarnation (tests only; no production caller): the release of
// some incarnation of id, the owner's, proven by its evidence.
func (s *Service) releasedAny(owner Owner, id string) (Release, error) {
	s.mu.Lock()
	err := s.absenceLocked(id + "'s absence")
	s.mu.Unlock()
	if err != nil {
		return Release{}, err
	}
	evs, err := s.st.evidenceOf(id)
	if err != nil {
		return Release{}, fmt.Errorf("%w: the evidence of %s could not be read: %v", ErrUnproven, id, err)
	}
	for _, ev := range evs {
		if ev.Owner == owner {
			return dispositionOf(ev), nil
		}
	}
	return Release{}, fmt.Errorf("%w: %s is not held, and no evidence of its release is kept: its absence is no authority", ErrUnproven, id)
}

// evidencePath names the evidence of the incarnation ref names: by its
// token; a record written before tokens by its cid and creation time.
func (st *store) evidencePath(ref Ref) string {
	return st.archivePath(Record{ID: ref.ID, Incarnation: ref.Incarnation, CID: ref.CID, Created: ref.Created})
}

// evidence reads the evidence of exactly the incarnation ref names: found
// is false when none is kept; err when what is kept there cannot be read
// as that incarnation's evidence.
func (st *store) evidence(ref Ref) (Record, bool, error) {
	return readEvidence(st.fs, st.evidencePath(ref), ref)
}

func readEvidence(fsys fileSystem, p string, ref Ref) (Record, bool, error) {
	// only through a real released/ (§11.9)
	if found, err := evidenceDir(fsys, filepath.Dir(p)); err != nil || !found {
		return Record{}, false, err
	}
	if _, err := fsys.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	data, err := fsys.ReadFile(p, maxRecordBytes)
	if err != nil {
		return Record{}, false, err
	}
	ev, _, err := decodeRecord(data)
	if err != nil {
		return Record{}, false, err
	}
	if ev.State != StateReleased || !ref.Names(ev) {
		return Record{}, false, fmt.Errorf("%s holds something other than the evidence of %s", p, ref)
	}
	return ev, true, nil
}

// evidenceOf is every evidence kept for id, any incarnation — only in a real
// released/ (§11.9).
func (st *store) evidenceOf(id string) ([]Record, error) {
	if found, err := evidenceDir(st.fs, st.archiveDir()); err != nil || !found {
		return nil, err
	}
	entries, err := st.fs.ReadDir(st.archiveDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, en := range entries {
		name := en.Name()
		if !strings.HasPrefix(name, id+".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := st.fs.ReadFile(filepath.Join(st.archiveDir(), name), maxRecordBytes)
		if err != nil {
			return nil, err
		}
		ev, _, err := decodeRecord(data)
		if err != nil || ev.ID != id || ev.State != StateReleased {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

// ReadEvidence is the evidence of exactly the incarnation ref names in a
// launcher's state directory, read only — without its lock, beside a
// running serve: the operator's view (found false: none is kept).
func ReadEvidence(stateDir string, ref Ref) (Record, bool, error) {
	st := &store{dir: filepath.Join(stateDir, "attempts"), fs: osFS{}}
	return st.evidence(ref)
}

// Evidence is every release's evidence in a launcher's state directory, read
// only, oldest decision first, with what could not be read: the operator's
// report of late or unconfirmed accounting (§11.8). A released/ that is not
// a real directory is reported, and nothing is read through it (§11.9).
func Evidence(stateDir string) ([]Record, []Problem, error) {
	dir := filepath.Join(stateDir, "released")
	found, err := evidenceDir(osFS{}, dir)
	switch {
	case errors.Is(err, ErrUnsafeState):
		return nil, []Problem{{Path: dir, Kind: "foreign", Detail: err.Error()}}, nil
	case err != nil:
		return nil, nil, err
	case !found:
		return nil, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []Record
	var problems []Problem
	for _, en := range entries {
		p := filepath.Join(dir, en.Name())
		if !strings.HasSuffix(en.Name(), ".json") {
			if !strings.HasSuffix(en.Name(), ".json.tmp") {
				problems = append(problems, Problem{Path: p, Kind: "foreign", Detail: "not a release's evidence"})
			}
			continue
		}
		data, err := osFS{}.ReadFile(p, maxRecordBytes)
		if err != nil {
			problems = append(problems, Problem{Path: p, Kind: "unreadable", Detail: err.Error()})
			continue
		}
		ev, kind, err := decodeRecord(data)
		if err != nil {
			problems = append(problems, Problem{Path: p, Kind: kind, Detail: err.Error()})
			continue
		}
		if ev.State != StateReleased {
			problems = append(problems, Problem{Path: p, Kind: "invalid", Detail: fmt.Sprintf("evidence in state %q", ev.State)})
			continue
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return dispositionOf(out[i]).DecidedUnixNano < dispositionOf(out[j]).DecidedUnixNano
	})
	return out, problems, nil
}
