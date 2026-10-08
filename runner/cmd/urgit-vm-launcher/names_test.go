package main

// Named destinations (CI-P4-NET-1, 2026-10-08; names.go). A granted name is
// pinned on the host when the VM's network is created: its public IPv4
// answers become exact forward accepts at the entry's proto and port, it
// gets no input-hook exception, a name that answers anything private is
// refused without effect, and the VM's responder answers only the pinned
// table and stops before the namespace goes. Every command goes to the
// recorded-command host and its nft model; the live test at the end runs
// the responder in a real network namespace of a private user namespace.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/pindns"
)

// a resolver model: names to answers, everything else not found
func resolverModel(answers map[string][]string) func(context.Context, string) ([]netip.Addr, error) {
	return func(_ context.Context, name string) ([]netip.Addr, error) {
		got, ok := answers[name]
		if !ok {
			return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
		}
		var out []netip.Addr
		for _, a := range got {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

type fakeResponder struct {
	mu      sync.Mutex
	nsPath  string
	listen  string
	table   pindns.Table
	started int
	closed  int
	order   *[]string
}

func (f *fakeResponder) start(nsPath, listen string, table pindns.Table) (io.Closer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nsPath, f.listen, f.table = nsPath, listen, table
	f.started++
	return closerFunc(func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.closed++
		if f.order != nil {
			*f.order = append(*f.order, "responder closed")
		}
		return nil
	}), nil
}

type closerFunc func() error

func (c closerFunc) Close() error { return c() }

// the model host's own addresses: a LAN address and one public one
var hostOwn = []netip.Addr{netip.MustParseAddr("192.168.40.7"), netip.MustParseAddr("93.184.215.14")}

func namedHost(t *testing.T, m *nftModel, answers map[string][]string) (*realHost, *recorder, *fakeResponder) {
	h, rec := modelHost(t, m)
	f := &fakeResponder{}
	h.lookupIP, h.startDNS, h.dns = resolverModel(answers), f.start, newDNSRegistry()
	h.ownAddrs = func() ([]netip.Addr, error) { return hostOwn, nil }
	return h, rec, f
}

var publicAnswers = map[string][]string{
	"archive.ubuntu.com":  {"91.189.91.82", "185.125.190.81", "91.189.91.82"},
	"bootstrap.urbit.org": {"104.21.64.85", "172.67.179.114"},
}

func TestNamedDestinationGrammar(t *testing.T) {
	good := map[string][3]string{
		"tcp:archive.ubuntu.com:80":   {"tcp", "archive.ubuntu.com", "80"},
		"udp:ns-1.x1.example:53":      {"udp", "ns-1.x1.example", "53"},
		"tcp:bootstrap.urbit.org:443": {"tcp", "bootstrap.urbit.org", "443"},
	}
	for d, want := range good {
		p, n, port, ok := namedDestination(d)
		if !ok || [3]string{p, n, port} != want {
			t.Fatalf("%s: %q %q %q %v", d, p, n, port, ok)
		}
	}
	for _, d := range []string{"tcp:198.51.100.20:80", "tcp:Archive.ubuntu.com:80", "tcp:*.ubuntu.com:80", "tcp:localhost:80",
		"tcp:archive.ubuntu.com.:80", "icmp:archive.ubuntu.com:1", "tcp:archive.ubuntu.com:0", "tcp:archive.ubuntu.com:080",
		"tcp:archive.ubuntu.com", "tcp:archive.ubuntu.com:80:81", "tcp:[archive.ubuntu.com]:80", "tcp:host.123:80"} {
		if _, _, _, ok := namedDestination(d); ok {
			t.Fatalf("NOT A NAMED DESTINATION, ACCEPTED: %s", d)
		}
	}
}

func TestPinnableIsPublicIPv4Only(t *testing.T) {
	for _, a := range []string{"91.189.91.82", "104.21.64.85", "185.199.108.133", "1.1.1.1"} {
		if !pinnable(netip.MustParseAddr(a)) {
			t.Fatalf("a public address refused: %s", a)
		}
	}
	for _, a := range []string{"10.113.0.1", "172.16.0.1", "192.168.40.7", "192.168.40.9", "127.0.0.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "192.0.2.1", "198.51.100.20", "203.0.113.9", "224.0.0.251", "255.255.255.255",
		"::1", "2606:4700::6810:4055", "::ffff:192.168.40.7"} {
		if pinnable(netip.MustParseAddr(a)) {
			t.Fatalf("AN UNPINNABLE ADDRESS WAS PINNABLE: %s", a)
		}
	}
}

// A granted name becomes one forward accept per pinned address at its proto
// and port (duplicates collapse), no input exception, and a responder on
// the VM's gateway in the VM's namespace answering exactly the pinned table.
func TestNamedDestinationIsPinnedInTheForwardChainOnly(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _, f := namedHost(t, m, publicAnswers)
	id := launcher.IDFor("t", "0v1")
	allow := []string{"tcp:archive.ubuntu.com:80", "tcp:bootstrap.urbit.org:443", "tcp:198.51.100.20:8472"}
	info, err := h.CreateNetwork(id, 1, allow)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, m.listing())
	}
	listing := m.listing()
	var forward [][]string
	for _, r := range chainOf(t, listing, "vm-1").rules {
		if slices.Contains(r.fields, "accept") {
			forward = append(forward, r.fields)
		}
	}
	want := [][]string{
		{"ip", "daddr", "91.189.91.82", "tcp", "dport", "80", "accept"},
		{"ip", "daddr", "185.125.190.81", "tcp", "dport", "80", "accept"},
		{"ip", "daddr", "104.21.64.85", "tcp", "dport", "443", "accept"},
		{"ip", "daddr", "172.67.179.114", "tcp", "dport", "443", "accept"},
		{"ip", "daddr", "198.51.100.20", "tcp", "dport", "8472", "accept"},
	}
	slices.SortFunc(forward, func(a, b []string) int { return strings.Compare(strings.Join(a, " "), strings.Join(b, " ")) })
	slices.SortFunc(want, func(a, b []string) int { return strings.Compare(strings.Join(a, " "), strings.Join(b, " ")) })
	if !slices.EqualFunc(forward, want, slices.Equal[[]string]) {
		t.Fatalf("THE FORWARD CHAIN IS NOT EXACTLY THE PINNED ADDRESSES:\n got %q\nwant %q", forward, want)
	}
	// the input chain's exceptions are the literal's alone
	in := chainOf(t, listing, "in-1")
	var inFields [][]string
	for _, r := range in.rules {
		inFields = append(inFields, r.fields)
	}
	if wantIn := [][]string{{"ip", "daddr", "198.51.100.20", "tcp", "dport", "8472", "accept"}, {"drop"}}; !slices.EqualFunc(inFields, wantIn, slices.Equal[[]string]) {
		t.Fatalf("A NAME GOT AN INPUT EXCEPTION: %q", inFields)
	}
	if p := inputContainment(listing, 1, allow); p != nil {
		t.Fatalf("the containment check refuses a named destination: %q", p)
	}
	// the responder: the VM's namespace, its gateway, the pinned table
	_, _, tap, _ := netAddrs(1)
	if f.started != 1 || f.nsPath != filepath.Join(h.netnsDir, id) || f.listen != tap+":53" || info.DNS != tap {
		t.Fatalf("responder started %d in %q on %q; NetInfo.DNS %q (gateway %s)", f.started, f.nsPath, f.listen, info.DNS, tap)
	}
	if got := f.table["archive.ubuntu.com"]; len(got) != 2 || got[0].String() != "91.189.91.82" || got[1].String() != "185.125.190.81" {
		t.Fatalf("the pinned table: %v", f.table)
	}
	if len(f.table) != 2 {
		t.Fatalf("the pinned table holds names it was not granted: %v", f.table)
	}
}

