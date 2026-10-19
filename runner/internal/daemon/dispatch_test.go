package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"urgit/runner/internal/config"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
)

// the manual workflow the plan package's compiler test reads, checked out
// beside a push-only workflow
func manualCheckout(t *testing.T) func(context.Context, *ship.Assignment, string) error {
	t.Helper()
	manual, err := os.ReadFile("../plan/testdata/manual.yml")
	if err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, _ *ship.Assignment, dir string) error {
		wf := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(wf, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(wf, "manual.yml"), manual, 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(wf, "fixture-chain.yml"), []byte(chainWorkflow), 0o644)
	}
}

// the explicit import posts, beside the lock, what each workflow declares
// for a manual run as the pinned compiler reads it at the revision: the
// workflows that declare workflow_dispatch, with their inputs in
// declaration order; a push-only workflow is not listed
func TestResolveSendsTheManualInventory(t *testing.T) {
	sh := &fakeShip{}
	d := newTestDaemon(t, &fakeBox{}, sh, 1)
	d.cfg.Resolver = true
	d.checkout = manualCheckout(t)
	a := &ship.Assignment{ID: "0v1.asg", Attempt: "0v1.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/ci/resolve",
		OID: "0000000000000000000000000000000000000001", Kind: "resolve", DeadlineSeconds: 60, Manifest: &jobManifest}
	d.handle(context.Background(), a)
	if len(sh.locks) != 2 {
		t.Fatalf("the resolver posted %d lock bodies, want the inventory and the lock: %v (abandons %v)", len(sh.locks), sh.locks, sh.abandons)
	}
	var body struct {
		Phase    string          `json:"phase"`
		Dispatch []plan.Dispatch `json:"dispatch"`
	}
	if err := json.Unmarshal([]byte(sh.locks[1]), &body); err != nil {
		t.Fatal(err)
	}
	if body.Phase != "lock" {
		t.Fatalf("the second post is phase %q", body.Phase)
	}
	if len(body.Dispatch) != 1 || body.Dispatch[0].Workflow != "manual.yml" || body.Dispatch[0].Name != "Manual release" {
		t.Fatalf("dispatch inventory: %+v", body.Dispatch)
	}
	var ids []string
	for _, in := range body.Dispatch[0].Inputs {
		ids = append(ids, in.ID)
	}
	if want := []string{"dry_run", "count", "level", "note", "target"}; len(ids) != len(want) || ids[0] != want[0] || ids[4] != want[4] {
		t.Fatalf("inputs %v, want %v", ids, want)
	}
}

// the manual payload of the runner tests: what the ship builds for a
// manual run of manual.yml with a boolean false and a number zero
// supplied and two defaults (ci-dispatch payload; ci-dispatch-vector pins
// its shape): a boolean as its text, a number as a JSON number
const manualPayload = `{"workflow":".github/workflows/manual.yml","ref":"refs/heads/master","inputs":{"dry_run":"false","count":0,"level":"warning","note":"hello"},"sender":{"login":"~zod"}}`

// the push payload of the runner tests: what the ship renders for a push
// run's attempt from its candidate (ci-dispatch push-payload): the ref,
// the base as before, the source the manifest signs as after, the actor
const pushPayload = `{"after":"0000000000000000000000000000000000000001","before":"00000000000000000000000000000000000000b0","deleted":false,"pusher":{"name":"~zod"},"ref":"refs/heads/master","sender":{"login":"~zod"}}`

func payloadDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// signed is a as the ship signs it for this daemon: the manifest m, the
// payload beside it
func signed(t *testing.T, priv ed25519.PrivateKey, a ship.Assignment, payload string, m sig.Manifest) *ship.Assignment {
	t.Helper()
	a.Manifest = &m
	a.EventPayload = payload
	msg := sig.ManifestMessage{Recipient: "0v1.daemon", Attempt: a.Attempt, Operation: "assign", Expiry: time.Now().Unix() + 300, Nonce: "0v7", Manifest: m}
	b, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	a.Sig = &ship.Signature{Version: sig.ManifestVersion, Recipient: msg.Recipient, Attempt: msg.Attempt, Operation: msg.Operation, Expiry: msg.Expiry, Nonce: msg.Nonce, Sig: hex.EncodeToString(ed25519.Sign(priv, b))}
	return &a
}

