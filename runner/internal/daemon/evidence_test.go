package daemon

// A held orphan's slot is freed when the launcher proves its release by
// its durable evidence (runner/launcher/INTEGRATION.md §11.8) — never when
// it is merely missing from the launcher's list: a record gone without
// evidence (here, the fixture's own file removed between two launcher
// lives, its VM still modelled running) keeps its slot withheld. Through
// the real Microvm backend and the launcher's real core, wire and client.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"urgit/runner/internal/launcher"
)

func TestAHeldOrphanIsFreedOnlyOnEvidence(t *testing.T) {
	for _, c := range []struct {
		name  string
		proof bool // the launcher released it, with its evidence
	}{{"released by the launcher (control)", true}, {"its record gone without evidence", false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			rec := f.orphanOf(t, "0v5.att")
			f.ship.status["0v5.att"] = "running"
			d := f.start(t)
			defer closeDaemon(d)
			if err := d.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if d.free() != 1 {
				t.Fatalf("fixture: the orphan is held: free %d", d.free())
			}
			if c.proof {
				cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
				if err != nil {
					t.Fatal(err)
				}
				_, err = cl.DestroyOf(rec.Ref())
				cl.Close()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(filepath.Join(f.srv.StateDir(), "attempts", rec.ID+".json")); err != nil {
					t.Fatal(err)
				}
				f.srv.Restart(t)
			}
			if err := d.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if c.proof {
				if d.free() != 2 {
					t.Fatalf("a held orphan whose release the launcher proves: free %d, want 2", d.free())
				}
				return
			}
			if d.free() != 1 {
				t.Fatalf("A HELD SLOT WAS FREED WITHOUT EVIDENCE: free %d, want 1", d.free())
			}
		})
	}
}
