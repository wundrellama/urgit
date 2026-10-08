package sandbox

// The Microvm proves a sandbox's release only by the launcher's durable
// evidence of exactly its incarnation (runner/launcher/INTEGRATION.md
// §11.8): released — proven; still held — not; its record gone without
// evidence (the fixture's own file removed between two launcher lives) —
// not. Through the real Microvm and the launcher's real core, wire and
// client.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

func TestMicrovmProvesAReleaseOnlyByItsEvidence(t *testing.T) {
	h := launchertest.NewHost()
	dir, digest := launchertest.Image(t, "img")
	srv := launchertest.Serve(t, h, launchertest.Config(digest, 4))
	box, err := NewMicrovm(&config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, ImagePath: dir, ActImage: "img"})
	if err != nil {
		t.Fatal(err)
	}
	m := box.(*Microvm)
	if err := m.SetOwner("0vd"); err != nil {
		t.Fatal(err)
	}
	cl, err := launcher.Dial(context.Background(), srv.Socket, "0vd")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	reserve := func(attempt string) Handle {
		r, err := cl.Reserve(launcher.ReserveRequest{Attempt: attempt, Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked"})
		if err != nil {
			t.Fatal(err)
		}
		return Handle{ID: "ci-" + attempt, Attempt: attempt, VM: r.ID, Incarnation: r.Incarnation, CID: r.CID, Created: r.Created}
	}
	released, heldNow, gone := reserve("0v1.att"), reserve("0v2.att"), reserve("0v3.att")
	if _, err := cl.DestroyOf(launcher.Ref{ID: released.VM, Incarnation: released.Incarnation, CID: released.CID, Created: released.Created}); err != nil {
		t.Fatal(err)
	}
	if err := m.ProveReleased(context.Background(), released); err != nil {
		t.Fatalf("a release the launcher proves: %v", err)
	}
	if err := m.ProveReleased(context.Background(), heldNow); err == nil {
		t.Fatal("A HELD SANDBOX WAS PROVEN RELEASED")
	}
	if err := os.Remove(filepath.Join(srv.StateDir(), "attempts", gone.VM+".json")); err != nil {
		t.Fatal(err)
	}
	srv.Restart(t)
	// the launcher's ErrUnproven, as its reply carries it (text)
	if err := m.ProveReleased(context.Background(), gone); err == nil || !strings.Contains(err.Error(), launcher.ErrUnproven.Error()) {
		t.Fatalf("A SANDBOX GONE WITHOUT EVIDENCE WAS PROVEN RELEASED: %v", err)
	}
}