// a manual run's assignment as the ship signs it: event workflow_dispatch,
// the payload's sha-256 as the signed context, the payload beside it
func manualAssignment(t *testing.T, priv ed25519.PrivateKey, payload string, m sig.Manifest) *ship.Assignment {
	t.Helper()
	return signed(t, priv, *manualJob(), payload, m)
}

func manualManifest() sig.Manifest {
	m := jobManifest
	m.Workflow, m.Job = "manual.yml", "report"
	m.Event, m.Context, m.Mode, m.Sandbox = "workflow_dispatch", payloadDigest(manualPayload), "manual", "vm"
	m.Needs, m.Prerequisites = "", ""
	return m
}

// manualJob is manual.yml's report job as the ship assigns it for a
// manual run: no prerequisites, the payload beside the manifest
func manualJob() *ship.Assignment {
	a := *jobAssignment
	m := manualManifest()
	a.Workflow, a.Job, a.NeedsContext, a.Manifest, a.EventPayload = "manual.yml", "report", "", &m, manualPayload
	return &a
}

// runAndRead runs a job through the daemon's real bundle and act path on
// the fake box, and returns act's argv and the projected workflow as the
// bundle carried it in
func runAndRead(t *testing.T, d *Daemon, box *fakeBox, a *ship.Assignment) (run, projected string) {
	t.Helper()
	d.box = &projectionReader{fakeBox: box, read: func(host string) {
		if data, err := os.ReadFile(filepath.Join(strings.TrimSuffix(host, "/."), "projected", a.Workflow)); err == nil {
			projected = string(data)
		}
	}}
	d.handle(context.Background(), a)
	for _, op := range box.ops {
		if strings.HasPrefix(op, "run ") {
			run = op
		}
	}
	return run, projected
}

// projectedRun is what act reads from a projection: the workflow's name
// (github.workflow) and the one job's run name (act's container identity
// beside it)
func projectedRun(t *testing.T, projected string) (workflow, job, runName string) {
	t.Helper()
	var doc struct {
		Name string `yaml:"name"`
		Jobs map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(projected), &doc); err != nil || len(doc.Jobs) != 1 {
		t.Fatalf("the bundle's projection (%v):\n%s", err, projected)
	}
	for id, j := range doc.Jobs {
		job, runName = id, j.Name
	}
	return doc.Name, job, runName
}

// the payload is bound by the signed context: the payload as signed is
// accepted; one altered after signing, a payload the manifest does not
// bind, and a bound context with no payload are each a tampered
// assignment, refused before any work
func TestManualPayloadIsBoundBySignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	d := &Daemon{daemonID: "0v1.daemon", ciKey: hex.EncodeToString(pub), log: log.New(io.Discard, "", 0)}
	if err := d.verifyAssignment(manualAssignment(t, priv, manualPayload, manualManifest())); err != nil {
		t.Fatalf("the payload as signed must be accepted: %v", err)
	}
	altered := manualAssignment(t, priv, manualPayload, manualManifest())
	altered.EventPayload = strings.Replace(manualPayload, `"dry_run":"false"`, `"dry_run":"true"`, 1)
	if err := d.verifyAssignment(altered); err == nil || !strings.Contains(err.Error(), "the event payload does not match the signed context") {
		t.Fatalf("a payload altered after signing: %v", err)
	}
	missing := manualAssignment(t, priv, "", manualManifest())
	if err := d.verifyAssignment(missing); err == nil || !strings.Contains(err.Error(), "the event payload does not match the signed context") {
		t.Fatalf("a bound context with no payload: %v", err)
	}
	none := manualManifest()
	none.Context = ""
	unbound := manualAssignment(t, priv, manualPayload, none)
	if err := d.verifyAssignment(unbound); err == nil || !strings.Contains(err.Error(), "an event payload the signed manifest does not bind") {
		t.Fatalf("a payload beside a manifest that binds none: %v", err)
	}
	// a push binds its payload the same way
	push := signed(t, priv, *jobAssignment, pushPayload, jobManifest)
	if err := d.verifyAssignment(push); err != nil {
		t.Fatalf("a push payload as signed must be accepted: %v", err)
	}
	push.EventPayload = strings.Replace(pushPayload, `"login":"~zod"`, `"login":"~nec"`, 1)
	if err := d.verifyAssignment(push); err == nil || !strings.Contains(err.Error(), "the event payload does not match the signed context") {
		t.Fatalf("a push payload naming another actor after signing: %v", err)
	}
}

