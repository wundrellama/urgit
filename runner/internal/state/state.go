// Package state persists what enrollment returned: the daemon id and the
// bearer. The enrollment token is never written here (D7 b). It also holds
// the daemon's retentions: the sandboxes whose release it cannot prove,
// each withholding a slot until the operator releases it
// (runner/launcher/INTEGRATION.md §§4–5, 8.4).
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"urgit/runner/internal/sig"
)

type State struct {
	// Format is the file's format: CurrentFormat as every save by this
	// version writes it, 0 (absent) for a file an earlier runner wrote last
	// — whose entries of the legacy shape Load marks (Legacy)
	Format   int    `json:"format,omitempty"`
	DaemonID string `json:"daemon_id"`
	Bearer   string `json:"bearer"`
	ShipURL  string `json:"ship_url"`
	// the ship's CI public key as handed over at enrollment (D5); every
	// assignment and grant must verify against it
	CIPublicKey string `json:"ci_public_key,omitempty"`
	// Quarantined are the sandboxes whose teardown failed (P4 D1, rider
	// 04): each keeps a slot withheld across restarts until an operator
	// releases it, so a crash never silently restores capacity a leftover
	// VM or disk still occupies.
	Quarantined []Quarantine `json:"quarantined,omitempty"`
	// Released are the retentions the operator released (recovery ruling
	// A; runner/launcher/INTEGRATION.md §8.4), each with when: the evidence
	// is kept, and nothing prunes it.
	Released []Quarantine `json:"released,omitempty"`
	// Refused are the authenticated recovery commands the daemon refused,
	// each recorded before its refusal was answered (INTEGRATION.md §11.12,
	// "Every final answer is recorded first"): the same command is answered
	// from its record, never judged again, and nothing prunes them
	// (QUESTIONS-SOURCE-01 §10).
	Refused []Refusal `json:"refused,omitempty"`
	// Ledger: this runner keeps its execution ledger beside the file
	// (INTEGRATION.md §11.14, "An assignment runs at most once here"); a
	// start without it is refused — what the runner took, and ran, went
	// with it (Ledger.Open)
	Ledger bool `json:"ledger,omitempty"`
	// Transition is the confirmed transition of a runner whose execution
	// history is incomplete (INTEGRATION.md §11.15): nil until the owner's
	// command is carried out; then the runner runs again, and refuses every
	// assignment its ship signed before it. A record the file holds is read,
	// never trusted, and kept as it is (§11.16): whether it is a transition
	// is Problem's to say, and the daemon's
	Transition *Transition `json:"transition,omitempty"`
	// Superseded are the transition records this runner found no
	// transition, each kept as the file held it when the owner's transition
	// replaced it (INTEGRATION.md §11.16): nothing erases a record, and
	// nothing prunes them (QUESTIONS-SOURCE-01 §10)
	Superseded []SupersededTransition `json:"superseded_transitions,omitempty"`

	// upgraded: loaded from a file an earlier runner wrote (Upgraded)
	upgraded bool
}

// Transition is the owner's confirmed transition of a runner whose
// execution history is incomplete (legacy-replay-upgrade ruling 01;
// INTEGRATION.md §11.15): the authorization epoch its command named — from
// then on every assignment the ship signs for the runner carries a nonce of
// Epoch·2^128 or more, and every one it signed before carries one below
// 2^128 — the command, the history evidence it was bound to, and when it
// was recorded.
//
// A record is read from the file without being trusted (INTEGRATION.md
// §11.16): whatever the file holds — an object or not, of any fields — it
// loads, and it is kept. Where writing its fields back would not give the
// file's JSON again, token for token, the record is kept as the file held
// it (raw), and every save writes it back so. Problem says whether it is a
// transition this version writes; whether it is this runner's, bound to its
// history as it is now, is the daemon's to say.
type Transition struct {
	Epoch    uint64 `json:"epoch"`
	Command  string `json:"command"`
	Evidence string `json:"evidence"`
	At       int64  `json:"at"`

	// raw is the record as the file held it, where its fields do not give
	// it back; "" for one they give back, and for one made in memory
	raw string
}

// transitionFields is a transition as this version writes it.
type transitionFields struct {
	Epoch    uint64 `json:"epoch"`
	Command  string `json:"command"`
	Evidence string `json:"evidence"`
	At       int64  `json:"at"`
}

