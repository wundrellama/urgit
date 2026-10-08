package launcher

// Settled admission (runner/launcher/INTEGRATION.md §11.10; settled-
// admission ruling 01). Every reserve on the wire names a request token,
// chosen by its owner and kept durably before the reserve is sent. The
// launcher admits a request at most once, ever, and settles it on demand:
// the reservation it admitted, still held; that reservation's release, on
// its durable evidence; or the request's closure, durable before the
// answer, after which it is never admitted. A list, a timeout, a closed
// transport or an elapsed wait settles nothing.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrSettled: a reserve whose request token was admitted before, or has
// been settled — a request is admitted at most once, ever.
var ErrSettled = errors.New("launcher: request already admitted or settled")

// The outcomes of a settlement.
const (
	// SettledAdmitted: the request's reservation is held now
	SettledAdmitted = "admitted"
	// SettledReleased: it admitted a reservation that has since been
	// released, on that release's durable evidence (§11.8)
	SettledReleased = "released"
	// SettledClosed: it holds nothing and never will: never admitted, or
	// admitted and never acknowledged; either way closed for good
	SettledClosed = "closed"
)

// Settlement is the launcher's conclusive answer for one reserve request.
type Settlement struct {
	Outcome string `json:"outcome"`
	// Record is the admitted reservation as it is held now (admitted)
	Record *Record `json:"record,omitempty"`
	// Release is that reservation's release's disposition (released)
	Release *Release `json:"release,omitempty"`
	// Why says how a closure was established (closed)
	Why string `json:"why,omitempty"`
}

// NewRequest is a fresh request token: 32 lowercase hex characters from
// the kernel's random generator, as an incarnation token is.
func NewRequest() (string, error) { return newIncarnation() }

// ValidRequest says whether s is a request token.
func ValidRequest(s string) bool { return validIncarnation(s) }

// requestNote is one request's entry in the ledger (<state>/requests/
// <request>.json): the reservation it admitted, or its closure.
type requestNote struct {
	Request string `json:"request"`
	Attempt string `json:"attempt"`
	Owner   Owner  `json:"owner"`
	Outcome string `json:"outcome"` // admitted | closed
	// the reservation it admitted (admitted)
	ID          string `json:"id,omitempty"`
	Incarnation string `json:"incarnation,omitempty"`
	CID         uint32 `json:"cid,omitempty"`
	Created     int64  `json:"created,omitempty"`
	At          int64  `json:"at"`
}

type requestFile struct {
	Format  int         `json:"format"`
	Request requestNote `json:"request"`
}

// maxNoteBytes bounds one ledger entry: the reader reads nothing larger,
// so nothing larger is written.
const maxNoteBytes = 4 << 10

func (n requestNote) check() error {
	switch {
	case !ValidRequest(n.Request):
		return fmt.Errorf("request %q is not a request token", n.Request)
	case n.Attempt == "" || n.Owner.Daemon == "":
		return errors.New("no attempt or no owner")
	case n.Outcome == SettledClosed:
		if n.ID != "" || n.Incarnation != "" {
			return errors.New("a closed request names a reservation")
		}
	case n.Outcome == SettledAdmitted:
		if n.ID == "" || !validIncarnation(n.Incarnation) {
			return errors.New("an admitted request names no reservation")
		}
	default:
		return fmt.Errorf("outcome %q", n.Outcome)
	}
	return nil
}

