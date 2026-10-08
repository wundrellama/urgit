package state

// The execution ledger (assignment-replay ruling 01, Q14; runner/launcher/
// INTEGRATION.md §11.14, "An assignment runs at most once here"): one
// record per attempt this runner takes for execution, in taken/, made
// durable before the attempt crosses into execution, and one in finished/
// once its run returns. A record is named by the attempt's canonical @uv
// spelling; its existence is the record, and its content evidence for the
// operator. Nothing here deletes, rewrites or expires a record (Q10 KEEP),
// and the ledger has no limit of its own: a filesystem that refuses a
// record fails the write, and the caller does not start the attempt.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"urgit/runner/internal/sig"
)

// Ledger is the execution ledger in Dir: <state dir>/executions.
type Ledger struct {
	Dir string
	// ops writes its records (osOps when nil); tests fail around the real
	// call
	ops ledgerOps
}

// ledgerOps is what writing a record does to the filesystem, one
// operation each.
type ledgerOps interface {
	CreateNew(path string) (writable, error)
	SyncDir(dir string) error
}

// CreateNew creates path exclusively, never through a symlink.
func (osOps) CreateNew(path string) (writable, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
}

func (l *Ledger) fs() ledgerOps {
	if l.ops == nil {
		return osOps{}
	}
	return l.ops
}

// LedgerFor is the ledger beside the state file at path.
func LedgerFor(path string) *Ledger {
	return &Ledger{Dir: filepath.Join(filepath.Dir(path), "executions")}
}

// Execution is a taken record's evidence: the attempt, when it was taken,
// and what the assignment said of it.
type Execution struct {
	Attempt  string `json:"attempt"`
	At       int64  `json:"at"`
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Workflow string `json:"workflow,omitempty"`
	Job      string `json:"job,omitempty"`
	Expiry   int64  `json:"expiry,omitempty"`
	Nonce    string `json:"nonce,omitempty"`
}

// History is when the ledger's history began (HISTORY): what ran here
// before it left no record (QUESTIONS-SOURCE-01 §15). Since is 0 when that
// is unknown (Open).
type History struct {
	Since int64 `json:"since"`
	// Enrolled: the history began with the runner's enrollment, and is
	// complete; otherwise it began with a state file kept before it, of
	// StateFormat (0: an earlier version's)
	Enrolled    bool `json:"enrolled"`
	StateFormat int  `json:"state_format"`
}

// ErrTaken is a taken record that exists already.
var ErrTaken = errors.New("the attempt is taken for execution already")

// LedgerError is a record that was not written durably: the step, and
// whether its file exists after the failure (then it may be there after a
// restart, and counts).
type LedgerError struct {
	Step    string // name, encode, create, write, sync, close, sync-dir
	Path    string
	Created bool
	Err     error
}

func (e *LedgerError) Error() string {
	msg := fmt.Sprintf("execution ledger %s: %s: %v", e.Path, e.Step, e.Err)
	if e.Created {
		msg += " (the record exists, its durability unproven)"
	}
	return msg
}

func (e *LedgerError) Unwrap() error { return e.Err }

// Open readies the ledger (§11.14). A new one is made: its directories,
// each synced into its parent, then HISTORY (h), durably, last; one whose
// making a stop cut short — no HISTORY yet, so nothing was recorded in it —
// is completed. A ledger made before (HISTORY there), or one this runner
// kept (kept: its state file says so), is never made again: missing, or
// missing a record directory, it is an error, since what this runner took
// went with it; so is a directory that is no directory (a file, a
// symlink), and anything that cannot be made. No daemon runs on a ledger
// that cannot record, or has lost its records. Open answers the HISTORY
// the ledger holds: the operator's evidence, on which no record depends.
// One missing from a ledger kept, no regular file, or unreadable answers a
// zero History (when the history began is unknown), and is left as it is.
func (l *Ledger) Open(h History, kept bool) (History, error) {
	path := filepath.Join(l.Dir, "HISTORY")
	info, herr := os.Lstat(path)
	made := kept || !errors.Is(herr, fs.ErrNotExist)
	for _, dir := range []string{l.Dir, filepath.Join(l.Dir, "taken"), filepath.Join(l.Dir, "finished")} {
		if err := l.ensureDir(dir, made); err != nil {
			return History{}, err
		}
	}
	switch {
	case herr == nil && !info.Mode().IsRegular():
		return History{}, nil
	case herr == nil:
		var have History
		if data, err := os.ReadFile(path); err != nil || json.Unmarshal(data, &have) != nil {
			return History{}, nil
		}
		return have, nil
	case !errors.Is(herr, fs.ErrNotExist):
		return History{}, herr
	case kept:
		return History{}, nil
	}
	if err := l.write(l.Dir, "HISTORY", h, true); err != nil {
		return History{}, err
	}
	return h, nil
}

