package sig

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"testing"
)

// the reference manifest of the cross-language vector (P10): the Hoon
// generator desk/gen/ci-provenance-vector.hoon builds the same noun from
// the same fields, prints the jam's hex and the signature for the fixed
// test seed, and asserts every single-field mutation below fails; this
// test asserts the same on the Go side and pins the jam bytes so the two
// sides cannot drift apart silently.
var referenceManifest = ManifestMessage{
	Recipient: "0v1.daemon", Attempt: "0v2.attempt", Operation: "assign", Expiry: 1_900_000_000, Nonce: "0v3.nonce",
	Manifest: Manifest{
		Incarnation: "0v4.incar", Repo: "erpit", Ref: "refs/heads/master", Candidate: "0v5.cand",
		OID: "a6d15eddefa898250bf5f656441a55a80273f751", Baseline: "0000000000000000000000000000000000000abc", Lock: "1111111111111111111111111111111111111111111111111111111111111111",
		Generation: 7, Workflow: "suite.yml", Job: "suite", Trust: "trusted", Sandbox: "vm", Mode: "required", Network: "locked", NetworkScope: "",
	},
}

// the vector's fixed seed: 32 bytes of 0x01..0x20 (the Hoon side derives
// the same pair with luck:ed from this seed as an atom)
func vectorKey() (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

// referenceJamHex is the jam of the reference message as BOTH sides
// produce it: printed by this test, and by desk/gen/ci-provenance-vector
// on ~tyv on 2026-09-21 (its `%jam-hex` line, byte-identical; its
// signature for the same seed was identical too). A drift on either side
// fails one of the two tests.
const referenceJamHex = "2103fec5d6a936c0d2b375bd2b037c6173736967ee800f30fb130fb88e5d7c0fc85bb12b19e02993834ba307e041aecc6cee05ad2c8c6ceea52d6c8eae4c1ef0da55ac01d027cc862ca6a68c8caccc2c0c270747a60646ccacc6cca6c686862626aca6260c0746e666c6eca6260e803f303030303030303030303030303030303030303030303030303030303030303030303030306162e300e81f131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313871fc063ae2e8daecc25af8d1de099ab4ba32b0778e9e4eae6e8cac881b76b07f8e5cae2ead2e4cac8013eb6b7b1b532b2"

func TestManifestSignatureRoundTripAndMutations(t *testing.T) {
	pub, priv := vectorKey()
	pubHex := hex.EncodeToString(pub)
	msg := referenceManifest
	bytes, err := msg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	sig := hex.EncodeToString(ed25519.Sign(priv, bytes))
	if err := VerifyManifest(pubHex, msg, sig); err != nil {
		t.Fatalf("the reference manifest must verify: %v", err)
	}
	t.Logf("reference jam hex: %s", hex.EncodeToString(bytes))
	t.Logf("reference sig hex: %s", sig)
	if hex.EncodeToString(bytes) != referenceJamHex {
		t.Fatalf("the jam bytes drifted from the pinned cross-language reference")
	}
	mutations := map[string]func(m *ManifestMessage){
		"recipient":   func(m *ManifestMessage) { m.Recipient = "0v9" },
		"attempt":     func(m *ManifestMessage) { m.Attempt = "0v9" },
		"operation":   func(m *ManifestMessage) { m.Operation = "grant:X" },
		"expiry":      func(m *ManifestMessage) { m.Expiry++ },
		"nonce":       func(m *ManifestMessage) { m.Nonce = "0v9" },
		"incarnation": func(m *ManifestMessage) { m.Manifest.Incarnation = "0v9" },
		"repo":        func(m *ManifestMessage) { m.Manifest.Repo = "other" },
		"ref":         func(m *ManifestMessage) { m.Manifest.Ref = "refs/heads/dev" },
		"candidate":   func(m *ManifestMessage) { m.Manifest.Candidate = "0v9" },
		"oid":         func(m *ManifestMessage) { m.Manifest.OID = "a6d15eddefa898250bf5f656441a55a80273f752" },
		"baseline":    func(m *ManifestMessage) { m.Manifest.Baseline = "" },
		"lock": func(m *ManifestMessage) {
			m.Manifest.Lock = "2222222222222222222222222222222222222222222222222222222222222222"
		},
		"generation":    func(m *ManifestMessage) { m.Manifest.Generation = 8 },
		"workflow":      func(m *ManifestMessage) { m.Manifest.Workflow = "fixtures.yml" },
		"job":           func(m *ManifestMessage) { m.Manifest.Job = "plan" },
		"trust":         func(m *ManifestMessage) { m.Manifest.Trust = "untrusted" },
		"sandbox":       func(m *ManifestMessage) { m.Manifest.Sandbox = "container" },
		"mode":          func(m *ManifestMessage) { m.Manifest.Mode = "trial" },
		"network":       func(m *ManifestMessage) { m.Manifest.Network = "integration" },
		"network-scope": func(m *ManifestMessage) { m.Manifest.NetworkScope = "tcp:198.51.100.20:8472" },
	}
	for name, mutate := range mutations {
		m := referenceManifest
		mutate(&m)
		if err := VerifyManifest(pubHex, m, sig); err == nil {
			t.Fatalf("mutation %s verified with the original signature", name)
		}
	}
	// a v1 message over the same fields never verifies a v2 signature
	v1 := Message{Recipient: msg.Recipient, Attempt: msg.Attempt, Operation: msg.Operation, Expiry: msg.Expiry, Nonce: msg.Nonce}
	if err := Verify(pubHex, v1, sig); err == nil {
		t.Fatal("a v1 verifier accepted a v2 signature")
	}
}

// the version atom is the first cell head, so a verifier of another
// version sees a different noun and refuses
func TestManifestNounStartsWithVersion(t *testing.T) {
	n, err := referenceManifest.Noun()
	if err != nil {
		t.Fatal(err)
	}
	if n.Head == nil || n.Head.Atom == nil || n.Head.Atom.Cmp(big.NewInt(ManifestVersion)) != 0 {
		t.Fatalf("noun head is not the version: %+v", n.Head)
	}
}

func TestScopeTextIsCanonical(t *testing.T) {
	if ScopeText([]string{"tcp:2.2.2.2:2", "tcp:1.1.1.1:1"}) != "tcp:1.1.1.1:1,tcp:2.2.2.2:2" || ScopeText(nil) != "" {
		t.Fatal("scope text is not canonical")
	}
}
