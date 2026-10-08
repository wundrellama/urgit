package daemon

// The daemon's reconcile takes the launcher's list as its proof that a held
// orphan is gone. A launcher whose inventory is partial — an entry it could
// not account for, the orphan's own record or another — refuses that list
// (runner/launcher/INTEGRATION.md §11.7; independent review 02): the
// reconcile fails and frees no held slot, through the real Microvm backend
// and the launcher's real core, wire and client, the launcher restarted
// over the same private files. With every entry accounted for, a released
// orphan's absence frees its slot.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"urgit/runner/internal/launcher"
)

func TestAPartialInventoryFreesNoHeldSlot(t *testing.T) {
	for _, c := range []struct {
		name     string
		released bool   // the orphan's launcher record was released first
		spoil    string // the attempt whose launcher record is spoiled ("" none)
		content  string
	}{
		{"its own launcher record truncated", false, "0v5.att", "{"},
		{"another launcher record of an unsupported format, its own released", true, "0v6.att", `{"format": 99}`},
		{"its own released, every entry accounted for (control)", true, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newVMFixture(t, 2)
			rec := f.orphanOf(t, "0v5.att")
			f.ship.status["0v5.att"] = "running"
			d := f.start(t)
			defer closeDaemon(d)
			if err := d.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := d.free()
			if before != 1 {
				t.Fatalf("fixture: the orphan is held: free %d (want 1 of 2)", before)
			}
			if c.released {
				cl, err := launcher.Dial(context.Background(), f.srv.Socket, vmDaemon)
				if err != nil {
					t.Fatal(err)
				}
				_, err = cl.DestroyOf(rec.Ref())
				cl.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if c.spoil != "" {
				p := filepath.Join(f.srv.StateDir(), "attempts", launcher.IDFor("t", c.spoil)+".json")
				if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			f.srv.Restart(t)
			if c.spoil != "" && len(f.srv.Service.Problems()) == 0 {
				t.Fatal("fixture: the spoiled record is unaccounted for")
			}
			err := d.Reconcile(context.Background())
			if c.spoil == "" {
				if err != nil || d.free() != before+1 {
					t.Fatalf("a released orphan, every entry accounted for: free %d, want %d (reconcile: %v)", d.free(), before+1, err)
				}
				return
			}
			if d.free() != before || err == nil {
				t.Fatalf("A PARTIAL INVENTORY FREED A HELD SLOT: free %d, then %d (reconcile: %v)", before, d.free(), err)
			}
		})
	}
}
