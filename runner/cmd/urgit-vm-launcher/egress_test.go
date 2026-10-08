package main

// N2 (Stage B review): the host masquerade must hold on whichever interface
// the routing table picks. A rule bound to one interface name never matches
// when the route uses another (two default routes, a USB NIC that unplugs),
// and the namespace address then leaves the host unmasked. The masquerade
// therefore names the namespace address and excludes only the launcher's
// own veths, and the retired egress_interface setting is refused.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
)

// The masquerade a created network installs names no interface: it is the
// VM's namespace address, on any interface but the launcher's veths, and
// no route is read to choose one.
func TestMasqueradeFollowsTheRouteNotAnInterfaceName(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, rec := modelHost(t, m)
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:192.0.2.1:8472"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	post := chainOf(t, m.listing(), "post")
	want := []string{"ip", "saddr", netnsSaddr(1), "oifname", "!=", `"vh*"`, "masquerade"}
	var got [][]string
	for _, r := range post.rules {
		got = append(got, r.fields)
	}
	if len(got) != 1 || !slices.Equal(got[0], want) {
		t.Fatalf("THE MASQUERADE IS BOUND TO AN INTERFACE NAME: %q, want %q", got, want)
	}
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "ip -4 route show") {
			t.Fatalf("an egress interface was chosen from the route: %s", c)
		}
	}
	// the rule a stale masquerade check and RemoveNetwork find is this one:
	// it carries the namespace address token
	if err := h.RemoveNetwork(launcher.IDFor("t", "0v1"), 1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rules := chainOf(t, m.listing(), "post").rules; len(rules) != 0 {
		t.Fatalf("the masquerade stayed after removal: %v", rules)
	}
}

// A configuration that still names an egress interface is refused: the
// launcher would not honour it, and a setting that silently does nothing
// misleads the administrator. The shipped example names none.
func TestRetiredEgressInterfaceIsRefused(t *testing.T) {
	example, err := os.ReadFile(filepath.Join("..", "..", "launcher", "urgit-vm-launcher.toml.example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(filepath.Join("..", "..", "launcher", "urgit-vm-launcher.toml.example")); err != nil {
		t.Fatalf("the example configuration: %v", err)
	}
	stale := filepath.Join(t.TempDir(), "stale.toml")
	if err := os.WriteFile(stale, append(example, []byte("egress_interface = \"eth9\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(stale); err == nil || !strings.Contains(err.Error(), "egress_interface is retired") {
		t.Fatalf("A CONFIGURATION NAMING AN EGRESS INTERFACE WAS ACCEPTED: %v", err)
	}
}
