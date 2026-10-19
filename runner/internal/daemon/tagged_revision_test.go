package daemon

// Private unit checks through the real assignment decoder, signature
// verifier and event reader. The wire envelope models controller output.
// No Git process, ship, launcher or sandbox is started.
import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/ship"
)

func TestTaggedRevisionAssignmentBindsItsOwnYAML(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := wireManifest("check", "", "")
	m.Ref, m.Baseline, m.Lock, m.Mode = "refs/tags/v1.2.3", wireOID, strings.Repeat("a", 64), "event"
	payload := `{"ref":"refs/tags/v1.2.3","after":"` + wireOID + `","before":"` + strings.Repeat("0", 40) + `","sender":{"login":"~zod"}}`
	m.Event, m.Context, m.Trigger = "push", digest(payload), "push refs/tags/v1.2.3 event 0v1.tag object "+strings.Repeat("b", 40)
	makeBody := func() map[string]any {
		body := wireBody(t, priv, m, "")
		a := body["assignment"].(map[string]any)
		a["event-payload"] = payload
		a["lock"] = map[string]any{"digest": m.Lock, "repo": m.Repo, "revision": wireOID, "nodes": []any{}}
		return body
	}
	good := pollWire(t, makeBody())
	d := wireDaemonUnder(pub)
	if err := d.verifyAssignment(good); err != nil {
		t.Fatal(err)
	}
	if e, err := eventOf(good); err != nil || e.Actor != "~zod" || e.Name != "push" {
		t.Fatalf("event %+v: %v", e, err)
	}
	// Mutation controls retain the original signature, changing only the
	// unsigned duplicate field. None may replace the captured input.
	for name, mutate := range map[string]func(map[string]any){
		"default branch YAML":       func(a map[string]any) { a["workflows-oid"] = strings.Repeat("d", 40) },
		"missing workflow revision": func(a map[string]any) { delete(a, "workflows-oid") },
		"moved ref":                 func(a map[string]any) { a["ref"] = "refs/tags/v2" },
		"actor trust":               func(a map[string]any) { a["trust"] = "untrusted" },
		"missing lock":              func(a map[string]any) { a["lock"] = nil },
		"another lock":              func(a map[string]any) { a["lock"].(map[string]any)["digest"] = strings.Repeat("c", 64) },
		"another lock revision":     func(a map[string]any) { a["lock"].(map[string]any)["revision"] = strings.Repeat("d", 40) },
		"another lock repository":   func(a map[string]any) { a["lock"].(map[string]any)["repo"] = "other" },
		"credentials":               func(a map[string]any) { a["grants"] = []any{map[string]any{"name": "DEPLOY", "value": "test-value"}} },
		"tag object as checkout":    func(a map[string]any) { a["oid"] = strings.Repeat("b", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			b := makeBody()
			mutate(b["assignment"].(map[string]any))
			if err := d.verifyAssignment(pollWire(t, b)); err == nil {
				t.Fatal("changed tag input was accepted")
			}
		})
	}
	// The plan carries the same workflow binding, and receives no grants.
	m.Mode, m.Workflow, m.Job = "plan", "", ""
	b := makeBody()
	b["assignment"].(map[string]any)["kind"] = "plan"
	if err := d.verifyAssignment(pollWire(t, b)); err != nil {
		t.Fatalf("tag plan: %v", err)
	}
}

