// Package inputpolicy defines the exact-input grammar shared by the resolver
// policy envelope and the runner owner's ceiling. It performs no network I/O.
package inputpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"urgit/runner/internal/netname"
)

const MaxSources = 16
const MaxURL = 512
const Prefix = "automatic-input-v1\n"

type Policy struct {
	Version int      `json:"version"`
	Sources []string `json:"sources"`
}

// URL requires one canonical spelling, with an exact DNS host and path. No
// implicit ports, IP spellings, escapes, queries, wildcards or dot segments.
// Redirects needing a signed query URL remain unsupported, not widened.
func URL(raw string) (*url.URL, error) {
	fail := func() (*url.URL, error) {
		return nil, fmt.Errorf("automatic input %q requires a canonical https://DNS-host/path URL (no credentials, port, query, fragment, escapes or dot segments)", raw)
	}
	if len(raw) == 0 || len(raw) > MaxURL {
		return fail()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host != u.Hostname() || !netname.Valid(u.Host) || net.ParseIP(u.Host) != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "%?#\\") || u.Path == "" || u.String() != raw {
		return fail()
	}
	for _, c := range u.Path {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/-._~", c)) {
			return fail()
		}
	}
	if strings.Contains(u.Path, "//") {
		return fail()
	}
	for _, s := range strings.Split(u.Path, "/") {
		if s == "." || s == ".." {
			return fail()
		}
	}
	return u, nil
}

func Validate(sources []string) error {
	if len(sources) > MaxSources {
		return fmt.Errorf("automatic inputs allow at most %d exact URLs", MaxSources)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if _, err := URL(s); err != nil {
			return err
		}
		if seen[s] {
			return fmt.Errorf("duplicate automatic input %q", s)
		}
		seen[s] = true
	}
	return nil
}

func Scope(sources []string) (string, error) {
	if err := Validate(sources); err != nil {
		return "", err
	}
	copy := append([]string(nil), sources...)
	sort.Strings(copy)
	return Prefix + strings.Join(copy, "\n"), nil
}
func Identity(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}
func (p *Policy) Scope() (string, error) {
	if p == nil || p.Version != 1 || p.Sources == nil {
		return "", fmt.Errorf("missing or malformed signed automatic-inputs policy")
	}
	return Scope(p.Sources)
}
func Allows(sources []string, raw string) bool {
	for _, s := range sources {
		if s == raw {
			return true
		}
	}
	return false
}
