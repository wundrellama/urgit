package daemon

// Native acceptance entry: the input is a private capture of the real Gall
// assignment endpoint, not a JSON envelope authored by this test.
import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"testing"

	"urgit/runner/internal/inputpolicy"
	"urgit/runner/internal/ship"
)

var nativeCapture = flag.String("native-assignment-capture", "", "private native assignment metadata file")

func TestNativeAssignmentCapture(t *testing.T) {
	if *nativeCapture == "" {
		t.Skip("requires a fresh native controller capture")
	}
	raw, err := os.ReadFile(*nativeCapture)
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Envelope         string   `json:"envelope"`
		Daemon           string   `json:"daemon"`
		PublicKey        string   `json:"public_key"`
		Mode             string   `json:"mode"`
		OID              string   `json:"oid"`
		Version          int      `json:"version"`
		AutomaticSources []string `json:"automatic_sources"`
		PolicyID         string   `json:"policy_id"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(meta.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Assignment *ship.Assignment `json:"assignment"`
	}
	if err := json.Unmarshal(wire, &env); err != nil {
		t.Fatal(err)
	}
	a := env.Assignment
	if a == nil || a.Manifest == nil || a.Sig == nil {
		t.Fatal("missing native assignment")
	}
	if a.Sig.Version != meta.Version || a.Manifest.Mode != meta.Mode || a.Manifest.OID != meta.OID || a.OID != meta.OID {
		t.Fatal("native assignment identity mismatch")
	}
	d := &Daemon{daemonID: meta.Daemon, ciKey: meta.PublicKey, log: log.New(io.Discard, "", 0)}
	if err := d.verifyAssignment(a); err != nil {
		t.Fatalf("actual Hoon assignment refused: %v (mode=%s, workflows-oid=%q)", err, meta.Mode, a.WorkflowsOID)
	}
	if meta.Mode == "resolve-tag" {
		if meta.AutomaticSources == nil || meta.PolicyID == "" {
			t.Fatal("native automatic capture metadata must declare automatic_sources and policy_id")
		}
		want, err := inputpolicy.Scope(meta.AutomaticSources)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.AutomaticInputs.Scope()
		if err != nil || got != want || got != a.Manifest.NetworkScope || meta.PolicyID != inputpolicy.Identity(want) {
			t.Fatalf("native policy/source serializer mismatch: %q %q %v", got, want, err)
		}
		for name, policy := range map[string]*inputpolicy.Policy{"missing": nil, "version": {Version: 0, Sources: []string{}}, "null-sources": {Version: 1}, "changed": {Version: 1, Sources: []string{"https://tampered.example/input"}}} {
			altered := *a
			altered.AutomaticInputs = policy
			if err := d.verifyAssignment(&altered); err == nil {
				t.Fatalf("%s native policy accepted", name)
			}
		}

		if a.WorkflowsOID != meta.OID {
			t.Fatal("captured workflow revision differs")
		}
		for name, value := range map[string]string{"missing": "", "different": "ffffffffffffffffffffffffffffffffffffffff"} {
			b := *a
			b.WorkflowsOID = value
			if err := d.verifyAssignment(&b); err == nil {
				t.Fatalf("%s workflow revision accepted", name)
			}
		}
		b := *a
		sg := *a.Sig
		sg.Version = 5
		b.Sig = &sg
		if err := d.verifyAssignment(&b); err == nil {
			t.Fatal("domain downgrade accepted")
		}
	}
	t.Logf("native Hoon producer -> actual Go decoder/verifier PASS, domain %d, mode %s", meta.Version, meta.Mode)
}
