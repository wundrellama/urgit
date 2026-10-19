package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

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
// must name the run the manifest signs for: its ref (act's github.ref)
// and, for a push, the signed source as after (act's github.sha); and a
// sender, the actor.
func eventOf(a *ship.Assignment) (actEvent, error) {
	if a.Manifest == nil {
		return actEvent{}, errors.New("no manifest")
	}
	switch a.Manifest.Event {
	case "push", "workflow_dispatch":
	default:
		return actEvent{}, errors.New("event " + a.Manifest.Event + " is not one this runner runs")
	}
	if a.Manifest.Context == "" || a.EventPayload == "" {
		return actEvent{}, errors.New("the signed manifest binds no event payload")
	}
	var payload struct {
		Ref    string `json:"ref"`
		After  string `json:"after"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal([]byte(a.EventPayload), &payload); err != nil {
		return actEvent{}, errors.New("the event payload is not a JSON object: " + err.Error())
	}
	if payload.Ref != a.Manifest.Ref {
		return actEvent{}, fmt.Errorf("the event payload names ref %q, not the signed %q", payload.Ref, a.Manifest.Ref)
	}
	if a.Manifest.Event == "push" && payload.After != a.Manifest.OID {
		return actEvent{}, fmt.Errorf("the push payload names source %q, not the signed %s", payload.After, a.Manifest.OID)
	}
	if payload.Sender.Login == "" {
		return actEvent{}, errors.New("the event payload names no sender")
	}
	return actEvent{Name: a.Manifest.Event, Payload: a.EventPayload, Actor: payload.Sender.Login}, nil
}
