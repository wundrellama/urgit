package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"urgit/runner/internal/ship"
)

// eventContext is what a run's signed manifest binds its event payload by
// (Delivery 1A): the sha-256 of the payload's bytes, 64 hex, as the ship
// digests the text it signed for.
func eventContext(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// eventFile is where a run's event payload sits in the bundle, and so at
// the guest's work root, where act reads it with -e
const eventFile = "event.json"

// actEvent is how act is told the event an assignment's job runs for
// (Delivery 1A): the event name it takes as its first argument, the
// payload it reads with -e, and the actor the payload names as its sender
// (github.actor).
type actEvent struct {
	Name    string
	Payload string
	Actor   string
}

// eventOf reads the event from the verified manifest. Its payload's bytes
// were checked against the signed context when the assignment arrived; a
// push carries one as a manual run does — the ship renders it from the
// candidate's ref, base, source and actor — so act never runs a job on
// its own defaults (an empty event, the actor nektos/act). The payload
// must name the run the manifest signs for: for a push and a manual run
// its ref (act's github.ref) and, for a push, the signed source as after
// (act's github.sha); for a pull request (Delivery 1C) its number, its
// action, its head and a base that is the signed ref's branch (act makes
// github.ref refs/pull/<number>/merge and takes github.sha from the
// checkout, the signed object); for a schedule its cron and a default
// branch that is the signed ref's branch (act makes github.ref that
// branch); and for every event a sender, the actor. A pull request's
// labels, when the payload lists them, are at most maxLabels objects with
// a name, as the ship's labels are; a labeled or unlabeled payload lists
// them and names its label, among them after labeled and not after
// unlabeled, and no other activity names a label (pullLabelsError).
func eventOf(a *ship.Assignment) (actEvent, error) {
	if a.Manifest == nil {
		return actEvent{}, errors.New("no manifest")
	}
	switch a.Manifest.Event {
	case "push", "workflow_dispatch", "pull_request", "schedule":
	default:
		return actEvent{}, errors.New("event " + a.Manifest.Event + " is not one this runner runs")
	}
	if a.Manifest.Context == "" || a.EventPayload == "" {
		return actEvent{}, errors.New("the signed manifest binds no event payload")
	}
	var payload struct {
		Ref      string   `json:"ref"`
		After    string   `json:"after"`
		Action   string   `json:"action"`
		Number   *float64 `json:"number"`
		Schedule string   `json:"schedule"`
		Label    *struct {
			Name string `json:"name"`
		} `json:"label"`
		PullRequest struct {
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Labels *[]json.RawMessage `json:"labels"`
		} `json:"pull_request"`
		Repository struct {
			DefaultBranch string `json:"default_branch"`
		} `json:"repository"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal([]byte(a.EventPayload), &payload); err != nil {
		return actEvent{}, errors.New("the event payload is not a JSON object: " + err.Error())
	}
	branch := strings.TrimPrefix(a.Manifest.Ref, "refs/heads/")
	switch a.Manifest.Event {
	case "push", "workflow_dispatch":
		if payload.Ref != a.Manifest.Ref {
			return actEvent{}, fmt.Errorf("the event payload names ref %q, not the signed %q", payload.Ref, a.Manifest.Ref)
		}
		if a.Manifest.Event == "push" && payload.After != a.Manifest.OID {
			return actEvent{}, fmt.Errorf("the push payload names source %q, not the signed %s", payload.After, a.Manifest.OID)
		}
	case "pull_request":
		if payload.Number == nil || *payload.Number < 1 || *payload.Number != math.Trunc(*payload.Number) {
			return actEvent{}, errors.New("the pull_request payload names no pull request number")
		}
		if payload.Action == "" {
			return actEvent{}, errors.New("the pull_request payload names no action")
		}
		if len(payload.PullRequest.Head.SHA) != 40 {
			return actEvent{}, errors.New("the pull_request payload names no head sha")
		}
		if branch == a.Manifest.Ref || payload.PullRequest.Base.Ref != branch {
			return actEvent{}, fmt.Errorf("the pull_request payload's base %q is not the signed ref %q", payload.PullRequest.Base.Ref, a.Manifest.Ref)
		}
		label := ""
		if payload.Label != nil {
			label = payload.Label.Name
		}
		if err := pullLabelsError(payload.Action, payload.PullRequest.Labels, payload.Label != nil, label); err != nil {
			return actEvent{}, err
		}
	case "schedule":
		if payload.Schedule == "" {
			return actEvent{}, errors.New("the schedule payload names no cron")
		}
		if branch == a.Manifest.Ref || payload.Repository.DefaultBranch != branch {
			return actEvent{}, fmt.Errorf("the schedule payload's default branch %q is not the signed ref %q", payload.Repository.DefaultBranch, a.Manifest.Ref)
		}
	}
	if payload.Sender.Login == "" {
		return actEvent{}, errors.New("the event payload names no sender")
	}
	return actEvent{Name: a.Manifest.Event, Payload: a.EventPayload, Actor: payload.Sender.Login}, nil
}

// maxLabels is the most labels a pull request carries, as the ship allows
const maxLabels = 20

// pullLabelsError checks a pull_request payload's labels and label. labels
// is nil when the payload lists none: a payload rendered before labels
// existed is never rewritten, so that is accepted for every activity but
// labeled and unlabeled, which always list them.
func pullLabelsError(action string, labels *[]json.RawMessage, hasLabel bool, label string) error {
	var names []string
	if labels != nil {
		if len(*labels) > maxLabels {
			return fmt.Errorf("the pull_request payload lists more than %d labels", maxLabels)
		}
		for _, raw := range *labels {
			var entry struct {
				Name *string `json:"name"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil || entry.Name == nil || *entry.Name == "" {
				return errors.New("the pull_request payload lists a label with no name")
			}
			names = append(names, *entry.Name)
		}
	}
	if action != "labeled" && action != "unlabeled" {
		if hasLabel {
			return fmt.Errorf("the %s payload names a label: only labeled and unlabeled do", action)
		}
		return nil
	}
	if labels == nil {
		return fmt.Errorf("the %s payload lists no pull_request.labels", action)
	}
	if label == "" {
		return fmt.Errorf("the %s payload names no label", action)
	}
	listed := false
	for _, name := range names {
		listed = listed || name == label
	}
	if action == "labeled" && !listed {
		return fmt.Errorf("the labeled payload's label %q is not among its labels", label)
	}
	if action == "unlabeled" && listed {
		return fmt.Errorf("the unlabeled payload's label %q is still among its labels", label)
	}
	return nil
}
