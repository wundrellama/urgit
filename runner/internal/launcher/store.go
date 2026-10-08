package launcher

// The durable half of the lifecycle (runner/launcher/LIFECYCLE.md §2, §5):
// one record file per reservation, published by write-temp, fsync, rename,
// fsync-directory and withdrawn by unlink, fsync-directory; an exclusive
// lock so one Service owns a state directory; and the open-time
// classification that loads what it can account for and fences on
// anything else.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// recordFormat is the version of the persisted envelope. A file in any
// other format — including the flat records written before this format
// existed — is refused at open with a diagnosis, never migrated.
const recordFormat = 1

// recordFile is the persisted envelope of one record.
type recordFile struct {
	Format int    `json:"format"`
	Record Record `json:"record"`
}

// maxRecordBytes bounds one record file: the loader reads nothing larger,
// so no publication writes anything larger (encodeRecord).
const maxRecordBytes = 1 << 20

// recordGrowthBytes is the room an admitted record keeps under
// maxRecordBytes for what its lifecycle adds after admission: a reason
// (maxReasonBytes), a disk path (maxPathBytes), the obligation a teardown
// began under and an incident's history (maxAttempts attempts and a
// release, each of bounded text: maxDetailBytes) — JSON escapes at most 6
// bytes per byte of any of them — a pid, a network index, the holdings,
// timestamps and a revision. Admission refuses a request whose record would
// leave less, so no later publication of an acknowledged record, nor its
// archived evidence, outgrows the loader.
const recordGrowthBytes = 128 << 10

// maxReasonBytes bounds a persisted reason: diagnostic text, made valid
// UTF-8 and cut at a rune boundary when encoded (boundReason).
const maxReasonBytes = 4 << 10

// maxPathBytes bounds a disk path the host reports (PATH_MAX).
const maxPathBytes = 4096

// maxUnit bounds every persisted size (cpus, MiB) so no sum over the
// records can overflow; Reserve refuses what the loader would refuse.
const maxUnit = 1 << 40

// A guest's vsock context id: 0, 1 and 2 are the hypervisor's, the
// local and the host's, and 0xFFFFFFFF is VMADDR_CID_ANY.
const (
	minCID = 3
	maxCID = math.MaxUint32 - 1
)

// A network index names a VM's veth pair and its per-VM nft chain, and
// the host adapter derives both of the VM's /30 subnets from 14 bits of
// it (runner/cmd/urgit-vm-launcher netAddrs, "i ≥ 1"): an index outside
// [MinNetIndex, MaxNetIndex] would alias another VM's addresses. The
// allocator hands out only this domain; the loader also accepts 0, which
// launchers before this bound allocated first.
const (
	MinNetIndex = 1
	MaxNetIndex = 1<<14 - 1
)

// fileSystem is the store's adapter: each method is one kernel operation
// (SyncDir is the directory's open, fsync and close). osFS is production;
// tests wrap it to fail before or after the real call.
type fileSystem interface {
	OpenFile(name string, flag int, perm os.FileMode) (writableFile, error)
	Rename(oldpath, newpath string) error
	Unlink(name string) error
	SyncDir(name string) error
	ReadDir(name string) ([]os.DirEntry, error)
	Lstat(name string) (os.FileInfo, error)
	ReadFile(name string, max int64) ([]byte, error)
}

type writableFile interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
	Stat() (os.FileInfo, error)
	Truncate(size int64) error
}

type osFS struct{}

