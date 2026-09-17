package sig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"testing"
)

// jam vectors from the dojo on the pinned pill (brass-408k):
//
//	(jam 0) = 2, (jam 1) = 12, (jam [0 0]) = 41 (a backreferenced atom
//	written directly because it is shorter), (jam [1 2 3]) and a tuple
//	with a repeated cord (the backreference case) as `@ud` values read
//	off the ship.
var jamVectors = []struct {
	name string
	noun *Noun
	want string
}{
	{"0", Atom(big.NewInt(0)), "2"},
	{"1", Atom(big.NewInt(1)), "12"},
	{"[0 0]", Cell(Atom(big.NewInt(0)), Atom(big.NewInt(0))), "41"},
	{"[1 2 3]", Tuple(Atom(big.NewInt(1)), Atom(big.NewInt(2)), Atom(big.NewInt(3))), JAM_1_2_3},
	{"[%assign 7 %assign]", Tuple(Cord("assign"), Atom(big.NewInt(7)), Cord("assign")), JAM_ASSIGN_7_ASSIGN},
}

func TestJamVectors(t *testing.T) {
	for _, v := range jamVectors {
		if v.want == "" {
			continue
		}
		want, _ := new(big.Int).SetString(v.want, 10)
		if got := Jam(v.noun); got.Cmp(want) != 0 {
			t.Errorf("jam %s = %s, want %s", v.name, got, want)
		}
	}
}

func TestParseUV(t *testing.T) {
	v, err := ParseUV("0v1.abcde")
	if err != nil {
		t.Fatal(err)
	}
	if v.Cmp(big.NewInt(1*32*32*32*32*32+10*32*32*32*32+11*32*32*32+12*32*32+13*32+14)) != 0 {
		t.Fatalf("%s", v)
	}
	if _, err := ParseUV("0x1"); err == nil {
		t.Fatal("0x is not a @uv")
	}
	if _, err := ParseUV("0v1w"); err == nil {
		t.Fatal("w is not a base-32 digit")
	}
}

// a cord's atom has its first byte least significant: 'ab' is 0x6261
func TestCord(t *testing.T) {
	if Cord("ab").Atom.Cmp(big.NewInt(0x6261)) != 0 {
		t.Fatalf("%s", Cord("ab").Atom.Text(16))
	}
	if got := LittleEndian(Cord("assign").Atom); string(got) != "assign" {
		t.Fatalf("%q", got)
	}
}

// a signature over the jam of the message verifies with the key that
// made it and with nothing else changed; any changed field refuses
func TestVerifyRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := Message{Recipient: "0v3.ngfcl.f9emr", Attempt: "0vhhmo2.caak2", Operation: "assign", Expiry: 1789600000, Nonce: "0v1.2345"}
	n, err := m.Noun()
	if err != nil {
		t.Fatal(err)
	}
	sg := ed25519.Sign(priv, LittleEndian(Jam(n)))
	if err := Verify(hex.EncodeToString(pub), m, hex.EncodeToString(sg)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Message{
		{Recipient: "0v4", Attempt: m.Attempt, Operation: m.Operation, Expiry: m.Expiry, Nonce: m.Nonce},
		{Recipient: m.Recipient, Attempt: "0v5", Operation: m.Operation, Expiry: m.Expiry, Nonce: m.Nonce},
		{Recipient: m.Recipient, Attempt: m.Attempt, Operation: "grant:TOKEN", Expiry: m.Expiry, Nonce: m.Nonce},
		{Recipient: m.Recipient, Attempt: m.Attempt, Operation: m.Operation, Expiry: m.Expiry + 1, Nonce: m.Nonce},
		{Recipient: m.Recipient, Attempt: m.Attempt, Operation: m.Operation, Expiry: m.Expiry, Nonce: "0v9"},
	} {
		if err := Verify(hex.EncodeToString(pub), bad, hex.EncodeToString(sg)); err == nil {
			t.Fatalf("a changed field must refuse: %+v", bad)
		}
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := Verify(hex.EncodeToString(other), m, hex.EncodeToString(sg)); err == nil {
		t.Fatal("another key must refuse")
	}
	if err := Verify("zz", m, hex.EncodeToString(sg)); err == nil {
		t.Fatal("a malformed key must refuse")
	}
}

// read off ~dep's dojo (brass-408k): `@ud`(jam [1 2 3]) and, for the
// backreference case, `@ud`(jam [%assign 7 %assign])
const (
	JAM_1_2_3           = "3426417"
	JAM_ASSIGN_7_ASSIGN = "698771254919741918199297"
)

// the live certificate (D5): with URGIT_CI_KEY_JSON set to what GET ci/key
// answered, the ship's signing key must verify the certificate over the
// CI public key, signed as (sign-raw pub [sgn.pub sgn.sek]:ship), whose
// message is the pub atom's little-endian bytes
func TestLiveCertificate(t *testing.T) {
	raw := os.Getenv("URGIT_CI_KEY_JSON")
	if raw == "" {
		t.Skip("URGIT_CI_KEY_JSON not set")
	}
	var key struct {
		Pub            string `json:"pub"`
		Cert           string `json:"cert"`
		ShipSigningKey string `json:"shipSigningKey"`
	}
	if err := json.Unmarshal([]byte(raw), &key); err != nil {
		t.Fatal(err)
	}
	pub, _ := hex.DecodeString(key.Pub)
	cert, _ := hex.DecodeString(key.Cert)
	shipPub, _ := hex.DecodeString(key.ShipSigningKey)
	if len(pub) != 32 || len(cert) != 64 || len(shipPub) != 32 {
		t.Fatalf("lengths pub=%d cert=%d ship=%d", len(pub), len(cert), len(shipPub))
	}
	msg := LittleEndian(FromLittleEndian(pub))
	if !ed25519.Verify(ed25519.PublicKey(shipPub), msg, cert) {
		t.Fatal("the certificate does not verify against the ship's signing key")
	}
	t.Logf("certificate verified: ship key %s certifies CI key %s", key.ShipSigningKey[:16], key.Pub[:16])
}
