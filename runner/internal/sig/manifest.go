package sig

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// ManifestVersion is the execution-manifest message version (P4 D5;
// version 3, Delivery 1A, binds the run's event). A verifier of another
// version fails closed; there is no shim.
const ManifestVersion = 3

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
	Mode         string `json:"mode"`    // required / trial / manual / shadow / plan / resolve
	Network      string `json:"network"` // locked or a profile name
	NetworkScope string `json:"network-scope"`
	// the event the run's jobs receive (Delivery 1A): push, or
	// workflow_dispatch for a manual run; '' for a resolve. Context is the
	// sha-256 (64 hex) of the event payload act reads, a push's as a
	// manual run's; '' for a resolve, which runs no job
	Event   string `json:"event"`
	Context string `json:"context"`
}

// ScopeText is the canonical text of a destination list: sorted,
// comma-joined; ” for none.
func ScopeText(dests []string) string {
	d := append([]string(nil), dests...)
	sort.Strings(d)
	return strings.Join(d, ",")
}

// Noun is [incarnation repo ref candidate oid baseline lock generation
// workflow job trust sandbox mode network network-scope event context].
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
		Cord(m.Network), Cord(m.NetworkScope), Cord(m.Event), Cord(m.Context),
	), nil
}

// ManifestMessage is the authorization: the v1 fields plus the manifest,
// signed as one noun [3 recipient attempt operation expiry nonce
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
	return Tuple(Atom(big.NewInt(ManifestVersion)), Atom(recipient), Atom(attempt), Cord(m.Operation), Atom(big.NewInt(m.Expiry)), Atom(nonce), mn), nil
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
