package signing

import (
	"crypto/ed25519"
	"errors"
)

func PublicKey(text string) (ed25519.PublicKey, error) {
	a, e := Parse(text, "0x", ed25519.PublicKeySize)
	if e != nil {
		return nil, e
	}
	if a.Sign() == 0 {
		return nil, errors.New("empty CI public key")
	}
	b, e := LE(a, ed25519.PublicKeySize)
	return ed25519.PublicKey(b), e
}
func Verify(pub ed25519.PublicKey, sig string, message []byte) error {
	a, e := Parse(sig, "0x", ed25519.SignatureSize)
	if e != nil {
		return errors.New("missing or malformed signature")
	}
	b, e := LE(a, ed25519.SignatureSize)
	if e != nil {
		return e
	}
	if !ed25519.Verify(pub, message, b) {
		return errors.New("signature does not verify with pinned CI public key")
	}
	return nil
}