// Two names on one CDN address and port need one forward accept, and each
// is answered with its own pinned addresses.
func TestSharedAddressGetsOneAccept(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _, f := namedHost(t, m, map[string][]string{
		"bootstrap.urbit.org": {"104.21.64.85"},
		"other.example":       {"104.21.64.85", "172.67.179.114"},
	})
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:bootstrap.urbit.org:443", "tcp:other.example:443"}); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, r := range chainOf(t, m.listing(), "vm-1").rules {
		if slices.Equal(r.fields, []string{"ip", "daddr", "104.21.64.85", "tcp", "dport", "443", "accept"}) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the shared address has %d accepts, want 1:\n%s", n, m.listing())
	}
	if len(f.table["bootstrap.urbit.org"]) != 1 || len(f.table["other.example"]) != 2 {
		t.Fatalf("the pinned table: %v", f.table)
	}
}

// A VM granted only literals gets no responder and no DNS address: its guest
// keeps an empty resolver, exactly as before named destinations.
func TestLiteralsOnlyStartNoResponder(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _, f := namedHost(t, m, publicAnswers)
	info, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, d1Allow)
	if err != nil {
		t.Fatal(err)
	}
	if f.started != 0 || info.DNS != "" {
		t.Fatalf("a literals-only VM got a responder (%d) or a DNS address %q", f.started, info.DNS)
	}
}

