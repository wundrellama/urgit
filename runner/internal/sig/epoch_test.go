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
const epochJamHex = "e106fc8bad536d80a567eb7a5706f8c2e6e6d2cedc011f60f6271e8001c72ebe0300000000000000000000008001792b7625033c65727069f4003cc89599cdbda0958591cdbdb485cdd195c9035ebb8a3500fa84d990c5d4949191959985e1e4e0c8d4c08899d598d9d4d8d0d0c484d5d484e1c0c8dccc98ddd4c401f007060606060606060606060606060606060606060606060606060606060606060606060606264c6c1c00fd636262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262626262e2f00378ccd5a5d195b9e4b5b1033c73756974e5002f9d5cdd1c5d1939f076ed00bf5c595c5d9a5c1939c0c7f636b65646ce003cdcbdc9ad99b1bddd7d91a5cdc185d18da103e0bf58d80d4e8c8d98198e194ccc985959184d0c4d0c0d0c59590d5958998c8ccc8c180ccc8c4d4cd84c8e4dcccd4d58ce98184d0c8c99994d8c990c0c4c4c4d183900fa93cd8ce58495e190d18899d18cd59499d9c08485c990cd94d994e590e184d18cd588cd98c194c590c98ccd88d184d5e4d8e0dcdce0d8e4d484d188cd8cc990c501f815ab4b6323eb81b1b37181937b2363614b73a3eb81b1bb71614b73a303d0e8ee4d6ecd8cededee8b2c6d0e2e8c6e0c0d64ae2e8daecc25af8d0d44ae2caeae6c8e0e44aecead2506"

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
