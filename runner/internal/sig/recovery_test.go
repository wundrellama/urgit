package sig

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

// the reference recovery command of the cross-language vector (legacy-
// recovery UI ruling 01): desk/gen/ci-recovery-vector.hoon builds the same
// noun from the same fields, prints the jam's hex and the signature for the
// fixed test seed (vectorKey), and asserts every single-field mutation
// fails; this test asserts the same on the Go side and pins the jam bytes.
var referenceRecovery = RecoveryMessage{
	Recipient: "0v1.daemon", Command: "0v2.command", Operation: "release-legacy", Expiry: 1_900_000_000, Nonce: "0v3.nonce",
	Selection: "ci-0v4.att/microvm///0/0//1726000000/1", Revision: 1, Evidence: strings.Repeat("ab", 32),
}

// referenceRecoveryJamHex is the jam of the reference message as this test
// prints it. The Hoon generator's run, which must print the same bytes, is
// NOT RUN here (it needs a ship): a drift on either side fails one of the
// two.
const referenceRecoveryJamHex = "017eb9b2b137bb32b97c1cf02fb64eb501967695b59819c057ae8cad2c6caeac85adec2c6c2c1ff001667fe201d7b18bef01d0652cad05c68ec6258c8eeea52d6d4ceecdaeede5e505e605e6e525e646c6060606060606e6258e01f81f26162616261626162616261626162616261626162616261626162616261626162616261626162616261626162616261626162616261626162616261626162606"

// referenceRecoverySigHex is the fixed seed's signature over it, which the
// Hoon generator must reproduce (NOT RUN here).
const referenceRecoverySigHex = "887ccc2dbb85ef586307e68b640587b4677e7aadda26d9e34c5f3910bbd977cc15956132cf7a9ee7fc8b212781890337d64f87c9ac5f5625d4301928c8cc5c03"

func TestRecoverySignatureRoundTripMutationsAndSeparation(t *testing.T) {
	pub, priv := vectorKey()
	pubHex := hex.EncodeToString(pub)
	bytes, err := referenceRecovery.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	signature := hex.EncodeToString(ed25519.Sign(priv, bytes))
	t.Logf("reference recovery jam hex: %s", hex.EncodeToString(bytes))
	t.Logf("reference recovery sig hex: %s", signature)
	if err := VerifyRecovery(pubHex, referenceRecovery, signature); err != nil {
		t.Fatalf("the reference recovery command must verify: %v", err)
	}
	if hex.EncodeToString(bytes) != referenceRecoveryJamHex || signature != referenceRecoverySigHex {
		t.Fatalf("THE RECOVERY JAM BYTES DRIFTED from the pinned reference")
	}
	mutations := map[string]func(m *RecoveryMessage){
		"recipient": func(m *RecoveryMessage) { m.Recipient = "0v9" },
		"command":   func(m *RecoveryMessage) { m.Command = "0v9" },
		"operation": func(m *RecoveryMessage) { m.Operation = "release" },
		"expiry":    func(m *RecoveryMessage) { m.Expiry++ },
		"nonce":     func(m *RecoveryMessage) { m.Nonce = "0v9" },
		"selection": func(m *RecoveryMessage) { m.Selection = "ci-0v4.att/microvm///0/0//1726000000/2" },
		"revision":  func(m *RecoveryMessage) { m.Revision = 2 },
		"evidence":  func(m *RecoveryMessage) { m.Evidence = strings.Repeat("cd", 32) },
	}
	for name, mutate := range mutations {
		m := referenceRecovery
		mutate(&m)
		if err := VerifyRecovery(pubHex, m, signature); err == nil {
			t.Fatalf("A RECOVERY SIGNATURE VERIFIED WITH ITS %s CHANGED", strings.ToUpper(name))
		}
	}
	// domain separation: the same fields as an assignment's message verify
	// under neither signature, in either direction
	assign := ManifestMessage{Recipient: referenceRecovery.Recipient, Attempt: referenceRecovery.Command, Operation: referenceRecovery.Operation,
		Expiry: referenceRecovery.Expiry, Nonce: referenceRecovery.Nonce, Manifest: referenceManifest.Manifest}
	if err := VerifyManifest(pubHex, assign, signature); err == nil {
		t.Fatalf("A RECOVERY SIGNATURE VERIFIED AS AN ASSIGNMENT'S")
	}
	assignBytes, err := assign.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecovery(pubHex, referenceRecovery, hex.EncodeToString(ed25519.Sign(priv, assignBytes))); err == nil {
		t.Fatalf("AN ASSIGNMENT'S SIGNATURE VERIFIED AS A RECOVERY COMMAND'S")
	}
	// the tag leads the noun: it is what keeps the two apart
	n, err := referenceRecovery.Noun()
	if err != nil {
		t.Fatal(err)
	}
	if n.Head == nil || n.Head.Atom == nil || n.Head.Atom.Cmp(Cord(RecoveryTag).Atom) != 0 {
		t.Fatalf("the recovery noun does not lead with its tag")
	}
	if _, err := (RecoveryMessage{Recipient: "not-uv", Command: "0v1", Nonce: "0v1"}).Noun(); err == nil {
		t.Fatalf("a recipient that is not a @uv was taken")
	}
	if _, err := (RecoveryMessage{Recipient: "0v1", Command: "0v1", Nonce: "0v1", Expiry: -1}).Noun(); err == nil {
		t.Fatalf("an expiry before the epoch was taken")
	}
}
