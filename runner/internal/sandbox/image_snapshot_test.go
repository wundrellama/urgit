package sandbox

// Independent review 12, R12-2 (runner/launcher/INTEGRATION.md §11.16):
// each Prepare boots the image it verified. The backend verifies its image
// directory at start and before every Prepare (M10). Two Prepares at once —
// a runner of capacity 2 — must not share one verification: a reserve names
// its own Prepare's verified digest, whatever another Prepare verified in
// between. And every Prepare checks all that the start checks: the kernel
// and rootfs digests against the manifest, the guest helper's protocol and
// the act image it preloads — so an image changed in place is booted only
// if it passes them all.
//
// The scheduling is forced, not hoped for: a launcher on a private socket
// holds the first Prepare's hello — after its verification, before its
// reserve — until the image has changed and a second Prepare has verified
// it and reserved. No test is serialized, and -race stays on.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/guest"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

// gated is a scripted launcher whose hellos pass through hold before they
// are answered: a hold that blocks keeps that connection's Prepare waiting
// between its verification and its reserve.
type gated struct {
	mu   sync.Mutex
	seen []launcher.Request
}

func serveGated(t *testing.T, hold func(launcher.Request), answer func(launcher.Request) launcher.Reply) (*gated, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "g.sock")
	if err != nil {
		t.Fatal(err)
	}
	g := &gated{}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		l.Close()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req launcher.Request
					if json.Unmarshal(line, &req) != nil {
						return
					}
					g.mu.Lock()
					g.seen = append(g.seen, req)
					g.mu.Unlock()
					reply := launcher.Reply{OK: true, Protocol: launcher.WireProtocol}
					if req.Op == "hello" {
						hold(req)
					} else {
						reply = answer(req)
					}
					data, _ := json.Marshal(reply)
					if _, err := c.Write(append(data, '\n')); err != nil {
						return
					}
				}
			}()
		}
	}()
	return g, "g.sock"
}

func (g *gated) reserves() map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]string{}
	for _, r := range g.seen {
		if r.Op == "reserve" {
			out[r.Attempt] = r.Image
		}
	}
	return out
}

// noRoom refuses every reserve, and settles each refused request closed: a
// Prepare that reached its reserve ends there, having named its image.
func noRoom(req launcher.Request) launcher.Reply {
	switch req.Op {
	case "reserve":
		return launcher.Reply{Error: "no room for this test's guest", Request: req.Request}
	case "settle":
		return launcher.Reply{OK: true, Settled: launcher.SettledClosed, SettledWhy: "never admitted; closed by this settlement"}
	}
	return launcher.Reply{Error: "unexpected " + req.Op}
}

// writeImage writes into dir an image whose manifest's digests match its
// kernel and rootfs, as launchertest.Image does, with this kernel, act
// image and helper protocol; it returns the manifest's sha256.
func writeImage(t *testing.T, dir, kernel, actImage string, helper int) string {
	t.Helper()
	write := func(name, data string) string {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(data))
		return hex.EncodeToString(sum[:])
	}
	k, r := write("vmlinux", kernel), write("rootfs.ext4", "rootfs")
	data, _ := json.Marshal(map[string]any{
		"version":   1,
		"kernel":    map[string]any{"name": "vmlinux", "sha256": k},
		"rootfs":    map[string]any{"name": "rootfs.ext4", "sha256": r, "baseline_mib": 1},
		"act_image": map[string]any{"reference": actImage},
		"helper":    map[string]any{"version": helper},
		"boot_args": "console=ttyS0",
	})
	write("manifest.json", string(data))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gatedMicrovm(t *testing.T, hold func(launcher.Request)) (*Microvm, *gated, string, string) {
	t.Helper()
	dir, digest := launchertest.Image(t, "img")
	g, socket := serveGated(t, hold, noRoom)
	box, err := NewMicrovm(&config.Config{Sandbox: "microvm", LauncherSocket: socket, ImagePath: dir, ActImage: "img"})
	if err != nil {
		t.Fatal(err)
	}
	m := box.(*Microvm)
	if err := m.SetOwner("0vd"); err != nil {
		t.Fatal(err)
	}
	return m, g, dir, digest
}

func specFor(attempt string) Spec {
	return Spec{Attempt: attempt, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, Deadline: time.Now().Add(time.Hour), Profile: "locked"}
}

