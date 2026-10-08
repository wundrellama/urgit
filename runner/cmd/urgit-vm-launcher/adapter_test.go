package main

// Stage 01 (runner/launcher/INTEGRATION.md §7): the host adapter creates
// only what it can prove absent, removes only what it owns, stops within
// the bounded shutdown the unit is written for, and says so in its log
// when a teardown it started cannot finish. Nothing here is executed on
// the host: commands go to a recorder.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// readOnly says whether a recorded command only reads host state.
func readOnly(cmd string) bool {
	for _, p := range []string{"nft -a list ", "nft list ", "ip -4 route show", "ip link show ", "ip -o link show", "ip netns list"} {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

// networkHost answers the network commands the way the real tools do on a
// host where listing holds the launcher's table and veths names the links
// that exist. An empty listing is no table: the nft commands then go to a
// fresh nftModel (D1), so the table the adapter builds is the one it lists
// back and verifies.
func networkHost(listing string, veths ...string) func(string) (string, bool, error) {
	return networkModelHost(newNFTModel("urgit-test"), listing, veths...)
}

// networkModelHost is networkHost over a model the test keeps.
func networkModelHost(m *nftModel, listing string, veths ...string) func(string) (string, bool, error) {
	return func(cmd string) (string, bool, error) {
		switch {
		case listing == "" && strings.HasPrefix(cmd, "nft ") && !strings.HasPrefix(cmd, "nft list "):
			return m.apply(cmd)
		case cmd == "nft -a list table inet urgit-test":
			return listing, true, nil
		case cmd == "ip -4 route show default":
			return "default via 192.0.2.1 dev eth9 proto dhcp src 192.0.2.7 metric 100\n", true, nil
		case strings.HasPrefix(cmd, "ip link show "):
			name := strings.TrimPrefix(cmd, "ip link show ")
			if slices.Contains(veths, name) {
				return "7: " + name + "@if2: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n", true, nil
			}
			return `Device "` + name + `" does not exist.` + "\n", true, errors.New("exit status 1")
		}
		return "", true, nil
	}
}

// CreateNetwork proves the attempt's network objects absent before it
// creates any (INTEGRATION.md §7.3). A per-VM chain, a rule naming the
// VM's veth, or the veth itself, left by anything else, is never reused —
// the chain's rules would be the new VM's policy — and nothing is created
// beside it: the step reports that it had no effect.
func TestCreateNetworkRefusesPreexistingObjects(t *testing.T) {
	cases := map[string]struct {
		listing string
		veths   []string
	}{
		"a stale per-VM chain":                        {listing: "table inet urgit-test { # handle 1\n\tchain vm-1 { # handle 4\n\t\tip daddr 203.0.113.9 tcp dport 443 accept # handle 5\n\t}\n}\n"},
		"a stale rule naming the veth":                {listing: "table inet urgit-test { # handle 1\n\tchain forward { # handle 2\n\t\ttype filter hook forward priority filter; policy accept;\n\t\tiifname \"vh1\" accept # handle 9\n\t}\n}\n"},
		"a stale masquerade of the namespace address": {listing: "table inet urgit-test { # handle 1\n\tchain post { # handle 3\n\t\ttype nat hook postrouting priority srcnat; policy accept;\n\t\tip saddr 10.113.0.6 oifname \"eth9\" masquerade # handle 11\n\t}\n}\n"},
		"a preexisting veth":                          {veths: []string{"vh1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{reply: networkHost(c.listing, c.veths...)}
			h := testHost(t, rec)
			_, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:192.0.2.1:8472"})
			var mutating []string
			for _, cmd := range rec.calls {
				if !readOnly(cmd) {
					mutating = append(mutating, cmd)
				}
			}
			if !errors.Is(err, launcher.ErrNoEffect) || len(mutating) != 0 {
				t.Fatalf("A PREEXISTING NETWORK OBJECT WAS REUSED: err %v; mutating commands %q", err, mutating)
			}
		})
	}
}

// With nothing of the attempt's present, the per-VM chain is created —
// `nft create chain`, which fails if it exists, never `add` — no
// "File exists" is taken for success, and no host command runs through a
// shell.
func TestCreateNetworkCreatesItsOwnChain(t *testing.T) {
	rec := &recorder{reply: networkHost("")}
	h := testHost(t, rec)
	if _, err := h.CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:192.0.2.1:8472"}); err != nil {
		t.Fatalf("create: %v (calls %q)", err, rec.calls)
	}
	if !slices.Contains(rec.calls, "nft create chain inet urgit-test vm-1") || slices.Contains(rec.calls, "nft add chain inet urgit-test vm-1") {
		t.Fatalf("THE PER-VM CHAIN WAS NOT CREATED AS NEW: %q", rec.calls)
	}
	for _, cmd := range rec.calls {
		if strings.HasPrefix(cmd, "sh ") {
			t.Fatalf("a host command ran through a shell: %s", cmd)
		}
	}
	// a chain that turns up between the proof and the creation is a failure
	exists := &recorder{reply: func(cmd string) (string, bool, error) {
		if cmd == "nft create chain inet urgit-test vm-1" {
			return "Error: Could not process rule: File exists\n", true, errors.New("exit status 1")
		}
		return networkHost("")(cmd)
	}}
	if _, err := testHost(t, exists).CreateNetwork(launcher.IDFor("t", "0v1"), 1, []string{"tcp:192.0.2.1:8472"}); err == nil || errors.Is(err, launcher.ErrNoEffect) {
		t.Fatalf("\"File exists\" for the per-VM chain was taken for success (or for no effect): %v", err)
	}
}

// The unit's stop policy is the bounded shutdown's (INTEGRATION.md §7.2):
// systemd waits longer than the stop's cleanup obligation (one allowance
// from the request, the wait for a start in progress included) before it
// kills, and signals the launcher alone, so the launcher's own cleanup
// commands are not killed under it.
func TestUnitStopPolicyCoversTheBoundedShutdown(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "launcher", "urgit-vm-launcher.service"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	keys := map[string]string{}
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && section == "[Service]" && !strings.HasPrefix(line, "#") {
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	stop, err := strconv.Atoi(strings.TrimSuffix(keys["TimeoutStopSec"], "s"))
	// the stop's obligation ends every teardown by CleanupBound after the
	// request; systemd's kill comes after that, with room for the outcomes'
	// publication — an outer backstop, not cleanup time
	need := launcher.CleanupBound + 30*time.Second
	if err != nil || time.Duration(stop)*time.Second <= need || keys["KillMode"] != "mixed" {
		t.Fatalf("THE UNIT'S STOP POLICY DOES NOT COVER THE BOUNDED SHUTDOWN: TimeoutStopSec=%q (need more than %v), KillMode=%q (need mixed)", keys["TimeoutStopSec"], need, keys["KillMode"])
	}
}

// listen replaces only a socket at its path: anything else there is not
// its to remove.
func TestListenRefusesANonSocketAtItsPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("l.sock", []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := listen(&Config{Socket: "l.sock"})
	if l != nil {
		l.Close()
	}
	data, rerr := os.ReadFile("l.sock")
	if err == nil || rerr != nil || string(data) != "not a socket" {
		t.Fatalf("LISTEN REMOVED A FILE THAT IS NOT A SOCKET: listen %v, the file now %q %v", err, data, rerr)
	}
	// a socket left by a previous serve is replaced
	if err := os.Remove("l.sock"); err != nil {
		t.Fatal(err)
	}
	first, err := listen(&Config{Socket: "l.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := first.(interface{ SetUnlinkOnClose(bool) }); ok {
		ul.SetUnlinkOnClose(false)
	}
	first.Close()
	again, err := listen(&Config{Socket: "l.sock"})
	if err != nil {
		t.Fatalf("a stale socket was not replaced: %v", err)
	}
	again.Close()
}

// stallCoreHost is the fake host with the bounding seam, whose disk step
// stands for a copy that ends when its context does (or after 15 s).
type stallCoreHost struct {
	*fakeHost
	entered chan struct{}
	once    sync.Once
}

func (h *stallCoreHost) Bound(ctx context.Context) launcher.Host {
	return &stallCoreView{stallCoreHost: h, ctx: ctx}
}

type stallCoreView struct {
	*stallCoreHost
	ctx context.Context
}

func (v *stallCoreView) PrepareDisk(id string, img launcher.Image, total int) (string, error) {
	v.once.Do(func() { close(v.entered) })
	select {
	case <-v.ctx.Done():
		return "", fmt.Errorf("disk copy cut short: %w", v.ctx.Err())
	case <-time.After(15 * time.Second):
	}
	return v.fakeHost.PrepareDisk(id, img, total)
}

// serve's stop cuts a create in progress short (BeginStop) and ends well
// inside the unit's stop timeout, instead of waiting for the create
// (INTEGRATION.md §7.2).
func TestServeStopCutsACreateShort(t *testing.T) {
	state := t.TempDir()
	h := &stallCoreHost{fakeHost: newFakeHost(), entered: make(chan struct{})}
	svc, err := launcher.NewService(coreConfig(state), h)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cfg := &Config{Socket: "l.sock", StateDir: state, IDPrefix: "t", RunnerUIDs: []int{os.Getuid()}}
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, svc, nil, log.New(&logs, "", 0)) }()
	var cl *launcher.Client
	for deadline := time.Now().Add(5 * time.Second); cl == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		cl, _ = launcher.Dial(context.Background(), "l.sock", "0vd")
	}
	if cl == nil {
		t.Fatalf("serve never answered: %s", logs.String())
	}
	defer cl.Close()
	r, err := cl.Reserve(launcher.ReserveRequest{Attempt: "0v1", Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	created := make(chan error, 1)
	go func() { _, err := cl.Create(r.Ref()); created <- err }()
	<-h.entered
	begun := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop: %s", logs.String())
	}
	if took := time.Since(begun); took > 5*time.Second {
		t.Fatalf("SERVE WAITED ON A CREATE IT SHOULD HAVE CUT SHORT: it stopped after %v; log:\n%s", took, logs.String())
	}
	select {
	case <-created:
	case <-time.After(20 * time.Second):
		t.Fatal("the create never answered")
	}
	if snap, err := launcher.ReadState(state, "t"); err != nil || len(snap.Records) != 0 {
		t.Fatalf("after the stop: %+v %v", snap, err)
	}
}

// A teardown the launcher starts on its own and cannot finish is in the
// log (INTEGRATION.md §7.4): the startup recovery pass finds a record past
// its deadline, the state directory refuses its `stopping` publication, so
// the teardown halts — nothing removed, still charged — and the log is the
// only place its reason is kept.
func TestServeLogsAHaltedTeardown(t *testing.T) {
	state := t.TempDir()
	due := interrupted("0vdue", launcher.StatePreparing)
	due.DeadlineUnix = time.Now().Add(-time.Minute).Unix()
	writeRecord(t, state, due)
	attempts := filepath.Join(state, "attempts")
	if err := os.Chmod(attempts, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(attempts, 0o700) })
	h := newFakeHost()
	svc, err := launcher.NewService(coreConfig(state), h)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cfg := &Config{Socket: "l.sock", StateDir: state, IDPrefix: "t", RunnerUIDs: []int{os.Getuid()}}
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, svc, nil, log.New(&logs, "", 0)) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), "TEARDOWN HALTED "+due.ID) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done: // its shutdown halts too, and says so; the error is expected
	case <-time.After(10 * time.Second):
		t.Fatalf("serve did not stop: %s", logs.String())
	}
	text := logs.String()
	if !strings.Contains(text, "TEARDOWN HALTED "+due.ID) || !strings.Contains(text, "not durable") {
		t.Fatalf("A HALTED TEARDOWN WAS NOT REPORTED: log:\n%s", text)
	}
	if h.saw("rmcgroup "+due.ID) || h.saw("rmjail "+due.ID) {
		t.Fatalf("a halted teardown removed something: %v", h.all())
	}
}

