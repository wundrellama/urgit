package netname

import (
	"strings"
	"testing"
)

// The cases are the same ones the ship's ci-policy-vector checks for
// `dns-name-valid`, so a divergence between the layers shows in both.
func TestValid(t *testing.T) {
	long := strings.Repeat("a", 63)
	cases := map[string]bool{
		"archive.ubuntu.com":        true,
		"bootstrap.urbit.org":       true,
		"raw.githubusercontent.com": true,
		"a-b.example":               true,
		"x1.y2.io":                  true,
		"xn--bcher-kva.example":     true,
		long + ".com":               true,
		// refused
		"":                                false,
		"localhost":                       false, // one label: a search domain would complete it
		"github.com.":                     false, // trailing dot
		".github.com":                     false,
		"git..hub.com":                    false,
		"GitHub.com":                      false, // policies compare as text
		"*.github.com":                    false,
		"git_hub.com":                     false,
		"-git.com":                        false,
		"git-.com":                        false,
		"git.com-":                        false,
		"140.82.112.3":                    false, // an IPv4 literal is never a name
		"300.82.112.3":                    false, // nor an invalid one
		"host.123":                        false, // the last label starts with a letter
		"[::1]":                           false,
		"::1":                             false,
		"a.b c":                           false,
		long + "a.com":                    false, // a 64-character label
		strings.Repeat("a.", 125) + "com": true,  // 253 characters
		strings.Repeat("a.", 126) + "com": false, // 255 characters
	}
	for host, want := range cases {
		if got := Valid(host); got != want {
			t.Errorf("Valid(%q) = %v, want %v", host, got, want)
		}
	}
}
