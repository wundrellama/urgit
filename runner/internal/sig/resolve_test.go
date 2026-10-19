package sig

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"testing"
)

func TestAutomaticResolveHasItsOwnSignedDomain(t *testing.T) {
	m := Manifest{Incarnation: "0v1", Candidate: "0v2", Mode: "resolve-tag"}
	msg := ManifestMessage{Recipient: "0v3", Attempt: "0v4", Operation: "assign", Expiry: 1, Nonce: "0v5", Manifest: m}
	noun, err := msg.Noun()
	if err != nil {
		t.Fatal(err)
	}
	// Compare against the actual canonical tuple, not only a version field.
	mn, err := m.Noun()
	if err != nil {
		t.Fatal(err)
	}
	expected := Tuple(Atom(big.NewInt(6)), Atom(big.NewInt(3)), Atom(big.NewInt(4)), Cord("assign"), Atom(big.NewInt(1)), Atom(big.NewInt(5)), mn)
	if Jam(noun).Cmp(Jam(expected)) != 0 {
		t.Fatal("automatic resolver is not signed in domain 6")
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	data, _ := msg.Bytes()
	signature := hex.EncodeToString(ed25519.Sign(priv, data))
	msg.Manifest.Mode = "resolve"
	if ManifestVersionFor(msg.Manifest) != 5 {
		t.Fatal("explicit import changed domain")
	}
	if err := VerifyManifest(hex.EncodeToString(pub), msg, signature); err == nil {
		t.Fatal("automatic signature authorizes explicit import")
	}
}
