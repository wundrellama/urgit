package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
)

// Delivery 1B correction 01 (R1): the consumer assignment of the parent's
// real run, through the runner's own decode path (ship.Client.Poll over the
// HTTP bytes) into verifyAssignment, with nonempty dependency bindings.
//
// Evidence level: UNIT. The bytes are a MODEL of the controller's emission,
// keyed exactly as desk/app/urgit-ci.hoon assignment-json and
// desk/lib/ci-provenance.hoon manifest-json emit them; the values are the
// parent's captured run (parent-review-01/SHADOW-FAILURE-DETAIL.json: the
// ids, the push payload and its digest, the producer's seven outputs). The
// original rejected reply was not retained, so the signature here is made
// with a test key over the manifest the ship signs.

const (
	wireDaemon    = "0v2.mp2v9.f4m52.fqnh1.il3ur.q1bc6"
	wireAttempt   = "0v2.nq828.o1jkt.cceve.b87pm.1a3iq"
	wireCandidate = "0v3.i0b8r.6gfu1.dnib7.flql9.26aqh"
	wireProducer  = "0v1.v0vt1.7tat1.h1ae4.k5u8g.4devu"
	wireOID       = "ef0fd0db33e4e3eb3cd1e0bc2a0762b715f91094"
	// the captured push payload; its sha-256 is the captured payloadSha256
	wirePayload       = `{"after":"ef0fd0db33e4e3eb3cd1e0bc2a0762b715f91094","ref":"refs/ci/resolve","before":"ef0fd0db33e4e3eb3cd1e0bc2a0762b715f91094","sender":{"login":"~nec"},"deleted":false,"pusher":{"name":"~nec"}}`
	wirePayloadSHA256 = "039d9f21682c8d3105e610a48740bf70d7cf8b3dc4ee4d09a39831c5a9bc41fa"
	// the consumer's needs context: the producer's result and its seven
	// declared outputs as the run published them
	wireNeeds = `{"producer":{"outputs":{"verdict":"go","first":"from-one","second":"from-two","empty":"","quoted":"it's \"quoted\"\nsecond line","json":"{\"n\":0,\"ok\":false,\"list\":[\"a\",\"b\"]}","composed":"release-from-one"},"result":"success"}}`
)

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// wireManifest is the consumer's (or, with no needs, the producer's)
// manifest as the controller signs it: manifest-of in urgit-ci.hoon
func wireManifest(job, needs, prerequisites string) sig.Manifest {
	return sig.Manifest{
		Incarnation: "0v3.ddiv1.0l548.cnt0h.khaov.2bfba", Repo: "wf1b-outputs", Ref: "refs/ci/resolve", Candidate: wireCandidate,
		OID: wireOID, Baseline: "", Lock: "", Generation: 3, Workflow: "outputs.yml", Job: job, Trust: "trusted",
		Sandbox: "vm", Mode: "shadow", Network: "locked", NetworkScope: "", Event: "push", Context: wirePayloadSHA256,
		Needs: needs, Prerequisites: prerequisites,
	}
}

