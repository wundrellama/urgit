// Package signing implements the canonical noun encoding used by the CI
// authorization envelope. It never holds a ship private key.
package signing

import (
	"encoding/json"
	"fmt"
	"math/big"
	"math/bits"
	"sort"
	"strings"
)

type Noun struct {
	atom       *big.Int
	head, tail *Noun
}

func Atom(a *big.Int) *Noun { return &Noun{atom: new(big.Int).Set(a)} }
func Uint(a uint64) *Noun   { return Atom(new(big.Int).SetUint64(a)) }
func Cord(s string) *Noun   { return Atom(fromLE([]byte(s))) }
func Octets(s string) *Noun { return Cell(Uint(uint64(len(s))), Cord(s)) }
func Cell(a, b *Noun) *Noun { return &Noun{head: a, tail: b} }
func Tuple(items ...*Noun) *Noun {
	if len(items) == 0 {
		return Uint(0)
	}
	n := items[len(items)-1]
	for i := len(items) - 2; i >= 0; i-- {
		n = Cell(items[i], n)
	}
	return n
}
func List(items ...*Noun) *Noun { return Tuple(append(items, Uint(0))...) }
func fromLE(b []byte) *big.Int {
	reversed := make([]byte, len(b))
	for i := range b {
		reversed[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(reversed)
}
func LE(a *big.Int, size int) ([]byte, error) {
	raw := a.Bytes()
	if size == 0 {
		size = len(raw)
	}
	if len(raw) > size {
		return nil, fmt.Errorf("atom exceeds %d bytes", size)
	}
	out := make([]byte, size)
	for i := range raw {
		out[i] = raw[len(raw)-1-i]
	}
	return out, nil
}
func Parse(text, prefix string, width int) (*big.Int, error) {
	if !strings.HasPrefix(text, prefix) || len(text) > width*2+100 {
		return nil, fmt.Errorf("invalid %s atom", prefix)
	}
	digits := strings.ReplaceAll(strings.TrimPrefix(text, prefix), ".", "")
	base := 16
	if prefix == "0v" {
		base = 32
	}
	for _, c := range digits {
		if !(c >= '0' && c <= '9' || c >= 'a' && c < rune('a'+base-10)) {
			return nil, fmt.Errorf("invalid %s atom", prefix)
		}
	}
	a, ok := new(big.Int).SetString(digits, base)
	if !ok || a.Sign() < 0 || a.BitLen() > width*8 {
		return nil, fmt.Errorf("invalid %s atom", prefix)
	}
	return a, nil
}

// CanonicalJSON matches +signing-json: ordinary JSON tags and scalar
// values (text as octs), arrays in their original order, objects as sorted pair lists.
func CanonicalJSON(v any) (*Noun, error) {
	switch x := v.(type) {
	case nil:
		return Uint(0), nil
	case string:
		return Cell(Cord("s"), Octets(x)), nil
	case bool:
		b := uint64(1)
		if x {
			b = 0
		}
		return Cell(Cord("b"), Uint(b)), nil
	case json.Number:
		return Cell(Cord("n"), Octets(string(x))), nil
	case []any:
		ns := make([]*Noun, len(x))
		for i, item := range x {
			n, e := CanonicalJSON(item)
			if e != nil {
				return nil, e
			}
			ns[i] = n
		}
		return Cell(Cord("a"), List(ns...)), nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ns := make([]*Noun, 0, len(keys))
		for _, k := range keys {
			n, e := CanonicalJSON(x[k])
			if e != nil {
				return nil, e
			}
			ns = append(ns, Cell(Octets(k), n))
		}
		return Cell(Cord("o"), List(ns...)), nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", v)
	}
}

type bitWriter struct {
	data     []byte
	position int
}

func (w *bitWriter) bit(b uint) {
	if w.position%8 == 0 {
		w.data = append(w.data, 0)
	}
	w.data[w.position/8] |= byte(b&1) << uint(w.position%8)
	w.position++
}
func (w *bitWriter) atom(a *big.Int, n int) {
	for i := 0; i < n; i++ {
		w.bit(a.Bit(i))
	}
}
func (w *bitWriter) mat(a *big.Int) {
	b := a.BitLen()
	if b == 0 {
		w.bit(1)
		return
	}
	c := bits.Len(uint(b))
	for i := 0; i < c; i++ {
		w.bit(0)
	}
	w.bit(1)
	w.atom(new(big.Int).SetUint64(uint64(b)), c-1)
	w.atom(a, b)
}

// Jam is the bit-for-bit +jam encoding, including its back-reference
// choice for repeated atoms and cells. Bytes are little endian.
func Jam(n *Noun) []byte {
	w := new(bitWriter)
	// Intern structural identities without copying whole subtrees into
	// map keys. Deep JSON then uses linear space, not quadratic strings.
	type identity struct {
		atom       string
		head, tail int
	}
	intern := map[identity]int{}
	ids := map[*Noun]int{}
	var identify func(*Noun) int
	identify = func(n *Noun) int {
		if id, ok := ids[n]; ok {
			return id
		}
		key := identity{}
		if n.atom != nil {
			key.atom = n.atom.Text(16)
		} else {
			key.head = identify(n.head)
			key.tail = identify(n.tail)
		}
		id, ok := intern[key]
		if !ok {
			id = len(intern) + 1
			intern[key] = id
		}
		ids[n] = id
		return id
	}
	identify(n)
	seen := map[int]int{}
	var visit func(*Noun)
	visit = func(n *Noun) {
		id := ids[n]
		pos, ok := seen[id]
		if ok && (n.atom == nil || n.atom.BitLen() > bits.Len(uint(pos))) {
			w.bit(1)
			w.bit(1)
			w.mat(new(big.Int).SetUint64(uint64(pos)))
			return
		}
		if !ok {
			seen[id] = w.position
		}
		if n.atom != nil {
			w.bit(0)
			w.mat(n.atom)
			return
		}
		w.bit(1)
		w.bit(0)
		visit(n.head)
		visit(n.tail)
	}
	visit(n)
	return w.data
}

// Message reconstructs [recipient attempt operation expiry nonce]. The
// @da epoch is Urbit's ~1970.1.1, in 2^-64-second units.
func Message(recipient, attempt string, operation *Noun, expiry int64, nonce string) ([]byte, error) {
	r, e := Parse(recipient, "0v", 32)
	if e != nil {
		return nil, e
	}
	a, e := Parse(attempt, "0v", 32)
	if e != nil {
		return nil, e
	}
	n, e := Parse(nonce, "0v", 32)
	if e != nil {
		return nil, e
	}
	if expiry < 0 {
		return nil, fmt.Errorf("negative expiry")
	}
	epoch, _ := new(big.Int).SetString("8000000cce9e0d800000000000000000", 16)
	date := new(big.Int).Lsh(new(big.Int).SetInt64(expiry), 64)
	date.Add(date, epoch)
	return Jam(Tuple(Atom(r), Atom(a), operation, Atom(date), Atom(n))), nil
}