// Exercise the real resolver walk/compiler and its two-phase lock upload.
// Checkout is a private fixture keyed by immutable revision. No Git or
// network fetch is needed because these workflows have no dependencies.
func TestResolveTaggedRevisionCompilesCapturedCatalog(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{}
	d := newTestDaemon(t, box, sh, 1)
	d.cfg.Resolver = true
	tagged := `name: Release checks
on:
  push:
    tags: ['v*', '!v*-rc']
    paths: ['src/**']
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo release
`
	today := strings.ReplaceAll(strings.ReplaceAll(tagged, "Release checks", "Today checks"), "['v*', '!v*-rc']", "['nightly']")
	revisions := map[string]string{wireOID: tagged, strings.Repeat("d", 40): today}
	d.checkout = func(_ context.Context, a *ship.Assignment, dir string) error {
		yaml, ok := revisions[a.OID]
		if !ok {
			return fmt.Errorf("unknown immutable revision %s", a.OID)
		}
		path := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(path, "release.yml"), []byte(yaml), 0644)
	}
	a := testResolverAssignment("resolve-tag", "demo", wireOID)
	d.handle(context.Background(), a)
	if len(sh.locks) != 2 {
		t.Fatalf("expected inventory and lock, got %v", sh.locks)
	}
	var body struct {
		Phase    string              `json:"phase"`
		Triggers plan.TriggerCatalog `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(sh.locks[1]), &body); err != nil {
		t.Fatal(err)
	}
	if body.Phase != "lock" || len(body.Triggers.Workflows) != 1 {
		t.Fatalf("catalog %+v", body)
	}
	w := body.Triggers.Workflows[0]
	if w.Name != "Release checks" || w.Workflow != "release.yml" || w.Refusal != "" || len(w.Events) != 1 {
		t.Fatalf("borrowed default YAML or lost catalog: %+v", w)
	}
	e := w.Events[0]
	if e.Event != "push" || e.Tags == nil || e.Paths == nil || len(e.Tags.Patterns) != 2 {
		t.Fatalf("filters %+v", e)
	}
	// Serialization must keep the match, the exclusion and the path filter.
	if !e.Tags.Patterns[0].Match("v1.2.3") || e.Tags.Patterns[0].Match("nightly") || !e.Tags.Patterns[1].Negated || !e.Tags.Patterns[1].Match("v1-rc") {
		t.Fatalf("tag patterns %+v", e.Tags)
	}
	if len(box.ops) != 0 {
		t.Fatal("resolution executed a workflow")
	}
}

func TestResolveTaggedRevisionFailureKeepsItsCapturedInput(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{}
	d := newTestDaemon(t, box, sh, 1)
	d.cfg.Resolver = true
	var checkouts []string
	d.checkout = func(_ context.Context, a *ship.Assignment, _ string) error {
		checkouts = append(checkouts, a.OID)
		return fmt.Errorf("captured revision unavailable")
	}
	a := testResolverAssignment("resolve-tag", "demo", wireOID)
	d.handle(context.Background(), a)
	if len(checkouts) != 1 || checkouts[0] != wireOID || len(box.ops) != 0 {
		t.Fatalf("resolution changed inputs or executed: checkouts %v, locks %v, operations %v", checkouts, sh.locks, box.ops)
	}
	if len(sh.locks) != 1 {
		t.Fatalf("expected one explicit resolver failure: %v", sh.locks)
	}
	var failure map[string]string
	if err := json.Unmarshal([]byte(sh.locks[0]), &failure); err != nil || len(failure) != 1 || failure["error"] != "checkout: captured revision unavailable" {
		t.Fatalf("failure became a usable lock: %v (%v)", failure, err)
	}
}

func TestTaggedRevisionGuardKeepsPromotedWorkflowBindings(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	for _, ref := range []string{"refs/heads/master", "refs/tags/historical"} {
		t.Run(ref, func(t *testing.T) {
			m := wireManifest("check", "", "")
			m.Ref, m.Baseline, m.Lock, m.Mode = ref, strings.Repeat("d", 40), strings.Repeat("a", 64), "event"
			payload := `{"ref":"` + ref + `","after":"` + wireOID + `","sender":{"login":"~zod"}}`
			m.Event, m.Context, m.Trigger = "push", digest(payload), "push "+ref+" event 0v1.legacy"
			body := wireBody(t, priv, m, "")
			a := body["assignment"].(map[string]any)
			a["event-payload"], a["workflows-oid"] = payload, m.Baseline
			a["lock"] = map[string]any{"digest": m.Lock, "repo": m.Repo, "revision": m.Baseline, "nodes": []any{}}
			if err := wireDaemonUnder(pub).verifyAssignment(pollWire(t, body)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