// wireBody is the poll's answer as the controller emits it: the assignment
// object of assignment-json, its manifest object of manifest-json (all
// nineteen signed fields), signed over m
func wireBody(t *testing.T, priv ed25519.PrivateKey, m sig.Manifest, needsContext string) map[string]any {
	t.Helper()
	expiry := time.Now().Unix() + 300
	msg := sig.ManifestMessage{Recipient: wireDaemon, Attempt: wireAttempt, Operation: "assign", Expiry: expiry, Nonce: "0v1.nonce", Manifest: m}
	b, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"incarnation": m.Incarnation, "repo": m.Repo, "ref": m.Ref, "candidate": m.Candidate, "oid": m.OID,
		"baseline": m.Baseline, "lock": m.Lock, "generation": m.Generation, "workflow": m.Workflow, "job": m.Job,
		"trust": m.Trust, "sandbox": m.Sandbox, "mode": m.Mode, "network": m.Network, "network-scope": m.NetworkScope,
		"event": m.Event, "context": m.Context, "needs": m.Needs, "prerequisites": m.Prerequisites,
	}
	return map[string]any{"assignment": map[string]any{
		"id": "0v5.m20o5.fecli.ofouk.d3pvk.9091h", "attempt": wireAttempt, "candidate": wireCandidate,
		"repo": m.Repo, "ref": m.Ref, "oid": m.OID, "head": wireOID, "base": wireOID, "trust": "trusted",
		"scratch-ref": "refs/ci/candidate/x", "kind": "job", "workflow": m.Workflow, "job": m.Job,
		"deadline-seconds": 3720, "assigned": "~2026.10.16..04.41.09", "grants": []any{},
		"sig": map[string]any{
			"version": sig.ManifestVersion, "recipient": wireDaemon, "attempt": wireAttempt, "operation": "assign",
			"expiry": expiry, "nonce": "0v1.nonce", "sig": hex.EncodeToString(ed25519.Sign(priv, b)),
		},
		"manifest": manifest, "harness-paths": []any{}, "workflows-oid": wireOID, "baseline-ref": nil,
		"lock": nil, "downloads": []any{}, "read-token": "0v1.token", "event-payload": wirePayload,
		"needs-context": needsContext, "mappings": []any{},
	}}
}

// pollWire answers one poll with body and returns the assignment as the
// runner's client decodes it
func pollWire(t *testing.T, body map[string]any) *ship.Assignment {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	client := ship.New("http://ship.test", "0v1.bearer")
	client.HTTP = &http.Client{Transport: inproc{"ship.test": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/urgit/api/ci/daemon/"+wireDaemon+"/assignment" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(data)
	})}}
	a, err := client.Poll(context.Background(), wireDaemon)
	if err != nil || a == nil {
		t.Fatalf("poll: %v %v", a, err)
	}
	return a
}

func wireDaemonUnder(pub ed25519.PublicKey) *Daemon {
	return &Daemon{daemonID: wireDaemon, ciKey: hex.EncodeToString(pub), log: log.New(io.Discard, "", 0)}
}

func manifestOf(body map[string]any) map[string]any {
	return body["assignment"].(map[string]any)["manifest"].(map[string]any)
}

