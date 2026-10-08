package sandbox

// A reserve whose answer is lost is settled by the launcher's list
// (recoverReservation). A launcher whose inventory is partial — an entry it
// could not account for, the reservation's own record or another — refuses
// that list (runner/launcher/INTEGRATION.md §11.7; independent review 02):
// the attempt is retained without a launcher identity, never taken for
// nothing reserved, through the real Microvm and the launcher's real core,
// wire and client, the launcher restarted over the same private files.
// With every entry accounted for, the list settles it.
//
// Policy-dependent (settled-admission ruling 01; INTEGRATION.md §11.10):
// the lost answer is now settled by its request (settle), not by the list,
// so the handle names the request its reserve made — the fixture's reserve
// answer carries it; a fresh one when nothing was reserved — and nothing
// reserved is "never admitted", where it was "holds no reservation". A
// partial inventory refuses the settlement as it refused the list.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

func TestALostReserveOnAPartialInventoryIsRetained(t *testing.T) {
	for _, c := range []struct {
		name     string
		reserved bool   // the reserve whose answer was lost made its reservation
		spoil    string // the attempt whose launcher record is spoiled ("" none)
		content  string
	}{
		{"its own record truncated", true, "0v1.att", "{"},
		{"another record of an unsupported format, nothing reserved", false, "0v2.att", `{"format": 99}`},
		{"reserved, every entry accounted for (control)", true, "", ""},
		{"nothing reserved, every entry accounted for (control)", false, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := launchertest.NewHost()
			dir, digest := launchertest.Image(t, "img")
			srv := launchertest.Serve(t, h, launchertest.Config(digest, 2))
			box, err := NewMicrovm(&config.Config{Sandbox: "microvm", LauncherSocket: srv.Socket, ImagePath: dir, ActImage: "img"})
			if err != nil {
				t.Fatal(err)
			}
			m := box.(*Microvm)
			if err := m.SetOwner("0vd"); err != nil {
				t.Fatal(err)
			}
			request, err := launcher.NewRequest()
			if err != nil {
				t.Fatal(err)
			}
			if c.reserved {
				cl, err := launcher.Dial(context.Background(), srv.Socket, "0vd")
				if err != nil {
					t.Fatal(err)
				}
				_, err = cl.Reserve(launcher.ReserveRequest{Attempt: "0v1.att", Image: digest, CPUs: 1, MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Hour).Unix(), Network: "locked", Request: request})
				cl.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if c.spoil != "" {
				p := filepath.Join(srv.StateDir(), "attempts", launcher.IDFor("t", c.spoil)+".json")
				if err := os.WriteFile(p, []byte(c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			srv.Restart(t)
			if c.spoil != "" && len(srv.Service.Problems()) == 0 {
				t.Fatal("fixture: the spoiled record is unaccounted for")
			}
			_, err = m.recoverReservation(Handle{ID: "ci-0v1.att", Attempt: "0v1.att", Request: request}, errors.New("the reserve's answer was lost"))
			var kept *RetainedError
			switch {
			case c.spoil != "":
				if !errors.As(err, &kept) || kept.Handle.VM != "" || kept.Handle.Attempt != "0v1.att" || kept.Handle.ID != "ci-0v1.att" {
					t.Fatalf("A LOST RESERVE ON A PARTIAL INVENTORY WAS TAKEN FOR NOTHING RESERVED: %v", err)
				}
			case c.reserved:
				if err == nil || errors.As(err, &kept) || len(srv.Held(t, "0vd")) != 0 {
					t.Fatalf("a lost reserve, every entry accounted for: rolled back by its incarnation: %v (held %+v)", err, srv.Held(t, "0vd"))
				}
			default:
				if err == nil || errors.As(err, &kept) || !strings.Contains(err.Error(), "never admitted") {
					t.Fatalf("nothing reserved, every entry accounted for: %v", err)
				}
			}
		})
	}
}
