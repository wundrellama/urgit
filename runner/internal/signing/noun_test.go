package signing

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// These bytes were produced by +jam and +sign-raw on the pinned fake
// ship. The file contains public keys/signatures only, never a seed.
func TestPinnedHoonVectors(t *testing.T) {
	data, e := os.ReadFile("testdata/hoon.json")
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		JSON                                                                  json.RawMessage `json:"json"`
		Recipient, Attempt, Nonce, Pub, Message, Sig, NetworkPub, CIPub, Cert string
		Expiry                                                                int64
		Jam                                                                   []string
	}
	if e = json.Unmarshal(data, &v); e != nil {
		t.Fatal(e)
	}
	simple := []*Noun{Cell(Uint(0), Uint(0)), Cell(Uint(1), Uint(1)), Cell(Cell(Uint(1), Uint(2)), Cell(Uint(1), Uint(2)))}
	for i, n := range simple {
		want, e := Parse(v.Jam[i], "0x", 512)
		if e != nil {
			t.Fatal(e)
		}
		if fromLE(Jam(n)).Cmp(want) != 0 {
			t.Fatalf("jam vector %d differs from Hoon", i)
		}
	}
	var body any
	decoder := json.NewDecoder(bytes.NewReader(v.JSON))
	decoder.UseNumber()
	if e = decoder.Decode(&body); e != nil {
		t.Fatal(e)
	}
	op, e := CanonicalJSON(body)
	if e != nil {
		t.Fatal(e)
	}
	msg, e := Message(v.Recipient, v.Attempt, Cell(Cord("assignment"), op), v.Expiry, v.Nonce)
	if e != nil {
		t.Fatal(e)
	}
	want, e := Parse(v.Message, "0x", 4096)
	if e != nil {
		t.Fatal(e)
	}
	if fromLE(msg).Cmp(want) != 0 {
		t.Fatal("canonical JSON/envelope differs from Hoon +jam")
	}
	pub, e := PublicKey(v.Pub)
	if e != nil {
		t.Fatal(e)
	}
	if e = Verify(pub, v.Sig, msg); e != nil {
		t.Fatal(e)
	}
	network, e := PublicKey(v.NetworkPub)
	if e != nil {
		t.Fatal(e)
	}
	ci, e := Parse(v.CIPub, "0x", 32)
	if e != nil {
		t.Fatal(e)
	}
	ciBytes, e := LE(ci, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = Verify(network, v.Cert, ciBytes); e != nil {
		t.Fatalf("network certificate: %v", e)
	}
}

func TestCanonicalStringsHaveLengths(t *testing.T) {
	a, _ := CanonicalJSON(map[string]any{"x": "value"})
	for _, body := range []map[string]any{{"x": "value\x00"}, {"x\x00": "value"}} {
		b, _ := CanonicalJSON(body)
		if bytes.Equal(Jam(a), Jam(b)) {
			t.Fatal("trailing NUL aliases signed JSON")
		}
	}
	if bytes.Equal(Jam(Octets("value")), Jam(Octets("value\x00"))) {
		t.Fatal("grant string alias")
	}
}
