package main

// D1, the host input path (P4-VM-STAGE-A-SOURCE-01, source only): a
// networked VM's traffic to the host's own addresses takes the input hook,
// which the forward chain never sees. The launcher's own table gets an input
// chain that drops its veth range, each VM's veth jumping first to its own
// chain of exact exceptions and a drop; a create verifies it, recovery
// refuses a VM found without it, and the cleanup removes exactly the VM's
// part. Every command goes to the recorded-command host and its nft model
// (nft_model_test.go); no host network change runs.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
)

var d1Allow = []string{"tcp:198.51.100.20:8472", "udp:203.0.113.9:53"}

func modelHost(t *testing.T, m *nftModel) (*realHost, *recorder) {
	rec := &recorder{reply: networkModelHost(m, "")}
	return testHost(t, rec), rec
}

func chainOf(t *testing.T, listing, name string) nftChain {
	t.Helper()
	for _, c := range parseTable(listing) {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no chain %s in:\n%s", name, listing)
	return nftChain{}
}

func fieldsOf(c nftChain) [][]string {
	var out [][]string
	for _, r := range c.rules {
		out = append(out, r.fields)
	}
	return out
}

// A create puts the input containment in place and verifies it: the input
// chain on the input hook drops the veth range; the VM's veth jumps, ahead
// of that drop, to in-<index>, which holds exactly the exact exceptions and
// a drop. A second VM shares the input chain and its one range drop.
func TestCreateNetworkContainsTheInputPath(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _ := modelHost(t, m)
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, d1Allow); err != nil {
		t.Fatalf("create: %v\n%s", err, m.listing())
	}
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v2"), 2, []string{"tcp:198.51.100.20:9000"}); err != nil {
		t.Fatalf("create of a second VM: %v\n%s", err, m.listing())
	}
	listing := m.listing()
	if p := inputContainment(listing, 1, d1Allow); p != nil {
		t.Fatalf("THE FIRST VM'S INPUT PATH IS NOT CONTAINED: %q\n%s", p, listing)
	}
	if p := inputContainment(listing, 2, []string{"tcp:198.51.100.20:9000"}); p != nil {
		t.Fatalf("THE SECOND VM'S INPUT PATH IS NOT CONTAINED: %q\n%s", p, listing)
	}
	input := chainOf(t, listing, "input")
	if !strings.Contains(input.hook, "hook input") {
		t.Fatalf("the input chain is not on the input hook: %q", input.hook)
	}
	want := [][]string{{"iifname", `"vh2"`, "jump", "in-2"}, {"iifname", `"vh1"`, "jump", "in-1"}, {"ip", "saddr", vethRange, "drop"}}
	if got := fieldsOf(input); !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("the input chain: %q, want %q", got, want)
	}
	in1 := fieldsOf(chainOf(t, listing, "in-1"))
	wantIn := [][]string{{"ip", "daddr", "198.51.100.20", "tcp", "dport", "8472", "accept"}, {"ip", "daddr", "203.0.113.9", "udp", "dport", "53", "accept"}, {"drop"}}
	if !slices.EqualFunc(in1, wantIn, slices.Equal[[]string]) {
		t.Fatalf("in-1: %q, want %q", in1, wantIn)
	}
	// a VM with no destination: its chain is the drop alone
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v3"), 3, nil); err != nil {
		t.Fatalf("create with no destination: %v", err)
	}
	if got := fieldsOf(chainOf(t, m.listing(), "in-3")); !slices.EqualFunc(got, [][]string{{"drop"}}, slices.Equal[[]string]) {
		t.Fatalf("in-3: %q", got)
	}
}

// An input rule missing is a failed create, never a contained VM: the jump,
// the range drop, the chain's drop or one exception that the host did not
// take — each reported by nft as done — fails the create's verification
// (it may have acted: the core tears it down), and so does a missing chain.
func TestCreateNetworkRefusesAnInputRuleMissing(t *testing.T) {
	cases := map[string]string{
		"the veth's jump":          "nft insert rule inet urgit-test input iifname vh1 jump in-1",
		"the veth range's drop":    "nft add rule inet urgit-test input ip saddr " + vethRange + " drop",
		"the VM chain's drop":      "nft add rule inet urgit-test in-1 drop",
		"an exception":             "nft add rule inet urgit-test in-1 ip daddr 198.51.100.20 tcp dport 8472 accept",
		"the input chain":          "nft add chain inet urgit-test input { type filter hook input priority 0; policy accept; }",
		"the VM's own input chain": "nft create chain inet urgit-test in-1",
	}
	for name, skipped := range cases {
		t.Run(name, func(t *testing.T) {
			m := newNFTModel("urgit-test")
			m.skip = func(cmd string) bool { return cmd == skipped }
			h, rec := modelHost(t, m)
			_, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, d1Allow)
			if !slices.Contains(rec.calls, skipped) {
				t.Fatalf("the case's command was never issued: %q", skipped)
			}
			if err == nil || errors.Is(err, launcher.ErrNoEffect) {
				t.Fatalf("A CREATE WITH AN INPUT RULE MISSING WAS REPORTED %v\n%s", err, m.listing())
			}
		})
	}
}