// A name that cannot be pinned refuses the create without effect: nothing
// mutating runs and no responder starts. One private answer among public
// ones refuses the name; it is never silently dropped.
func TestUnpinnableNameRefusesWithoutEffect(t *testing.T) {
	answers := map[string][]string{
		"rebind.example":   {"192.168.40.7"},
		"mixed.example":    {"104.21.64.85", "10.113.0.6"},
		"loopback.example": {"127.0.0.1"},
		"v6only.example":   {},
		"metadata.example": {"169.254.169.254"},
		"self.example":     {"104.21.64.85", "93.184.215.14"},
	}
	for _, d := range []string{"tcp:rebind.example:80", "tcp:mixed.example:443", "tcp:loopback.example:80",
		"tcp:v6only.example:80", "tcp:metadata.example:80", "tcp:nonexistent.example:80", "tcp:self.example:443"} {
		m := newNFTModel("urgit-test")
		h, rec, f := namedHost(t, m, answers)
		_, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:198.51.100.20:8472", d})
		var mutating []string
		for _, c := range rec.calls {
			if !readOnly(c) {
				mutating = append(mutating, c)
			}
		}
		if !errors.Is(err, launcher.ErrNoEffect) || len(mutating) != 0 || f.started != 0 {
			t.Fatalf("%s: AN UNPINNABLE NAME WAS ACTED ON: %v; mutating %q; responders %d", d, err, mutating, f.started)
		}
	}
	// no resolver configured: a name refuses, a literal still works
	m := newNFTModel("urgit-test")
	h, _ := modelHost(t, m)
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:archive.ubuntu.com:80"}); !errors.Is(err, launcher.ErrNoEffect) {
		t.Fatalf("a name without a resolver: %v", err)
	}
}