// ensureDir makes dir, synced into its parent, unless it is a directory
// already — or the ledger was made before (made): then its absence is an
// error. A symlink or any other file is an error.
func (l *Ledger) ensureDir(dir string, made bool) error {
	info, err := os.Lstat(dir)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("execution ledger %s: not a directory", dir)
	case !errors.Is(err, fs.ErrNotExist):
		return err
	case made:
		return fmt.Errorf("execution ledger %s is missing, and the ledger was made before: the attempts this runner took went with it; nothing runs until it is restored, or the runner is enrolled anew", dir)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return l.fs().SyncDir(filepath.Dir(dir))
}

// Has says what the ledger holds of attempt: taken (a record in taken/ or
// in finished/), and finished. An error is no answer, and the caller takes
// the attempt for taken (§11.14).
func (l *Ledger) Has(attempt string) (taken, finished bool, err error) {
	if err := recordName(attempt); err != nil {
		return true, false, err
	}
	taken, err = exists(filepath.Join(l.Dir, "taken", attempt))
	if err != nil {
		return true, false, err
	}
	finished, err = exists(filepath.Join(l.Dir, "finished", attempt))
	if err != nil {
		return true, false, err
	}
	return taken || finished, finished, nil
}

func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, err
}

// Take records e.Attempt as taken for execution, durably. ErrTaken: its
// record exists already. Any other error leaves the attempt unstarted: a
// *LedgerError says whether the record's file was created.
func (l *Ledger) Take(e Execution) error {
	return l.write(filepath.Join(l.Dir, "taken"), e.Attempt, e, false)
}

// Finish records attempt as finished, durably; a record there already is
// no error.
func (l *Ledger) Finish(attempt string, at int64) error {
	err := l.write(filepath.Join(l.Dir, "finished"), attempt, struct {
		Attempt string `json:"attempt"`
		At      int64  `json:"at"`
	}{attempt, at}, false)
	if errors.Is(err, ErrTaken) {
		return nil
	}
	return err
}

// recordName refuses a name that is not an attempt's canonical spelling:
// no other text becomes a path.
func recordName(attempt string) error {
	if canon, err := sig.CanonicalUV(attempt); err != nil || canon != attempt {
		return &LedgerError{Step: "name", Path: attempt, Err: errors.New("not an attempt's canonical spelling")}
	}
	return nil
}

// write creates dir/name exclusively (never through a symlink), writes v,
// and syncs the file and dir. special: a name of the ledger's own
// (HISTORY), not an attempt's.
func (l *Ledger) write(dir, name string, v any, special bool) error {
	if !special {
		if err := recordName(name); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, name)
	fail := func(step string, err error) error {
		created, _ := exists(path)
		return &LedgerError{Step: step, Path: path, Created: created, Err: err}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fail("encode", err)
	}
	f, err := l.fs().CreateNew(path)
	if errors.Is(err, fs.ErrExist) {
		return ErrTaken
	}
	if err != nil {
		return fail("create", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return fail("write", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fail("sync", err)
	}
	if err := f.Close(); err != nil {
		return fail("close", err)
	}
	if err := l.fs().SyncDir(dir); err != nil {
		return fail("sync-dir", err)
	}
	return nil
}