func (osFS) OpenFile(name string, flag int, perm os.FileMode) (writableFile, error) {
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (osFS) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

// Unlink never removes a directory (unlink(2) refuses one), unlike
// os.Remove: a foreign directory in a record's path is left alone.
func (osFS) Unlink(name string) error {
	if err := syscall.Unlink(name); err != nil {
		return &os.PathError{Op: "unlink", Path: name, Err: err}
	}
	return nil
}

func (osFS) SyncDir(name string) error {
	d, err := os.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

func (osFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }

func (osFS) Lstat(name string) (os.FileInfo, error) { return os.Lstat(name) }

// ReadFile reads a regular file without following a symlink or blocking
// on a FIFO, refusing anything larger than max.
func (osFS) ReadFile(name string, max int64) ([]byte, error) {
	f, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file (%s)", name, kindOf(st.Mode()))
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: larger than %d bytes", name, max)
	}
	return data, nil
}

// outcome of a publication: what the authoritative record may hold now.
type outcome int

const (
	notWritten outcome = iota // certainly unchanged: the rename was never issued
	written                   // renamed and the directory synced
	uncertain                 // the rename may have happened, or its durability is unproven
)

// DurabilityError is a publication or withdrawal that did not complete:
// the step, the path and whether the authoritative record may have
// changed. errors.Is(err, ErrNotDurable) holds for every one.
type DurabilityError struct {
	Op        string // encode, create-temp, write, sync, close, rename, sync-dir, unlink
	Path      string
	Uncertain bool  // the record may hold the new bytes (publish) or may still exist durably (withdraw)
	Err       error // the kernel's (or the adapter's) own error
	Leftover  error // a temp this publication created and could not remove
}

func (e *DurabilityError) Error() string {
	msg := fmt.Sprintf("%v: %s %s: %v", ErrNotDurable, e.Op, e.Path, e.Err)
	if e.Uncertain {
		msg += " (outcome uncertain)"
	}
	if e.Leftover != nil {
		msg += fmt.Sprintf("; temp left behind: %v", e.Leftover)
	}
	return msg
}

func (e *DurabilityError) Unwrap() error        { return e.Err }
func (e *DurabilityError) Is(target error) bool { return target == ErrNotDurable }

// store is one launcher's record directory (<state>/attempts); prefix
// is its id namespace.
type store struct {
	dir    string
	prefix string
	fs     fileSystem
}

func (st *store) path(id string) string     { return filepath.Join(st.dir, id+".json") }
func (st *store) tempPath(id string) string { return st.path(id) + ".tmp" }

// encodeRecord is the one encoder of a record file. It normalizes the
// only field that may be lossy (the diagnostic reason: boundReason),
// refuses anything larger than the loader reads, and proves the bytes
// reload as this very record — decoded strictly and validated exactly as
// open does, then compared field by field — so no publication writes,
// and no acknowledgement follows, a record the loader would refuse or
// read differently. It returns the bytes and the record as encoded.
func encodeRecord(prefix string, r Record) ([]byte, Record, error) {
	r = normalized(r)
	data, err := json.MarshalIndent(recordFile{Format: recordFormat, Record: r}, "", "  ")
	if err != nil {
		return nil, r, err
	}
	data = append(data, '\n')
	if len(data) > maxRecordBytes {
		return nil, r, fmt.Errorf("the record encodes to %d bytes; the loader reads at most %d", len(data), maxRecordBytes)
	}
	if !validID(prefix, r.ID) {
		return nil, r, fmt.Errorf("id %q is outside the namespace %s", r.ID, prefix)
	}
	back, _, err := decodeRecord(data)
	if err != nil {
		return nil, r, fmt.Errorf("the record would not decode: %w", err)
	}
	validate := back
	if r.State == StateReleased {
		// archived evidence: everything a live record is checked for, in
		// the state only the archive holds, with the disposition only the
		// archive keeps (INTEGRATION.md §11.8)
		validate.State, validate.Release = StateQuarantined, nil
	}
	if _, err := validateRecord(prefix, r.ID, validate); err != nil {
		return nil, r, fmt.Errorf("the record would not load: %w", err)
	}
	if !reflect.DeepEqual(back, r) {
		return nil, r, errors.New("the record would not reload as written: a field does not survive its encoding")
	}
	return data, r, nil
}

// boundReason makes a reason valid UTF-8 (a host's message may not be)
// and cuts it to maxReasonBytes at a rune boundary.
func boundReason(s string) string { return boundText(s, maxReasonBytes) }

// boundText makes s valid UTF-8 and cuts it to max bytes at a rune
// boundary.
func boundText(s string, max int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= max {
		return s
	}
	const cut = " … (truncated)"
	n := max - len(cut)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + cut
}

// maxLabelBytes bounds a record's label: display text for the operator.
const maxLabelBytes = 200

// boundLabel makes a label printable — valid UTF-8, no control
// characters — and cuts it to maxLabelBytes.
func boundLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
	return boundText(s, maxLabelBytes)
}