// Teardown stops the responder before the namespace is deleted (its
// sockets would keep the namespace alive), and removes every forward
// accept the pinning added.
func TestRemoveNetworkStopsTheResponderFirst(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, rec, f := namedHost(t, m, publicAnswers)
	var order []string
	f.order = &order
	id := launcher.IDFor("t", "0v1")
	if _, err := h.CreateNetwork(id, 1, []string{"tcp:archive.ubuntu.com:80"}); err != nil {
		t.Fatal(err)
	}
	before := len(rec.calls)
	inner := rec.reply
	rec.reply = func(cmd string) (string, bool, error) {
		if strings.HasPrefix(cmd, "ip netns del") {
			order = append(order, "netns deleted")
		}
		return inner(cmd)
	}
	if err := h.RemoveNetwork(id, 1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !slices.Equal(order, []string{"responder closed", "netns deleted"}) {
		t.Fatalf("THE RESPONDER WAS NOT STOPPED BEFORE THE NAMESPACE: %q (calls %q)", order, rec.calls[before:])
	}
	for _, c := range parseTable(m.listing()) {
		if c.name == "vm-1" || c.name == "in-1" {
			t.Fatalf("a chain of the VM stayed: %s", c.name)
		}
	}
	// a second removal finds nothing and stops nothing twice
	if err := h.RemoveNetwork(id, 1); err != nil || f.closed != 1 {
		t.Fatalf("a second removal: %v; closed %d times", err, f.closed)
	}
}

// check accepts a name in the ceiling and still refuses one outside the
// grammar.
func TestCheckAcceptsANamedCeilingEntry(t *testing.T) {
	m := newNFTModel("urgit-test")
	h, _ := modelHost(t, m)
	h.images = map[string]launcher.Image{}
	h.cfg.Ceiling = []string{"tcp:archive.ubuntu.com:80", "tcp:198.51.100.20:8472"}
	if err := h.check(); err != nil && strings.Contains(err.Error(), "ceiling entry") {
		t.Fatalf("check refused a named ceiling entry: %v", err)
	}
	h.cfg.Ceiling = []string{"tcp:Archive.ubuntu.com:80"}
	if err := h.check(); err == nil || !strings.Contains(err.Error(), "ceiling entry tcp:Archive.ubuntu.com:80:") {
		t.Fatalf("check passed a ceiling entry outside the grammar: %v", err)
	}
}

// The boot arguments name the responder only when the VM has one.
func TestBootArgsNameTheResponderOnlyWhenThereIsOne(t *testing.T) {
	spec := launcher.VMSpec{Image: launcher.Image{BootArgs: "console=ttyS0"}, Attempt: "0v1", CPUs: 1, MemoryMiB: 128}
	if a := vmBootArgs(spec); a != "console=ttyS0 urgit.attempt=0v1" {
		t.Fatalf("a locked VM: %q", a)
	}
	spec.Net = &launcher.NetInfo{TAP: "tap0", GuestIP: "172.16.0.6/30", Gateway: "172.16.0.5"}
	if a := vmBootArgs(spec); a != "console=ttyS0 urgit.attempt=0v1 urgit.ip=172.16.0.6/30,172.16.0.5" {
		t.Fatalf("no responder: %q", a)
	}
	spec.Net.DNS = "172.16.0.5"
	if a := vmBootArgs(spec); a != "console=ttyS0 urgit.attempt=0v1 urgit.ip=172.16.0.6/30,172.16.0.5 urgit.dns=172.16.0.5" {
		t.Fatalf("a responder: %q", a)
	}
}

// Live: the responder runs inside a real network namespace (a private user
// namespace's, so no root is needed), answers a client in that namespace,
// and is not reachable from the launcher's own. The test re-runs itself
// under `unshare --user --map-root-user --net`; it is skipped only when the
// host does not allow unprivileged user namespaces at all.
func TestPinnedResponderLivesInTheVMNamespace(t *testing.T) {
	if os.Getenv("URGIT_NAMES_LIVE_CHILD") != "1" {
		unshare, err := exec.LookPath("unshare")
		if err != nil {
			t.Skip("no unshare on this host")
		}
		if out, err := exec.Command(unshare, "--user", "--map-root-user", "--net", "true").CombinedOutput(); err != nil {
			t.Skipf("this host refuses unprivileged user namespaces: %v %s", err, out)
		}
		cmd := exec.Command(unshare, "--user", "--map-root-user", "--net", os.Args[0],
			"-test.run=^TestPinnedResponderLivesInTheVMNamespace$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "URGIT_NAMES_LIVE_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestPinnedResponderLivesInTheVMNamespace") {
			t.Fatalf("the live child failed: %v\n%s", err, out)
		}
		return
	}
	// in the child: root of a private user namespace, in a fresh network
	// namespace (the "host"). Make a second one (the "VM's"), up its lo.
	vm := exec.Command("unshare", "--net", "sleep", "60")
	if err := vm.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { vm.Process.Kill(); vm.Wait() }()
	ns := fmt.Sprintf("/proc/%d/ns/net", vm.Process.Pid)
	deadline := time.Now().Add(5 * time.Second)
	for {
		self, _ := os.Readlink("/proc/self/ns/net")
		other, err := os.Readlink(ns)
		if err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the VM's namespace never appeared: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out, err := exec.Command("nsenter", "--net="+ns, "ip", "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Fatalf("lo up in the VM's namespace: %v %s", err, out)
	}
	table := pindns.Table{"archive.ubuntu.com": {netip.MustParseAddr("185.125.190.81")}}
	srv, err := startPinnedDNS(ns, "127.0.0.1:53", table)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// a client in the VM's namespace gets the pinned answer. Each query
	// dials its own socket there: the resolver runs its A and AAAA queries
	// at once and closes each socket after its exchange.
	r := &net.Resolver{PreferGo: true, Dial: func(_ context.Context, network, _ string) (net.Conn, error) {
		var conn net.Conn
		err := inNetns(ns, func() (err error) { conn, err = net.Dial(network, "127.0.0.1:53"); return })
		return conn, err
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := r.LookupHost(ctx, "archive.ubuntu.com.")
	if err != nil || !slices.Equal(got, []string{"185.125.190.81"}) {
		t.Fatalf("THE PINNED NAME WAS NOT ANSWERED IN THE VM'S NAMESPACE: %v %v", got, err)
	}
	// the launcher's own namespace has no responder: its port 53 is free
	pc, err := net.ListenPacket("udp4", "127.0.0.1:53")
	if err != nil {
		t.Fatalf("THE RESPONDER'S SOCKET IS IN THE LAUNCHER'S NAMESPACE: %v", err)
	}
	pc.Close()
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	// after Close the port is free in the VM's namespace too
	if err := inNetns(ns, func() error {
		pc, err := net.ListenPacket("udp4", "127.0.0.1:53")
		if err == nil {
			pc.Close()
		}
		return err
	}); err != nil {
		t.Fatalf("the responder's socket outlived Close: %v", err)
	}
}
