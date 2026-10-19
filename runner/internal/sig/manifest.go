package sig

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
)

// ManifestVersion is the execution-manifest message version (P4 D5;
// version 3, Delivery 1A, binds the run's event; version 4, Delivery 1B,
// binds a job's needs context and the attempts it came from; version 5,
// Delivery 1C, binds the identity of the event the run was admitted for).
// A verifier of another version fails closed; there is no shim.
const ManifestVersion = 5

// AutomaticResolveVersion is a distinct authorization domain for the newly
// unprivileged resolver. Older runners reject it before dispatch instead of
// interpreting the new purpose with their unrestricted version-5 fetcher.
// Existing jobs and explicit import obligations keep version 5 unchanged.
const AutomaticResolveVersion = 6

func ManifestVersionFor(m Manifest) int {
	if m.Mode == "resolve-tag" {
		return AutomaticResolveVersion
	}
	return ManifestVersion
}

// Manifest is the execution manifest the ship binds an authorization to
// (specs/ci-execution-contract.md §5): everything the attempt may act on.
type Manifest struct {
	Incarnation  string `json:"incarnation"` // @uv text
	Repo         string `json:"repo"`
	Ref          string `json:"ref"`
	Candidate    string `json:"candidate"` // @uv text, 0v0 for a resolve
	OID          string `json:"oid"`       // 40 hex, '' for a resolve
	Baseline     string `json:"baseline"`  // 40 hex, '' for trial/resolve
	Lock         string `json:"lock"`      // 64 hex, '' when none
	Generation   uint64 `json:"generation"`
	Workflow     string `json:"workflow"`
	Job          string `json:"job"`
	Trust        string `json:"trust"`
	Sandbox      string `json:"sandbox"` // vm / container
	Mode         string `json:"mode"`    // required / trial / manual / shadow / plan / resolve / resolve-tag
	Network      string `json:"network"` // locked or a profile name
	NetworkScope string `json:"network-scope"`
	// the event the run's jobs receive (Delivery 1A): push, or
	// workflow_dispatch for a manual run; '' for a resolve. Context is the
	// sha-256 (64 hex) of the event payload act reads, a push's as a
	// manual run's; '' for a resolve, which runs no job
	Event   string `json:"event"`
	Context string `json:"context"`
	// the needs context a job's expressions read (Delivery 1B): Needs is
	// the sha-256 (64 hex) of the context text the ship renders from the
	// accepted attempts of the job's needs, '' for a job with no needs and
	// for a plan or a resolve; Prerequisites names those attempts, each
	// `<job>=<attempt>`, by job id, comma-joined, '' with no needs
	Needs         string `json:"needs"`
	Prerequisites string `json:"prerequisites"`
	// the admitted event's identity (Delivery 1C), canonical text the ship
	// renders from the run's immutable trigger, beginning with the event's
	// name: `push <ref> <old>..<new> event <id>`, `pull_request #<n>
	// <action> head <sha> base <sha> event <id>`, `schedule <workflow>#<i>
	// due <ISO-8601Z> occurrence <id>`, `workflow_dispatch <workflow>
	// request <request>`, a staged push `push <ref> <base>..<object>`; ''
	// for a resolve
	Trigger string `json:"trigger"`
}

// ManifestFields is the JSON name of every field the manifest noun signs,
// in the noun's order: the keys an assignment's `manifest` object must
// carry, every one of them, for the runner to rebuild the signed noun.
func ManifestFields() []string {
	t := reflect.TypeOf(Manifest{})
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
	}
	return names
}

// ScopeText is the canonical text of a destination list: sorted,
// comma-joined; ” for none.
func ScopeText(dests []string) string {
	d := append([]string(nil), dests...)
	sort.Strings(d)
	return strings.Join(d, ",")
}

// Noun is [incarnation repo ref candidate oid baseline lock generation
// workflow job trust sandbox mode network network-scope event context
// needs prerequisites trigger].
func (m Manifest) Noun() (*Noun, error) {
	inc, err := ParseUV(m.Incarnation)
	if err != nil {
		return nil, fmt.Errorf("incarnation: %w", err)
	}
	cand, err := ParseUV(m.Candidate)
	if err != nil {
		return nil, fmt.Errorf("candidate: %w", err)
	}
	return Tuple(
		Atom(inc), Cord(m.Repo), Cord(m.Ref), Atom(cand), Cord(m.OID), Cord(m.Baseline), Cord(m.Lock),
		Atom(new(big.Int).SetUint64(m.Generation)), Cord(m.Workflow), Cord(m.Job), Cord(m.Trust), Cord(m.Sandbox), Cord(m.Mode),
		Cord(m.Network), Cord(m.NetworkScope), Cord(m.Event), Cord(m.Context), Cord(m.Needs), Cord(m.Prerequisites),
		Cord(m.Trigger),
	), nil
}

// ManifestMessage is the authorization: the v1 fields plus the manifest,
// signed as one noun [5 recipient attempt operation expiry nonce
// manifest].
type ManifestMessage struct {
	Recipient string
	Attempt   string
	Operation string
	Expiry    int64
	Nonce     string
	Manifest  Manifest
}

func (m ManifestMessage) Noun() (*Noun, error) {
	recipient, err := ParseUV(m.Recipient)
	if err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	attempt, err := ParseUV(m.Attempt)
	if err != nil {
		return nil, fmt.Errorf("attempt: %w", err)
	}
	nonce, err := ParseUV(m.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	if m.Expiry < 0 {
		return nil, errors.New("expiry before the epoch")
	}
	mn, err := m.Manifest.Noun()
	if err != nil {
		return nil, err
	}
	return Tuple(Atom(big.NewInt(int64(ManifestVersionFor(m.Manifest)))), Atom(recipient), Atom(attempt), Cord(m.Operation), Atom(big.NewInt(m.Expiry)), Atom(nonce), mn), nil
}

// Bytes is what is signed: the little-endian bytes of the jam.
func (m ManifestMessage) Bytes() ([]byte, error) {
	n, err := m.Noun()
	if err != nil {
		return nil, err
	}
	return LittleEndian(Jam(n)), nil
}

// VerifyManifest checks a signature over the message.
func VerifyManifest(pubHex string, m ManifestMessage, sigHex string) error {
	n, err := m.Noun()
	if err != nil {
		return err
	}
	return verifyNoun(pubHex, n, sigHex)
}