// normalized is r as every publication writes it: its diagnostic texts
// bounded and valid, its empty lists nil, its incident within its bounds.
func normalized(r Record) Record {
	r.Alive, r.Liveness = false, ""
	r.Reason = boundReason(r.Reason)
	r.Label = boundLabel(r.Label)
	r.CleanupTrigger = boundText(r.CleanupTrigger, maxDetailBytes)
	if r.Release != nil {
		rel := *r.Release
		rel.By, rel.Trigger = boundText(rel.By, 64), boundText(rel.Trigger, maxDetailBytes)
		r.Release = &rel
	}
	if len(r.Destinations) == 0 {
		r.Destinations = nil
	}
	if r.Incident != nil {
		inc := *r.Incident
		inc.Trigger = boundText(inc.Trigger, maxDetailBytes)
		if len(inc.Attempts) > maxAttempts {
			inc.Attempts = inc.Attempts[len(inc.Attempts)-maxAttempts:]
		}
		attempts := make([]CleanupAttempt, len(inc.Attempts))
		for i, a := range inc.Attempts {
			a.By, a.Result, a.Detail = boundText(a.By, 64), boundText(a.Result, 64), boundText(a.Detail, maxDetailBytes)
			if len(a.Left) > maxLeftEntries {
				a.Left = a.Left[:maxLeftEntries]
			}
			left := make([]string, len(a.Left))
			for j, l := range a.Left {
				left[j] = boundText(l, 64)
			}
			if len(left) == 0 {
				left = nil
			}
			a.Left = left
			attempts[i] = a
		}
		if len(attempts) == 0 {
			attempts = nil
		}
		inc.Attempts = attempts
		r.Incident = &inc
	}
	return r
}

// publish makes r the durable record of r.ID. Only `written` may be
// acknowledged or followed by the host effect the record names; a record
// encodeRecord refuses is never written.
func (st *store) publish(r Record) (outcome, error) {
	return st.publishAt(r, st.path(r.ID), st.tempPath(r.ID))
}

// publishAt is the one publication path: r's encoding written to tmp,
// fsynced, renamed to final and st.dir fsynced (both paths in st.dir). It
// publishes the live records and, in archive's evidence store, a released
// incident's evidence.
func (st *store) publishAt(r Record, final, tmp string) (outcome, error) {
	data, _, err := encodeRecord(st.prefix, r)
	if err != nil {
		return notWritten, &DurabilityError{Op: "encode", Path: final, Err: err}
	}
	return st.publishBytes(data, final, tmp)
}

// publishBytes is that path's syscalls, for bytes already encoded and
// proven to reload: a record's or its evidence's (publishAt), or a
// request's ledger entry (keepNote; INTEGRATION.md §11.10).
func (st *store) publishBytes(data []byte, final, tmp string) (outcome, error) {
	// the open neither follows a link, nor truncates, nor waits for a
	// FIFO's reader: a directory, symlink or FIFO someone put here fails
	// it (EISDIR, ELOOP, ENXIO) and is left untouched
	f, err := st.fs.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return notWritten, &DurabilityError{Op: "create-temp", Path: tmp, Err: err}
	}
	// what it found is overwritten only if it is this launcher's own
	// leftover: a regular file with no other name
	if err := ownTemp(f); err != nil {
		f.Close()
		return notWritten, &DurabilityError{Op: "create-temp", Path: tmp, Err: err}
	}
	abandon := func(op string, err error) (outcome, error) {
		de := &DurabilityError{Op: op, Path: tmp, Err: err}
		if uerr := st.fs.Unlink(tmp); uerr != nil && !errors.Is(uerr, fs.ErrNotExist) {
			de.Leftover = uerr
		}
		return notWritten, de
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return abandon("truncate", err)
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.Close()
		return abandon("write", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return abandon("sync", err)
	}
	if err := f.Close(); err != nil {
		return abandon("close", err)
	}
	if err := st.fs.Rename(tmp, final); err != nil {
		de := &DurabilityError{Op: "rename", Path: final, Uncertain: true, Err: err}
		if uerr := st.fs.Unlink(tmp); uerr != nil && !errors.Is(uerr, fs.ErrNotExist) {
			de.Leftover = uerr
		}
		return uncertain, de
	}
	if err := st.fs.SyncDir(st.dir); err != nil {
		return uncertain, &DurabilityError{Op: "sync-dir", Path: st.dir, Uncertain: true, Err: err}
	}
	return written, nil
}

