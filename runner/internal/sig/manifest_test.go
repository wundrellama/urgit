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
		Event: "workflow_dispatch", Context: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		Needs: "d3c9ae8d4bf4c5ef60aa2d3e6e9d8a4c5b3f0e1d2c3b4a5968778695a4b3c2d1", Prerequisites: "build=0v6.prod,lint=0v7.lint",
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

// referenceJamHex is the jam of the reference message (manifest version
// 4, its event and its needs context bound) as BOTH sides produce it: printed by this test, and
// by desk/gen/ci-provenance-vector on a fresh fake ship (its `%jam-hex`
// line must be byte-identical, and its signature for the same seed
// identical too). A drift on either side fails one of the two tests.
const referenceJamHex = "6106fc8bad536d80a567eb7a5706f8c2e6e6d2cedc011f60f6271e701dbbf81e90b7625732c053260797460fc0835c99d9dc0b5a5918d9dc4b5bd81c5d993ce0b5ab5803a04f980d594c4d1919599959184e0e8e4c0d8c98598d994d8d0d0d4d4c584d4d180e8ccccd8cd94d4d1c007f60606060606060606060606060606060606060606060606060606060606060606060606060c2c4c601d03f2626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626260e3f80c75c5d1a5d994b5e1b3bc033579746570ef0d2c9d5cdd19591036fd70ef0cb95c5d5a5c99591037c6c6f636b65e40cc0c3dd9bdc9a19dbdbdd1759da1c5c18dd183a00fe8b85dde0c4d88899e198c1c48c999585d1c4d0c4d0c09095d5908595c9c8c8cc88c1c0ccd8c484cde4d8c4dcdc84e58c89d1c4c09899d9c498c9c0c0c4d4849103a03fd9cc584e58190e198d9819cd584d99990d4c58980cd94c994d590e194e18cd588dd88c194c590c99ccd88c184d584d8e0dcecd0d8e4d4e4d188dd8cc980c590ce057ac2e8d8dac07c6cec6054eee8d8c852dcd8dae07c6eec6852dcd8d0e"

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
		"event":         func(m *ManifestMessage) { m.Manifest.Event = "push" },
		"context": func(m *ManifestMessage) {
			m.Manifest.Context = "ca7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
		},
		"needs": func(m *ManifestMessage) {
			m.Manifest.Needs = "e3c9ae8d4bf4c5ef60aa2d3e6e9d8a4c5b3f0e1d2c3b4a5968778695a4b3c2d1"
		},
		"needs-dropped":         func(m *ManifestMessage) { m.Manifest.Needs = "" },
		"prerequisites":         func(m *ManifestMessage) { m.Manifest.Prerequisites = "build=0v8.stale,lint=0v7.lint" },
		"prerequisites-dropped": func(m *ManifestMessage) { m.Manifest.Prerequisites = "" },
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