// encodeNote is the one encoder of a ledger entry: bounded, and proven to
// reload as this very entry.
func encodeNote(n requestNote) ([]byte, error) {
	if err := n.check(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(requestFile{Format: recordFormat, Request: n}, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > maxNoteBytes {
		return nil, fmt.Errorf("the entry encodes to %d bytes; the reader reads at most %d", len(data), maxNoteBytes)
	}
	if back, err := decodeNote(data); err != nil || back != n {
		return nil, fmt.Errorf("the entry would not reload as written: %v", err)
	}
	return data, nil
}

// decodeNote parses one ledger entry strictly: no unknown field, no
// trailing data, a valid entry.
func decodeNote(data []byte) (requestNote, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f requestFile
	if err := dec.Decode(&f); err != nil {
		return requestNote{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return requestNote{}, errors.New("trailing data after the entry")
	}
	if f.Format != recordFormat {
		return requestNote{}, fmt.Errorf("entry format %d; this launcher reads format %d", f.Format, recordFormat)
	}
	if err := f.Request.check(); err != nil {
		return requestNote{}, err
	}
	return f.Request, nil
}

// requestsDir is <state>/requests: the ledger. Nothing in the launcher
// deletes from it (INTEGRATION.md §11.10).
func (st *store) requestsDir() string { return filepath.Join(filepath.Dir(st.dir), "requests") }

func (st *store) notePath(request string) string {
	return filepath.Join(st.requestsDir(), request+".json")
}

// readNote is request's ledger entry, read only through a real requests/
// directory (§11.9): found is false when none is kept.
func (st *store) readNote(request string) (requestNote, bool, error) {
	if found, err := evidenceDir(st.fs, st.requestsDir()); err != nil || !found {
		return requestNote{}, false, err
	}
	p := st.notePath(request)
	if _, err := st.fs.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return requestNote{}, false, nil
	} else if err != nil {
		return requestNote{}, false, err
	}
	data, err := st.fs.ReadFile(p, maxNoteBytes)
	if err != nil {
		return requestNote{}, false, err
	}
	n, err := decodeNote(data)
	if err != nil {
		return requestNote{}, false, fmt.Errorf("%s: %w", p, err)
	}
	if n.Request != request {
		return requestNote{}, false, fmt.Errorf("%s holds another request's entry", p)
	}
	return n, true, nil
}

// keepNote keeps n as its request's ledger entry durably, and certifies
// every link down to it before it returns, as archive keeps evidence
// (INTEGRATION.md §§11.9, 11.10): requests/ is made if missing and must be a
// real directory; the state directory is fsynced after requests/ was seen,
// at every call; then n is published, or — when this request's entry is
// there already and is n — its bytes and requests/ are fsynced again. An
// entry that says anything else is never overwritten, and refuses.
func (st *store) keepNote(n requestNote) error {
	dir := st.requestsDir()
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return &DurabilityError{Op: "mkdir", Path: dir, Err: err}
	}
	if found, err := evidenceDir(st.fs, dir); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("%s is gone since it was made", dir)
		}
		return err
	}
	if err := st.fs.SyncDir(filepath.Dir(dir)); err != nil {
		return &DurabilityError{Op: "sync-dir", Path: filepath.Dir(dir), Uncertain: true, Err: err}
	}
	final := st.notePath(n.Request)
	if _, err := st.fs.Lstat(final); err == nil {
		data, err := st.fs.ReadFile(final, maxNoteBytes)
		if err != nil {
			return fmt.Errorf("the ledger entry %s holds something unreadable, not overwritten: %w", final, err)
		}
		if had, err := decodeNote(data); err != nil || had != n {
			return fmt.Errorf("the ledger entry %s says something else, not overwritten", final)
		}
		if err := st.syncFile(final); err != nil {
			return &DurabilityError{Op: "sync", Path: final, Uncertain: true, Err: err}
		}
		if err := st.fs.SyncDir(dir); err != nil {
			return &DurabilityError{Op: "sync-dir", Path: dir, Uncertain: true, Err: err}
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &DurabilityError{Op: "lstat", Path: final, Err: err}
	}
	data, err := encodeNote(n)
	if err != nil {
		return &DurabilityError{Op: "encode", Path: final, Err: err}
	}
	ledger := &store{dir: dir, prefix: st.prefix, fs: st.fs}
	_, err = ledger.publishBytes(data, final, final+".tmp")
	return err
}

// admissibleLocked is nil when request may be admitted: this process has
// not begun to settle it, no held record carries it, and the ledger holds
// no entry for it — a request is admitted at most once, ever (under s.mu).
func (s *Service) admissibleLocked(request string) error {
	if s.requests[request] {
		return fmt.Errorf("%w: request %s has been settled", ErrSettled, request)
	}
	for _, e := range s.vms {
		if e.rec.Request == request {
			return fmt.Errorf("%w: request %s admitted %s already", ErrSettled, request, e.rec.Ref())
		}
	}
	n, found, err := s.st.readNote(request)
	switch {
	case err != nil:
		return fmt.Errorf("%w: request %s's ledger entry could not be read, so it may have been admitted or settled: %v", ErrUnsafeState, request, err)
	case found:
		return fmt.Errorf("%w: request %s was %s before", ErrSettled, request, n.Outcome)
	}
	return nil
}

// heldRequestLocked is the held record that request admitted, if any
// (under s.mu).
func (s *Service) heldRequestLocked(request string) *entry {
	for _, e := range s.vms {
		if e.rec.Request == request {
			return e
		}
	}
	return nil
}