// UnmarshalJSON reads a transition record without trusting it: its fields
// where they can be read, and the record itself wherever they do not give it
// back. It never refuses one.
func (t *Transition) UnmarshalJSON(data []byte) error {
	var f transitionFields
	_ = json.Unmarshal(data, &f) // what does not read stays zero: Problem says why
	*t = Transition{Epoch: f.Epoch, Command: f.Command, Evidence: f.Evidence, At: f.At}
	if again, err := json.Marshal(f); err != nil || !jsonEqual(again, data) {
		t.raw = string(data)
	}
	return nil
}

// MarshalJSON writes a record as the file held it, where its fields would
// not give it back, and otherwise as this version writes a transition.
func (t Transition) MarshalJSON() ([]byte, error) {
	if t.raw != "" {
		return []byte(t.raw), nil
	}
	return json.Marshal(transitionFields{Epoch: t.Epoch, Command: t.Command, Evidence: t.Evidence, At: t.At})
}

// Record is the record as the file holds it, or as this version writes it.
func (t *Transition) Record() json.RawMessage {
	data, _ := t.MarshalJSON()
	return json.RawMessage(data)
}

// Problem says why t is no transition this version writes, or "" when it is
// one: an object of exactly its four fields, each named once and of its
// type; an authorization epoch of 1 or more; the command a @uv in the ship's
// spelling; the evidence a sha256, in hex; and its time.
func (t *Transition) Problem() string {
	if t.raw != "" {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(t.raw), &fields); err != nil || fields == nil {
			return "the record is no object"
		}
		for _, name := range []string{"epoch", "command", "evidence", "at"} {
			if _, ok := fields[name]; !ok {
				return "the record has no " + name
			}
		}
		if len(fields) != 4 {
			return "the record has a field this version does not write"
		}
		// a map holds one of two names alike, as a reading keeps one of
		// them and drops the other (INTEGRATION.md §11.16)
		names, _, _ := members([]byte(t.raw))
		if name := nameTwice(names); name != "" {
			return "the record names its " + name + " more than once"
		}
		d := json.NewDecoder(strings.NewReader(t.raw))
		d.DisallowUnknownFields()
		var f transitionFields
		if err := d.Decode(&f); err != nil {
			return fmt.Sprintf("a field of the record is not of its type (%v)", err)
		}
	}
	switch {
	case t.Epoch == 0:
		return "its authorization epoch is 0: a transition takes a runner to epoch 1 or more"
	case !canonicalUV(t.Command):
		return fmt.Sprintf("its command %.64q is no @uv in the ship's spelling", t.Command)
	case !sha256Hex(t.Evidence):
		return "its evidence is no sha256 in hex"
	case t.At <= 0:
		return "its time is not recorded"
	}
	return ""
}

// SupersededTransition is a transition record the runner found no
// transition, kept as the file held it when the owner's transition replaced
// it (INTEGRATION.md §11.16): why it was none, the command whose transition
// replaced it, and when.
type SupersededTransition struct {
	Record  json.RawMessage `json:"record"`
	Problem string          `json:"problem"`
	By      string          `json:"by"`
	At      int64           `json:"at"`
}

// members is the object raw holds, member by member in the order it names
// them — a name as often as it is named — or ok false when raw holds no
// object.
func members(raw []byte) (names []string, values []json.RawMessage, ok bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, false
	}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return nil, nil, false
		}
		name, _ := tok.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, nil, false
		}
		names, values = append(names, name), append(values, v)
	}
	return names, values, true
}

// nameTwice is the first of names that names a field an earlier one names —
// the same name, or one in another case, as the decoder matches a field —
// or "".
func nameTwice(names []string) string {
	for i, n := range names {
		for _, m := range names[:i] {
			if strings.EqualFold(m, n) {
				return n
			}
		}
	}
	return ""
}

// namedTwice says where the state file data, or any object in it, names a
// field twice (nameTwice), or "": the decoder keeps the last of the two, and
// a save erases the other (INTEGRATION.md §11.16). The values kept as the
// file holds them are read for no field, and not walked: the transition
// record — Transition.Problem says what it is — and each superseded record.
func namedTwice(data []byte) string {
	return namedTwiceAt(data, nil)
}

func namedTwiceAt(raw []byte, path []string) string {
	if keptAsHeld(path) {
		return ""
	}
	if names, values, ok := members(raw); ok {
		if name := nameTwice(names); name != "" {
			where := "it"
			if len(path) > 0 {
				where = "its " + strings.Join(path, ".")
			}
			return fmt.Sprintf("%s names %q more than once", where, name)
		}
		for i, v := range values {
			if why := namedTwiceAt(v, append(slices.Clone(path), names[i])); why != "" {
				return why
			}
		}
		return ""
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for i, v := range list {
			if why := namedTwiceAt(v, append(slices.Clone(path), strconv.Itoa(i))); why != "" {
				return why
			}
		}
	}
	return ""
}

