// Package launcher is the privileged VM launcher's core and its wire
// protocol (specs/ci-execution-contract.md §9; rider 01). The core owns
// reservations, ownership, admission, the independent deadline and
// quarantine; every privileged host operation goes through the Host
// interface, which the root binary implements with the jailer, cgroups
// and network namespaces and which tests replace with a fake. The daemon
// talks to it through Client over a unix socket the launcher owns.
//
// runner/launcher/LIFECYCLE.md is the model this file implements: every
// acknowledgement and every host effect follows a completed durable
// publication, a release follows a successful cleanup and a durable
// withdrawal, and one operation at a time holds a record.
package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Rider 04's product defaults and the campaign's bounds live with the
// daemon's config; the launcher only ever applies its own root-owned
// budget on top. OverheadMiB is the per-guest host reservation.
const OverheadMiB = 1024

// CleanupBound is rider 04's 120 s: owned resources are stopped and
// removed, or explicitly quarantined, within it.
const CleanupBound = 120 * time.Second

const defaultIDPrefix = "urgit-p4o"

var (
	ErrUnknown    = errors.New("launcher: no such vm")
	ErrNotOwner   = errors.New("launcher: not the owner of this vm")
	ErrOverBudget = errors.New("launcher: over budget")
	ErrExists     = errors.New("launcher: attempt already reserved")
	ErrInvalid    = errors.New("launcher: invalid request")
	ErrState      = errors.New("launcher: wrong state for this operation")

	// ErrNoEffect is what a Host wraps into an error when it knows the
	// operation changed nothing. Any other error is taken to mean the
	// operation may have acted.
	ErrNoEffect = errors.New("launcher: host operation failed without effect")
	// ErrNotDurable: a publication or withdrawal did not complete.
	ErrNotDurable = errors.New("launcher: state change is not durable")
	// ErrQuarantined: the reservation stays counted until a cleanup
	// succeeds and is recorded (Reply.Quarantined on the wire).
	ErrQuarantined = errors.New("launcher: reservation quarantined")
	// ErrUnsafeState: the state directory holds entries the launcher
	// could not account for; no new reservation or boot until an
	// operator resolves them.
	ErrUnsafeState = errors.New("launcher: unsafe state")
	// ErrStateBusy: another Service holds the state directory.
	ErrStateBusy = errors.New("launcher: state directory is held by another launcher")
	// ErrClosed: the service is shutting down or closed.
	ErrClosed = errors.New("launcher: closed")
	// ErrExpired: the reservation's selected absolute deadline has passed;
	// nothing new is admitted, provisioned, started or connected for it.
	ErrExpired = errors.New("launcher: deadline passed")
	// ErrRetained: the request failed and the reservation it made stays
	// charged — a reservation that was not acknowledged and whose record
	// could not be withdrawn (R8). The answer names it (id, cid, created),
	// so its owner can destroy exactly that incarnation (Reply.Retained on
	// the wire; runner/launcher/INTEGRATION.md §3).
	ErrRetained = errors.New("launcher: reservation retained")
)

// retainedError keeps a message readable, says ErrRetained, and names
// what stays charged (a failed Reserve's reply stays empty: nothing is
// acknowledged).
type retainedError struct {
	msg     string
	causes  []error
	charged ReserveReply
}

func (e *retainedError) Error() string        { return e.msg }
func (e *retainedError) Is(target error) bool { return target == ErrRetained }
func (e *retainedError) Unwrap() []error      { return e.causes }

// quarantineError keeps a message readable and says ErrQuarantined.
type quarantineError struct {
	msg    string
	causes []error
}

func (e *quarantineError) Error() string        { return e.msg }
func (e *quarantineError) Is(target error) bool { return target == ErrQuarantined }
func (e *quarantineError) Unwrap() []error      { return e.causes }

// Owner is who may touch a VM: the peer uid the socket reported and the
// daemon id it announced in hello. Both must match.
type Owner struct {
	UID    uint32 `json:"uid"`
	Daemon string `json:"daemon"`
}

// Image is one golden image the launcher may boot, named by its manifest
// digest and located under the launcher's root-owned image directory.
type Image struct {
	Manifest    string `json:"manifest"`
	Kernel      string `json:"kernel"`
	Rootfs      string `json:"rootfs"`
	BaselineMiB int    `json:"baseline_mib"`
	BootArgs    string `json:"boot_args"`
}

// Config is the launcher's root-owned configuration.
type Config struct {
	StateDir        string
	Images          map[string]Image
	BudgetCPUs      int
	BudgetMemoryMiB int // guest RAM + overhead, summed over every reservation
	MaxGuests       int
	// Ceiling is the launcher's network ceiling: `proto:addr:port` entries a
	// networked reservation may name; anything else is refused.
	Ceiling      []string
	CleanupBound time.Duration
	IDPrefix     string

	// now is the clock every deadline decision reads (time.Now when nil);
	// tests move it forward instead of starting work past a deadline.
	now func() time.Time
	// at, when set (tests only; production leaves it nil), is called at
	// the named points of the service's terminal sequence and of a reaper
	// pass, so a test can hold a Shutdown or the reaper exactly there and
	// prove what may happen meanwhile.
	at func(point string)
}

// NetInfo is what the host's network setup hands back for the kernel
// command line. DNS is the address of the VM's pinned-name responder
// (CI-P4-NET-1, named destinations): set only when the VM was granted a
// named destination; empty, the guest keeps an empty resolver.
type NetInfo struct {
	TAP     string
	GuestIP string
	Gateway string
	DNS     string
	Index   int
}

// VMSpec is everything StartVM needs; every path in it was chosen by the
// launcher from its own config, never by the request.
type VMSpec struct {
	Image     Image
	DiskPath  string
	CPUs      int
	MemoryMiB int
	CID       uint32
	Attempt   string
	Net       *NetInfo
}

// Host is the privileged operation set. Every method is idempotent
// enough to be retried by cleanup — a Remove* of something absent
// succeeds — and every failure is reported, never swallowed. A create
// operation that fails without having acted wraps ErrNoEffect — also
// when it refuses because the object it would create already exists and
// is not this reservation's; the core then records no holding for it and
// never removes it. Any other error is treated as "may have acted".
// RemoveNetwork gets the network index the record holds, so a retry after
// a partial removal still finds what it has to remove.
type Host interface {
	PrepareDisk(id string, img Image, totalMiB int) (path string, err error)
	CreateCgroup(id string, cpus, memMiB int) error
	CreateNetwork(id string, index int, allow []string) (NetInfo, error)
	StartVM(id string, spec VMSpec) (pid int, err error)
	Connect(id string, port uint32) (*os.File, error)
	// Liveness says whether pid runs as id's VMM (Running), verifiably
	// does not (Gone), or cannot be verified (Unknown, with its reason):
	// Unknown is never proof of absence (INTEGRATION.md §11.2)
	Liveness(id string, pid int) (Liveness, error)
	// Kill signals pid only once it is verified Running as id's VMM; one
	// Gone is no error; one Unknown is an error, and never signalled
	Kill(id string, pid int, sig string) error
	RemoveNetwork(id string, index int) error
	RemoveCgroup(id string) error
	RemoveDisk(id string) error
	RemoveJail(id string) error
}

// Bounder is a Host whose operations can be bounded (the real adapter;
// runner/launcher/INTEGRATION.md §7.2): Bound returns a view of the host
// whose every operation ends by ctx's deadline or cancellation, and
// reports one it had to cut short as possibly acted, never ErrNoEffect.
// The core hands each teardown its cleanup obligation's deadline and each
// create step the create's context through it; a Host without it (the
// test models) is called directly.
type Bounder interface {
	Bound(ctx context.Context) Host
}

// hostWith is the host bounded by ctx, when the host can be.
func (s *Service) hostWith(ctx context.Context) Host {
	if b, ok := s.host.(Bounder); ok {
		return b.Bound(ctx)
	}
	return s.host
}

// startBound bounds a VMM start the core passes to a Bounder: the jailer
// and the wait for its pid. A start is not cut short the moment a rollback
// is asked for — killing a jailer mid-start can leave a VMM of unknown pid
// — but it never outlives a quarter of the cleanup allowance after the
// earliest trigger of one (the job's deadline, a destroy's request, the
// stop's): a start cut there is a VMM of unknown pid, quarantined —
// charged and visible, never reported stopped — by that trigger's
// deadline (runner/launcher/INTEGRATION.md §7.2).
const startBound = 30 * time.Second

// Outcome is how a teardown the service started on its own — the
// reaper's, a restart's recovery — ended when it did not release its
// record in time (INTEGRATION.md §7.4), and every release whose accounting
// was confirmed late, an owner's destroy's too (§11.8): Kind is "halted"
// (its stopping record was not durable: execution stopped, nothing
// removed, still charged), "quarantined", "unconfirmed" (its release is
// authorized, its accounting pending: still charged) or "released" — only
// with LateAccounting, a release whose cleanup was on time and whose
// accounting was confirmed at or after its obligation's deadline (late
// bookkeeping, reported apart). Late says the obligation's deadline had
// passed when the teardown began or when its cleanup finished — such a
// teardown never releases (recovery ruling A), so it is always reported.
// Reason is the record's reason as memory holds it — for a halt, the only
// place it is kept.
type Outcome struct {
	ID             string
	Attempt        string
	Owner          Owner
	By             string // reaper, recovery, withdrawal, destroy
	Kind           string
	Late           bool
	LateAccounting bool
	Reason         string
	Err            error
}

// SetReport names the function told every Outcome (serve logs them).
func (s *Service) SetReport(report func(Outcome)) {
	s.mu.Lock()
	s.report = report
	s.mu.Unlock()
}

// reportOutcome tells the report how the teardown the service ran on e by
// itself ended, unless it released the record with its accounting on time.
func (s *Service) reportOutcome(e *entry, by string, err error) {
	s.mu.Lock()
	o := Outcome{ID: e.rec.ID, Attempt: e.rec.Attempt, Owner: e.rec.Owner, By: by, Late: e.late, Reason: e.rec.Reason, Err: err}
	switch {
	case e.gone && !e.disposition.Late():
		s.mu.Unlock()
		return
	case e.gone:
		// on-time cleanup, late bookkeeping: reported apart (§11.8)
		o.Kind, o.LateAccounting = "released", true
	case e.acct != nil:
		o.Kind = "unconfirmed"
	case e.rec.State == StateQuarantined:
		o.Kind = "quarantined"
	default:
		o.Kind = "halted"
	}
	report := s.report
	s.mu.Unlock()
	if report != nil {
		report(o)
	}
}

// State of a reservation. Every record the service holds counts toward
// the budget, whatever its state; reaped and destroyed appear only in
// History (a released record has no file).
type State string

const (
	StatePreparing   State = "preparing"
	StateRunning     State = "running"
	StateStopping    State = "stopping"
	StateQuarantined State = "quarantined"
	StateReaped      State = "reaped"
	StateDestroyed   State = "destroyed"
	// StateReleased: an incident the operator released (§8.3); its record
	// is kept as evidence under <state>/released
	StateReleased State = "released"
)

