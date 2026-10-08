package ship

// The recovery channel (legacy-recovery UI ruling 01; runner/launcher/
// INTEGRATION.md §11.12; specs/ci-execution-contract.md §8b): the daemon
// reports the slots it withholds, receives the operator's recovery command
// from Urgit on its own poll, and answers it. Every request carries the
// daemon's bearer; nothing else reaches the host.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// RecoveryCommand is a recovery command as the ship hands it over: one
// operation, bound to one retention, signed with the CI key for this
// daemon (sig.RecoveryMessage).
type RecoveryCommand struct {
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Operation string `json:"operation"`
	Selection string `json:"selection"`
	Revision  uint64 `json:"revision"`
	Evidence  string `json:"evidence"`
	Label     string `json:"label"`
	Expiry    int64  `json:"expiry"` // unix seconds
	Nonce     string `json:"nonce"`
	Sig       string `json:"sig"`
}

// RecoveryAnswer is the daemon's answer to a command: completed (the
// release durable, and the capacity now), refused (why, the charge kept),
// or uncertain (whether the release is durable is not known yet).
type RecoveryAnswer struct {
	Command    string          `json:"command"`
	Status     string          `json:"status"`
	Detail     string          `json:"detail"`
	Selection  string          `json:"selection"`
	Revision   uint64          `json:"revision"`
	Evidence   string          `json:"evidence"`
	ReleasedAt int64           `json:"releasedAt,omitempty"`
	Capacity   *ReportCapacity `json:"capacity,omitempty"`
}

// Answer statuses.
const (
	AnswerCompleted = "completed"
	AnswerRefused   = "refused"
	AnswerUncertain = "uncertain"
)

// RetentionReport is what a daemon tells the ship of the slots it
// withholds: every retention, how its slot returns, and for a legacy
// retention the evidence a release would stand on. It authorizes nothing.
type RetentionReport struct {
	Version    int              `json:"version"`
	At         int64            `json:"at"`
	Sandbox    string           `json:"sandbox"`
	Capacity   ReportCapacity   `json:"capacity"`
	Retentions []RetentionEntry `json:"retentions"`
	Truncated  int              `json:"truncated"`
	Released   []ReleasedEntry  `json:"released"`
	// History is the runner's execution history (INTEGRATION.md §11.15)
	History *HistoryReport `json:"history,omitempty"`
}

// HistoryReport is a runner's execution history as the owner inspects it
// (legacy-replay-upgrade ruling 01; runner/launcher/INTEGRATION.md §11.15):
// its selection and revision; whether it is known, and complete (begun
// with the runner's enrollment); whether the runner waits for its
// transition; the authorization epoch it is at, and its transition if any;
// and the digest of its evidence, which a transition is bound to.
type HistoryReport struct {
	Selection   string            `json:"selection"`
	Revision    uint64            `json:"revision"`
	Since       int64             `json:"since"`
	Known       bool              `json:"known"`
	Complete    bool              `json:"complete"`
	StateFormat int               `json:"stateFormat"`
	Paused      bool              `json:"paused"`
	Epoch       uint64            `json:"epoch"`
	Evidence    string            `json:"evidence"`
	Transition  *TransitionReport `json:"transition,omitempty"`
	// InvalidTransition is a record the state file holds that is no
	// transition: why, and the record as the file holds it, clipped
	// (INTEGRATION.md §11.16)
	InvalidTransition *InvalidTransitionReport `json:"invalidTransition,omitempty"`
	Explanation       string                   `json:"explanation"`
}

// TransitionReport is a runner's recorded transition.
type TransitionReport struct {
	Epoch   uint64 `json:"epoch"`
	Command string `json:"command"`
	At      int64  `json:"at"`
}

// InvalidTransitionReport is a transition record a runner's state file
// holds that is no transition (INTEGRATION.md §11.16): why, and the record
// as the file holds it, clipped. The runner waits, and keeps the record.
type InvalidTransitionReport struct {
	Problem string `json:"problem"`
	Record  string `json:"record"`
}

// ReportCapacity is the runner's slots: configured, withheld by
// retentions, held for orphans, running, and advertised to the ship.
type ReportCapacity struct {
	Configured int `json:"configured"`
	Withheld   int `json:"withheld"`
	Held       int `json:"held"`
	Running    int `json:"running"`
	Advertised int `json:"advertised"`
}