// Two Prepares at once, the image changed in place between their
// verifications: each reserve names the image its own Prepare verified.
func TestConcurrentPreparesEachReserveTheImageTheyVerified(t *testing.T) {
	var armed atomic.Bool
	reached, release := make(chan struct{}), make(chan struct{})
	m, g, dir, first := gatedMicrovm(t, func(req launcher.Request) {
		if armed.CompareAndSwap(true, false) {
			close(reached)
			<-release
		}
	})
	// a test that ends early lets the held hello go first, so that the
	// launcher's cleanup (registered before this) finds nothing held
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	t.Cleanup(letGo)
	armed.Store(true)
	done := make(chan error, 1)
	go func() {
		_, err := m.Prepare(context.Background(), specFor("0v1.att"))
		done <- err
	}()
	select {
	case <-reached: // the first Prepare verified the image, and waits for its launcher
	case <-time.After(10 * time.Second):
		t.Fatal("fixture: the first Prepare never reached the launcher")
	}
	second := writeImage(t, dir, "kernel 2", "img", guest.ProtocolVersion)
	if second == first {
		t.Fatal("fixture: the image did not change")
	}
	_, errB := m.Prepare(context.Background(), specFor("0v2.att"))
	letGo()
	var errA error
	select {
	case errA = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fixture: the first Prepare never returned")
	}
	for _, err := range []error{errA, errB} {
		if err == nil || !strings.Contains(err.Error(), "no room for this test's guest") {
			t.Fatalf("fixture: each Prepare ends at its reserve, refused: %v, %v", errA, errB)
		}
	}
	got := g.reserves()
	if got["0v1.att"] != first || got["0v2.att"] != second {
		t.Fatalf("A PREPARE RESERVED AN IMAGE ITS OWN VERIFICATION NEVER SAW: the first verified %s and reserved %s; the second verified %s and reserved %s",
			first[:12], got["0v1.att"], second[:12], got["0v2.att"])
	}
}

// An image changed in place after the start is booted only if it passes
// every check the start makes; a tampered one never is. Nothing is reserved
// for an image refused.
func TestAPrepareChecksAllThatTheStartChecks(t *testing.T) {
	for _, c := range []struct {
		name, want string
		change     func(t *testing.T, dir string)
	}{
		{"another helper protocol", "helper speaks protocol", func(t *testing.T, dir string) {
			writeImage(t, dir, "kernel 2", "img", guest.ProtocolVersion+1)
		}},
		{"another act image", "is not the image the guest preloads", func(t *testing.T, dir string) {
			writeImage(t, dir, "kernel 2", "other/act:latest", guest.ProtocolVersion)
		}},
		{"a tampered rootfs", "refusing to boot it", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "rootfs.ext4"), []byte("rootfs, tampered"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, g, dir, _ := gatedMicrovm(t, func(launcher.Request) {})
			c.change(t, dir)
			_, err := m.Prepare(context.Background(), specFor("0v1.att"))
			if !errors.Is(err, ErrMicrovmUnavailable) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("AN IMAGE CHANGED IN PLACE WAS NOT REFUSED (%s): %v", c.name, err)
			}
			if got := g.reserves(); len(got) != 0 {
				t.Fatalf("A RESERVE WAS SENT FOR AN IMAGE CHANGED IN PLACE (%s): %v", c.name, got)
			}
		})
	}
}

// An image changed in place that passes every check is the image the next
// Prepare boots — M10's verification before every Prepare — and the one
// the backend then names.
func TestAnImageChangedInPlaceThatPassesIsTheOneBooted(t *testing.T) {
	m, g, dir, first := gatedMicrovm(t, func(launcher.Request) {})
	second := writeImage(t, dir, "kernel 2", "img", guest.ProtocolVersion)
	if _, err := m.Prepare(context.Background(), specFor("0v1.att")); err == nil || !strings.Contains(err.Error(), "no room for this test's guest") {
		t.Fatalf("fixture: the Prepare ends at its reserve, refused: %v", err)
	}
	if got := g.reserves()["0v1.att"]; got != second || second == first {
		t.Fatalf("THE PREPARE DID NOT BOOT THE IMAGE IT VERIFIED: reserved %s, the image now %s (at start %s)", got, second[:12], first[:12])
	}
	if !strings.Contains(m.Name(), second[:12]) {
		t.Fatalf("THE BACKEND DOES NOT NAME THE IMAGE IT LAST VERIFIED: %s", m.Name())
	}
}