// Record is the persisted ownership and resource record of one VM. The
// Has* holdings mean "may exist on the host": each is recorded durably
// before its create operation and cleared only after its removal
// succeeded (LIFECYCLE.md §3). HasVMM with PID 0 is a VMM whose start
// outcome is unknown.
type Record struct {
	ID string `json:"id"`
	// Incarnation is the record's incarnation token (INTEGRATION.md §11.1):
	// fixed at admission, it names this incarnation and no other; a record
	// written before tokens has none
	Incarnation  string   `json:"incarnation,omitempty"`
	Attempt      string   `json:"attempt"`
	Owner        Owner    `json:"owner"`
	Image        string   `json:"image"`
	CPUs         int      `json:"cpus"`
	MemoryMiB    int      `json:"memory_mib"`
	DiskMiB      int      `json:"disk_mib"`
	DeadlineUnix int64    `json:"deadline_unix"`
	Network      string   `json:"network"`
	Destinations []string `json:"destinations,omitempty"`
	CID          uint32   `json:"cid"`
	State        State    `json:"state"`
	PID          int      `json:"pid,omitempty"`
	DiskPath     string   `json:"disk_path,omitempty"`
	HasDisk      bool     `json:"has_disk"`
	HasCgroup    bool     `json:"has_cgroup"`
	HasNetwork   bool     `json:"has_network"`
	HasVMM       bool     `json:"has_vmm"`
	NetIndex     int      `json:"net_index,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Created      int64    `json:"created"`
	Updated      int64    `json:"updated"`
	// Label is the job the reservation runs, as its owner named it at
	// reserve (the ship's repository, workflow and job): shown to the
	// operator for selection, never trusted for identity
	Label string `json:"label,omitempty"`
	// CleanupTrigger and CleanupDue are the cleanup obligation a teardown
	// began under — what asked for it and its deadline (unix seconds) —
	// published with its `stopping` record, so a restart knows them too
	CleanupTrigger string `json:"cleanup_trigger,omitempty"`
	CleanupDue     int64  `json:"cleanup_due,omitempty"`
	// Incident is a quarantined record's history (INTEGRATION.md §8.1);
	// nil until a teardown first ends it quarantined
	Incident *Incident `json:"incident,omitempty"`
	// Rev counts the record's durable changes; a release names the
	// revision the operator inspected (§8.3)
	Rev uint64 `json:"rev,omitempty"`
	// Request is the token of the reserve request that admitted it
	// (INTEGRATION.md §11.10): what a settlement of that request finds; a
	// record admitted in process without one has none
	Request string `json:"request,omitempty"`
	// Release is a released record's disposition, kept only with its
	// evidence in released/ (INTEGRATION.md §11.8); nil on a live record
	Release *Release `json:"release,omitempty"`
	Alive   bool     `json:"alive"` // filled at inspect time, never persisted
	// Liveness is what the host said of the VMM at inspect time — running,
	// gone, or unknown with its reason (§11.2); never persisted
	Liveness string `json:"liveness,omitempty"`
}

func (r *Record) holds() bool { return r.HasDisk || r.HasCgroup || r.HasNetwork || r.HasVMM }

// entry is one record in memory with its operation token. Every field is
// guarded by Service.mu; rec changes only while busy is held by the
// goroutine changing it.
type entry struct {
	rec    Record
	busy   string // the operation holding this record ("" = none)
	cancel string // why a running create should roll back at its next step
	// cancelAt is when that rollback was first asked for: the trigger its
	// cleanup obligation runs from (the service's clock)
	cancelAt time.Time
	// stop cancels a running create's context: the host step in progress,
	// if it can be cut short, ends now (reported as possibly acted)
	stop context.CancelFunc
	// cutStart, while the create starts its VMM, cuts that start short at
	// the given instant (the service's clock); nil at any other time
	cutStart func(at time.Time)
	// late: the teardown running now (or last) began, or its cleanup
	// finished, at or after its cleanup deadline (INTEGRATION.md §11.3)
	late bool
	// acct: its release is authorized and its accounting not confirmed yet
	// (§11.8): its evidence, then its record's withdrawal, must be durable
	// before its capacity returns; nil otherwise
	acct *accounting
	// owed: the cleanup deadline of this record's obligation while no
	// teardown has met it (the service's clock) — set when a teardown
	// begins, kept when it halts, cleared when it releases or quarantines
	// the record: a later teardown (the owner's retry, the reaper's) ends
	// by it too, however late its own trigger
	owed time.Time
	gone bool // out of Service.vms for good
	// released: gone by a release whose evidence and accounting were
	// confirmed (§11.8), with that release's disposition — what a waiter
	// the release overtook is answered from
	released    bool
	disposition Release
	// recover: loaded from an interrupted teardown (quarantined); its
	// cleanup is still owed, and performing it releases nothing
	recover bool
}

// Service is the launcher core.
type Service struct {
	cfg       Config
	host      Host
	st        *store
	lock      *os.File
	mu        sync.Mutex
	cond      *sync.Cond
	vms       map[string]*entry
	hist      []Record
	nextCID   uint32 // allocation cursors, always inside their domains
	nextNet   int
	fence     []Problem
	leftovers []string
	busy      int  // entries whose token is held
	tearing   int  // of those, the ones a teardown holds (a create's rollback included)
	waiting   int  // operations blocked in await for a token
	closing   bool // no new operation; tokens drain
	report    func(Outcome)
	// stopAt is when the launcher's stop was first asked for (BeginStop,
	// Shutdown): the trigger of every teardown the stop runs
	stopAt time.Time
	// terminating: a Shutdown or Close owns the service's end, from its
	// drain until the state lock is released; any other terminal call
	// waits for it
	terminating bool
	// requests: the request tokens this process has begun to settle —
	// admitted no more from then on, whatever the ledger's state; settling:
	// the ones a settle is answering now (INTEGRATION.md §11.10)
	requests map[string]bool
	settling map[string]bool
	// unheld counts the work in progress on the state that holds no
	// record's token — an unheld settlement's closure or certification,
	// the records directory's recertification — from its beginning, which
	// only an open service admits, to its end on any path: no terminal
	// call gives the state lock up before it is 0, and no teardown waits
	// for it (INTEGRATION.md §11.11)
	unheld int
	// uncertified: the records directory's own fsync failed at open, or at
	// its every retry since (recertify), so an absence it shows is visible,
	// not durable: a withdrawal whose directory fsync failed may come back
	// (INTEGRATION.md §11.6). Meanwhile nothing is admitted or booted and
	// no absence is answered as a release — the owners' list included —
	// while the records that can be read are still enforced
	uncertified error
}

// NewService takes the state directory's lock, certifies the namespace
// the records live in, loads every record it can account for (a restart
// keeps every reservation, its ownership and its charge) and starts
// nothing: interrupted work is recovered by the first ReapOnce. Anything
// it cannot account for fences the service (Problems).
func NewService(cfg Config, host Host) (*Service, error) {
	return openService(cfg, host, osFS{})
}

func openService(cfg Config, host Host, fsys fileSystem) (*Service, error) {
	if cfg.CleanupBound == 0 {
		cfg.CleanupBound = CleanupBound
	}
	if cfg.IDPrefix == "" {
		cfg.IDPrefix = defaultIDPrefix
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.StateDir == "" {
		return nil, errors.New("launcher: state dir is required")
	}
	// the constructor creates at most the state directory itself and its
	// records directory; the state directory's parent is the
	// installation's, and must already exist
	state := filepath.Clean(cfg.StateDir)
	if err := os.Mkdir(state, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("launcher: state dir %s (its parent is the installation's to create): %w", state, err)
	}
	lock, err := lockState(state)
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, host: host, lock: lock, vms: map[string]*entry{}, nextCID: minCID, nextNet: MinNetIndex, requests: map[string]bool{}, settling: map[string]bool{}}
	s.cond = sync.NewCond(&s.mu)
	s.st = &store{dir: filepath.Join(state, "attempts"), prefix: cfg.IDPrefix, fs: fsys}
	fail := func(err error) (*Service, error) {
		s.closeLock()
		return nil, err
	}
	certify := func(dir, links string) error {
		if err := fsys.SyncDir(dir); err != nil {
			return fmt.Errorf("%w: sync-dir %s (certifying %s): %v", ErrNotDurable, dir, links, err)
		}
		return nil
	}
	// Every link from the installation's directory down to the records
	// is certified at every open, not only when this open created it: a
	// link an earlier open created and whose parent's fsync failed is
	// visible yet not durable, and visibility is no proof. So the state
	// directory's parent is fsynced (the state directory's own link),
	// then, once the records directory exists, the state directory (the
	// records directory's link and the lock's). Each publication then
	// fsyncs the records directory after its rename.
	if err := certify(filepath.Dir(state), "the state directory's link"); err != nil {
		return fail(err)
	}
	fi, err := os.Lstat(s.st.dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Mkdir(s.st.dir, 0o700); err != nil {
			return fail(err)
		}
	case err != nil:
		return fail(err)
	case !fi.IsDir():
		return fail(fmt.Errorf("%w: %s is a %s, not the records directory", ErrUnsafeState, s.st.dir, kindOf(fi.Mode())))
	}
	if err := certify(state, "the records directory's link"); err != nil {
		return fail(err)
	}
	// then the records' own presence and absence, before anything reads
	// them: a withdrawal whose directory fsync failed left a visible
	// absence that is not durable, and no reader may take it for a
	// release (§11.6). Its failure fences instead of failing the open:
	// the records that can be read are loaded and still enforced — their
	// deadlines reaped, their owners' destroys run (a known VMM is
	// stopped even when nothing can be published)
	uncertified := fsys.SyncDir(s.st.dir)
	snap, err := readState(fsys, s.st.dir, cfg.IDPrefix)
	if err != nil {
		return fail(err)
	}
	var topCID uint32
	topNet := -1
	for _, r := range snap.Records {
		e := &entry{rec: r, recover: snap.Interrupted[r.ID]}
		if ev, ok := snap.Authorized[r.ID]; ok {
			// its release was authorized before the restart, on its timely
			// cleanup's durable evidence: its accounting is pending, and the
			// first reaper pass confirms it (INTEGRATION.md §11.8). The record
			// is what its evidence says remained — nothing
			live := ev
			live.State, live.Release, live.Rev = StateStopping, nil, r.Rev
			live.Reason = "its release was authorized on timely cleanup before a restart (its evidence is kept); its accounting is pending"
			e.rec = live
			// its evidence was read, not kept, by this process: visible is not
			// durable, so its first retry keeps it — certifies every link down
			// to it — before anything is withdrawn (§11.9)
			e.acct = &accounting{ev: ev, due: time.Unix(0, ev.Release.DueUnixNano), reason: live.Reason}
		}
		s.vms[r.ID] = e
		topCID, topNet = max(topCID, r.CID), max(topNet, r.NetIndex)
	}
	// the cursors start past the highest value recovered, inside their
	// domains (the loader admits only cids in [minCID, maxCID] and indexes
	// in [0, MaxNetIndex]); past the top they start again at the bottom.
	// They are hints: the allocators skip every value a record holds.
	if topCID != 0 {
		s.nextCID = cycleNext(topCID, minCID, maxCID)
	}
	if topNet >= 0 {
		s.nextNet = int(cycleNext(uint32(topNet), MinNetIndex, MaxNetIndex))
	}
	s.fence = snap.Problems
	s.leftovers = snap.Leftovers
	s.uncertified = uncertified
	return s, nil
}

// cycleNext is the value after v in the cycle [lo, hi].
func cycleNext(v, lo, hi uint32) uint32 {
	if v >= hi || v < lo {
		return lo
	}
	return v + 1
}

// firstFree scans the cycle [lo, hi] from cursor for a value inUse does
// not hold, visiting at most every value once; ok is false when the
// whole domain is in use. It never leaves the domain and never wraps
// through values outside it.
func firstFree(cursor, lo, hi uint32, inUse func(uint32) bool) (v uint32, ok bool) {
	if cursor < lo || cursor > hi {
		cursor = lo
	}
	v = cursor
	for n := uint64(0); n <= uint64(hi-lo); n++ {
		if !inUse(v) {
			return v, true
		}
		v = cycleNext(v, lo, hi)
	}
	return 0, false
}

// Problems are the state entries this service could not account for at
// open — and the records directory itself while it is uncertified
// (§11.6); while any remains, Reserve and Create refuse (ErrUnsafeState;
// ErrNotDurable for the directory).
func (s *Service) Problems() []Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Problem(nil), s.fence...)
	if s.uncertified != nil {
		out = append(out, Problem{Path: s.st.dir, Kind: "uncertified", Detail: "its fsync failed, so an absence it shows is not durable: nothing is admitted or booted, and nothing absent counts as released, until a retry succeeds: " + s.uncertified.Error()})
	}
	return out
}

// Leftovers are this launcher's own unfinished publications found at open
// (never authoritative; the next publication of that id overwrites them).
func (s *Service) Leftovers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.leftovers...)
}

// unsafeLocked is the fence's refusal, if any.
func (s *Service) unsafeLocked() error {
	if s.uncertified != nil {
		return fmt.Errorf("%w: the records directory is not certified (%v): no new reservation or boot until its fsync succeeds", ErrNotDurable, s.uncertified)
	}
	if len(s.fence) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d state entries could not be accounted for (first: %s); no new reservation or boot until an operator resolves them",
		ErrUnsafeState, len(s.fence), s.fence[0])
}

// take gives op the entry's token; put gives it back. Both under s.mu.
func (s *Service) take(e *entry, op string) {
	e.busy = op
	s.busy++
	if op == "teardown" {
		s.tearing++
	}
}

func (s *Service) put(e *entry) {
	if e.busy == "" {
		return
	}
	if e.busy == "teardown" {
		s.tearing--
	}
	e.busy = ""
	e.cancel, e.cancelAt = "", time.Time{}
	s.busy--
	s.cond.Broadcast()
}

// retag turns the token e's holder has into op's (under s.mu): a create
// that rolls back holds a teardown's token from then on.
func (s *Service) retag(e *entry, op string) {
	if e.busy == "teardown" {
		s.tearing--
	}
	if op == "teardown" {
		s.tearing++
	}
	e.busy = op
	s.cond.Broadcast()
}

// cancelCreateLocked asks e's running create to roll back at its next
// step, for the trigger at — a destroy's request, the deadline, the stop —
// from which its cleanup obligation runs (under s.mu). Its cancellable
// host step in progress ends now; a VMM start in progress has until a
// quarter of the obligation's allowance after the first trigger
// (INTEGRATION.md §7.2).
func (s *Service) cancelCreateLocked(e *entry, why string, at time.Time) {
	if e.busy != "create" {
		return
	}
	if e.cancel == "" {
		e.cancel, e.cancelAt = why, at
	}
	if e.stop != nil {
		e.stop()
	}
	if e.cutStart != nil {
		e.cutStart(e.cancelAt.Add(s.cfg.CleanupBound / 4))
	}
}

// drop releases the entry for good (under s.mu, token held): a later
// reservation of the same id is a different entry.
func (s *Service) drop(e *entry) {
	if cur, ok := s.vms[e.rec.ID]; ok && cur == e {
		delete(s.vms, e.rec.ID)
	}
	e.gone = true
}

// await waits (under s.mu) until e's token is free and takes it for op;
// a create in progress is asked to roll back for the trigger at. It never
// re-resolves the id: a waiter whose entry was released returns errGone,
// whatever reserved that id since.
func (s *Service) await(e *entry, op, why string, at time.Time) error {
	for e.busy != "" && !e.gone && !s.closing {
		s.cancelCreateLocked(e, why, at)
		s.waiting++
		s.cond.Wait()
		s.waiting--
	}
	switch {
	case e.gone:
		return errGone
	case s.closing:
		return ErrClosed
	}
	s.take(e, op)
	return nil
}

var errGone = errors.New("launcher: released meanwhile")

// set changes the in-memory record (the token holder only).
func (s *Service) set(e *entry, change func(*Record)) {
	s.mu.Lock()
	change(&e.rec)
	s.mu.Unlock()
}

// intend records a holding in memory and publishes it; the host effect
// it names may start only on nil. On failure the effect is not attempted,
// so the in-memory holding is undone whatever the publication's outcome:
// memory says what may exist on the host. A durable record that may
// still name the holding is dealt with by the release, which withdraws
// the record durably or keeps the charge (and a restart then reads the
// conservative record).
func (s *Service) intend(e *entry, do, undo func(*Record)) error {
	s.mu.Lock()
	do(&e.rec)
	e.rec.Updated = s.cfg.now().Unix()
	e.rec.Rev++
	snap := e.rec
	s.mu.Unlock()
	if out, err := s.st.publish(snap); out != written {
		s.set(e, undo)
		return err
	}
	return nil
}

// commit publishes the record with change applied and applies change in
// memory only once the publication is durable.
func (s *Service) commit(e *entry, change func(*Record)) error {
	s.mu.Lock()
	snap := e.rec
	change(&snap)
	snap.Updated = s.cfg.now().Unix()
	snap.Rev++
	s.mu.Unlock()
	if out, err := s.st.publish(snap); out != written {
		return err
	}
	s.mu.Lock()
	e.rec = snap
	s.mu.Unlock()
	return nil
}

// save publishes the record as it is in memory; any outcome but
// `written` is an error.
func (s *Service) save(e *entry) error {
	s.mu.Lock()
	e.rec.Updated = s.cfg.now().Unix()
	e.rec.Rev++
	snap := e.rec
	s.mu.Unlock()
	if out, err := s.st.publish(snap); out != written {
		return err
	}
	return nil
}

// refusalLocked is the owner's answer for a quarantined record (§9):
// reported, charged, untouched.
func (s *Service) refusalLocked(e *entry) error {
	return &quarantineError{msg: fmt.Sprintf("%s is quarantined (%s); only the operator's clear releases it: a cleanup retry, then a separate release of the inspected incident (urgit-vm-launcher recover)", e.rec.ID, e.rec.Reason)}
}

// IDFor names a VM for an attempt: the prefix and the first 24 hex of the
// attempt id's sha256, which fits the jailer's 64-character alphanumeric
// rule whatever the attempt text looks like.
func IDFor(prefix, attempt string) string {
	sum := sha256.Sum256([]byte(attempt))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}

// ReserveRequest is what the daemon asks for.
type ReserveRequest struct {
	Attempt      string   `json:"attempt"`
	Image        string   `json:"image"`
	CPUs         int      `json:"cpus"`
	MemoryMiB    int      `json:"memory_mib"`
	DiskMiB      int      `json:"disk_mib"`
	DeadlineUnix int64    `json:"deadline_unix"`
	Network      string   `json:"network"`
	Destinations []string `json:"destinations,omitempty"`
	// Label is the job, for the operator's selection only (bounded,
	// printable; INTEGRATION.md §8.1)
	Label string `json:"label,omitempty"`
	// Request is the request's token (INTEGRATION.md §11.10): admitted at
	// most once, ever, and settled on demand. Every reserve on the wire
	// names one; in process, one that names none can never be settled
	Request string `json:"request,omitempty"`
}

// ReserveReply names the reservation: its id and its incarnation token
// (INTEGRATION.md §11.1), which every later mutation names too; its cid
// and creation time for display; the request that admitted it.
type ReserveReply struct {
	ID          string `json:"id"`
	Incarnation string `json:"incarnation"`
	CID         uint32 `json:"cid"`
	Created     int64  `json:"created"`
	Request     string `json:"request,omitempty"`
}

// usage is the charge of every record held, whatever its state.
func (s *Service) usage() (cpus, mem, guests int) {
	for _, e := range s.vms {
		cpus += e.rec.CPUs
		mem += e.rec.MemoryMiB + OverheadMiB
		guests++
	}
	return
}

// destinationAllowed says whether a `proto:host:port` entry is inside
// the launcher's ceiling (exact entry match; the ceiling is a list of
// exact destinations by design — no ranges to widen by mistake). A named
// entry matches only the same name: the host adapter pins it to addresses
// when the VM's network is created.
func (s *Service) destinationAllowed(d string) bool {
	for _, c := range s.cfg.Ceiling {
		if c == d {
			return true
		}
	}
	return false
}

// Reserve admits or refuses a reservation atomically: budget over every
// held record, the network scope against the ceiling, the image known,
// the values sane. It is acknowledged only once its record is durable.
func (s *Service) Reserve(owner Owner, req ReserveRequest) (ReserveReply, error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return ReserveReply{}, ErrClosed
	}
	e, err := s.admitLocked(owner, req)
	if err != nil {
		s.mu.Unlock()
		return ReserveReply{}, err
	}
	r := e.rec
	s.mu.Unlock()

	// its request's ledger entry first (INTEGRATION.md §11.10): a request
	// is admitted at most once, ever, and a settlement finds what it
	// admitted — so the entry is durable before the record, and nothing is
	// acknowledged without both
	if r.Request != "" {
		note := requestNote{Request: r.Request, Attempt: r.Attempt, Owner: r.Owner, Outcome: SettledAdmitted, ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, At: r.Created}
		if err := s.st.keepNote(note); err != nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.drop(e)
			s.put(e)
			return ReserveReply{}, fmt.Errorf("reserve %s: not acknowledged: its request's ledger entry is not durable: %w", r.ID, err)
		}
	}
	out, perr := s.st.publish(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.put(e)
	switch out {
	case written:
		return ReserveReply{ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, Request: r.Request}, nil
	case notWritten:
		s.drop(e)
		return ReserveReply{}, fmt.Errorf("reserve %s: not acknowledged: %w", r.ID, perr)
	}
	// uncertain: the record may be durable; withdraw it before forgetting
	s.mu.Unlock()
	_, rerr := s.st.remove(r.ID)
	s.mu.Lock()
	if rerr == nil {
		s.drop(e)
		return ReserveReply{}, fmt.Errorf("reserve %s: not acknowledged (its record was withdrawn): %w", r.ID, perr)
	}
	// never acknowledged, and possibly durable: it stays charged as an
	// ordinary unacknowledged reservation (no host resource, no
	// quarantine) until its owner destroys it or its deadline reaps it —
	// which is also what a restart finds, whatever the directory holds
	e.rec.Reason = fmt.Sprintf("not acknowledged: %v; its record could not be withdrawn: %v", perr, rerr)
	// nothing is acknowledged; the error names what stays charged, so the
	// owner can destroy exactly this incarnation (INTEGRATION.md §3)
	return ReserveReply{}, &retainedError{
		msg:     fmt.Sprintf("reserve %s: not acknowledged; it stays charged until its owner destroys it or its deadline passes: %v", r.ID, errors.Join(perr, rerr)),
		causes:  []error{perr, rerr},
		charged: ReserveReply{ID: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created, Request: r.Request},
	}
}

// admitLocked validates and admits, inserting the record with its token
// held (charged from this instant). Under s.mu.
func (s *Service) admitLocked(owner Owner, req ReserveRequest) (*entry, error) {
	if req.Attempt == "" {
		return nil, fmt.Errorf("%w: attempt is required", ErrInvalid)
	}
	if req.CPUs < 1 || req.MemoryMiB < 128 || req.DiskMiB < 1 {
		return nil, fmt.Errorf("%w: cpus %d memory_mib %d disk_mib %d", ErrInvalid, req.CPUs, req.MemoryMiB, req.DiskMiB)
	}
	if req.CPUs > maxUnit || req.MemoryMiB > maxUnit || req.DiskMiB > maxUnit {
		return nil, fmt.Errorf("%w: cpus %d memory_mib %d disk_mib %d exceed %d", ErrInvalid, req.CPUs, req.MemoryMiB, req.DiskMiB, maxUnit)
	}
	// the selected absolute deadline: an expired one admits nothing (no
	// maximum is imposed; the runner selects it)
	if now := s.cfg.now().Unix(); req.DeadlineUnix <= now {
		return nil, fmt.Errorf("%w: deadline %d is not after now (%d)", ErrExpired, req.DeadlineUnix, now)
	}
	if owner.Daemon == "" {
		return nil, fmt.Errorf("%w: no owner daemon", ErrInvalid)
	}
	img, ok := s.cfg.Images[req.Image]
	if !ok || img.Manifest == "" {
		return nil, fmt.Errorf("%w: image %q is not installed under the launcher's image directory", ErrInvalid, req.Image)
	}
	dests := append([]string(nil), req.Destinations...)
	sort.Strings(dests)
	switch {
	case req.Network == "locked":
		if len(dests) > 0 {
			return nil, fmt.Errorf("%w: a locked reservation names no destinations", ErrInvalid)
		}
	case req.Network == "":
		return nil, fmt.Errorf("%w: network profile is required (locked, or a profile name)", ErrInvalid)
	default:
		// a networked profile names its destinations explicitly; "no
		// destinations" is spelled locked, never inferred
		if len(dests) == 0 {
			return nil, fmt.Errorf("%w: network profile %s names no destinations; use locked for none", ErrInvalid, req.Network)
		}
		for _, d := range dests {
			if !s.destinationAllowed(d) {
				return nil, fmt.Errorf("%w: destination %s is outside the launcher's ceiling for profile %s", ErrInvalid, d, req.Network)
			}
		}
	}
	if err := s.unsafeLocked(); err != nil {
		return nil, err
	}
	// a request is admitted at most once, ever (INTEGRATION.md §11.10)
	if req.Request != "" {
		if !ValidRequest(req.Request) {
			return nil, fmt.Errorf("%w: request %q is not a request token (32 lowercase hex characters)", ErrInvalid, req.Request)
		}
		if err := s.admissibleLocked(req.Request); err != nil {
			return nil, err
		}
	}
	id := IDFor(s.cfg.IDPrefix, req.Attempt)
	if existing, ok := s.vms[id]; ok {
		if existing.rec.Attempt == req.Attempt {
			return nil, fmt.Errorf("%w: %s (%s)", ErrExists, req.Attempt, existing.rec.State)
		}
		return nil, fmt.Errorf("%w: id collision", ErrInvalid)
	}
	// every term is bounded before it is added: no sum can wrap
	cpus, mem, guests := s.usage()
	if req.CPUs > s.cfg.BudgetCPUs-cpus || req.MemoryMiB+OverheadMiB > s.cfg.BudgetMemoryMiB-mem || guests >= s.cfg.MaxGuests {
		return nil, fmt.Errorf("%w: cpus %d+%d/%d, memory %d+%d/%d MiB (guest + %d overhead), guests %d+1/%d", ErrOverBudget, cpus, req.CPUs, s.cfg.BudgetCPUs, mem, req.MemoryMiB+OverheadMiB, s.cfg.BudgetMemoryMiB, OverheadMiB, guests, s.cfg.MaxGuests)
	}
	// a cid no held record has, inside the domain the loader accepts; the
	// cursor moves only once the reservation is admitted
	held := make(map[uint32]bool, len(s.vms))
	for _, e := range s.vms {
		held[e.rec.CID] = true
	}
	cid, ok := firstFree(s.nextCID, minCID, maxCID, func(c uint32) bool { return held[c] })
	if !ok {
		return nil, fmt.Errorf("%w: every vsock cid is held", ErrOverBudget)
	}
	token, err := newIncarnation()
	if err != nil {
		return nil, err
	}
	now := s.cfg.now().Unix()
	rec := Record{
		ID: id, Incarnation: token, Attempt: req.Attempt, Owner: owner, Image: img.Manifest, CPUs: req.CPUs, MemoryMiB: req.MemoryMiB, DiskMiB: req.DiskMiB,
		DeadlineUnix: req.DeadlineUnix, Network: req.Network, Destinations: dests, CID: cid, State: StatePreparing, Created: now, Updated: now,
		Label: boundLabel(req.Label), Request: req.Request,
	}
	// the record must reload as itself, and keep room for everything its
	// lifecycle adds, before anything is charged or acknowledged
	data, rec, err := encodeRecord(s.cfg.IDPrefix, rec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if room := maxRecordBytes - recordGrowthBytes; len(data) > room {
		return nil, fmt.Errorf("%w: the reservation's record encodes to %d bytes; an admitted record keeps %d of the loader's %d for its lifecycle, so at most %d", ErrInvalid, len(data), recordGrowthBytes, maxRecordBytes, room)
	}
	s.nextCID = cycleNext(cid, minCID, maxCID)
	e := &entry{rec: rec}
	s.vms[id] = e
	s.take(e, "reserve")
	return e, nil
}

// absenceLocked is nil when an absence may be answered as one — the
// inventory is authoritative: its records directory certified (§11.6) and
// every entry in it accounted for (§11.7) — and otherwise why not (under
// s.mu). A record that is not loaded may then have been withdrawn without
// a durable trace, or be an entry the loader could not read, so no
// absence is proof of a release, for any owner, id or incarnation.
func (s *Service) absenceLocked(what string) error {
	switch {
	case s.uncertified != nil:
		return fmt.Errorf("%w: the records directory is not certified (%v), so %s is no proof of a release", ErrNotDurable, s.uncertified, what)
	case len(s.fence) > 0:
		return fmt.Errorf("%w: %d state entries could not be accounted for (first: %s): any of them may be the record in question, so %s is no proof of a release", ErrUnsafeState, len(s.fence), s.fence[0], what)
	}
	return nil
}

func (s *Service) ownedLocked(owner Owner, id string) (*entry, error) {
	e, ok := s.vms[id]
	if !ok {
		if err := s.absenceLocked(id + "'s absence"); err != nil {
			return nil, err
		}
		return nil, ErrUnknown
	}
	if e.rec.Owner != owner {
		return nil, ErrNotOwner
	}
	return e, nil
}

// ownedOfLocked is ownedLocked of the incarnation same accepts (nil: the
// one id names now): another incarnation is ErrStale (INTEGRATION.md
// §11.1).
func (s *Service) ownedOfLocked(owner Owner, id string, same func(Record) bool) (*entry, error) {
	e, err := s.ownedLocked(owner, id)
	if err == nil && same != nil && !same(e.rec) {
		return nil, fmt.Errorf("%w: %s is another incarnation now (%s); the one named is gone", ErrStale, id, e.rec.Ref())
	}
	return e, err
}

// Create provisions and boots a reserved VM, recording each holding
// before the host operation that creates it. Any failure, or a destroy
// or deadline that arrives meanwhile, rolls back through teardown: the
// reservation is released when everything was removed and the release
// is durable, quarantined otherwise.
func (s *Service) Create(owner Owner, id string) (int, error) {
	return s.create(owner, id, nil)
}

// CreateOf is Create of exactly the incarnation ref names (INTEGRATION.md
// §11.1): another incarnation of the id is refused (ErrStale), untouched.
func (s *Service) CreateOf(owner Owner, ref Ref) (int, error) {
	return s.create(owner, ref.ID, ref.Names)
}

func (s *Service) create(owner Owner, id string, same func(Record) bool) (int, error) {
	s.mu.Lock()
	e, err := s.ownedOfLocked(owner, id, same)
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	switch {
	case s.closing:
		err = ErrClosed
	case e.busy != "":
		err = fmt.Errorf("%w: %s in progress", ErrState, e.busy)
	case e.rec.State != StatePreparing:
		err = fmt.Errorf("%w: %s", ErrState, e.rec.State)
	case e.rec.holds():
		err = fmt.Errorf("%w: %s holds resources from an interrupted create; it is torn down, not resumed", ErrState, id)
	case s.cfg.now().Unix() >= e.rec.DeadlineUnix:
		// the selected absolute deadline, enforced here and at every step:
		// the reaper's next pass is no permission to begin expired work
		err = fmt.Errorf("%w: %s's deadline %d has passed; it is not started (the reaper releases it)", ErrExpired, id, e.rec.DeadlineUnix)
	default:
		err = s.unsafeLocked()
	}
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	img, ok := s.cfg.Images[e.rec.Image]
	if !ok {
		s.mu.Unlock()
		return 0, fmt.Errorf("%w: image %s is no longer installed", ErrInvalid, e.rec.Image)
	}
	networked := e.rec.Network != "locked" && len(e.rec.Destinations) > 0
	for _, d := range e.rec.Destinations {
		if !s.destinationAllowed(d) {
			s.mu.Unlock()
			return 0, fmt.Errorf("%w: destination %s is outside the launcher's ceiling", ErrInvalid, d)
		}
	}
	index := 0
	if networked {
		if index, ok = s.allocNetLocked(); !ok {
			s.mu.Unlock()
			return 0, fmt.Errorf("%w: every network index is held", ErrOverBudget)
		}
	}
	s.take(e, "create")
	if networked {
		// the record names its index from now until it is released, so no
		// other create is given it — also while a durable record might
		// name it before memory does (an uncertain intent)
		e.rec.NetIndex = index
	}
	// the create's context: the selected deadline, cut short by a destroy,
	// the reaper or the launcher's stop (cancelCreateLocked)
	ctx, stop := context.WithDeadline(context.Background(), time.Unix(e.rec.DeadlineUnix, 0))
	defer stop()
	e.stop = stop
	s.mu.Unlock()

	pid, step, cause := s.provision(ctx, e, img, networked, index)
	s.mu.Lock()
	e.stop = nil
	if cause == nil {
		s.put(e)
		s.mu.Unlock()
		return pid, nil
	}
	// the one cleanup path, still holding the token, now a teardown's
	// (teardown returns it); its obligation runs from what asked for the
	// rollback — a destroy, the stop — or else from the failure itself, and
	// never from later than the job's deadline (obligationLocked;
	// INTEGRATION.md §7.2)
	trigger := s.cfg.now()
	if e.cancel != "" {
		trigger = e.cancelAt
	}
	s.retag(e, "teardown")
	s.mu.Unlock()
	terr := s.teardown(e, StateDestroyed, fmt.Sprintf("%s failed: %v; rolled back", step, cause), releasing, trigger.Add(s.cfg.CleanupBound))
	switch {
	case terr == nil:
		return 0, fmt.Errorf("%s: %w (rolled back)", step, cause)
	case errors.Is(terr, ErrQuarantined):
		return 0, &quarantineError{
			msg:    fmt.Sprintf("%s: %v; rollback failed, reservation quarantined: %v", step, cause, terr),
			causes: []error{cause, terr},
		}
	}
	// halted before any removal, or released without a confirmed
	// withdrawal: still charged, not quarantined
	return 0, fmt.Errorf("%s: %w; rollback not finished: %w", step, cause, terr)
}

// allocNetLocked gives a network index in [MinNetIndex, MaxNetIndex] that
// no held record names, from a rotating cursor; false when every one is
// held. Under s.mu.
func (s *Service) allocNetLocked() (int, bool) {
	held := make(map[uint32]bool, len(s.vms))
	for _, e := range s.vms {
		if e.rec.NetIndex >= MinNetIndex {
			held[uint32(e.rec.NetIndex)] = true
		}
	}
	v, ok := firstFree(uint32(s.nextNet), MinNetIndex, MaxNetIndex, func(i uint32) bool { return held[i] })
	if !ok {
		return 0, false
	}
	s.nextNet = int(cycleNext(v, MinNetIndex, MaxNetIndex))
	return int(v), true
}

// provision runs the create steps under e's token. It returns the step
// that failed and why; nothing is undone here. The disk, cgroup and
// network steps run under ctx (the deadline; a destroy, the reaper or the
// stop cancels it); the VMM start has startBound, cut a quarter of the
// allowance after the earliest trigger (startBound's comment). A step
// that answers ErrNoEffect created nothing: its holding is undone, so the
// rollback never removes what it did not make (INTEGRATION.md §7.3).
func (s *Service) provision(ctx context.Context, e *entry, img Image, networked bool, index int) (pid int, step string, cause error) {
	s.mu.Lock()
	r := e.rec // the immutable request fields
	s.mu.Unlock()
	host := s.hostWith(ctx)
	undoIfNoEffect := func(err error, undo func(*Record)) {
		if errors.Is(err, ErrNoEffect) {
			s.set(e, undo)
		}
	}
	// mayAct: a destroy or the reaper may have asked the create to roll
	// back, and no host effect begins at or after the selected deadline
	mayAct := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if e.cancel != "" {
			return errors.New("cancelled: " + e.cancel)
		}
		if now := s.cfg.now().Unix(); now >= r.DeadlineUnix {
			return fmt.Errorf("%w: deadline %d reached (now %d)", ErrExpired, r.DeadlineUnix, now)
		}
		return nil
	}
	intent := func(err error) error { return fmt.Errorf("recording the intent: %w", err) }
	// begin records a holding, its intent durable first, and checks
	// mayAct before and again just before the effect the holding names;
	// if it may not act then, the effect is never attempted and the
	// holding is undone in memory, as when its intent fails
	begin := func(name string, do, undo func(*Record)) (string, error) {
		if err := mayAct(); err != nil {
			return "create", err
		}
		if err := s.intend(e, do, undo); err != nil {
			return name, intent(err)
		}
		if err := mayAct(); err != nil {
			s.set(e, undo)
			return "create", err
		}
		return "", nil
	}

	if step, err := begin("prepare disk", func(r *Record) { r.HasDisk = true }, func(r *Record) { r.HasDisk = false }); err != nil {
		return 0, step, err
	}
	disk, err := host.PrepareDisk(r.ID, img, img.BaselineMiB+r.DiskMiB)
	if err != nil {
		undoIfNoEffect(err, func(r *Record) { r.HasDisk = false })
		return 0, "prepare disk", err
	}
	// recorded only if the record still reloads as written
	if len(disk) > maxPathBytes || !utf8.ValidString(disk) {
		return 0, "prepare disk", fmt.Errorf("the host reported an unusable disk path (%d bytes)", len(disk))
	}
	s.set(e, func(r *Record) { r.DiskPath = disk })

	if step, err := begin("create cgroup", func(r *Record) { r.HasCgroup = true }, func(r *Record) { r.HasCgroup = false }); err != nil {
		return 0, step, err
	}
	if err := host.CreateCgroup(r.ID, r.CPUs, r.MemoryMiB+OverheadMiB); err != nil {
		undoIfNoEffect(err, func(r *Record) { r.HasCgroup = false })
		return 0, "create cgroup", err
	}

	var net *NetInfo
	if networked {
		if step, err := begin("create network", func(r *Record) { r.HasNetwork, r.NetIndex = true, index }, func(r *Record) { r.HasNetwork = false }); err != nil {
			return 0, step, err
		}
		info, err := host.CreateNetwork(r.ID, index, r.Destinations)
		if err != nil {
			undoIfNoEffect(err, func(r *Record) { r.HasNetwork = false })
			return 0, "create network", err
		}
		info.Index = index
		net = &info
	}

	if step, err := begin("start vm", func(r *Record) { r.HasVMM = true }, func(r *Record) { r.HasVMM = false }); err != nil {
		return 0, step, err
	}
	// the start has startBound, and no more than a quarter of the cleanup
	// allowance after the earliest trigger of a rollback: the job's
	// deadline, set here, or a destroy's request or the stop's
	// (cancelCreateLocked), whichever is earliest; cut there, it is a VMM
	// of unknown pid, for the rollback to quarantine by that trigger's
	// deadline
	startCtx, cancelStart := context.WithTimeout(context.Background(), startBound)
	defer cancelStart()
	var (
		cut   *time.Timer
		cutAt time.Time
	)
	s.mu.Lock()
	e.cutStart = func(at time.Time) {
		if cut != nil && !at.Before(cutAt) {
			return
		}
		if cut != nil {
			cut.Stop()
		}
		cutAt, cut = at, time.AfterFunc(at.Sub(s.cfg.now()), cancelStart)
	}
	e.cutStart(time.Unix(r.DeadlineUnix, 0).Add(s.cfg.CleanupBound / 4))
	if e.cancel != "" {
		e.cutStart(e.cancelAt.Add(s.cfg.CleanupBound / 4))
	}
	s.mu.Unlock()
	pid, err = s.hostWith(startCtx).StartVM(r.ID, VMSpec{Image: img, DiskPath: disk, CPUs: r.CPUs, MemoryMiB: r.MemoryMiB, CID: r.CID, Attempt: r.Attempt, Net: net})
	s.mu.Lock()
	e.cutStart = nil
	if cut != nil {
		cut.Stop()
	}
	s.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrNoEffect) {
			s.set(e, func(r *Record) { r.HasVMM = false })
		}
		return 0, "start vm", err
	}
	if pid <= 0 {
		// a start that names no process may still have left one: the
		// holding stays and no pid is recorded (a VMM of unknown pid)
		return 0, "start vm", fmt.Errorf("the host reported vmm pid %d", pid)
	}
	// known in memory at once: a rollback from here stops this pid
	s.set(e, func(r *Record) { r.PID = pid })
	// a start that ended after a rollback was asked for, or at or past the
	// deadline, is no success to publish: its VMM, known now, is rolled back
	if err := mayAct(); err != nil {
		return 0, "start vm", fmt.Errorf("the vmm started (pid %d) after its rollback was asked for: %w", pid, err)
	}
	if err := s.commit(e, func(r *Record) { r.State = StateRunning }); err != nil {
		return 0, "record running", err
	}
	return pid, "", nil
}

// Connect hands back a connected vsock fd for the owner's running VM —
// never past its deadline: a connection is how work reaches the guest.
func (s *Service) Connect(owner Owner, id string, port uint32) (*os.File, error) {
	return s.connect(owner, id, port, nil)
}

// ConnectOf is Connect to exactly the incarnation ref names (§11.1).
func (s *Service) ConnectOf(owner Owner, ref Ref, port uint32) (*os.File, error) {
	return s.connect(owner, ref.ID, port, ref.Names)
}

func (s *Service) connect(owner Owner, id string, port uint32, same func(Record) bool) (*os.File, error) {
	s.mu.Lock()
	e, err := s.ownedOfLocked(owner, id, same)
	switch {
	case err != nil:
	case s.closing:
		err = ErrClosed
	case e.busy != "":
		err = fmt.Errorf("%w: %s in progress", ErrState, e.busy)
	case e.rec.State != StateRunning:
		err = fmt.Errorf("%w: %s", ErrState, e.rec.State)
	case s.cfg.now().Unix() >= e.rec.DeadlineUnix:
		err = fmt.Errorf("%w: %s's deadline %d has passed", ErrExpired, id, e.rec.DeadlineUnix)
	}
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.take(e, "connect")
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(e.rec.DeadlineUnix, 0))
	s.mu.Unlock()
	f, err := s.hostWith(ctx).Connect(id, port)
	cancel()
	s.mu.Lock()
	s.put(e)
	s.mu.Unlock()
	return f, err
}

// Inspect returns the owner's record with liveness verified now.
func (s *Service) Inspect(owner Owner, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.ownedLocked(owner, id)
	if err != nil {
		return Record{}, err
	}
	out := e.rec
	s.liveLocked(&out)
	return out, nil
}

// Stop signals the VMM (TERM) without removing anything.
func (s *Service) Stop(owner Owner, id string) error {
	return s.stop(owner, id, nil)
}

// StopOf is Stop of exactly the incarnation ref names (§11.1).
func (s *Service) StopOf(owner Owner, ref Ref) error {
	return s.stop(owner, ref.ID, ref.Names)
}

func (s *Service) stop(owner Owner, id string, same func(Record) bool) error {
	s.mu.Lock()
	e, err := s.ownedOfLocked(owner, id, same)
	switch {
	case err != nil:
	case s.closing:
		err = ErrClosed
	case e.busy != "":
		err = fmt.Errorf("%w: %s in progress", ErrState, e.busy)
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	pid := e.rec.PID
	if pid == 0 {
		s.mu.Unlock()
		return nil
	}
	s.take(e, "stop")
	s.mu.Unlock()
	err = s.host.Kill(id, pid, "TERM")
	s.mu.Lock()
	s.put(e)
	s.mu.Unlock()
	return err
}

// Destroy stops the VMM and removes every owned resource; it waits for an
// operation in progress on the record (asking a create to roll back), and
// any failure quarantines the record with the reason while keeping its
// reservation counted. A quarantined record — found so, or quarantined
// while this destroy waited — is refused untouched: §9 reserves its
// clearance to the operator's release, and a cleanup that would now
// succeed is not that clearance. An id it does not hold is answered done
// only on the durable evidence of a release of it, this owner's
// (INTEGRATION.md §11.8); its absence alone is no authority (ErrUnproven).
func (s *Service) Destroy(owner Owner, id string) error {
	_, err := s.destroy(owner, id, nil)
	return err
}

// DestroyOf is Destroy of exactly the incarnation ref names
// (INTEGRATION.md §§6, 11.1): when the record its id names now is another
// incarnation, or none, the one named is answered done only on the durable
// evidence of its release (§11.8) — an authorized release whose answer was
// lost is proven by it; its absence alone is no authority (ErrUnproven). A
// stale caller never reaches the reservation the same attempt holds now —
// whatever a restart or the clock did to the cid and the creation time.
func (s *Service) DestroyOf(owner Owner, ref Ref) error {
	_, err := s.destroy(owner, ref.ID, &ref)
	return err
}

// DestroyOfRelease is DestroyOf answering, when it is done, the release's
// disposition: whether its accounting was confirmed late (§11.8), for the
// wire's reply.
func (s *Service) DestroyOfRelease(owner Owner, ref Ref) (Release, error) {
	return s.destroy(owner, ref.ID, &ref)
}

func (s *Service) destroy(owner Owner, id string, ref *Ref) (Release, error) {
	s.mu.Lock()
	// the owner's request is the trigger: the whole cleanup — the wait for
	// an operation in progress included — ends within one allowance of it
	asked := s.cfg.now()
	e, held := s.vms[id]
	if !held || ref != nil && !ref.Names(e.rec) {
		// not held: done only on the durable evidence of its release, never
		// on its absence (INTEGRATION.md §11.8)
		s.mu.Unlock()
		if ref == nil {
			return s.releasedAny(owner, id)
		}
		return s.Released(owner, *ref)
	}
	if e.rec.Owner != owner {
		s.mu.Unlock()
		return Release{}, ErrNotOwner
	}
	err := s.await(e, "teardown", "destroy requested by its owner", asked)
	if err == nil && e.rec.State == StateQuarantined {
		// an incident — an operator's release of it pending too — is the
		// operator's (§8.3)
		refusal := s.refusalLocked(e)
		s.put(e)
		s.mu.Unlock()
		return Release{}, refusal
	}
	pending := err == nil && e.acct != nil
	s.mu.Unlock()
	if errors.Is(err, errGone) {
		// released meanwhile by another operation: done only if that was a
		// release whose evidence and accounting are confirmed
		s.mu.Lock()
		defer s.mu.Unlock()
		if e.released {
			return e.disposition, nil
		}
		return Release{}, fmt.Errorf("%w: %s went away while this destroy waited, without a release's evidence", ErrUnproven, id)
	}
	if err != nil {
		return Release{}, err
	}
	if pending {
		err = s.finishAccounting(e)
	} else {
		err = s.teardown(e, StateDestroyed, "destroyed by owner", releasing, asked.Add(s.cfg.CleanupBound))
	}
	s.mu.Lock()
	rel, done := e.disposition, e.released
	s.mu.Unlock()
	if err != nil || !done {
		return Release{}, err
	}
	if rel.Late() {
		// late bookkeeping is reported apart, the owner's destroy's too
		s.reportOutcome(e, "destroy", nil)
	}
	return rel, nil
}

// ending is how a teardown may end.
type ending int

const (
	// releasing: owner destroy, deadline, create rollback, interrupted
	// create, shutdown — released on proven cleanup and a durable
	// withdrawal, quarantined on any failure; one begun after its
	// obligation's deadline is a late recovery, retaining (recovery ruling
	// A; INTEGRATION.md §7.2)
	releasing ending = iota
	// retaining: the owed cleanup of an interrupted teardown found at open,
	// and a late recovery — never released, whatever the cleanup achieves
	retaining
	// retrying: the operator's cleanup retry of an incident — as
	// retaining, and a VMM of unknown pid is looked for (§8.2)
	retrying
)

// incidentSeed is what a teardown that ends in quarantine records: the
// attempt's author, and — for the record's first quarantine — the
// obligation it was under.
type incidentSeed struct {
	by      string
	trigger string
	due     time.Time
	missed  bool
}

// teardown is the one cleanup path (owner destroy, create rollback,
// deadline reaper, restart recovery, the operator's retry, shutdown). The
// caller holds e's token; teardown gives it back. It stops the VMM within
// the bound, removes every holding, and releases the record only when
// nothing may remain and its withdrawal is durable; otherwise the record
// is quarantined and stays counted.
//
// No quarantine is decided before the record's durable form loads as
// quarantined: a record not already quarantined first records this
// teardown as `stopping`, and if that is not written the teardown halts
// before any removal. So a quarantine whose own publication then fails is
// still held by the durable `stopping`, which a restart loads as
// quarantined (interruptedTeardown).
//
// The teardown ends by its cleanup obligation's deadline (obligationLocked):
// one allowance (CleanupBound) after the earliest trigger that asked for
// it — the owner's destroy, the job's deadline, the launcher's stop, a
// create's failure — `by` being the caller's, never a fresh allowance
// from its own start, nor one per step (INTEGRATION.md §7.2). What is
// left when it begins is split: the VMM's stop takes at most half, the
// removals the rest, and a host that can be bounded gets that one
// deadline for every call. A teardown that begins after its deadline has
// passed — the launcher was not running, or a halted teardown is retried
// after it — is LATE: the obligation was not met, so it is a late
// recovery of an incident (recovery ruling A): one bounded allowance from
// its start, and whatever it achieves the record ends quarantined and
// charged, its incident recording the missed deadline, until the
// operator's separate release (INTEGRATION.md §8). Every teardown that
// ends in quarantine records its attempt in the record's incident.
func (s *Service) teardown(e *entry, final State, why string, how ending, by time.Time) error {
	now := s.cfg.now()
	s.mu.Lock()
	by = s.obligationLocked(e, how, by)
	seed := incidentSeed{by: map[ending]string{releasing: "teardown", retaining: "recovery", retrying: "operator retry"}[how], trigger: why, due: by}
	if how != releasing {
		// an incident found at open, or retried: its obligation is the
		// one its record carries; this effort is bounded, not that
		seed.trigger, seed.due = e.rec.CleanupTrigger, time.Time{}
		if e.rec.CleanupDue != 0 {
			seed.due = time.Unix(e.rec.CleanupDue, 0)
		}
		if seed.trigger == "" {
			seed.trigger = "quarantined before its cleanup obligation was recorded: " + e.rec.Reason
		}
		seed.missed = how == retaining && !seed.due.IsZero() && now.After(seed.due)
	}
	s.mu.Unlock()
	late := !by.After(now)
	lateRecovery := false
	if late {
		why = fmt.Sprintf("%s; LATE: its cleanup deadline %s had passed when this teardown began", why, by.UTC().Format(time.RFC3339Nano))
		by = now.Add(s.cfg.CleanupBound)
		if how == releasing {
			// a missed obligation is an incident: this cleanup is a bounded
			// recovery effort, and it never releases
			how, lateRecovery = retaining, true
			seed.by, seed.missed = "late recovery", true
		}
	}
	start := time.Now()
	deadline := start.Add(by.Sub(now)) // the service's clock and the host's advance together
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	host := s.hostWith(ctx)
	s.mu.Lock()
	id, prev := e.rec.ID, e.rec.State
	e.rec.State = StateStopping
	e.late = late
	if prev != StateQuarantined {
		// the obligation this teardown began under, published with its
		// `stopping` record: a restart that finds it knows them too
		e.rec.CleanupTrigger, e.rec.CleanupDue = boundText(seed.trigger, maxDetailBytes), seed.due.Unix()
	}
	s.mu.Unlock()
	if prev != StateQuarantined {
		if err := s.save(e); err != nil {
			return s.halt(e, prev, err, host, start, deadline)
		}
	}
	s.mu.Lock()
	r := e.rec
	s.mu.Unlock()

	var notes, problems []string
	var errs []error // the host failures, wrapped by a quarantine error
	vmmGone := !r.HasVMM
	if r.HasVMM {
		switch {
		case r.PID != 0:
			// a proven-dead pid clears the holding at once
			if vmmGone = s.stopVMM(host, id, r.PID, start, deadline, &problems); vmmGone {
				s.set(e, func(r *Record) { r.HasVMM, r.PID = false, 0 })
			}
		case how == retrying:
			// the operator's retry looks for it, where the host can
			if vmmGone = s.findVMM(host, id, start, deadline, &notes, &problems); vmmGone {
				s.set(e, func(r *Record) { r.HasVMM, r.PID = false, 0 })
			}
		default:
			problems = append(problems, "vmm start outcome unknown (no pid recorded): nothing is removed from under it; the operator's cleanup retry looks for it")
		}
	}
	// nothing is removed from under a VMM that may still run
	if vmmGone {
		if r.HasNetwork {
			if err := host.RemoveNetwork(id, r.NetIndex); err != nil {
				problems, errs = append(problems, "network: "+err.Error()), append(errs, err)
			} else {
				s.set(e, func(r *Record) { r.HasNetwork = false })
			}
		}
		if r.HasCgroup {
			if err := host.RemoveCgroup(id); err != nil {
				problems, errs = append(problems, "cgroup: "+err.Error()), append(errs, err)
			} else {
				s.set(e, func(r *Record) { r.HasCgroup = false })
			}
		}
		diskGone := true
		if r.HasDisk || r.DiskPath != "" {
			if err := host.RemoveDisk(id); err != nil {
				problems, errs = append(problems, "disk: "+err.Error()), append(errs, err)
				diskGone = false
			}
		}
		// the jail is this record's only if its disk step, or a VMM start,
		// may have made it: a record that holds neither removes no jail,
		// whatever occupies the path its id names (INTEGRATION.md §7.3)
		if r.HasDisk || r.DiskPath != "" || r.HasVMM {
			if err := host.RemoveJail(id); err != nil {
				problems, errs = append(problems, "jail: "+err.Error()), append(errs, err)
				diskGone = false
			}
		}
		if diskGone {
			s.set(e, func(r *Record) { r.HasDisk, r.DiskPath = false, "" })
		}
	}

	// The outcome is decided here, after the last host effect returned and
	// before anything is withdrawn: a releasing teardown whose cleanup
	// finished at or after its obligation's deadline missed it, however it
	// began — whatever bounded its host calls — and ends as a late recovery
	// does: quarantined, charged, its incident missed (INTEGRATION.md
	// §11.3). What it removed mitigated the hazard; it is no release.
	// Three instants stay apart from here on (§11.3; QUESTIONS-SOURCE-01
	// §8): the obligation's deadline (by), the end of the host cleanup
	// (cleaned), and the end of the withdrawal's publication or its
	// confirmation; a late cleanup is never re-labelled a timely one.
	cleaned := s.cfg.now()
	missedWhen := "its teardown began"
	if how == releasing && !cleaned.Before(by) {
		how, lateRecovery, missedWhen = retaining, true, "its cleanup finished"
		seed.missed = true
		notes = append(notes, fmt.Sprintf("its cleanup finished at %s, not before its obligation's deadline %s", instant(cleaned), instant(by)))
		s.mu.Lock()
		e.late = true
		s.mu.Unlock()
	}
	if len(problems) > 0 || how != releasing {
		outcome := "cleanup failed: " + strings.Join(problems, "; ")
		if len(problems) == 0 {
			outcome = "cleanup completed; it stays quarantined until the operator's release"
		}
		qerr := s.quarantine(e, why+"; "+outcome, notes, problems, seed, errs...)
		if qerr == nil && lateRecovery {
			// its caller asked for a release: this is none
			return &quarantineError{msg: fmt.Sprintf("%s released nothing: its cleanup obligation's deadline had passed when %s, so it is an incident, quarantined and charged until the operator's release", id, missedWhen)}
		}
		return qerr
	}
	// the physical cleanup met its obligation: the release is authorized
	// (late-accounting ruling 01; INTEGRATION.md §11.8). Its evidence —
	// this exact incarnation, the obligation, the cleanup's verified end,
	// the disposition — is kept durably first, then its record is withdrawn
	// durably, and only then does its capacity return. Its accounting is
	// not bounded by the obligation (§7.2's disclosed limit): one confirmed
	// at or after the deadline still returns it, reported apart and kept in
	// its evidence, never claimed on time; one not confirmed keeps it
	// charged, pending, retried.
	s.mu.Lock()
	e.owed = time.Time{} // met: everything stopped and removed in time
	e.rec.Reason = strings.Join(append([]string{why + fmt.Sprintf("; released on timely cleanup: its cleanup finished at %s, before its obligation's deadline %s", instant(cleaned), instant(by))}, notes...), "; ")
	s.authorizeLocked(e, Release{By: ByTimelyCleanup, Final: final, Trigger: boundText(seed.trigger, maxDetailBytes),
		DueUnixNano: by.UnixNano(), CleanedUnixNano: cleaned.UnixNano(), DecidedUnixNano: s.cfg.now().UnixNano()})
	s.mu.Unlock()
	return s.finishAccounting(e)
}

// obligationLocked is the deadline a teardown of e asked for with `by`
// ends by (under s.mu): the earliest of `by`, the job's deadline plus one
// allowance — for every releasing teardown the deadline is itself a
// trigger, whoever notices it — and the deadline an earlier teardown of the
// record set and did not meet (it halted). The operator's retry and a
// quarantined record's owed cleanup run from their own request: the
// quarantine met the obligation. It is kept as the record's until a
// teardown releases or quarantines it.
func (s *Service) obligationLocked(e *entry, how ending, by time.Time) time.Time {
	if how == releasing && e.rec.DeadlineUnix != 0 {
		if d := time.Unix(e.rec.DeadlineUnix, 0).Add(s.cfg.CleanupBound); d.Before(by) {
			by = d
		}
	}
	if !e.owed.IsZero() && e.owed.Before(by) {
		by = e.owed
	}
	e.owed = by
	return by
}

// quarantine ends a teardown in quarantine (token held, given back): the
// record keeps its charge and its remaining holdings, its incident records
// this attempt — opened with the obligation it was under on the record's
// first quarantine — and the quarantine is published; if that publication
// fails, the durable `stopping` this teardown wrote first still holds it (a
// restart loads it quarantined).
func (s *Service) quarantine(e *entry, reason string, notes, problems []string, seed incidentSeed, causes ...error) error {
	at := s.cfg.now().Unix()
	s.set(e, func(r *Record) {
		r.State = StateQuarantined
		r.Reason = strings.Join(append([]string{reason}, notes...), "; ")
		left := remaining(*r)
		result := "resolved"
		if len(left) > 0 {
			result = "unresolved"
		}
		if r.Incident == nil {
			r.Incident = &Incident{Trigger: boundText(seed.trigger, maxDetailBytes), Missed: seed.missed}
			if !seed.due.IsZero() {
				r.Incident.Due = seed.due.Unix()
			}
		}
		detail := boundText(strings.Join(append(append([]string(nil), problems...), notes...), "; "), maxDetailBytes)
		r.Incident = withAttempt(r.Incident, CleanupAttempt{At: at, By: seed.by, Result: result, Left: left, Detail: detail})
	})
	if err := s.save(e); err != nil {
		s.set(e, func(r *Record) {
			r.Reason += "; quarantine not recorded durably (the durable stopping record holds it): " + err.Error()
		})
	}
	s.mu.Lock()
	id := e.rec.ID
	e.owed = time.Time{} // met: explicit, charged, visible
	s.put(e)
	s.mu.Unlock()
	if len(problems) == 0 {
		return nil // retained by rule, nothing failed
	}
	return &quarantineError{msg: fmt.Sprintf("cleanup of %s failed, reservation quarantined: %s", id, strings.Join(problems, "; ")), causes: causes}
}

// halt ends a teardown whose `stopping` record could not be written: no
// holding is removed and no quarantine is decided. A known live VMM is
// still stopped (that only ends execution); the record keeps its previous
// state, its charge and its obligation's deadline, and a retry — the
// owner's, the deadline reaper's — starts over by that same deadline
// (late, when it has passed by then).
func (s *Service) halt(e *entry, prev State, cause error, host Host, start, deadline time.Time) error {
	s.mu.Lock()
	id, r := e.rec.ID, e.rec
	s.mu.Unlock()
	var problems []string
	stopped := ""
	if r.HasVMM && r.PID != 0 && s.stopVMM(host, id, r.PID, start, deadline, &problems) {
		stopped = fmt.Sprintf("; vmm pid %d stopped", r.PID)
	}
	detail := strings.Join(append([]string{cause.Error() + stopped}, problems...), "; ")
	s.mu.Lock()
	e.rec.State = prev
	e.rec.Reason = "teardown halted before any removal: its stopping record is not durable: " + detail
	s.put(e)
	s.mu.Unlock()
	return fmt.Errorf("teardown of %s halted before any removal (%s): %w", id, detail, cause)
}

// stopVMM ends a known pid within the first half of what the teardown has
// left of its obligation when it begins at start (deadline): TERM and a
// quarter of it, then KILL and another quarter (INTEGRATION.md §7.2); the
// removals keep the rest. It reports whether the pid is verified gone
// (§11.2): one still running is reported so, never as stopped, and one
// whose state the host cannot verify is never signalled and never taken
// for gone — nothing is removed from under it.
func (s *Service) stopVMM(host Host, id string, pid int, start, deadline time.Time, problems *[]string) bool {
	l, why := host.Liveness(id, pid)
	if l == Gone {
		return true
	}
	gone := func() bool {
		l, why = host.Liveness(id, pid)
		return l == Gone
	}
	left := max(deadline.Sub(start), 0)
	var kills []string
	signal := func(sig string) {
		if l != Running {
			return // only a VMM verified running is signalled
		}
		if err := host.Kill(id, pid, sig); err != nil {
			kills = append(kills, sig+": "+err.Error())
		}
	}
	signal("TERM")
	if waitGone(gone, start.Add(left/4)) {
		return true
	}
	signal("KILL")
	if waitGone(gone, start.Add(left/2)) {
		return true
	}
	msg := fmt.Sprintf("vmm pid %d still alive after TERM and KILL within %s", pid, left/2)
	if l == Unknown {
		msg = fmt.Sprintf("vmm pid %d: its state cannot be verified (%v): not signalled, not taken for gone", pid, why)
	}
	if len(kills) > 0 {
		msg += " (" + strings.Join(kills, "; ") + ")"
	}
	*problems = append(*problems, msg)
	return false
}

// findVMM looks for the VMM of id whose pid was never recorded — the
// operator's retry (INTEGRATION.md §§8.2, 11.2) — where the host can, and
// stops, verified, what it finds. It says whether no VMM of the id runs
// any more, which only a complete scan can say: a host that cannot look,
// a scan that failed or could not classify every process, or a VMM that
// survives leave the holding unresolved, and nothing is removed from
// under it. The VMMs a scan did verify are stopped even so.
func (s *Service) findVMM(host Host, id string, start, deadline time.Time, notes, problems *[]string) bool {
	finder, ok := host.(VMMFinder)
	if !ok {
		*problems = append(*problems, "vmm start outcome unknown (no pid recorded), and this host cannot look for it: nothing is removed from under it")
		return false
	}
	pids, err := finder.FindVMMs(id)
	gone := true
	for _, pid := range pids {
		if !s.stopVMM(host, id, pid, start, deadline, problems) {
			gone = false
		}
	}
	at := s.cfg.now().UTC().Format(time.RFC3339)
	if err != nil {
		stopped := ""
		if len(pids) > 0 {
			stopped = fmt.Sprintf("; the VMMs it verified (pid %v) were stopped", pids)
		}
		*problems = append(*problems, "vmm start outcome unknown (no pid recorded); the look for it is incomplete, so it proves nothing gone: "+err.Error()+stopped)
		return false
	}
	if len(pids) == 0 {
		*notes = append(*notes, "vmm start outcome unknown (no pid recorded): no process of this id found by a complete scan of the host at "+at)
		return true
	}
	if gone {
		*notes = append(*notes, fmt.Sprintf("vmm of unknown pid found by the host at %s as pid %v, and stopped", at, pids))
	}
	return gone
}

// instant is t as the records and reports state it.
func instant(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// waitGone polls gone until it says yes or the deadline passes.
func waitGone(gone func() bool, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		if gone() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return gone()
}

// liveLocked fills r's inspect-time liveness (never persisted): Alive only
// for a VMM verified running, and what the host said (§11.2).
func (s *Service) liveLocked(r *Record) {
	r.Alive, r.Liveness = false, ""
	if r.PID == 0 {
		return
	}
	l, err := s.host.Liveness(r.ID, r.PID)
	r.Alive, r.Liveness = l == Running, l.String()
	if err != nil {
		r.Liveness += ": " + boundText(err.Error(), maxDetailBytes)
	}
}

// List is the owner's VMs — once the inventory is authoritative (§11.7):
// its consumers take a VM missing from it for a released one, so while an
// entry is unaccounted for or the records directory is uncertified, it is
// refused, never answered short.
func (s *Service) List(owner Owner) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.absenceLocked("a record's absence from the list"); err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range s.vms {
		if e.rec.Owner == owner {
			c := e.rec
			s.liveLocked(&c)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out, nil
}

// All is every loaded record, for the root CLI: a presence, never an
// absence (§11.7).
func (s *Service) All() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, e := range s.vms {
		out = append(out, e.rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

// History is the terminal records this process saw (the persisted files
// are removed when a record ends; the reasons live in the log and here).
func (s *Service) History() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.hist...)
}

// Reap enforces the independent deadline (D1; rider 04): every running
// or preparing VM past its deadline is torn down within the cleanup
// bound of that deadline whatever its daemon is doing, and the record says
// so. Its first pass runs at once — the interrupted work of a previous
// process — and one more every tick; no pass waits for another's
// teardowns, so a slow teardown never delays the next record's
// (runner/launcher/INTEGRATION.md §7.2). It returns once ctx has ended and
// its teardowns have.
func (s *Service) Reap(ctx context.Context, every time.Duration) {
	var wg sync.WaitGroup
	defer wg.Wait()
	s.reapPass(&wg) // interrupted work of a previous process, before the first tick
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.reapPass(&wg)
	}
}

// recertify retries the records directory's barrier while it is
// uncertified (each reaper pass, §11.6): once an fsync of it succeeds,
// every absence it shows is durable, and its fence is lifted. The fsync
// runs outside the mutex, counted as work no token holds: none begins once
// the service stops, and the state lock is not given up while one runs
// (§11.11).
func (s *Service) recertify() {
	s.mu.Lock()
	if s.closing || s.uncertified == nil {
		s.mu.Unlock()
		return
	}
	s.unheld++
	s.mu.Unlock()
	err := s.st.fs.SyncDir(s.st.dir)
	s.mu.Lock()
	s.uncertified = err
	s.unheld--
	s.cond.Broadcast()
	s.mu.Unlock()
}

// ReapOnce is one reaper pass that returns when its teardowns have ended.
func (s *Service) ReapOnce() {
	var wg sync.WaitGroup
	s.reapPass(&wg)
	wg.Wait()
}

// candidates is every held entry in a stable order (creation, id).
func (s *Service) candidates() []*entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidatesLocked()
}

func (s *Service) candidatesLocked() []*entry {
	out := make([]*entry, 0, len(s.vms))
	for _, e := range s.vms {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rec.Created != out[j].rec.Created {
			return out[i].rec.Created < out[j].rec.Created
		}
		return out[i].rec.ID < out[j].rec.ID
	})
	return out
}

// reapPass is one pass of the reaper; its teardowns join wg. Each
// candidate is re-checked under the lock when its turn comes; one that is
// busy is left for the next pass (a running create past its deadline is
// asked to roll back, for its deadline as the trigger: its cancellable
// step ends now, a VMM start within a quarter of the allowance). It
// finishes unconfirmed withdrawals; performs the owed cleanup of an
// interrupted teardown found at open, which stays quarantined; tears down
// what is past its deadline — by the deadline plus one allowance — and a
// create a previous process did not finish (torn down, never resumed). A
// quarantined record is otherwise never touched: only the operator's release
// releases it. Each teardown runs on its own, so none waits for another
// (INTEGRATION.md §7.2), and every one that does not release its record,
// or began late, is reported.
func (s *Service) reapPass(wg *sync.WaitGroup) {
	s.recertify()
	now := s.cfg.now()
	candidates := s.candidates()
	// a test may hold the pass here: its candidates are taken and none has
	// been looked at, so a record can change under the snapshot
	s.reached("reaper: candidates taken")
	for _, e := range candidates {
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return
		}
		due := (e.rec.State == StateRunning || e.rec.State == StatePreparing) && e.rec.DeadlineUnix != 0 && now.Unix() >= e.rec.DeadlineUnix
		if e.gone || e.busy != "" {
			if !e.gone && due {
				// the job's deadline is the trigger of the rollback's obligation
				s.cancelCreateLocked(e, fmt.Sprintf("deadline %d reached", e.rec.DeadlineUnix), time.Unix(e.rec.DeadlineUnix, 0))
			}
			s.mu.Unlock()
			continue
		}
		// the pass's own trigger is now; for a releasing teardown the job's
		// deadline, when it came first, is the one (obligationLocked)
		by := now.Add(s.cfg.CleanupBound)
		var run func() error
		who := "reaper"
		switch {
		case e.acct != nil:
			who = "withdrawal"
			run = func() error { return s.finishAccounting(e) }
		case e.recover && e.rec.State == StateQuarantined:
			// quarantined already (explicit, charged): its owed cleanup is
			// one bounded effort that releases nothing
			e.recover = false
			who = "recovery"
			run = func() error {
				return s.teardown(e, StateQuarantined, "interrupted teardown: its owed cleanup performed after a restart", retaining, by)
			}
		case due:
			why := fmt.Sprintf("deadline %d reached; reaped by the launcher", e.rec.DeadlineUnix)
			run = func() error { return s.teardown(e, StateReaped, why, releasing, by) }
		case e.rec.State == StatePreparing && e.rec.holds():
			// its trigger is its discovery now — or its deadline, when that
			// came first
			who = "recovery"
			run = func() error {
				return s.teardown(e, StateDestroyed, "interrupted create recovered after a restart; torn down, not resumed", releasing, by)
			}
		default:
			s.mu.Unlock()
			continue
		}
		s.take(e, "teardown")
		s.mu.Unlock()
		// the outcome is the record's: released, quarantined, or still
		// charged with why — and, when it is not a release in time, reported
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.reportOutcome(e, who, run())
		}()
	}
}

// Budget reports the launcher's budget and current usage for hello.
func (s *Service) Budget() (cpus, mem, guests, usedCPUs, usedMem, usedGuests int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u1, u2, u3 := s.usage()
	return s.cfg.BudgetCPUs, s.cfg.BudgetMemoryMiB, s.cfg.MaxGuests, u1, u2, u3
}

// drainLocked refuses new operations and waits (under s.mu) until no
// token is held but teardowns' — a teardown already has its own deadline
// and is waited for before the lock is released, not before the next
// teardown may start — or ctx ends; it reports how many are still held.
func (s *Service) drainLocked(ctx context.Context) int {
	return s.drainUntil(ctx, func() int { return s.busy - s.tearing })
}

// drainAllLocked is drainLocked waiting for every token, teardowns' too,
// and for the work no token holds (unheld: INTEGRATION.md §11.11).
func (s *Service) drainAllLocked(ctx context.Context) int {
	return s.drainUntil(ctx, func() int { return s.busy + s.unheld })
}

func (s *Service) drainUntil(ctx context.Context, held func() int) int {
	s.closing = true
	s.cond.Broadcast()
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	for held() > 0 && ctx.Err() == nil {
		s.cond.Wait()
	}
	return held()
}

// BeginStop is the launcher's first step when it stops (serve, before
// Shutdown): no new operation is accepted, the stop request becomes the
// trigger of every teardown the stop runs, and every create in progress is
// asked to roll back for it — its cancellable host step ends now, a VMM
// start within a quarter of the allowance — so the Shutdown that follows
// drains no provisioning, and every owned VM is cleaned up, or explicitly
// quarantined, within one allowance of the request (INTEGRATION.md §7.2).
// Nothing is torn down here.
func (s *Service) BeginStop(why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	if s.stopAt.IsZero() {
		s.stopAt = s.cfg.now()
	}
	for _, e := range s.vms {
		s.cancelCreateLocked(e, why, s.stopAt)
	}
	s.cond.Broadcast()
}

// awaitTerminalLocked waits (under s.mu) until no other terminal
// sequence owns the service, or ctx ends.
func (s *Service) awaitTerminalLocked(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stop()
	for s.terminating && ctx.Err() == nil {
		s.cond.Wait()
	}
	if s.terminating {
		return fmt.Errorf("launcher: another shutdown or close is in progress: %w", ctx.Err())
	}
	return nil
}

// endTerminalLocked gives the service's end back (under s.mu).
func (s *Service) endTerminalLocked() {
	s.terminating = false
	s.cond.Broadcast()
}

// reached tells a test's barrier (Config.at) that point was reached.
func (s *Service) reached(point string) {
	if s.cfg.at != nil {
		s.cfg.at(point)
	}
}

// Shutdown is the launcher's ownership-scoped stop: no new operation,
// every operation in progress but a teardown finishes, then every running
// or preparing record is torn down (failures quarantine and are returned).
// It owns the service's end from its drain until the state lock is
// released: a Close or another Shutdown meanwhile waits for it, so the
// lock is never given to a new opener while this one can still act. Its
// teardowns run side by side and end by one allowance after the stop
// request (BeginStop's, else this call's), or after the job's deadline
// when that came first — the drain before them spends that same
// allowance, it restarts nothing (INTEGRATION.md §7.2); the teardowns it
// found in progress have their own triggers; a record another operation
// leaves running or preparing is torn down once it is idle (once per
// Shutdown). Shutdown alone cuts no create short: one in progress
// finishes first, as it always did, and a teardown that begins after the
// stop's allowance has passed is LATE — serve calls BeginStop first,
// which cuts it short.
// The lock is released when every token is back and the work no token
// holds has ended — a settlement, a recertification, which no teardown
// waits for (INTEGRATION.md §11.11) — so nothing of this process can still
// write; or, when a wait gives up at ctx's end, by the process's exit.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if err := s.awaitTerminalLocked(ctx); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.lock == nil {
		s.mu.Unlock()
		return nil
	}
	// the stop request (BeginStop's, else this call's) is the trigger of
	// every teardown this stop runs: the drain before them uses the same
	// allowance, nothing restarts it
	if s.stopAt.IsZero() {
		s.stopAt = s.cfg.now()
	}
	by := s.stopAt.Add(s.cfg.CleanupBound)
	s.terminating = true
	if n := s.drainLocked(ctx); n > 0 {
		// nothing of this Shutdown acts any more: a later call may try again
		s.endTerminalLocked()
		s.mu.Unlock()
		return fmt.Errorf("launcher: shutdown: %d operation(s) still in progress: %w", n, ctx.Err())
	}
	s.mu.Unlock()
	s.reached("shutdown: drained")
	var (
		wg   sync.WaitGroup
		emu  sync.Mutex
		errs []error
		seen = map[*entry]bool{}
	)
	s.mu.Lock()
	for {
		for _, e := range s.candidatesLocked() {
			if e.gone || e.busy != "" || seen[e] {
				continue
			}
			var run func() error
			switch {
			case e.acct != nil:
				run = func() error { return s.finishAccounting(e) }
			case e.rec.State == StateRunning || e.rec.State == StatePreparing:
				run = func() error { return s.teardown(e, StateDestroyed, "launcher shutdown", releasing, by) }
			default:
				// quarantined (an owed cleanup included): left for the
				// operator; its durable form loads quarantined again
				continue
			}
			seen[e] = true
			s.take(e, "teardown")
			id := e.rec.ID
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := run(); err != nil {
					emu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", id, err))
					emu.Unlock()
				}
			}()
		}
		if s.busy == 0 {
			break
		}
		s.cond.Wait()
	}
	s.mu.Unlock()
	wg.Wait()
	s.mu.Lock()
	// the work no token holds — an unheld settlement, the records
	// directory's recertification — ends before the state is handed on. No
	// teardown above waited for it; the caller's deadline bounds this wait
	// as it bounds the drain, and past it the lock is kept, for the
	// process's exit or a later Close to give up (INTEGRATION.md §11.11)
	if n := s.drainUntil(ctx, func() int { return s.unheld }); n > 0 {
		s.endTerminalLocked()
		s.mu.Unlock()
		return errors.Join(append(errs, fmt.Errorf("launcher: shutdown: %d settlement(s) or recertification(s) still in progress; the state directory is kept: %w", n, ctx.Err()))...)
	}
	if err := s.closeLock(); err != nil {
		errs = append(errs, err)
	}
	s.endTerminalLocked()
	s.mu.Unlock()
	return errors.Join(errs...)
}

// Close stops the service without touching any VM: no new operation, the
// ones in progress finish — a settlement and a recertification included
// (INTEGRATION.md §11.11) — then the state lock is released (a restart in
// the same process opens a new Service). A Shutdown in progress keeps the
// service's end: Close waits for it and releases nothing before it has
// finished acting.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.awaitTerminalLocked(context.Background())
	if s.lock == nil {
		return nil
	}
	s.terminating = true
	s.drainAllLocked(context.Background())
	err := s.closeLock()
	s.endTerminalLocked()
	return err
}

func (s *Service) closeLock() error {
	if s.lock == nil {
		return nil
	}
	err := s.lock.Close() // closing the only descriptor releases the flock
	s.lock = nil
	return err
}
