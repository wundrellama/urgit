package sandbox

// Pinned addresses shown per run (CI-P4-NET-1, 2026-10-08): the launcher's
// create answers each granted DNS name with the addresses it pinned it to;
// the microvm backend keeps exactly that answer for the sandbox, after it
// checks the answer names the granted names and nothing else.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

func pinnedMicrovm(t *testing.T, pins []launcher.Pin, ceiling []string) (*Microvm, *launchertest.Host, *launchertest.Server) {
	t.Helper()
	h := launchertest.NewHost()
	h.Pins = pins
	dir, digest := launchertest.Image(t, "img")
	cfg := launchertest.Config(digest, 2)
	cfg.Ceiling = ceiling
	srv := launchertest.Serve(t, h, cfg)
	box, err := NewMicrovm(&config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, ImagePath: dir, ActImage: "img"})
	if err != nil {
		t.Fatal(err)
	}
	m := box.(*Microvm)
	if err := m.SetOwner("0vd"); err != nil {
		t.Fatal(err)
	}
	return m, h, srv
}

var namedScope = []string{"tcp:198.51.100.20:8472", "tcp:archive.ubuntu.com:80", "tcp:bootstrap.urbit.org:443", "tcp:archive.ubuntu.com:443"}

func namedSpec(attempt string) Spec {
	return Spec{Attempt: attempt, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, Deadline: time.Now().Add(time.Hour), Profile: "egress", Destinations: namedScope}
}

var goodPins = []launcher.Pin{
	{Name: "archive.ubuntu.com", Addrs: []string{"91.189.91.82", "185.125.190.81"}},
	{Name: "bootstrap.urbit.org", Addrs: []string{"104.21.64.85"}},
}

func TestPreparedSandboxKeepsWhatTheLauncherPinned(t *testing.T) {
	m, _, srv := pinnedMicrovm(t, goodPins, namedScope)
	h, err := m.Prepare(context.Background(), namedSpec("0v1.att"))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got := m.Pinned(h)
	if !slices.EqualFunc(got, goodPins, func(a, b launcher.Pin) bool { return a.Name == b.Name && slices.Equal(a.Addrs, b.Addrs) }) {
		t.Fatalf("THE SANDBOX'S PINS ARE NOT THE LAUNCHER'S ANSWER: %+v", got)
	}
	// a copy: changing it changes nothing kept
	got[0].Addrs[0] = "1.1.1.1"
	if m.Pinned(h)[0].Addrs[0] != "91.189.91.82" {
		t.Fatal("Pinned answered the backend's own slice")
	}
	if err := m.Destroy(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if m.Pinned(h) != nil {
		t.Fatalf("a destroyed sandbox still has pins: %+v", m.Pinned(h))
	}
	if held := srv.Held(t, "0vd"); len(held) != 0 {
		t.Fatalf("held after destroy: %+v", held)
	}
}

// A locked sandbox, and one granted only literals, have no pins.
func TestNoNameNoPins(t *testing.T) {
	m, _, _ := pinnedMicrovm(t, nil, []string{"tcp:198.51.100.20:8472"})
	h, err := m.Prepare(context.Background(), Spec{Attempt: "0v1.att", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, Deadline: time.Now().Add(time.Hour), Profile: "egress", Destinations: []string{"tcp:198.51.100.20:8472"}})
	if err != nil {
		t.Fatal(err)
	}
	if p := m.Pinned(h); p != nil {
		t.Fatalf("literals only: %+v", p)
	}
	h2, err := m.Prepare(context.Background(), Spec{Attempt: "0v2.att", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, Deadline: time.Now().Add(time.Hour), Profile: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if p := m.Pinned(h2); p != nil {
		t.Fatalf("locked: %+v", p)
	}
}

// An answer that is not exactly the granted names, each with distinct
// IPv4 addresses, is not this reservation's: the VM is rolled back and
// nothing stays held.
func TestAWrongPinAnswerRollsTheVMBack(t *testing.T) {
	for name, pins := range map[string][]launcher.Pin{
		"a name missing":     goodPins[:1],
		"a name not granted": append(slices.Clone(goodPins), launcher.Pin{Name: "evil.example", Addrs: []string{"203.0.113.9"}}),
		"no answer at all":   nil,
		"out of order":       {goodPins[1], goodPins[0]},
		"no addresses":       {{Name: "archive.ubuntu.com"}, goodPins[1]},
		"an IPv6 address":    {{Name: "archive.ubuntu.com", Addrs: []string{"2620:2d:4000:1::16"}}, goodPins[1]},
		"a padded address":   {{Name: "archive.ubuntu.com", Addrs: []string{"091.189.91.82"}}, goodPins[1]},
		"a repeated address": {{Name: "archive.ubuntu.com", Addrs: []string{"91.189.91.82", "91.189.91.82"}}, goodPins[1]},
		"not an address":     {{Name: "archive.ubuntu.com", Addrs: []string{"archive.ubuntu.com"}}, goodPins[1]},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, srv := pinnedMicrovm(t, pins, namedScope)
			_, err := m.Prepare(context.Background(), namedSpec("0v1.att"))
			if err == nil || !strings.Contains(err.Error(), "boot") {
				t.Fatalf("A WRONG PIN ANSWER WAS ACCEPTED: %v", err)
			}
			if held := srv.Held(t, "0vd"); len(held) != 0 {
				t.Fatalf("the refused VM is still held: %+v", held)
			}
		})
	}
}

func TestCheckPinnedNamesOnlyTheGrantedNames(t *testing.T) {
	if err := checkPinned(namedScope, goodPins); err != nil {
		t.Fatal(err)
	}
	if err := checkPinned([]string{"tcp:198.51.100.20:8472", "udp:[2001:db8::1]:53"}, nil); err != nil {
		t.Fatalf("literals pin nothing: %v", err)
	}
	if err := checkPinned([]string{"tcp:198.51.100.20:8472"}, goodPins); err == nil {
		t.Fatal("PINS FOR A LITERALS-ONLY SCOPE WERE ACCEPTED")
	}
	pinOf := func(n int) launcher.Pin {
		p := launcher.Pin{Name: "archive.ubuntu.com"}
		for i := 1; i <= n; i++ {
			p.Addrs = append(p.Addrs, fmt.Sprintf("91.189.91.%d", i))
		}
		return p
	}
	if err := checkPinned([]string{"tcp:archive.ubuntu.com:80"}, []launcher.Pin{pinOf(64)}); err != nil {
		t.Fatalf("64 addresses: %v", err)
	}
	if err := checkPinned([]string{"tcp:archive.ubuntu.com:80"}, []launcher.Pin{pinOf(65)}); err == nil {
		t.Fatal("65 ADDRESSES WERE ACCEPTED")
	}
}
