package inputpolicy

import (
	"strings"
	"testing"
)

func TestExactInputGrammar(t *testing.T) {
	for _, s := range []string{"https://downloads.example/a", "https://cdn.example/", "https://repo.de/a-1_2.tgz"} {
		if _, err := URL(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"", "http://a.example/a", "HTTPS://a.example/a", "https://A.example/a", "https://a.example", "https://a.example./a", "https://a.example:443/a", "https://user:pw@a.example/a", "https://a.example/a?x=y", "https://a.example/a#f", "https://a.example/%2e%2e/a", "https://a.example/a/../b", "https://a.example/a/.", "https://a.example//b", "https://127.0.0.1/a", "https://2130706433/a", "https://[::ffff:127.0.0.1]/a", "https://a/a", "https://a.example/\\b", "file:///a", "ssh://a.example/a", "https://a.example/" + strings.Repeat("x", 512)} {
		if _, err := URL(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if _, err := (&Policy{Version: 1, Sources: nil}).Scope(); err == nil {
		t.Fatal("null sources accepted")
	}
	empty, err := (&Policy{Version: 1, Sources: []string{}}).Scope()
	if err != nil || empty != Prefix {
		t.Fatal(empty, err)
	}
	scope, err := Scope([]string{"https://z.example/a", "https://a.example/b"})
	if err != nil || scope != Prefix+"https://a.example/b\nhttps://z.example/a" {
		t.Fatal(scope, err)
	}
	if err := Validate([]string{"https://a.example/a", "https://a.example/a"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if Allows([]string{"https://a.example/a"}, "https://a.example/ab") {
		t.Fatal("prefix grant")
	}
}