// A teardown the launcher starts after its cleanup deadline has passed —
// here a record found at start whose deadline passed a minute ago, while no
// launcher ran — still cleans up, but it releases nothing (recovery ruling
// A): serve's log says it is an incident, LATE, and names the operator's
// recovery.
func TestServeLogsALateIncident(t *testing.T) {
	state := t.TempDir()
	late := interrupted("0vlate", launcher.StatePreparing)
	late.DeadlineUnix = time.Now().Add(-time.Minute).Unix()
	writeRecord(t, state, late)
	h := newFakeHost()
	svc, err := launcher.NewService(coreConfig(state), h)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	cfg := &Config{Socket: "l.sock", StateDir: state, IDPrefix: "t", RunnerUIDs: []int{os.Getuid()}}
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, svc, nil, log.New(&logs, "", 0)) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), "QUARANTINED "+late.ID+" (owner") {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("serve did not stop: %s", logs.String())
	}
	text := logs.String()
	if !strings.Contains(text, "QUARANTINED "+late.ID+" (owner") || !strings.Contains(text, "LATE: its cleanup deadline") || !strings.Contains(text, "urgit-vm-launcher recover") {
		t.Fatalf("A LATE INCIDENT WAS NOT LOGGED AS SUCH: log:\n%s", text)
	}
	if !h.saw("rmcgroup "+late.ID) || !h.saw("rmjail "+late.ID) {
		t.Fatalf("the late recovery did not clean up: %v", h.all())
	}
}
