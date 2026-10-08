package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The guest's resolver: empty unless the launcher names its pinned-name
// responder; then that responder only, no search domain. A malformed value
// leaves it empty (no name resolves), never something wider.
func TestResolverIsTheResponderOrNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "resolv.conf")
	read := func() string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if err := configureResolver(p, ""); err != nil || read() != "" {
		t.Fatalf("no responder: %v %q", err, read())
	}
	if err := configureResolver(p, "172.16.0.5"); err != nil {
		t.Fatal(err)
	}
	if got, want := read(), "nameserver 172.16.0.5\noptions ndots:1 attempts:1 timeout:2\n"; got != want {
		t.Fatalf("a responder:\n got %q\nwant %q", got, want)
	}
	for _, bad := range []string{"172.16.0.5 8.8.8.8", "172.16.0.5\nnameserver 8.8.8.8", "::1", "dns.example", "172.016.0.5", "8.8.8.8,1.1.1.1"} {
		os.WriteFile(p, []byte("nameserver 9.9.9.9\n"), 0o644)
		if err := configureResolver(p, bad); err == nil || read() != "" {
			t.Fatalf("%q: A MALFORMED RESPONDER WAS WRITTEN (or the old resolver kept): %v %q", bad, err, read())
		}
	}
}