// RetentionEntry is one retention as the operator inspects it.
type RetentionEntry struct {
	Selection string `json:"selection"`
	Revision  uint64 `json:"revision"`
	Handle    string `json:"handle"`
	Attempt   string `json:"attempt"`
	Label     string `json:"label"`
	Backend   string `json:"backend"`
	// Kind: legacy, admission, vm, docker, unknown-backend or unmarked
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
	Reason   string `json:"reason"`
	Retained int64  `json:"retained"`
	// Legacy is the entry's legacy mark, when it has one
	Legacy *LegacyMark `json:"legacy,omitempty"`
	// Release is how its slot returns: urgit (a legacy retention),
	// settlement (an admission), cli (urgit-runner -recover, the daemon
	// stopped), none
	Release     string      `json:"release"`
	Explanation string      `json:"explanation"`
	Eligible    bool        `json:"eligible"`
	Evidence    string      `json:"evidence"`
	Conditions  []Condition `json:"conditions,omitempty"`
	Facts       *Facts      `json:"facts,omitempty"`
}

// LegacyMark is a legacy retention's provenance.
type LegacyMark struct {
	Format int   `json:"format"`
	Found  int64 `json:"found"`
}

// Condition is one condition of a legacy release, met or not, and why.
type Condition struct {
	Name   string `json:"name"`
	Met    bool   `json:"met"`
	Detail string `json:"detail"`
}

// Facts are what the launcher answered for a legacy retention.
type Facts struct {
	Socket   string   `json:"socket"`
	Launcher string   `json:"launcher"`
	Protocol int      `json:"protocol"`
	Records  int      `json:"records"`
	Held     []string `json:"held"`
	Error    string   `json:"error,omitempty"`
}

// ReleasedEntry is one released retention, for the operator's history.
type ReleasedEntry struct {
	Selection  string `json:"selection"`
	Handle     string `json:"handle"`
	Attempt    string `json:"attempt"`
	Label      string `json:"label"`
	ReleasedAt int64  `json:"releasedAt"`
	Command    string `json:"command,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// ErrNoRecovery is a ship that has no recovery channel (a ship before it:
// its route unknown). The daemon carries on without reporting.
var ErrNoRecovery = errors.New("the ship has no recovery channel")

// PollWork is Poll, answering a recovery command too when the ship hands
// one over (only to a client whose Recovery says it carries them out): an
// assignment, a command, or neither (the window closed, 204).
func (c *Client) PollWork(ctx context.Context, daemonID string) (*Assignment, *RecoveryCommand, error) {
	resp, err := c.do(ctx, http.MethodGet, "/daemon/"+daemonID+"/assignment", nil)
	if err != nil {
		return nil, nil, err
	}
	switch resp.Status {
	case http.StatusNoContent:
		return nil, nil, nil
	case http.StatusUnauthorized:
		if strings.Contains(resp.Error(), "revoked") {
			return nil, nil, ErrRevoked
		}
		return nil, nil, ErrUnauthorized
	case http.StatusOK:
		var answer struct {
			Assignment *Assignment      `json:"assignment"`
			Recovery   *RecoveryCommand `json:"recovery"`
		}
		if err := json.Unmarshal(resp.Body, &answer); err != nil {
			return nil, nil, fmt.Errorf("poll: %w", err)
		}
		switch {
		case answer.Recovery != nil && answer.Assignment == nil:
			if answer.Recovery.ID == "" {
				return nil, nil, errors.New("poll: recovery command without an id")
			}
			return nil, answer.Recovery, nil
		case answer.Assignment != nil && answer.Recovery == nil:
			if answer.Assignment.Attempt == "" {
				return nil, nil, errors.New("poll: assignment without an attempt")
			}
			return answer.Assignment, nil, nil
		}
		return nil, nil, errors.New("poll: an answer is one assignment or one recovery command")
	default:
		return nil, nil, fmt.Errorf("poll: %s", resp.Error())
	}
}

// PostRetentions reports the daemon's retentions (its bearer only).
func (c *Client) PostRetentions(ctx context.Context, daemonID string, report RetentionReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/daemon/"+daemonID+"/retentions", body)
	if err != nil {
		return err
	}
	switch {
	case resp.Status >= 200 && resp.Status <= 299:
		return nil
	case resp.Status == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.Status == http.StatusNotFound && strings.Contains(resp.Error(), "route not found"):
		return ErrNoRecovery
	}
	return fmt.Errorf("retentions: %s", resp.Error())
}

// PostRecoveryAnswer answers a recovery command (its bearer only). found is
// false when the ship holds no such command of this daemon: the answer has
// nowhere to go.
func (c *Client) PostRecoveryAnswer(ctx context.Context, daemonID string, answer RecoveryAnswer) (found bool, err error) {
	body, err := json.Marshal(answer)
	if err != nil {
		return false, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/daemon/"+daemonID+"/recovery", body)
	if err != nil {
		return false, err
	}
	switch {
	case resp.Status >= 200 && resp.Status <= 299:
		return true, nil
	case resp.Status == http.StatusUnauthorized:
		return false, ErrUnauthorized
	case resp.Status == http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("recovery answer: %s", resp.Error())
}