// Recovery: a networked VM this launcher may still run is served only with
// its input containment in place. Its chain missing, its jump missing or the
// range drop missing refuses (named with the record); a VM whose VMM is gone
// or that holds no network needs none; a VMM whose pid was never recorded
// may run, and is checked.
func TestRecoveryRefusesTheInputChainMissing(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _ := modelHost(t, m)
	id := launcher.IDFor("t", "0v1")
	if _, err := h.CreateNetwork(id, 1, d1Allow); err != nil {
		t.Fatalf("create: %v", err)
	}
	fakeProcess(t, h, 4242, id)
	rec := launcher.Record{ID: id, HasNetwork: true, NetIndex: 1, HasVMM: true, PID: 4242, Destinations: d1Allow, State: launcher.StateRunning}
	if p := h.inputContainmentProblems([]launcher.Record{rec}); p != nil {
		t.Fatalf("a contained VM was refused: %q", p)
	}
	spoil := map[string]func(m *nftModel){
		"the VM's input chain missing":  func(m *nftModel) { m.dropChain("in-1") },
		"the veth's jump missing":       func(m *nftModel) { m.dropRule("input", `"vh1"`, "jump") },
		"the veth range's drop missing": func(m *nftModel) { m.dropRule("input", vethRange, "drop") },
		"the veth's jump after the range drop": func(m *nftModel) {
			m.dropRule("input", `"vh1"`, "jump")
			if _, _, err := m.apply("nft add rule inet urgit-test input iifname vh1 jump in-1"); err != nil {
				panic(err)
			}
		},
	}
	for name, s := range spoil {
		m := newNFTModel("urgit-test")
		h, _ := modelHost(t, m)
		if _, err := h.CreateNetwork(id, 1, d1Allow); err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		fakeProcess(t, h, 4242, id)
		s(m)
		p := h.inputContainmentProblems([]launcher.Record{rec})
		if len(p) == 0 || !strings.Contains(p[0], id) {
			t.Fatalf("%s: RECOVERY SERVED A VM WITHOUT ITS INPUT CONTAINMENT: %q\n%s", name, p, m.listing())
		}
		unknown := rec
		unknown.PID = 0 // a start whose pid was never recorded: it may run
		if p := h.inputContainmentProblems([]launcher.Record{unknown}); len(p) == 0 {
			t.Fatalf("%s: a VMM of unrecorded pid was taken for gone", name)
		}
		gone := rec
		gone.PID = 4343 // no such process: nothing behind the veth to contain
		locked := launcher.Record{ID: launcher.IDFor("t", "0v9"), HasVMM: true, PID: 4242, State: launcher.StateRunning}
		if p := h.inputContainmentProblems([]launcher.Record{gone, locked}); p != nil {
			t.Fatalf("%s: a VM with no live VMM or no network was refused: %q", name, p)
		}
	}
}