// the events this runner runs: push and workflow_dispatch for a job or a
// plan, none for a resolve; any other is abandoned, never run as a push
func TestHonourableRefusesAnEventItDoesNotRun(t *testing.T) {
	d := &Daemon{cfg: &config.Config{Sandbox: "docker-rootless"}, box: &fakeBox{}, log: log.New(io.Discard, "", 0)}
	pr := *jobAssignment
	m := jobManifest
	m.Event = "pull_request"
	pr.Manifest = &m
	if err := d.honourable(&pr); err == nil || !strings.Contains(err.Error(), "event pull_request is not one this runner runs") {
		t.Fatalf("an unknown event: %v", err)
	}
	none := *jobAssignment
	mn := jobManifest
	mn.Event = ""
	none.Manifest = &mn
	if err := d.honourable(&none); err == nil || !strings.Contains(err.Error(), "names no event") {
		t.Fatalf("a job with no event: %v", err)
	}
	manual := *manualJob()
	mm := manualManifest()
	mm.Sandbox = "container"
	manual.Manifest = &mm
	if err := d.honourable(&manual); err != nil {
		t.Fatalf("a manual job on a runner that can take it: %v", err)
	}
	// an event with no payload bound would run on act's own defaults (an
	// empty event, the actor nektos/act): given back, for a push as for a
	// manual run
	for _, event := range []string{"push", "workflow_dispatch"} {
		bare := *jobAssignment
		mb := jobManifest
		mb.Event, mb.Context = event, ""
		bare.Manifest, bare.EventPayload = &mb, ""
		if err := d.honourable(&bare); err == nil || !strings.Contains(err.Error(), "binds no event payload") {
			t.Fatalf("a %s job whose manifest binds no payload: %v", event, err)
		}
	}
}