// ownTemp says whether an opened publication temp is this launcher's to
// overwrite: a regular file with exactly one name.
func ownTemp(f writableFile) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("a %s occupies the publication path", kindOf(fi.Mode()))
	}
	if n := links(fi); n != 1 {
		return fmt.Errorf("the publication path is a file with %d names, not this launcher's", n)
	}
	return nil
}

// links is a file's hard link count (1 where the platform does not say).
func links(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}

// remove withdraws id's record durably. unlinked says whether the record
// file is gone from the directory — looked up again after a failed
// unlink, which may still have acted; err is a failure of either step.
// With any error the caller keeps the charge: a file still there keeps
// its evidence, and a gone file's absence is not durable until the
// directory's fsync succeeds.
func (st *store) remove(id string) (unlinked bool, err error) {
	p := st.path(id)
	if uerr := st.fs.Unlink(p); uerr != nil && !errors.Is(uerr, fs.ErrNotExist) {
		if _, lerr := st.fs.Lstat(p); !errors.Is(lerr, fs.ErrNotExist) {
			return false, &DurabilityError{Op: "unlink", Path: p, Uncertain: true, Err: uerr}
		}
	}
	if err := st.fs.SyncDir(st.dir); err != nil {
		return true, &DurabilityError{Op: "sync-dir", Path: st.dir, Uncertain: true, Err: err}
	}
	return true, nil
}

// lockState takes <state>/launcher.lock exclusively for the caller's
// lifetime. flock locks belong to the open file, so a second Service in
// the same process is refused like a second process.
func lockState(stateDir string) (*os.File, error) {
	path := filepath.Join(stateDir, "launcher.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("launcher: state lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrStateBusy, path)
		}
		return nil, fmt.Errorf("launcher: state lock %s: %w", path, err)
	}
	return f, nil
}

// Problem is a state entry the loader could not account for. Its file is
// left exactly as found.
type Problem struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // foreign, not-regular, unreadable, malformed, unsupported, identity, invalid, duplicate; uncertified (the records directory, Service.Problems)
	Detail string `json:"detail"`
}

func (p Problem) String() string { return p.Kind + " " + p.Path + ": " + p.Detail }

// Snapshot is what a state directory holds, classified.
type Snapshot struct {
	Records   []Record  // loaded (charged), by creation time then id
	Leftovers []string  // this launcher's own unfinished publications (never authoritative)
	Problems  []Problem // anything else: the service is fenced while any remains
	// Interrupted names the records found `stopping`: a teardown whose
	// outcome was never recorded. They are loaded quarantined (see
	// interruptedTeardown) and their cleanup is still owed.
	Interrupted map[string]bool
	// Authorized is the evidence of the records found `stopping` whose
	// release a timely cleanup authorized before the restart (INTEGRATION.md
	// §11.8): each is loaded as that release, its accounting pending — not
	// an incident — by the id it names
	Authorized map[string]Record
}

// interruptedTeardown is how a record found `stopping` is loaded. Every
// quarantine decision is made after the record's `stopping` was written,
// so this record may have ended in a quarantine whose own publication
// failed: it is quarantined — an incident — and only the operator's
// release, after its cleanup is resolved, returns its charge.
func interruptedTeardown(r Record) Record {
	reason := "teardown interrupted: its outcome was never recorded and may have been a quarantine; only the operator's release, after its cleanup is resolved, returns its charge"
	if r.Reason != "" {
		reason += " (was: " + r.Reason + ")"
	}
	r.State, r.Reason = StateQuarantined, reason
	return r
}

// ReadState classifies a launcher's state directory without taking its
// lock or touching the host: the `list` subcommand's read-only view.
func ReadState(stateDir, idPrefix string) (Snapshot, error) {
	if idPrefix == "" {
		idPrefix = defaultIDPrefix
	}
	snap, err := readState(osFS{}, filepath.Join(stateDir, "attempts"), idPrefix)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, nil
	}
	return snap, err
}

