package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/ship"
)

var testSigningKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{19}, ed25519.SeedSize))

func testAtom(b []byte) string {
	rev := make([]byte, len(b))
	for i := range b {
		rev[len(b)-1-i] = b[i]
	}
	return "0x" + new(big.Int).SetBytes(rev).Text(16)
}
func testPub() string { return testAtom(testSigningKey.Public().(ed25519.PublicKey)) }
func decodeTestAssignment(t *testing.T, a *ship.Assignment) *ship.Assignment {
	t.Helper()
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	var out ship.Assignment
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return &out
}
func signedTestAssignment(t *testing.T, input *ship.Assignment) *ship.Assignment {
	t.Helper()
	a := *input
	a.Recipient = "0v1.daemon"
	a.Expiry = time.Now().Add(5 * time.Minute).Unix()
	a.Nonce = "0v9"
	a.Grants = append([]ship.Grant{}, input.Grants...)
	for i := range a.Grants {
		a.Grants[i].Nonce = "0va"
		msg, e := grantMessage(&a, a.Grants[i])
		if e != nil {
			t.Fatal(e)
		}
		a.Grants[i].Sig = testAtom(ed25519.Sign(testSigningKey, msg))
	}
	out := decodeTestAssignment(t, &a)
	msg, e := assignmentMessage(out)
	if e != nil {
		t.Fatal(e)
	}
	out.Sig = testAtom(ed25519.Sign(testSigningKey, msg))
	return out
}

func TestSignatureRefusalDoesNoWork(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ship.Assignment, *Daemon)
	}{
		{"unsigned", func(a *ship.Assignment, d *Daemon) { a.Sig = "" }},
		{"wrong-pub", func(a *ship.Assignment, d *Daemon) {
			d.cfg.CIPub = testAtom(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{20}, 32)).Public().(ed25519.PublicKey))
		}},
		{"recipient", func(a *ship.Assignment, d *Daemon) { a.Recipient = "0v2" }},
		{"oid", func(a *ship.Assignment, d *Daemon) { a.OID = strings.Repeat("f", 40) }},
		{"trust", func(a *ship.Assignment, d *Daemon) { a.Trust = "untrusted" }},
		{"workflow", func(a *ship.Assignment, d *Daemon) { a.Workflow = "other.yml" }},
		{"trailing-NUL", func(a *ship.Assignment, d *Daemon) { a.Grants[0].Value += "\x00" }},
		{"grant-value", func(a *ship.Assignment, d *Daemon) { a.Grants[0].Value = "altered" }},
		{"expired-envelope", func(a *ship.Assignment, d *Daemon) { a.Expiry = time.Now().Add(-time.Minute).Unix() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			sh := &fakeShip{}
			srv := httptest.NewServer(sh.handler(t))
			defer srv.Close()
			box := &fakeBox{}
			d := newTestDaemon(t, box, srv, 1)
			in := *jobAssignment
			in.Grants = []ship.Grant{{Name: "TOKEN", Value: "fixture-credential", Expiry: time.Now().Add(time.Minute).Unix()}}
			a := signedTestAssignment(t, &in)
			test.mutate(a, d)
			a = decodeTestAssignment(t, a)
			var logs bytes.Buffer
			d.log = log.New(&logs, "", 0)
			if !d.handle(context.Background(), a) {
				t.Fatal("refusal lost capacity")
			}
			if len(box.ops) != 0 || len(sh.abandons) != 0 || len(sh.events) != 0 {
				t.Fatal("unauthenticated message caused work")
			}
			if !strings.Contains(logs.String(), "assignment refused:") {
				t.Fatal("refusal not logged")
			}
		})
	}
}

func TestGrantSignatureIsCheckedIndependently(t *testing.T) {
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	d := newTestDaemon(t, &fakeBox{}, srv, 1)
	input := *jobAssignment
	input.Grants = []ship.Grant{{Name: "TOKEN", Value: "fixture-credential", Expiry: time.Now().Add(time.Minute).Unix()}}
	a := signedTestAssignment(t, &input)
	a.Grants[0].Sig = "0x1"
	a = decodeTestAssignment(t, a)
	msg, e := assignmentMessage(a)
	if e != nil {
		t.Fatal(e)
	}
	a.Sig = testAtom(ed25519.Sign(testSigningKey, msg))
	e = d.verifyAssignment(a, time.Now().Unix())
	if e == nil || !strings.Contains(e.Error(), "credential grant: signature does not verify") {
		t.Fatalf("bad grant under a valid assignment signature: %v", e)
	}
}
