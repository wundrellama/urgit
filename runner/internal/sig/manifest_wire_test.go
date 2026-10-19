package sig

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Delivery 1B correction 01 (R1). The signed manifest crosses the wire as
// the assignment's `manifest` object, which the runner decodes into
// Manifest. The 1B controller emitted that object without `needs` and
// `prerequisites`, which its v4 signature binds: the runner decoded both as
// empty and refused every assignment with dependencies (`signature does not
// verify`), while no-needs work happened to verify. The in-memory golden
// (manifest_test.go) never crossed that boundary; these tests do.

// the JSON names of the signed fields, in the noun's order: the names the
// controller must emit (desk/lib/ci-provenance.hoon manifest-json)
var signedFields = []string{
	"incarnation", "repo", "ref", "candidate", "oid", "baseline", "lock", "generation", "workflow", "job",
	"trust", "sandbox", "mode", "network", "network-scope", "event", "context", "needs", "prerequisites", "trigger",
}

func TestManifestFieldsAreTheSignedFields(t *testing.T) {
	if got := ManifestFields(); !reflect.DeepEqual(got, signedFields) {
		t.Fatalf("ManifestFields() = %v, want %v", got, signedFields)
	}
}

// the reference manifest as the controller's serializer renders it
// (testdata/reference-manifest-v5.json: the ship's @uv spellings, the
// generation a JSON number) decodes into the manifest the reference
// signature binds, with its nonempty needs and prerequisites; the same
// object without those two keys — the 1B emission — decodes to another
// manifest, and the signature refuses it, as the runner did
func TestReferenceManifestJSONVerifies(t *testing.T) {
	data, err := os.ReadFile("testdata/reference-manifest-v5.json")
	if err != nil {
		t.Fatal(err)
	}
	pub, priv := vectorKey()
	want, err := referenceManifest.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	signature := hex.EncodeToString(ed25519.Sign(priv, want))
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	for _, f := range signedFields {
		if _, ok := keys[f]; !ok {
			t.Fatalf("the pinned emission omits %s", f)
		}
	}
	var decoded Manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Needs == "" || decoded.Prerequisites != "build=0v6.prod,lint=0v7.lint" {
		t.Fatalf("the dependency bindings did not decode: %+v", decoded)
	}
	msg := referenceManifest
	msg.Manifest = decoded
	if err := VerifyManifest(hex.EncodeToString(pub), msg, signature); err != nil {
		t.Fatalf("the reference signature must verify over the decoded emission: %v", err)
	}
	delete(keys, "needs")
	delete(keys, "prerequisites")
	short, _ := json.Marshal(keys)
	var omitted Manifest
	if err := json.Unmarshal(short, &omitted); err != nil {
		t.Fatal(err)
	}
	msg.Manifest = omitted
	if err := VerifyManifest(hex.EncodeToString(pub), msg, signature); err == nil || !strings.Contains(err.Error(), "signature does not verify") {
		t.Fatalf("the 1B emission (no needs, no prerequisites) must fail as the runner saw it: %v", err)
	}
}

// STATIC, on the source: the controller's one manifest serializer emits
// every signed field under its decoded name, and the assignment uses it.
// This is the check that would have caught the 1B omission without a ship;
// desk/gen/ci-provenance-vector.hoon checks the serializer's values natively.
func TestTheControllerSerializerEmitsEverySignedField(t *testing.T) {
	lib, err := os.ReadFile("../../../desk/lib/ci-provenance.hoon")
	if err != nil {
		t.Fatal(err)
	}
	arm := regexp.MustCompile(`(?s)\n\+\+  manifest-json\n(.*?)\n::`).FindSubmatch(lib)
	if arm == nil {
		t.Fatal("desk/lib/ci-provenance.hoon has no ++  manifest-json arm")
	}
	var emitted []string
	for _, m := range regexp.MustCompile(`\['([a-z-]+)' `).FindAllSubmatch(arm[1], -1) {
		emitted = append(emitted, string(m[1]))
	}
	if !reflect.DeepEqual(emitted, signedFields) {
		t.Fatalf("manifest-json emits %v, the signature binds %v", emitted, signedFields)
	}
	app, err := os.ReadFile("../../../desk/app/urgit-ci.hoon")
	if err != nil {
		t.Fatal(err)
	}
	body := regexp.MustCompile(`(?s)\n\+\+  assignment-json\n(.*?)\n\+\+  `).FindSubmatch(app)
	if body == nil {
		t.Fatal("desk/app/urgit-ci.hoon has no ++  assignment-json arm")
	}
	if !strings.Contains(string(body[1]), "['manifest' (manifest-json:ci-provenance manifest)]") {
		t.Fatal("assignment-json does not emit its manifest through manifest-json:ci-provenance")
	}
	if strings.Contains(string(app), "s+context.manifest") {
		t.Fatal("the controller still renders manifest fields inline beside the shared serializer")
	}
}