func readState(fsys fileSystem, dir, prefix string) (Snapshot, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("launcher: read state %s: %w", dir, err)
	}
	var snap Snapshot
	problem := func(path, kind, detail string) {
		snap.Problems = append(snap.Problems, Problem{Path: path, Kind: kind, Detail: detail})
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, ".json.tmp"):
			id := strings.TrimSuffix(name, ".json.tmp")
			if !validID(prefix, id) {
				problem(p, "foreign", "a publication temp outside this launcher's id namespace "+prefix)
				continue
			}
			fi, err := fsys.Lstat(p)
			if err != nil {
				problem(p, "unreadable", err.Error())
				continue
			}
			if !fi.Mode().IsRegular() {
				problem(p, "not-regular", "a "+kindOf(fi.Mode())+" occupies a record's publication path")
				continue
			}
			if n := links(fi); n != 1 {
				problem(p, "foreign", fmt.Sprintf("a file with %d names occupies a record's publication path", n))
				continue
			}
			snap.Leftovers = append(snap.Leftovers, p)
		case strings.HasSuffix(name, ".json"):
			id := strings.TrimSuffix(name, ".json")
			if !validID(prefix, id) {
				problem(p, "foreign", "a record name outside this launcher's id namespace "+prefix)
				continue
			}
			fi, err := fsys.Lstat(p)
			if err != nil {
				problem(p, "unreadable", err.Error())
				continue
			}
			if !fi.Mode().IsRegular() {
				problem(p, "not-regular", "a "+kindOf(fi.Mode())+" occupies a record path")
				continue
			}
			data, err := fsys.ReadFile(p, maxRecordBytes)
			if err != nil {
				problem(p, "unreadable", err.Error())
				continue
			}
			r, kind, err := decodeRecord(data)
			if err != nil {
				problem(p, kind, err.Error())
				continue
			}
			if kind, err := validateRecord(prefix, id, r); err != nil {
				problem(p, kind, err.Error())
				continue
			}
			if r.State == StateStopping {
				// a timely cleanup's evidence of exactly this incarnation
				// authorized its release before the restart: its accounting
				// is pending (INTEGRATION.md §11.8); without it, the outcome
				// was never recorded, and timing kept in memory is gone
				ev, found, err := readEvidence(fsys, evidencePathIn(dir, r), r.Ref())
				if err == nil && found && timely(ev, r) {
					if snap.Authorized == nil {
						snap.Authorized = map[string]Record{}
					}
					snap.Authorized[r.ID] = ev
				} else {
					if snap.Interrupted == nil {
						snap.Interrupted = map[string]bool{}
					}
					snap.Interrupted[r.ID] = true
					r = interruptedTeardown(r)
				}
			}
			snap.Records = append(snap.Records, r)
		default:
			problem(p, "foreign", "not a record or a publication temp of this launcher")
		}
	}
	sort.Slice(snap.Records, func(i, j int) bool {
		a, b := snap.Records[i], snap.Records[j]
		if a.Created != b.Created {
			return a.Created < b.Created
		}
		return a.ID < b.ID
	})
	// two records sharing a vsock cid or a network index are both kept
	// (charged, cleanable) and fence the service
	cids := map[uint32]string{}
	nets := map[int]string{}
	for _, r := range snap.Records {
		if other, ok := cids[r.CID]; ok {
			problem(filepath.Join(dir, r.ID+".json"), "duplicate", fmt.Sprintf("cid %d is also %s's", r.CID, other))
		}
		cids[r.CID] = r.ID
		if r.HasNetwork {
			if other, ok := nets[r.NetIndex]; ok {
				problem(filepath.Join(dir, r.ID+".json"), "duplicate", fmt.Sprintf("network index %d is also %s's", r.NetIndex, other))
			}
			nets[r.NetIndex] = r.ID
		}
	}
	return snap, nil
}