// No exception is broader than its entry. A destination that is a network,
// a port range, port 0, a padded port, a name outside the shared grammar
// (upper case, a wildcard, one label: names_test.go covers the named
// destinations the grammar admits), an IPv6 or IPv4-mapped address, another
// proto or no port is refused before anything is created, and check names
// it in the ceiling. An exception the table holds broader than its entry —
// a network, no port, an extra accept — fails the create's verification and
// the recovery check.
func TestNoInputExceptionBroaderThanItsEntry(t *testing.T) {
	broad := []string{"tcp:198.51.100.0/24:8472", "tcp:198.51.100.20:1-65535", "tcp:198.51.100.20:0", "tcp:198.51.100.20:08472",
		"tcp:Host.example:8472", "tcp:*.example.com:8472", "tcp:localhost:8472",
		"tcp:[::1]:8472", "tcp:[::ffff:198.51.100.20]:8472", "icmp:198.51.100.20:1", "tcp:198.51.100.20",
		"tcp:198.51.100.20:8472,8473", "tcp:198.51.100.20:+8472", "tcp:198.51.100.20:65536"}
	for _, d := range broad {
		if _, err := exactDestination(d); err == nil {
			t.Fatalf("A DESTINATION BROADER THAN ONE ENTRY WAS ACCEPTED: %s", d)
		}
		m := newNFTModel("urgit-test")
		h, rec := modelHost(t, m)
		_, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{d})
		var mutating []string
		for _, c := range rec.calls {
			if !readOnly(c) {
				mutating = append(mutating, c)
			}
		}
		if !errors.Is(err, launcher.ErrNoEffect) || len(mutating) != 0 {
			t.Fatalf("%s: A BROAD DESTINATION WAS ACTED ON: %v; mutating %q", d, err, mutating)
		}
		h.cfg.Ceiling = []string{d}
		h.images = map[string]launcher.Image{}
		if err := h.check(); err == nil || !strings.Contains(err.Error(), "ceiling entry "+d+":") {
			t.Fatalf("%s: check passed a broad ceiling entry: %v", d, err)
		}
	}
	for _, d := range d1Allow {
		if got, err := exactDestination(d); err != nil || len(got) != 3 {
			t.Fatalf("an exact entry %s: %v %v", d, got, err)
		}
	}
	rewrites := map[string]func(string) string{
		"a network for the address": func(s string) string {
			return strings.Replace(s, "daddr 198.51.100.20 tcp", "daddr 198.51.100.0/24 tcp", 1)
		},
		"no port": func(s string) string { return strings.Replace(s, " tcp dport 8472 accept", " accept", 1) },
	}
	for name, rw := range rewrites {
		m := newNFTModel("urgit-test")
		m.rewrite = func(s string) string {
			if strings.HasPrefix(s, "ip daddr 198.51.100.20 tcp dport 8472 accept") && !strings.Contains(s, "jump") {
				return rw(s)
			}
			return s
		}
		h, _ := modelHost(t, m)
		if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, d1Allow); err == nil || errors.Is(err, launcher.ErrNoEffect) {
			t.Fatalf("%s: A CREATE WHOSE EXCEPTION IS BROADER THAN ITS ENTRY WAS REPORTED %v\n%s", name, err, m.listing())
		}
	}
	// an extra accept added behind the launcher's back, found at recovery
	m := newNFTModel("urgit-test")
	h, _ := modelHost(t, m)
	id := launcher.IDFor("t", "0v1")
	if _, err := h.CreateNetwork(id, 1, d1Allow); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := m.apply("nft insert rule inet urgit-test in-1 ip daddr 198.51.100.20 accept"); err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, h, 4242, id)
	rec := launcher.Record{ID: id, HasNetwork: true, NetIndex: 1, HasVMM: true, PID: 4242, Destinations: d1Allow}
	if p := h.inputContainmentProblems([]launcher.Record{rec}); len(p) == 0 || !strings.Contains(strings.Join(p, " "), "not exactly its exact exceptions") {
		t.Fatalf("RECOVERY SERVED A VM WITH AN EXCEPTION BROADER THAN ITS ENTRY: %q", p)
	}
}

// The cleanup matches the create: RemoveNetwork deletes the VM's input jump
// (in the chain the listing shows it in), then its in-<index> chain once
// nothing jumps to it, with its forward rules, masquerade and vm-<index>;
// another VM's containment and the shared input chain with its range drop
// stay. A second removal finds nothing and succeeds; a stale in-<index>
// chain refuses a later create without effect.
func TestRemoveNetworkRemovesItsInputContainment(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, rec := modelHost(t, m)
	mine, other := launcher.IDFor("t", "0v1"), launcher.IDFor("t", "0v2")
	if _, err := h.CreateNetwork(mine, 1, d1Allow); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.CreateNetwork(other, 2, []string{"tcp:198.51.100.20:9000"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := h.RemoveNetwork(mine, 1); err != nil {
		t.Fatalf("remove: %v\n%s", err, m.listing())
	}
	listing := m.listing()
	for _, c := range parseTable(listing) {
		if c.name == "in-1" || c.name == "vm-1" {
			t.Fatalf("THE VM'S CHAIN %s STAYED:\n%s", c.name, listing)
		}
		for _, r := range c.rules {
			if slices.Contains(r.fields, `"vh1"`) || slices.Contains(r.fields, netnsSaddr(1)) {
				t.Fatalf("A RULE OF THE VM STAYED in %s: %v", c.name, r.fields)
			}
		}
	}
	if p := inputContainment(listing, 2, []string{"tcp:198.51.100.20:9000"}); p != nil {
		t.Fatalf("ANOTHER VM'S CONTAINMENT WAS TOUCHED: %q\n%s", p, listing)
	}
	jumpGone, chainGone := -1, -1
	for i, c := range rec.calls {
		switch {
		case strings.HasPrefix(c, "nft delete rule inet urgit-test input handle"):
			jumpGone = i
		case c == "nft delete chain inet urgit-test in-1":
			chainGone = i
		}
	}
	if jumpGone < 0 || chainGone < jumpGone {
		t.Fatalf("the jump must go before its chain: %q", rec.calls)
	}
	if err := h.RemoveNetwork(mine, 1); err != nil {
		t.Fatalf("a second removal: %v", err)
	}
	stale := "table inet urgit-test { # handle 1\n\tchain in-1 { # handle 4\n\t\tdrop # handle 5\n\t}\n}\n"
	r2 := &recorder{reply: networkHost(stale)}
	if _, err := testHost(t, r2).CreateNetwork(mine, 1, d1Allow); !errors.Is(err, launcher.ErrNoEffect) {
		t.Fatalf("A STALE in-1 CHAIN WAS TAKEN OVER: %v", err)
	}
	for _, c := range r2.calls {
		if !readOnly(c) {
			t.Fatalf("a mutating command beside a stale in-1: %s", c)
		}
	}
}