// a manual run's job runs act on its manual event, never on push: act is
// told workflow_dispatch, reads the payload the ship signed for from the
// bundle (-e), and runs as the actor the payload names (github.actor).
// The payload enters the guest byte for byte inside the one bundle copy,
// beside a projection that keeps the workflow's own name (github.workflow)
// and gives the job a run name of the attempt's (CI-PROJECT-1.1).
func TestManualJobRunsActOnWorkflowDispatch(t *testing.T) {
	sh := &fakeShip{job: "report"}
	box := &fakeBox{stream: `{"job":"Manual release/0v1.att/report","jobID":"report","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	d := newTestDaemon(t, box, sh, 1)
	d.checkout = manualCheckout(t)
	run, projected := runAndRead(t, d, box, manualJob())
	want := "run /usr/local/bin/act workflow_dispatch -W /work/projected/manual.yml -j report -e /work/event.json --actor ~zod -P ubuntu-latest=img "
	if !strings.HasPrefix(run, want) {
		t.Fatalf("act argv:\n%s\nwant the prefix:\n%s", run, want)
	}
	if strings.Contains(run, " push ") {
		t.Fatalf("a manual run invoked act push: %s", run)
	}
	if box.eventJSON != manualPayload {
		t.Fatalf("the bundle's event.json is not the signed payload:\n%q\nwant\n%q", box.eventJSON, manualPayload)
	}
	var bundle struct {
		Event       string `json:"event"`
		EventSHA256 string `json:"event_sha256"`
	}
	if err := json.Unmarshal([]byte(box.bundleJSON), &bundle); err != nil || bundle.Event != "workflow_dispatch" || bundle.EventSHA256 != payloadDigest(manualPayload) {
		t.Fatalf("bundle.json names the event %+v (%v): %s", bundle, err, box.bundleJSON)
	}
	if workflow, job, runName := projectedRun(t, projected); workflow != "Manual release" || job != "report" || runName != "0v1.att/report" {
		t.Fatalf("act would read github.workflow %q, github.job %q, run name %q:\n%s", workflow, job, runName, projected)
	}
	if len(sh.results) != 1 || len(sh.abandons) != 0 {
		t.Fatalf("results %v abandons %v", sh.results, sh.abandons)
	}
}

// a push runs act on its push event with the push's own context: the
// payload the ship rendered from the candidate (ref, before, after, the
// pusher) and signed for, read with -e, and the actor it names. The
// projection keeps the workflow's own name, as for a manual run.
func TestPushJobRunsActOnItsSignedPushContext(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{stream: `{"job":"fixture-chain/0v1.att/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	d := newTestDaemon(t, box, sh, 1)
	run, projected := runAndRead(t, d, box, jobAssignment)
	want := "run /usr/local/bin/act push -W /work/projected/fixture-chain.yml -j b -e /work/event.json --actor ~zod -P ubuntu-latest=img "
	if !strings.HasPrefix(run, want) {
		t.Fatalf("a push job's argv:\n%s\nwant the prefix:\n%s", run, want)
	}
	if box.eventJSON != pushPayload {
		t.Fatalf("the bundle's event.json is not the signed push payload:\n%q\nwant\n%q", box.eventJSON, pushPayload)
	}
	var bundle struct {
		Event       string `json:"event"`
		EventSHA256 string `json:"event_sha256"`
	}
	if err := json.Unmarshal([]byte(box.bundleJSON), &bundle); err != nil || bundle.Event != "push" || bundle.EventSHA256 != payloadDigest(pushPayload) {
		t.Fatalf("bundle.json names the event %+v (%v): %s", bundle, err, box.bundleJSON)
	}
	if workflow, job, runName := projectedRun(t, projected); workflow != "fixture-chain" || job != "b" || runName != "0v1.att/b" {
		t.Fatalf("act would read github.workflow %q, github.job %q, run name %q:\n%s", workflow, job, runName, projected)
	}
	if len(sh.results) != 1 || len(sh.abandons) != 0 {
		t.Fatalf("results %v abandons %v", sh.results, sh.abandons)
	}
}

// the payload must name the run the manifest signs for: its ref, and for
// a push the signed source as after, so act's github.ref and github.sha
// are the signed ones; and a sender, so github.actor is never act's
// default. A payload that does not is refused before act runs.
func TestEventPayloadMustNameTheSignedRun(t *testing.T) {
	cases := map[string]*ship.Assignment{}
	otherSource := *jobAssignment
	otherSource.EventPayload = strings.Replace(pushPayload, `"after":"0000000000000000000000000000000000000001"`, `"after":"0000000000000000000000000000000000000002"`, 1)
	cases["a push naming another source"] = &otherSource
	otherRef := *jobAssignment
	otherRef.EventPayload = strings.Replace(pushPayload, `"ref":"refs/heads/master"`, `"ref":"refs/heads/other"`, 1)
	cases["a push naming another ref"] = &otherRef
	noSender := *jobAssignment
	noSender.EventPayload = strings.Replace(pushPayload, `,"sender":{"login":"~zod"}`, ``, 1)
	cases["a push naming no sender"] = &noSender
	manualRef := manualJob()
	manualRef.EventPayload = strings.Replace(manualPayload, `"ref":"refs/heads/master"`, `"ref":"refs/heads/other"`, 1)
	cases["a manual run naming another ref"] = manualRef
	for name, a := range cases {
		if _, err := eventOf(a); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, a := range []*ship.Assignment{jobAssignment, manualJob()} {
		event, err := eventOf(a)
		if err != nil || event.Name != a.Manifest.Event || event.Payload != a.EventPayload || event.Actor != "~zod" {
			t.Fatalf("%s: %+v %v", a.Manifest.Event, event, err)
		}
	}
}

// act 0.2.89 types `inputs` from the payload it reads with -e: an
// input's value is the payload's own (`inputs.count` is 0 only when the
// payload carries the number 0; "0" stays text), except that a boolean
// input is true exactly when its value is the text "true"; an input the
// payload omits takes the declared default as text. inputsAsAct is a
// TRANSCRIPTION of that rule (pkg/runner/expression.go:500-515; the event
// decoded as pkg/runner/run_context.go:942 decodes it), not act: it pins
// why the ship writes a number as a JSON number and a boolean as its
// text. The guest run that shows act doing it is the parent's (UNRUN here).
func inputsAsAct(t *testing.T, payload string, declared []plan.DispatchInput) (inputs, eventInputs map[string]any) {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatal(err)
	}
	eventInputs, _ = event["inputs"].(map[string]any)
	inputs = map[string]any{}
	for _, in := range declared {
		value, ok := eventInputs[in.ID]
		if !ok || value == nil {
			def := ""
			if in.Default != nil {
				def = *in.Default
			}
			value = def
		}
		if in.Type == "boolean" {
			inputs[in.ID] = value == "true"
		} else {
			inputs[in.ID] = value
		}
	}
	return inputs, eventInputs
}

// toJSON is act's toJSON (pkg/exprparser/functions.go:155-166)
func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestManualPayloadTypesInputsAsActReadsThem(t *testing.T) {
	data, err := os.ReadFile("../plan/testdata/manual.yml")
	if err != nil {
		t.Fatal(err)
	}
	d, err := plan.DispatchOf(data, "manual.yml")
	if err != nil || d == nil {
		t.Fatalf("manual.yml: %+v %v", d, err)
	}
	inputs, eventInputs := inputsAsAct(t, manualPayload, d.Inputs)
	want := map[string]string{"count": `0`, "dry_run": `false`, "level": `"warning"`, "note": `"hello"`}
	for id, json := range want {
		if got := toJSON(t, inputs[id]); got != json {
			t.Errorf("toJSON(inputs.%s) = %s, want %s", id, got, json)
		}
	}
	// github.event.inputs keeps the payload as written: the boolean as
	// GitHub's text, the number as the payload's number
	if got := toJSON(t, eventInputs["dry_run"]); got != `"false"` {
		t.Errorf("toJSON(github.event.inputs.dry_run) = %s", got)
	}
	if got := toJSON(t, eventInputs["count"]); got != `0` {
		t.Errorf("toJSON(github.event.inputs.count) = %s", got)
	}
	// the counterexample the representation avoids: zero as text is text
	text, _ := inputsAsAct(t, strings.Replace(manualPayload, `"count":0`, `"count":"0"`, 1), d.Inputs)
	if got := toJSON(t, text["count"]); got != `"0"` {
		t.Fatalf("a number sent as text: toJSON(inputs.count) = %s", got)
	}
	// and a JSON boolean would never be true to act: only the text is
	booleans, _ := inputsAsAct(t, strings.Replace(manualPayload, `"dry_run":"false"`, `"dry_run":true`, 1), d.Inputs)
	if booleans["dry_run"] != false {
		t.Fatalf("a JSON true: inputs.dry_run = %v", booleans["dry_run"])
	}
}