// decodeRecord parses one record file strictly: the envelope and the
// record admit no unknown field and no trailing data.
func decodeRecord(data []byte) (Record, string, error) {
	var probe struct {
		Format *int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Record{}, "malformed", fmt.Errorf("not a JSON record: %v", err)
	}
	if probe.Format == nil {
		return Record{}, "unsupported", errors.New("no format field: not a record of this launcher version (the pre-repair flat format is not migrated)")
	}
	if *probe.Format != recordFormat {
		return Record{}, "unsupported", fmt.Errorf("record format %d; this launcher reads format %d", *probe.Format, recordFormat)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f recordFile
	if err := dec.Decode(&f); err != nil {
		return Record{}, "malformed", err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Record{}, "malformed", errors.New("trailing data after the record")
	}
	f.Record.Alive, f.Record.Liveness = false, ""
	return f.Record, "", nil
}

// validateRecord checks everything a record may authorize: identity,
// owner, charge, state and holdings.
func validateRecord(prefix, fileID string, r Record) (string, error) {
	switch {
	case r.ID != fileID:
		return "identity", fmt.Errorf("record id %q in the file of %q", r.ID, fileID)
	case r.Attempt == "":
		return "identity", errors.New("no attempt")
	case IDFor(prefix, r.Attempt) != r.ID:
		return "identity", fmt.Errorf("id %s is not attempt %q's (%s)", r.ID, r.Attempt, IDFor(prefix, r.Attempt))
	case r.Incarnation != "" && !validIncarnation(r.Incarnation):
		// a record written before tokens has none (INTEGRATION.md §11.1),
		// and nothing guesses one for it
		return "identity", fmt.Errorf("incarnation %q is not a token (32 lowercase hex characters)", r.Incarnation)
	case r.Request != "" && !ValidRequest(r.Request):
		return "identity", fmt.Errorf("request %q is not a request token (32 lowercase hex characters)", r.Request)
	case r.Owner.Daemon == "":
		return "invalid", errors.New("no owner daemon")
	case r.Image == "":
		return "invalid", errors.New("no image")
	case r.CPUs < 1 || r.CPUs > maxUnit || r.MemoryMiB < 128 || r.MemoryMiB > maxUnit || r.DiskMiB < 1 || r.DiskMiB > maxUnit:
		return "invalid", fmt.Errorf("charge cpus %d memory_mib %d disk_mib %d", r.CPUs, r.MemoryMiB, r.DiskMiB)
	case r.DeadlineUnix <= 0:
		return "invalid", fmt.Errorf("deadline %d", r.DeadlineUnix)
	case r.CID < minCID || r.CID > maxCID:
		return "invalid", fmt.Errorf("cid %d outside [%d, %d]", r.CID, minCID, uint32(maxCID))
	case r.Network == "":
		return "invalid", errors.New("no network profile")
	case r.Network == "locked" && (len(r.Destinations) > 0 || r.HasNetwork):
		return "invalid", errors.New("a locked record with a network")
	case r.Network != "locked" && len(r.Destinations) == 0:
		return "invalid", fmt.Errorf("network profile %s with no destinations", r.Network)
	case r.NetIndex < 0 || r.NetIndex > MaxNetIndex || r.PID < 0:
		return "invalid", fmt.Errorf("net_index %d (at most %d) pid %d", r.NetIndex, MaxNetIndex, r.PID)
	case r.PID > 0 && !r.HasVMM:
		return "invalid", fmt.Errorf("pid %d without a vmm holding", r.PID)
	case len(r.Label) > maxLabelBytes:
		return "invalid", fmt.Errorf("a label of %d bytes", len(r.Label))
	case r.Incident != nil && (len(r.Incident.Attempts) > maxAttempts || r.Incident.Count < len(r.Incident.Attempts)):
		return "invalid", fmt.Errorf("an incident of %d attempts (count %d, at most %d kept)", len(r.Incident.Attempts), r.Incident.Count, maxAttempts)
	case r.Release != nil:
		// a disposition is kept only with the evidence (INTEGRATION.md §11.8)
		return "invalid", errors.New("a live record with a release disposition")
	}
	switch r.State {
	case StatePreparing, StateStopping, StateQuarantined:
	case StateRunning:
		if !r.HasVMM || r.PID == 0 {
			return "invalid", errors.New("running without a recorded vmm pid")
		}
	default:
		return "invalid", fmt.Errorf("state %q is not a live reservation state", r.State)
	}
	return "", nil
}

// validID says whether id is prefix-<24 lowercase hex>, IDFor's shape.
func validID(prefix, id string) bool {
	rest, ok := strings.CutPrefix(id, prefix+"-")
	if !ok || len(rest) != 24 {
		return false
	}
	for _, c := range rest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func kindOf(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m&os.ModeSymlink != 0:
		return "symlink"
	case m&os.ModeNamedPipe != 0:
		return "fifo"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeDevice != 0:
		return "device"
	case m.IsRegular():
		return "regular file"
	}
	return "special file"
}

// archiveDir is <state>/released: the evidence of every release — the
// operator's of an incident (INTEGRATION.md §8.3), and a timely cleanup's
// (§11.8). Nothing in the launcher deletes from it.
func (st *store) archiveDir() string { return filepath.Join(filepath.Dir(st.dir), "released") }

// evidenceDir says whether dir, released/, exists as a real directory.
// Evidence is kept and read only there: never through a symlink or anything
// else at that name, which no barrier of the launcher's certifies
// (INTEGRATION.md §11.9).
func evidenceDir(fsys fileSystem, dir string) (bool, error) {
	fi, err := fsys.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !fi.IsDir():
		return false, fmt.Errorf("%w: %s is a %s, not the evidence directory: no evidence is kept or read through it", ErrUnsafeState, dir, kindOf(fi.Mode()))
	}
	return true, nil
}