// Settle is the conclusive answer for request, owner's reserve for attempt
// (INTEGRATION.md §11.10): admitted — the reservation it admitted is held
// now, its record returned (a reserve still publishing it is waited for);
// released — it admitted a reservation that has since been released, on
// that release's durable evidence; closed — it holds nothing and never
// will: the ledger says it was closed, or that it admitted a reservation
// that was never acknowledged, or it was never seen and is closed now,
// durably, before the answer. From its first settle on, this process
// admits it no more. A settlement in progress keeps the state: no terminal
// call gives the state lock up before it ends, and once the service stops
// none begins (ErrClosed; §11.11). Anything it cannot answer conclusively it refuses —
// another owner's request or another attempt's, an inventory that is not
// authoritative, a ledger or evidence it cannot read, a closure or a
// certification not durable — and a refusal closes and releases nothing.
func (s *Service) Settle(owner Owner, attempt, request string) (Settlement, error) {
	switch {
	case !ValidRequest(request):
		return Settlement{}, fmt.Errorf("%w: request %q is not a request token (32 lowercase hex characters)", ErrInvalid, request)
	case attempt == "":
		return Settlement{}, fmt.Errorf("%w: attempt is required", ErrInvalid)
	}
	s.mu.Lock()
	for {
		if s.closing {
			s.mu.Unlock()
			return Settlement{}, ErrClosed
		}
		if e := s.heldRequestLocked(request); e != nil {
			switch {
			case e.rec.Owner != owner:
				s.mu.Unlock()
				return Settlement{}, ErrNotOwner
			case e.rec.Attempt != attempt:
				s.mu.Unlock()
				return Settlement{}, fmt.Errorf("%w: request %s is attempt %s's, not %s's", ErrStale, request, e.rec.Attempt, attempt)
			case e.busy == "reserve":
				// its admission is still being published: its outcome, never
				// this instant, answers
				s.cond.Wait()
				continue
			}
			rec := e.rec
			s.mu.Unlock()
			return Settlement{Outcome: SettledAdmitted, Record: &rec}, nil
		}
		if !s.settling[request] {
			break
		}
		s.cond.Wait()
	}
	// not held: every answer from here rests on an absence
	if err := s.absenceLocked("request " + request + "'s absence"); err != nil {
		s.mu.Unlock()
		return Settlement{}, err
	}
	// counted from here, where closing was checked, to its end on every
	// path: no terminal call gives the state lock up while it may still
	// write the ledger (INTEGRATION.md §11.11; independent review 05)
	s.requests[request], s.settling[request] = true, true
	s.unheld++
	s.mu.Unlock()
	st, err := s.settleUnheld(owner, attempt, request)
	s.mu.Lock()
	s.unheld--
	delete(s.settling, request)
	s.cond.Broadcast()
	s.mu.Unlock()
	return st, err
}

// settleUnheld answers a request no held record carries, from the ledger
// and the evidence, every answer certified by this process first (a
// settle of request holds settling[request]).
func (s *Service) settleUnheld(owner Owner, attempt, request string) (Settlement, error) {
	n, found, err := s.st.readNote(request)
	if err != nil {
		return Settlement{}, fmt.Errorf("%w: request %s's ledger entry could not be read: %v", ErrUnsafeState, request, err)
	}
	if !found {
		// never seen: closed now, durably, before the answer
		n = requestNote{Request: request, Attempt: attempt, Owner: owner, Outcome: SettledClosed, At: s.cfg.now().Unix()}
		if err := s.st.keepNote(n); err != nil {
			return Settlement{}, fmt.Errorf("%w: request %s was never admitted, but its closure is not durable yet: %v", ErrNotDurable, request, err)
		}
		return Settlement{Outcome: SettledClosed, Why: "never admitted; closed by this settlement"}, nil
	}
	switch {
	case n.Owner != owner:
		return Settlement{}, fmt.Errorf("%w: request %s is another owner's", ErrNotOwner, request)
	case n.Attempt != attempt:
		return Settlement{}, fmt.Errorf("%w: request %s is attempt %s's, not %s's", ErrStale, request, n.Attempt, attempt)
	}
	// the entry as found is certified by this process before it answers
	// (§11.9): visible is not durable
	if err := s.st.keepNote(n); err != nil {
		return Settlement{}, fmt.Errorf("%w: request %s's ledger entry is not certified: %v", ErrNotDurable, request, err)
	}
	if n.Outcome == SettledClosed {
		return Settlement{Outcome: SettledClosed, Why: "closed before"}, nil
	}
	// admitted, and not held now: released, on its evidence — or never
	// acknowledged, withdrawn without a release
	ref := Ref{ID: n.ID, Incarnation: n.Incarnation, CID: n.CID, Created: n.Created}
	ev, found, err := s.st.evidence(ref)
	switch {
	case err != nil:
		return Settlement{}, fmt.Errorf("%w: request %s admitted %s, whose evidence could not be read: %v", ErrUnproven, request, ref, err)
	case found && ev.Owner != owner:
		return Settlement{}, fmt.Errorf("%w: request %s admitted %s, whose evidence is another owner's", ErrUnproven, request, ref)
	case found:
		rel := dispositionOf(ev)
		return Settlement{Outcome: SettledReleased, Release: &rel}, nil
	}
	return Settlement{Outcome: SettledClosed, Why: fmt.Sprintf("it admitted %s, which was never acknowledged: no record and no evidence of a release", ref)}, nil
}
