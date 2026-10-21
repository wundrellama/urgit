package daemon

import (
	"os"
	"strings"
	"testing"

	"urgit/runner/internal/ship"
)

// Pull request labels: the labeled and unlabeled activities, and the
// pull_request.labels every pull request payload now carries. The pinned
// payloads in testdata/labels are what desk/lib/ci-trigger
// pull-event-payload renders for desk/gen/ci-trigger-vector's facts. That
// vector embeds the same bytes and checks the ship's payload is the same
// JSON (the parent runs it on a ship). plan's TestHoonVectorsEmbedThePins
// checks the embedding. Here the runner's own validation, eventOf, reads
// them.

func labelPayload(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/labels/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(data), "\n")
}

// a pull request job into main: the pinned payloads' base
func labelJob(payload, action string) *ship.Assignment {
	a := eventJob("pull_request", payload, "pull_request #7 "+action+" head 00000000000000000000000000000000000000d1 base 00000000000000000000000000000000000000b1 event 0v1.ev")
	m := *a.Manifest
	m.Ref = "refs/heads/main"
	a.Manifest = &m
	a.Ref = "refs/heads/main"
	return a
}

// the ship's labeled, unlabeled and synchronize payloads are accepted, each
// run as pull_request with its sender (the labeler) as the actor
func TestEventOfAcceptsTheLabelPayloads(t *testing.T) {
	for _, c := range []struct{ name, actor string }{{"labeled", "~zod"}, {"unlabeled", "~zod"}, {"synchronize", "~nec"}} {
		payload := labelPayload(t, c.name)
		event, err := eventOf(labelJob(payload, c.name))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if event.Name != "pull_request" || event.Payload != payload || event.Actor != c.actor {
			t.Fatalf("%s: %+v", c.name, event)
		}
	}
}

// a payload rendered before labels existed is never rewritten, so one with
// no pull_request.labels is still accepted for every other activity
func TestEventOfAcceptsAPayloadFromBeforeLabels(t *testing.T) {
	old := prPayload
	if strings.Contains(old, `"labels"`) {
		t.Fatal("the model payload already carries labels")
	}
	if _, err := eventOf(eventJob("pull_request", old, prTrigger)); err != nil {
		t.Fatalf("an opened payload from before labels: %v", err)
	}
}

// a label payload that does not say what it labelled, or says it wrongly,
// is refused with the reason; so is a malformed label list on any action
func TestEventOfRefusesAMalformedLabelPayload(t *testing.T) {
	labeled := labelPayload(t, "labeled")
	unlabeled := labelPayload(t, "unlabeled")
	sync := labelPayload(t, "synchronize")
	many := `[` + strings.TrimSuffix(strings.Repeat(`{"name":"x"},`, 21), ",") + `]`
	for name, c := range map[string]struct{ payload, action, want string }{
		"labeled with no label":               {strings.Replace(labeled, `,"label":{"name":"ci:fixtures"}`, ``, 1), "labeled", "names no label"},
		"labeled with an empty label":         {strings.Replace(labeled, `"label":{"name":"ci:fixtures"}`, `"label":{"name":""}`, 1), "labeled", "names no label"},
		"labeled with no labels":              {strings.Replace(labeled, `,"labels":[{"name":"ci:fixtures"},{"name":"needs review"}]`, ``, 1), "labeled", "lists no pull_request.labels"},
		"labeled, the label not listed":       {strings.Replace(labeled, `{"name":"ci:fixtures"},{"name":"needs review"}`, `{"name":"needs review"}`, 1), "labeled", "is not among"},
		"unlabeled, the label still listed":   {strings.Replace(unlabeled, `[{"name":"needs review"}]`, `[{"name":"ci:fixtures"},{"name":"needs review"}]`, 1), "unlabeled", "is still among"},
		"unlabeled with no label":             {strings.Replace(unlabeled, `,"label":{"name":"ci:fixtures"}`, ``, 1), "unlabeled", "names no label"},
		"a label on another activity":         {strings.Replace(sync, `"sender":{"login":"~nec"}`, `"sender":{"login":"~nec"},"label":{"name":"ci:fixtures"}`, 1), "synchronize", "only labeled and unlabeled"},
		"a label entry with no name":          {strings.Replace(sync, `[{"name":"ci:fixtures"}]`, `[{"color":"red"}]`, 1), "synchronize", "a label with no name"},
		"a label entry with an empty name":    {strings.Replace(sync, `[{"name":"ci:fixtures"}]`, `[{"name":""}]`, 1), "synchronize", "a label with no name"},
		"a label entry that is not an object": {strings.Replace(sync, `[{"name":"ci:fixtures"}]`, `["ci:fixtures"]`, 1), "synchronize", "a label with no name"},
		"labels that are not a list":          {strings.Replace(sync, `[{"name":"ci:fixtures"}]`, `{"name":"ci:fixtures"}`, 1), "synchronize", "not a JSON object"},
		"more than twenty labels":             {strings.Replace(sync, `[{"name":"ci:fixtures"}]`, many, 1), "synchronize", "more than 20 labels"},
	} {
		_, err := eventOf(labelJob(c.payload, c.action))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", name, err, c.want)
		}
	}
}