// evidencePathIn is the evidence path of r for the records directory dir.
func evidencePathIn(dir string, r Record) string {
	return (&store{dir: dir}).archivePath(r)
}

// archivePath names one incarnation's evidence, never another's: by its
// token (INTEGRATION.md §11.1); a record written before tokens by its cid
// and creation time.
func (st *store) archivePath(r Record) string {
	if r.Incarnation != "" {
		return filepath.Join(st.archiveDir(), fmt.Sprintf("%s.%s.json", r.ID, r.Incarnation))
	}
	return filepath.Join(st.archiveDir(), fmt.Sprintf("%s.%d.%d.json", r.ID, r.CID, r.Created))
}

// archive keeps r — a release's evidence — durably in released/, through
// the one publication path (publishAt), and certifies every link from the
// state directory down to it before it returns (INTEGRATION.md §11.9).
// released/ is made if missing and must be a real directory. The state
// directory — released/'s link — is fsynced after released/ was seen, at
// every call: mkdir's EEXIST, an earlier attempt or an earlier life names a
// directory, not a barrier issued after it. Then E is published, or, when
// this incarnation's evidence is there already (an earlier attempt's or an
// earlier life's), its bytes and released/ are fsynced again. An existing
// file is never overwritten: anything but this incarnation's evidence at
// its path refuses the release.
func (st *store) archive(r Record) error {
	dir := st.archiveDir()
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
	final := st.archivePath(r)
	if _, err := st.fs.Lstat(final); err == nil {
		data, err := st.fs.ReadFile(final, maxRecordBytes)
		if err != nil {
			return fmt.Errorf("the evidence path %s holds something unreadable, not overwritten: %w", final, err)
		}
		had, _, err := decodeRecord(data)
		if err != nil || !r.Ref().Names(had) || had.CID != r.CID || had.Created != r.Created || had.State != StateReleased {
			return fmt.Errorf("the evidence path %s holds something other than this incarnation's evidence, not overwritten", final)
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
	evidence := &store{dir: dir, prefix: st.prefix, fs: st.fs}
	_, err := evidence.publishAt(r, final, final+".tmp")
	return err
}

// syncFile fsyncs the bytes of a file found already there — evidence an
// earlier attempt or life published — opened read-only, without following
// a link or blocking on a FIFO.
func (st *store) syncFile(p string) error {
	f, err := st.fs.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// rearchive replaces this incarnation's evidence with r, a later version of
// it — its accounting's confirmation recorded (INTEGRATION.md §11.8) —
// through the one publication path; anything else at its path is never
// overwritten. It certifies nothing anew: archive certified every link down
// to E before the record was withdrawn, and whichever version of E a crash
// leaves is valid evidence (INTEGRATION.md §11.9).
func (st *store) rearchive(r Record) error {
	final := st.archivePath(r)
	data, err := st.fs.ReadFile(final, maxRecordBytes)
	if err != nil {
		return fmt.Errorf("its evidence %s could not be read to be updated: %w", final, err)
	}
	had, _, err := decodeRecord(data)
	if err != nil || !r.Ref().Names(had) || had.CID != r.CID || had.Created != r.Created || had.State != StateReleased {
		return fmt.Errorf("the evidence path %s holds something other than this incarnation's evidence, not overwritten", final)
	}
	evidence := &store{dir: st.archiveDir(), prefix: st.prefix, fs: st.fs}
	_, err = evidence.publishAt(r, final, final+".tmp")
	return err
}
