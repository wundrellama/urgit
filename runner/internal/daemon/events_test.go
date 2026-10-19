package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"strings"
	"testing"

	"urgit/runner/internal/config"
	"urgit/runner/internal/ship"
)

// Delivery 1C: the automatic events a run may be admitted for. The
// payloads are MODELS of what the ship renders (desk/lib/ci-trigger
// pull-payload and schedule-payload; the Hoon vector ci-trigger-vector
// pins the ship's own bytes): a native pull request's activity, keyed as
// GitHub's pull_request payload, and a due schedule occurrence.

const prPayload = `{"action":"opened","number":3,"pull_request":{"number":3,"state":"open","merged":false,"title":"Add x","head":{"ref":"feature","sha":"00000000000000000000000000000000000000c3","repo":{"name":"r"}},"base":{"ref":"master","sha":"00000000000000000000000000000000000000b0","repo":{"name":"r"}},"user":{"login":"~zod"}},"repository":{"name":"r","default_branch":"master"},"sender":{"login":"~zod"}}`

const schedulePayload = `{"schedule":"30 5 * * 1-5","repository":{"name":"r","default_branch":"master"},"sender":{"login":"~zod"}}`

const (
	prTrigger       = "pull_request #3 opened head 00000000000000000000000000000000000000c3 base 00000000000000000000000000000000000000b0 event 0v1.ev"
	scheduleTrigger = "schedule fixture-chain.yml#0 due 2026-10-19T09:30:00Z occurrence 0v1.oc"
)

func eventJob(event, payload, trigger string) *ship.Assignment {
	a := *jobAssignment
	m := jobManifest
	m.Event, m.Context, m.Mode, m.Sandbox, m.Trigger = event, payloadDigest(payload), "event", "container", trigger
	a.Manifest, a.EventPayload = &m, payload
	return &a
}

func honourableDaemon() *Daemon {
	return &Daemon{cfg: &config.Config{Sandbox: "docker-rootless"}, box: &fakeBox{}, log: log.New(io.Discard, "", 0)}
}

// pull_request and schedule are events this runner runs; an event it does
// not is given back, never run as a push
func TestHonourableTakesTheAutomaticEvents(t *testing.T) {
	d := honourableDaemon()
	for _, a := range []*ship.Assignment{eventJob("pull_request", prPayload, prTrigger), eventJob("schedule", schedulePayload, scheduleTrigger)} {
		if err := d.honourable(a); err != nil {
			t.Fatalf("%s: %v", a.Manifest.Event, err)
		}
	}
	for _, event := range []string{"pull_request_target", "issues", "workflow_run"} {
		a := eventJob(event, prPayload, event+" x")
		if err := d.honourable(a); err == nil || !strings.Contains(err.Error(), "event "+event+" is not one this runner runs") {
			t.Fatalf("%s: %v", event, err)
		}
	}
}

// the signed trigger (manifest version 5) names the run's own event: one
// that names no event, or another, is a tampered or mismatched assignment
func TestTheSignedTriggerNamesTheRunsEvent(t *testing.T) {
	d := honourableDaemon()
	none := eventJob("pull_request", prPayload, "")
	if err := d.honourable(none); err == nil || !strings.Contains(err.Error(), "names no trigger") {
		t.Fatalf("no trigger: %v", err)
	}
	other := eventJob("push", pushPayload, scheduleTrigger)
	if err := d.honourable(other); err == nil || !strings.Contains(err.Error(), "names another event") {
		t.Fatalf("a schedule trigger on a push: %v", err)
	}
	prefix := eventJob("pull_request", prPayload, "pull_request_target #3")
	if err := d.honourable(prefix); err == nil || !strings.Contains(err.Error(), "names another event") {
		t.Fatalf("a trigger that only starts with the event's letters: %v", err)
	}
}

