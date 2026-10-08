package sig

// Legacy-replay-upgrade ruling 01 (runner/launcher/INTEGRATION.md §11.15):
// an assignment of authorization epoch E is signed with a nonce of E·2^128
// or more, above the sixteen bytes every earlier nonce is. The signed noun
// carries the nonce whole: a signature over it verifies, none verifies for
// its low bits alone, and its spelling as the ship writes it round-trips.
// The reference message of the cross-language vector with an epoch-1 nonce
// has its jam pinned as Go computes it; the Hoon generator's matching case
// (desk/gen/ci-provenance-vector.hoon) is NOT RUN here.

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"testing"
)

// epochNonceOf is low under epoch e: e above bit 128.
func epochNonceOf(e int64, low *big.Int) *big.Int {
	n := new(big.Int).Lsh(big.NewInt(e), 128)
	return n.Add(n, low)
}

func TestAnEpochNonceIsSignedWhole(t *testing.T) {
	_, priv := vectorKey()
	pub := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	low, err := ParseUV("0v3.nonce")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []int64{1, 2, 1 << 40} {
		n := epochNonceOf(e, low)
		m := referenceManifest
		m.Nonce = FormatUV(n)
		b, err := m.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		sigHex := hex.EncodeToString(ed25519.Sign(priv, b))
		if err := VerifyManifest(pub, m, sigHex); err != nil {
			t.Fatalf("AN EPOCH NONCE'S SIGNATURE DID NOT VERIFY (epoch %d): %v", e, err)
		}
		lowOnly := m
		lowOnly.Nonce = FormatUV(low)
		if VerifyManifest(pub, lowOnly, sigHex) == nil {
			t.Fatalf("A SIGNATURE OVER AN EPOCH NONCE VERIFIED FOR ITS LOW BITS (epoch %d)", e)
		}
		if back, err := ParseUV(m.Nonce); err != nil || back.Cmp(n) != 0 {
			t.Fatalf("an epoch nonce's spelling does not round-trip (epoch %d): %q %v", e, m.Nonce, err)
		}
	}
}

// epochJamHex is the signed bytes of the reference message with the
// epoch-1 nonce (1·2^128 + 0v3.nonce), as Go computes them: pinned so that
// a drift on this side fails, and the Hoon generator has its target.
const epochJamHex = "2103fec5d6a936c0d2b375bd2b037c6173736967ee800f30fb130fc0806317df010000000000000000000000c080bc15bb92019e3239b8347a001ee4cacce65ed0cac2c8e65edac2e6e8cae401af5dc51a007dc26cc8626acac8c8caccc2707270646a60c4cc6acc6c6a6c686862c26a6ac27060646e66cc6e6ae200f8030303030303030303030303030303030303030303030303030303030303030303030303031326360e80fe31313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313171f8013ce6ead2e8ca5cf2dad8019eb9ba34ba7280974eae6e8eae8c1c78bb76805fae2cae2e4dae8c1ce0637b1b5b2b230b"

func TestTheEpochVectorIsPinned(t *testing.T) {
	low, err := ParseUV("0v3.nonce")
	if err != nil {
		t.Fatal(err)
	}
	m := referenceManifest
	m.Nonce = FormatUV(epochNonceOf(1, low))
	b, err := m.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(b); got != epochJamHex {
		t.Fatalf("THE EPOCH VECTOR'S BYTES DRIFTED: %s (nonce %s)", got, m.Nonce)
	}
}
