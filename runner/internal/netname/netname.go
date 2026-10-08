// Package netname is the one grammar of a DNS name in a network
// destination (CI-P4-NET-1, named destinations, 2026-10-08): the runner's
// profile check and the VM launcher's ceiling and policy checks use it, and
// the ship's `dns-name-valid` (desk/lib/ci-provenance.hoon) spells the same
// rule, so the three layers agree on what one entry names.
//
// A name is resolved and pinned by the VM launcher on the host, never by the
// guest. The grammar is deliberately narrow so that one entry names exactly
// one host: lower case only (a policy is compared as text), no wildcard, no
// trailing dot, no single label (a host resolver's search domain would
// complete it), and a last label that starts with a letter, so that no IPv4
// literal, valid or not, is ever read as a name.
package netname

// Valid says whether host is a lower-case DNS name: 1-253 characters, two
// or more dot-separated labels of 1-63 characters from a-z, 0-9 and '-',
// no label starting or ending with '-', and a last label starting with a
// letter.
func Valid(host string) bool {
	if len(host) < 1 || len(host) > 253 {
		return false
	}
	labels := 0
	start := 0
	last := ""
	for i := 0; i <= len(host); i++ {
		if i < len(host) && host[i] != '.' {
			continue
		}
		label := host[start:i]
		if !validLabel(label) {
			return false
		}
		labels++
		last = label
		start = i + 1
	}
	return labels >= 2 && last[0] >= 'a' && last[0] <= 'z'
}

func validLabel(label string) bool {
	if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
			return false
		}
	}
	return true
}
