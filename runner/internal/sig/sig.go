// Package sig verifies what the ship signs with its CI key (D5): every
// assignment and every credential grant carries an ed25519 signature over
// the jam of [recipient attempt operation expiry nonce]. The daemon holds
// the public key alone, pinned at enrollment; it rebuilds the noun from
// the fields the ship sent, jams it exactly as hoon.hoon does, and checks
// the signature with crypto/ed25519. Nothing here decides; a bad
// signature is a refusal the caller logs.
package sig

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Noun is an atom (Atom != nil) or a cell.
type Noun struct {
	Atom       *big.Int
	Head, Tail *Noun
}

func Atom(v *big.Int) *Noun { return &Noun{Atom: v} }
func Cell(h, t *Noun) *Noun { return &Noun{Head: h, Tail: t} }

// Cord is the atom whose little-endian bytes are the text (a @t).
func Cord(s string) *Noun {
	b := []byte(s)
	le := make([]byte, len(b))
	for i := range b {
		le[len(b)-1-i] = b[i]
	}
	return Atom(new(big.Int).SetBytes(le))
}

// Tuple is [a b c ...] = [a [b [c ...]]].
func Tuple(items ...*Noun) *Noun {
	n := items[len(items)-1]
	for i := len(items) - 2; i >= 0; i-- {
		n = Cell(items[i], n)
	}
	return n
}

// ParseUV reads a @uv literal (0v followed by base-32 digits 0-9a-v with
// dot separators) into an atom.
func ParseUV(text string) (*big.Int, error) {
	if !strings.HasPrefix(text, "0v") {
		return nil, fmt.Errorf("not a @uv: %q", text)
	}
	digits := strings.ReplaceAll(text[2:], ".", "")
	if digits == "" {
		return nil, fmt.Errorf("not a @uv: %q", text)
	}
	v := new(big.Int)
	for _, c := range digits {
		var d int64
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case c >= 'a' && c <= 'v':
			d = int64(c-'a') + 10
		default:
			return nil, fmt.Errorf("not a @uv: %q", text)
		}
		v.Mul(v, big.NewInt(32))
		v.Add(v, big.NewInt(d))
	}
	return v, nil
}

// LittleEndian is the atom's bytes least-significant first with no
// trailing zeros: the byte string hoon signs as (met 3 a)^a.
func LittleEndian(a *big.Int) []byte {
	be := a.Bytes()
	le := make([]byte, len(be))
	for i := range be {
		le[len(be)-1-i] = be[i]
	}
	return le
}

// FromLittleEndian is the inverse of LittleEndian.
func FromLittleEndian(le []byte) *big.Int {
	be := make([]byte, len(le))
	for i := range le {
		be[len(le)-1-i] = le[i]
	}
	return new(big.Int).SetBytes(be)
}

// met0 is (met 0 a): the bit length.
func met0(a *big.Int) int { return a.BitLen() }

// bits is a little-endian bit accumulator: bit i of the atom is the i-th
// bit written.
type bits struct {
	v *big.Int
	n int
}

func (b *bits) put(value *big.Int, width int) {
	v := new(big.Int).Lsh(value, uint(b.n))
	b.v.Or(b.v, v)
	b.n += width
}

// mat is ++mat: [p=bit-width q=bits] of the length-prefixed atom.
func mat(a *big.Int) (int, *big.Int) {
	if a.Sign() == 0 {
		return 1, big.NewInt(1)
	}
	bl := met0(a)
	c := met0(big.NewInt(int64(bl)))
	// q = (cat 0 (bex c) (mix (end [0 (dec c)] b) (lsh [0 (dec c)] a)))
	low := new(big.Int).And(big.NewInt(int64(bl)), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(c-1)), big.NewInt(1)))
	rest := new(big.Int).Or(low, new(big.Int).Lsh(a, uint(c-1)))
	q := new(big.Int).Lsh(big.NewInt(1), uint(c))
	q.Or(q, new(big.Int).Lsh(rest, uint(c+1)))
	return c + c + bl, q
}

func key(n *Noun) string {
	if n.Atom != nil {
		return "a" + n.Atom.Text(16)
	}
	return "c(" + key(n.Head) + "," + key(n.Tail) + ")"
}

// Jam is ++jam: the noun serialized with the same bit layout and the
// same backreference rule (an atom is written directly when that is no
// longer than the backreference; a cell seen before is always a
// backreference), so the bytes the ship signed are reproduced exactly.
func Jam(n *Noun) *big.Int {
	out := &bits{v: new(big.Int)}
	seen := map[string]int{}
	var walk func(n *Noun)
	walk = func(n *Noun) {
		k := key(n)
		if at, ok := seen[k]; ok {
			if n.Atom != nil && met0(n.Atom) <= met0(big.NewInt(int64(at))) {
				w, q := mat(n.Atom)
				out.put(big.NewInt(0), 1)
				out.put(q, w)
				return
			}
			w, q := mat(big.NewInt(int64(at)))
			out.put(big.NewInt(3), 2)
			out.put(q, w)
			return
		}
		seen[k] = out.n
		if n.Atom != nil {
			w, q := mat(n.Atom)
			out.put(big.NewInt(0), 1)
			out.put(q, w)
			return
		}
		out.put(big.NewInt(1), 2)
		walk(n.Head)
		walk(n.Tail)
	}
	walk(n)
	return out.v
}

// Message is what the ship signs for one authorization: the recipient
// daemon, the attempt, the operation ("assign", or "grant:<name>"), the
// expiry as unix seconds, and the nonce (D5).
type Message struct {
	Recipient string // @uv text
	Attempt   string // @uv text
	Operation string
	Expiry    int64
	Nonce     string // @uv text
}

// Noun is [recipient attempt operation expiry nonce] with the ids and the
// nonce as the atoms their @uv text denotes.
func (m Message) Noun() (*Noun, error) {
	recipient, err := ParseUV(m.Recipient)
	if err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	attempt, err := ParseUV(m.Attempt)
	if err != nil {
		return nil, fmt.Errorf("attempt: %w", err)
	}
	nonce, err := ParseUV(m.Nonce)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	if m.Expiry < 0 {
		return nil, errors.New("expiry before the epoch")
	}
	return Tuple(Atom(recipient), Atom(attempt), Cord(m.Operation), Atom(big.NewInt(m.Expiry)), Atom(nonce)), nil
}

// Verify checks sigHex (64 little-endian bytes of the signature atom)
// over the jam of the message with pubHex (32 little-endian bytes of the
// public key atom): the wire forms GET ci/key and the assignment carry.
func Verify(pubHex string, m Message, sigHex string) error {
	pub, err := hex.DecodeString(strings.TrimPrefix(pubHex, "0x"))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("public key is not 32 hex bytes")
	}
	sg, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil || len(sg) != ed25519.SignatureSize {
		return errors.New("signature is not 64 hex bytes")
	}
	n, err := m.Noun()
	if err != nil {
		return err
	}
	msg := LittleEndian(Jam(n))
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sg) {
		return errors.New("signature does not verify")
	}
	return nil
}