// keptAsHeld says whether the value at path in a state file is one kept as
// the file holds it: its transition record, or a superseded record.
func keptAsHeld(path []string) bool {
	switch len(path) {
	case 1:
		return strings.EqualFold(path[0], "transition")
	case 3:
		return strings.EqualFold(path[0], "superseded_transitions") && strings.EqualFold(path[2], "record")
	}
	return false
}

// jsonEqual says whether a and b are the same JSON, token for token.
func jsonEqual(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// canonicalUV says whether text is a @uv as the ship writes it.
func canonicalUV(text string) bool {
	c, err := sig.CanonicalUV(text)
	return err == nil && c == text
}

// sha256Hex says whether text is a sha256, in lower-case hex.
func sha256Hex(text string) bool {
	if len(text) != 64 {
		return false
	}
	for _, c := range text {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Quarantine is one withheld slot, why, and exactly what it withholds
// (INTEGRATION.md §1): the sandbox's handle and backend, its attempt, and
// for microvm the launcher's record and incarnation, for docker-rootless
// the compatibility mode's objects. An entry written before stage 01
// carries the handle, the reason and the time only.
type Quarantine struct {
	Handle string `json:"handle"`
	Reason string `json:"reason"`
	At     int64  `json:"at"`

	Backend string `json:"backend,omitempty"` // microvm | docker-rootless
	Attempt string `json:"attempt,omitempty"`
	// microvm: the launcher's record and its incarnation — its token
	// (runner/launcher/INTEGRATION.md §11.1), with its cid and creation
	// time; VM empty is a reservation whose launcher identity was never
	// learnt, Incarnation empty an entry for a record written before tokens
	VM          string `json:"vm,omitempty"`
	Incarnation string `json:"incarnation,omitempty"`
	CID         uint32 `json:"cid,omitempty"`
	Created     int64  `json:"created,omitempty"`
	// docker-rootless
	Network   string `json:"network,omitempty"`
	Volume    string `json:"volume,omitempty"`
	Container string `json:"container,omitempty"`

	// Request, with no VM, makes the entry an unsettled admission
	// (runner/launcher/INTEGRATION.md §11.10; settled-admission ruling 01):
	// the reserve request this daemon sent, or may have sent, for the
	// attempt, recorded before it was sent and kept until the launcher
	// settles it — never released on an empty inventory
	Request string `json:"request,omitempty"`

	// Label is the job, for the operator's selection only.
	Label string `json:"label,omitempty"`
	// Rev counts the entry's changes: a release names the revision the
	// operator inspected.
	Rev uint64 `json:"rev,omitempty"`
	// Attempts are the operator's cleanup retries of a docker-rootless
	// retention (a microvm one's are the launcher's), newest last.
	Attempts []Attempt `json:"attempts,omitempty"`
	// ReleasedAt is when the operator released it (in State.Released).
	ReleasedAt int64 `json:"released_at,omitempty"`

	// Legacy is the entry's provenance when a runner of this version first
	// loaded it from a file an earlier runner wrote, with the legacy shape
	// (runner/launcher/INTEGRATION.md §11.12; legacy-recovery UI ruling 01):
	// set by Load only, never by any writer of this version
	Legacy *Legacy `json:"legacy,omitempty"`
	// Command and Evidence are what released a legacy retention (in
	// State.Released): the operator's recovery command from Urgit, and the
	// digest of the evidence the daemon released it on
	Command  string `json:"command,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// Legacy is where a legacy entry was found: the format of the file an
// earlier runner wrote it in, and when this version first loaded it (unix
// seconds).
type Legacy struct {
	Format int   `json:"format"`
	Found  int64 `json:"found"`
}

// Refusal is an authenticated recovery command the daemon refused, bound to
// what it named: the command, the entry's selection and revision, the
// evidence the operator confirmed, and the evidence found when the launcher
// answered; why, and when (unix seconds).
type Refusal struct {
	Command   string `json:"command"`
	Selection string `json:"selection"`
	Revision  uint64 `json:"revision"`
	Evidence  string `json:"evidence"`
	Found     string `json:"found,omitempty"`
	Reason    string `json:"reason"`
	At        int64  `json:"at"`
}

// SameCommand says whether a and b name one recovery command: one atom,
// whatever its spellings — the signature binds the atom, not the text
// (INTEGRATION.md §11.12, "One command, one identity"). A command that is
// not a @uv is only its exact text.
func SameCommand(a, b string) bool {
	if a == b {
		return true
	}
	x, err := sig.ParseUV(a)
	if err != nil {
		return false
	}
	y, err := sig.ParseUV(b)
	if err != nil {
		return false
	}
	return x.Cmp(y) == 0
}

// RefusalOf is the recorded refusal of command id, if there is one: found
// by the command's atom, whatever spelling it was recorded under.
func (s *State) RefusalOf(id string) (Refusal, bool) {
	for _, r := range s.Refused {
		if SameCommand(r.Command, id) {
			return r, true
		}
	}
	return Refusal{}, false
}

// ReleaseOf is the released entry a recovery command released, if there is
// one: found by the command's atom, as RefusalOf.
func (s *State) ReleaseOf(id string) (Quarantine, bool) {
	for _, q := range s.Released {
		if q.Command != "" && SameCommand(q.Command, id) {
			return q, true
		}
	}
	return Quarantine{}, false
}

// CurrentFormat is the format every save by this version stamps (Save).
const CurrentFormat = 1

// LegacyShape says whether q has the shape only a runner before settled
// admission wrote: a microvm slot naming neither its launcher identity nor
// its reserve request. The shape alone proves nothing (IsLegacy).
func (q Quarantine) LegacyShape() bool {
	return q.Backend == "microvm" && q.VM == "" && q.Request == ""
}

// IsLegacy says whether q is a legacy retention: of the legacy shape, and
// marked when a file an earlier runner wrote was loaded — its release comes
// from Urgit, on its evidence (INTEGRATION.md §11.12).
func (q Quarantine) IsLegacy() bool { return q.Legacy != nil && q.LegacyShape() }

// markLegacy marks, in a file an earlier runner wrote last (Format below
// CurrentFormat), every retention of the legacy shape, with the file's
// format and when it was found; each marked entry changed, so its revision
// moves. The file's format becomes this version's, which a save makes
// durable with the marks. A file of this version is never marked. It says
// how many entries it marked.
func (s *State) markLegacy(found int64) int {
	if s.Format >= CurrentFormat {
		return 0
	}
	n := 0
	for i := range s.Quarantined {
		q := &s.Quarantined[i]
		if q.Legacy == nil && q.LegacyShape() {
			q.Legacy = &Legacy{Format: s.Format, Found: found}
			q.Rev++
			n++
		}
	}
	s.Format = CurrentFormat
	return n
}

// Attempt is one cleanup retry: when, what it achieved, what it left.
type Attempt struct {
	At     int64    `json:"at"`
	By     string   `json:"by"`
	Result string   `json:"result"` // resolved | unresolved
	Left   []string `json:"left,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// maxAttempts is how many retries an entry keeps.
const maxAttempts = 8

// Admission says whether q is an unsettled admission: a reserve request
// whose outcome the daemon has not settled (Request, and no VM).
func (q Quarantine) Admission() bool { return q.Request != "" && q.VM == "" }

// Selection is the entry's text form for the operator's scripts: its
// handle, backend, launcher identity (record, incarnation token, cid,
// creation time), its reserve request (an admission's), time and revision.
func (q Quarantine) Selection() string {
	return fmt.Sprintf("%s/%s/%s/%s/%d/%d/%s/%d/%d", q.Handle, q.Backend, q.VM, q.Incarnation, q.CID, q.Created, q.Request, q.At, q.Rev)
}

// ErrStale: a selection names no retention the file holds now, or one that
// changed since it was inspected.
var ErrStale = errors.New("the selection is stale")

// Find is the retention sel names exactly — identity and time; and, with
// rev, the revision too.
func (s *State) Find(sel string, rev bool) (int, error) {
	f := strings.Split(strings.TrimSpace(sel), "/")
	if len(f) != 9 {
		return -1, fmt.Errorf("a selection is <handle>/<backend>/<vm>/<incarnation>/<cid>/<created>/<request>/<at>/<rev>: %q", sel)
	}
	for i, q := range s.Quarantined {
		p := strings.Split(q.Selection(), "/")
		if slices.Equal(p[:8], f[:8]) {
			if rev && p[8] != f[8] {
				return -1, fmt.Errorf("%w: %s changed since it was inspected (revision %s then, %s now): inspect it again", ErrStale, q.Handle, f[8], p[8])
			}
			return i, nil
		}
	}
	return -1, fmt.Errorf("%w: no retention %s is recorded now: released, or another retention holds its handle", ErrStale, f[0])
}

// Record adds a to entry i's retries and moves its revision.
func (s *State) Record(i int, a Attempt) {
	q := &s.Quarantined[i]
	q.Attempts = append(q.Attempts, a)
	if len(q.Attempts) > maxAttempts {
		q.Attempts = q.Attempts[len(q.Attempts)-maxAttempts:]
	}
	q.Rev++
}

// Release moves entry i to Released, stamped at: the slot it withheld
// returns at the daemon's next start; the entry is kept as evidence.
func (s *State) Release(i int, at int64) Quarantine {
	q := s.Quarantined[i]
	q.ReleasedAt = at
	s.Quarantined = slices.Delete(slices.Clone(s.Quarantined), i, i+1)
	s.Released = append(s.Released, q)
	return q
}

// Same says whether q and o are one retention: the same handle and, where
// both name them, the same backend and the same launcher record and
// incarnation — by its token (INTEGRATION.md §11.1); an entry for a record
// written before tokens by its cid and creation time, and never the same as
// one with a token. An entry that names no backend (written before stage
// 01) or no launcher record (retained before its identity was known) is
// the same as every retention of its handle: the launcher holds one record
// per attempt at a time, and a Docker handle names the same objects each
// time. An unsettled admission is the same as the admission of its own
// request only (§11.10).
func (q Quarantine) Same(o Quarantine) bool {
	switch {
	case q.Handle != o.Handle:
		return false
	case q.Admission() || o.Admission():
		// an unsettled admission is its request, and nothing else: never the
		// reservation it may have made, which follows its own disposition
		// (INTEGRATION.md §11.10)
		return q.Admission() && o.Admission() && q.Request == o.Request
	case q.Backend == "" || o.Backend == "":
		return true
	case q.Backend != o.Backend:
		return false
	case q.VM == "" || o.VM == "":
		return true
	case q.Incarnation != "" || o.Incarnation != "":
		return q.VM == o.VM && q.Incarnation == o.Incarnation
	}
	return q.VM == o.VM && q.CID == o.CID && q.Created == o.Created
}

// CompletedBy is q with the identity it lacks taken from o, one retention
// (Same); q's reason and time stay.
func (q Quarantine) CompletedBy(o Quarantine) Quarantine {
	if q.Backend == "" {
		q.Backend = o.Backend
	}
	if q.Backend != o.Backend {
		return q
	}
	if q.Attempt == "" {
		q.Attempt = o.Attempt
	}
	if q.VM == "" {
		q.VM, q.Incarnation, q.CID, q.Created = o.VM, o.Incarnation, o.CID, o.Created
	}
	if q.Network == "" && q.Volume == "" && q.Container == "" {
		q.Network, q.Volume, q.Container = o.Network, o.Volume, o.Container
	}
	if q.Label == "" {
		q.Label = o.Label
	}
	return q
}

// Retain records q unless a retention of its identity is recorded (which
// q then completes); it says whether q is new, a slot more withheld.
func (s *State) Retain(q Quarantine) bool {
	for i, have := range s.Quarantined {
		if have.Same(q) {
			s.Quarantined[i] = have.CompletedBy(q)
			return false
		}
	}
	s.Quarantined = append(s.Quarantined, q)
	return true
}

// Dedupe merges the entries that are one retention (a file an older daemon
// wrote may hold one quarantine several times, §2b) and says how many
// entries it merged away. Nothing is released: each retention keeps one
// entry.
func (s *State) Dedupe() int {
	var kept []Quarantine
	for _, q := range s.Quarantined {
		merged := false
		for i := range kept {
			if kept[i].Same(q) {
				kept[i], merged = kept[i].CompletedBy(q), true
				break
			}
		}
		if !merged {
			kept = append(kept, q)
		}
	}
	n := len(s.Quarantined) - len(kept)
	s.Quarantined = kept
	return n
}

// ClearQuarantine removes the quarantine records named by handle ("all"
// for every one) and returns the handles cleared. It is a bare removal
// with no proof of its own, and no operator entry point uses it: the
// operator's release is Find, a proof, then Release (recovery ruling A).
func (s *State) ClearQuarantine(handle string) []string {
	var cleared []string
	kept := s.Quarantined[:0:0]
	for _, q := range s.Quarantined {
		if handle == "all" || q.Handle == handle {
			cleared = append(cleared, q.Handle)
			continue
		}
		kept = append(kept, q)
	}
	s.Quarantined = kept
	return cleared
}

// Load returns nil, nil when the file does not exist. A file an earlier
// runner wrote has its legacy entries marked (markLegacy): in memory, until
// a save makes the marks and the format durable together. A file that
// names a field twice is refused, and left as it is (namedTwice;
// INTEGRATION.md §11.16): no runner writes one, and which of the two is
// meant is not known.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state file %s: %w", path, err)
	}
	if where := namedTwice(data); where != "" {
		return nil, fmt.Errorf("state file %s: %s: no runner writes such a file, and which of the two is meant is not known; it is left as it is", path, where)
	}
	if s.DaemonID == "" || s.Bearer == "" {
		return nil, fmt.Errorf("state file %s: daemon_id and bearer are required", path)
	}
	s.upgraded = s.Format < CurrentFormat
	s.markLegacy(time.Now().Unix())
	return &s, nil
}

// Upgraded says whether s was loaded from a file an earlier runner wrote
// last: its format, and any legacy marks, are durable only once saved.
func (s *State) Upgraded() bool { return s.upgraded }

// SaveError is a save that did not complete: the step, and whether the
// file may already hold the new content (the rename may have happened, or
// its durability is unproven).
type SaveError struct {
	Step      string // mkdir, encode, create, write, sync, close, rename, sync-dir
	Path      string
	Uncertain bool
	Err       error
}

func (e *SaveError) Error() string {
	msg := fmt.Sprintf("state file %s: %s: %v", e.Path, e.Step, e.Err)
	if e.Uncertain {
		msg += " (the file may hold either version)"
	}
	return msg
}

func (e *SaveError) Unwrap() error { return e.Err }

// fileOps is what Save does to the filesystem, one operation each; tests
// wrap osOps to fail around the real call.
type fileOps interface {
	MkdirAll(dir string) error
	Create(path string) (writable, error)
	Rename(from, to string) error
	SyncDir(dir string) error
}

type writable interface {
	Write(p []byte) (int, error)
	Chmod(mode os.FileMode) error
	Sync() error
	Close() error
}

type osOps struct{}

func (osOps) MkdirAll(dir string) error { return os.MkdirAll(dir, 0o700) }
func (osOps) Create(path string) (writable, error) {
	// never through a symlink, never blocking on a FIFO
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
}
func (osOps) Rename(from, to string) error { return os.Rename(from, to) }
func (osOps) SyncDir(dir string) error {
	d, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

// Save writes the file with mode 0600, creating the directory, replacing
// it atomically and durably — write a temp, fsync it, rename it over the
// file, fsync the directory — so a crash leaves either version whole and
// a nil answer means the new one survives (INTEGRATION.md §5). Every
// failure is a *SaveError naming its step.
func Save(path string, s *State) error { return save(osOps{}, path, s) }

func save(ops fileOps, path string, s *State) error {
	// whatever this version writes is this version's format: a file it
	// saved is never marked (markLegacy)
	s.Format = CurrentFormat
	dir := filepath.Dir(path)
	fail := func(step string, uncertain bool, err error) error {
		return &SaveError{Step: step, Path: path, Uncertain: uncertain, Err: err}
	}
	if err := ops.MkdirAll(dir); err != nil {
		return fail("mkdir", false, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fail("encode", false, err)
	}
	tmp := path + ".tmp"
	f, err := ops.Create(tmp)
	if err != nil {
		return fail("create", false, err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return fail("write", false, err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fail("chmod", false, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fail("sync", false, err)
	}
	if err := f.Close(); err != nil {
		return fail("close", false, err)
	}
	if err := ops.Rename(tmp, path); err != nil {
		return fail("rename", true, err)
	}
	if err := ops.SyncDir(dir); err != nil {
		return fail("sync-dir", true, err)
	}
	return nil
}

// ErrLocked: another process holds the state file.
var ErrLocked = errors.New("the state file is held by another process")

// Lock is the exclusive hold on a state file (<path>.lock, flock): the
// running daemon's for its whole life, the operator's retry's or
// release's for its run, so neither rewrites the file under the other.
type Lock struct{ f *os.File }

// Acquire takes path's lock, creating the directory and the lock file if
// needed; it never waits (ErrLocked).
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s.lock", ErrLocked, path)
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release gives the lock back; a nil Lock has nothing to give back.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close() // closing the only descriptor releases the flock
	l.f = nil
	return err
}