// the payload must name the run the manifest signs for: a pull request's
// base is the signed ref's branch, its number, action and head are given;
// a schedule's default branch is the signed ref's, its cron is given; and
// each names a sender
func TestEventOfReadsAPullRequestAndASchedule(t *testing.T) {
	for _, a := range []*ship.Assignment{eventJob("pull_request", prPayload, prTrigger), eventJob("schedule", schedulePayload, scheduleTrigger)} {
		event, err := eventOf(a)
		if err != nil || event.Name != a.Manifest.Event || event.Payload != a.EventPayload || event.Actor != "~zod" {
			t.Fatalf("%s: %+v %v", a.Manifest.Event, event, err)
		}
	}
	refused := map[string]*ship.Assignment{
		"a pull request into another branch":   eventJob("pull_request", strings.Replace(prPayload, `"ref":"master"`, `"ref":"other"`, 1), prTrigger),
		"a pull request with no number":        eventJob("pull_request", strings.Replace(prPayload, `"number":3,"pull_request"`, `"pull_request"`, 1), prTrigger),
		"a pull request numbered 0":            eventJob("pull_request", strings.Replace(prPayload, `"number":3,"pull_request"`, `"number":0,"pull_request"`, 1), prTrigger),
		"a pull request numbered 2.5":          eventJob("pull_request", strings.Replace(prPayload, `"number":3,"pull_request"`, `"number":2.5,"pull_request"`, 1), prTrigger),
		"a pull request with no action":        eventJob("pull_request", strings.Replace(prPayload, `"action":"opened",`, ``, 1), prTrigger),
		"a pull request with no head sha":      eventJob("pull_request", strings.Replace(prPayload, `"sha":"00000000000000000000000000000000000000c3",`, ``, 1), prTrigger),
		"a pull request naming no sender":      eventJob("pull_request", strings.Replace(prPayload, `,"sender":{"login":"~zod"}`, ``, 1), prTrigger),
		"a schedule of another default branch": eventJob("schedule", strings.Replace(schedulePayload, `"default_branch":"master"`, `"default_branch":"main"`, 1), scheduleTrigger),
		"a schedule with no cron":              eventJob("schedule", strings.Replace(schedulePayload, `"schedule":"30 5 * * 1-5",`, ``, 1), scheduleTrigger),
		"a schedule naming no sender":          eventJob("schedule", strings.Replace(schedulePayload, `,"sender":{"login":"~zod"}`, ``, 1), scheduleTrigger),
	}
	for name, a := range refused {
		if _, err := eventOf(a); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// the signed payload of a pull_request run is bound like any other: one
// altered after signing (another action, another head) is refused, and so
// is a trigger altered after signing
func TestPullRequestPayloadIsBoundBySignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	d := &Daemon{daemonID: "0v1.daemon", ciKey: hex.EncodeToString(pub), log: log.New(io.Discard, "", 0)}
	m := *eventJob("pull_request", prPayload, prTrigger).Manifest
	if err := d.verifyAssignment(signed(t, priv, *jobAssignment, prPayload, m)); err != nil {
		t.Fatalf("a pull_request payload as signed: %v", err)
	}
	for name, altered := range map[string]string{
		"another action": strings.Replace(prPayload, `"action":"opened"`, `"action":"closed"`, 1),
		"another head":   strings.Replace(prPayload, `"sha":"00000000000000000000000000000000000000c3"`, `"sha":"00000000000000000000000000000000000000c4"`, 1),
	} {
		b := signed(t, priv, *jobAssignment, prPayload, m)
		b.EventPayload = altered
		if err := d.verifyAssignment(b); err == nil || !strings.Contains(err.Error(), "the event payload does not match the signed context") {
			t.Fatalf("%s after signing: %v", name, err)
		}
	}
	tampered := m
	tampered.Trigger = strings.Replace(prTrigger, "opened", "closed", 1)
	c := signed(t, priv, *jobAssignment, prPayload, m)
	c.Manifest = &tampered
	if err := d.verifyAssignment(c); err == nil || !strings.Contains(err.Error(), "signature does not verify") {
		t.Fatalf("a trigger altered after signing: %v", err)
	}
}

// act runs the job on the event the ship signed for: pull_request and
// schedule, each with its payload read with -e and its sender as actor
func TestAutomaticEventJobsRunActOnTheirEvent(t *testing.T) {
	for _, c := range []struct {
		event, payload, trigger string
	}{{"pull_request", prPayload, prTrigger}, {"schedule", schedulePayload, scheduleTrigger}} {
		sh := &fakeShip{}
		box := &fakeBox{stream: `{"job":"fixture-chain/0v1.att/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
		d := newTestDaemon(t, box, sh, 1)
		run, _ := runAndRead(t, d, box, eventJob(c.event, c.payload, c.trigger))
		want := "run /usr/local/bin/act " + c.event + " -W /work/projected/fixture-chain.yml -j b -e /work/event.json --actor ~zod -P ubuntu-latest=img "
		if !strings.HasPrefix(run, want) {
			t.Fatalf("%s argv:\n%s\nwant the prefix:\n%s", c.event, run, want)
		}
		if box.eventJSON != c.payload {
			t.Fatalf("%s: event.json is not the signed payload: %q", c.event, box.eventJSON)
		}
		if len(sh.results) != 1 || len(sh.abandons) != 0 {
			t.Fatalf("%s: results %v abandons %v", c.event, sh.results, sh.abandons)
		}
	}
}