func TestConsumerAssignmentWireVerifies(t *testing.T) {
	if digest(wirePayload) != wirePayloadSHA256 {
		t.Fatal("the captured payload does not hash to the captured digest")
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := wireManifest("consumer", digest(wireNeeds), "producer="+wireProducer)
	a := pollWire(t, wireBody(t, priv, m, wireNeeds))
	if err := wireDaemonUnder(pub).verifyAssignment(a); err != nil {
		t.Fatalf("the consumer's complete assignment must verify: %v", err)
	}
	if a.Manifest.Needs != digest(wireNeeds) || a.Manifest.Prerequisites != "producer="+wireProducer || a.NeedsContext != wireNeeds {
		t.Fatalf("the dependency bindings did not cross the wire: %+v / %q", a.Manifest, a.NeedsContext)
	}
}

// the 1B controller's emission: the manifest object without needs and
// prerequisites. Decoded, the manifest's dependency bindings are empty and
// the signature over the real ones refuses it (the parent's `signature does
// not verify`); the runner now names the omission before that
func TestConsumerAssignmentWireWithoutDependencyFieldsIsRefusedByName(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := wireManifest("consumer", digest(wireNeeds), "producer="+wireProducer)
	body := wireBody(t, priv, m, wireNeeds)
	delete(manifestOf(body), "needs")
	delete(manifestOf(body), "prerequisites")
	a := pollWire(t, body)
	err := wireDaemonUnder(pub).verifyAssignment(a)
	if err == nil || !strings.Contains(err.Error(), "the signed manifest omits needs, prerequisites") {
		t.Fatalf("the 1B emission must be refused naming the omitted fields: %v", err)
	}
	if a.Manifest.Needs != "" || a.Manifest.Prerequisites != "" {
		t.Fatalf("omitted fields decode empty: %+v", a.Manifest)
	}
	msg := sig.ManifestMessage{Recipient: a.Sig.Recipient, Attempt: a.Sig.Attempt, Operation: a.Sig.Operation, Expiry: a.Sig.Expiry, Nonce: a.Sig.Nonce, Manifest: *a.Manifest}
	if err := sig.VerifyManifest(hex.EncodeToString(pub), msg, a.Sig.Sig); err == nil || !strings.Contains(err.Error(), "signature does not verify") {
		t.Fatalf("the decoded 1B emission must fail the signature, as the real runner did: %v", err)
	}
}

// every other way the dependency binding can differ from what the ship
// signed is refused: a changed value, a stale producer attempt, an emptied
// binding, a context the manifest binds another digest for
func TestConsumerAssignmentWireTamperIsRefused(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := wireManifest("consumer", digest(wireNeeds), "producer="+wireProducer)
	cases := map[string]func(body map[string]any){
		"needs context changed": func(body map[string]any) {
			body["assignment"].(map[string]any)["needs-context"] = strings.Replace(wireNeeds, `"verdict":"go"`, `"verdict":"stop"`, 1)
		},
		"stale producer attempt": func(body map[string]any) {
			manifestOf(body)["prerequisites"] = "producer=0v1.stale"
		},
		"needs binding emptied": func(body map[string]any) { manifestOf(body)["needs"] = "" },
		"needs digest changed": func(body map[string]any) {
			manifestOf(body)["needs"] = digest(wireNeeds + " ")
		},
		"needs context dropped": func(body map[string]any) { body["assignment"].(map[string]any)["needs-context"] = "" },
	}
	for name, mutate := range cases {
		body := wireBody(t, priv, m, wireNeeds)
		mutate(body)
		if err := wireDaemonUnder(pub).verifyAssignment(pollWire(t, body)); err == nil {
			t.Fatalf("%s verified", name)
		}
	}
}

// a job with no needs carries both fields, empty, and verifies; the
// runner refuses an emission without them whatever their values would be
func TestNoNeedsAssignmentWireCarriesTheFieldsToo(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := wireManifest("producer", "", "")
	body := wireBody(t, priv, m, "")
	if err := wireDaemonUnder(pub).verifyAssignment(pollWire(t, body)); err != nil {
		t.Fatalf("a no-needs assignment with every field must verify: %v", err)
	}
	delete(manifestOf(body), "needs")
	delete(manifestOf(body), "prerequisites")
	if err := wireDaemonUnder(pub).verifyAssignment(pollWire(t, body)); err == nil || !strings.Contains(err.Error(), "omits needs, prerequisites") {
		t.Fatalf("an emission without the fields is refused even when they would be empty: %v", err)
	}
}

// the bundle's own record (bundle.json, copied into the guest) names the
// dependency binding beside the event's, as the verified manifest carries
// it: the needs context's digest and the producer attempts; a job without
// needs names none
func TestBundleRecordNamesTheDependencyBinding(t *testing.T) {
	read := func(a *ship.Assignment) map[string]any {
		box := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
		d := newTestDaemon(t, box, &fakeShip{}, 1)
		d.handle(context.Background(), a)
		var record map[string]any
		if err := json.Unmarshal([]byte(box.bundleJSON), &record); err != nil {
			t.Fatalf("bundle.json: %v: %q", err, box.bundleJSON)
		}
		return record
	}
	record := read(jobAssignment)
	if record["needs_sha256"] != payloadDigest(chainNeeds) || record["prerequisites"] != "a=0v1.prod" {
		t.Fatalf("bundle.json does not name the dependency binding: %v", record)
	}
	first := *jobAssignment
	m := jobManifest
	m.Job, m.Needs, m.Prerequisites = "a", "", ""
	first.Job, first.Manifest, first.NeedsContext = "a", &m, ""
	none := read(&first)
	if none["event"] != "push" {
		t.Fatalf("job a's bundle was not assembled: %v", none)
	}
	if _, has := none["needs_sha256"]; has {
		t.Fatalf("a job without needs names a needs binding: %v", none)
	}
	if _, has := none["prerequisites"]; has {
		t.Fatalf("a job without needs names prerequisites: %v", none)
	}
}
