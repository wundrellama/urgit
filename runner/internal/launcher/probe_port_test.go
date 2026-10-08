package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestComparisonReservationRequiresDurableRecordPort is the independent
// probe (.scratch/repair/reference/probe_test.go, kept byte-identical
// there with its RED evidence) ported to this package. Same fixture: a
// private directory, a nil Host, only Reserve and NewService, and a real
// kernel fault — a directory at the record's temporary path, so the
// write the publication needs is refused. Same invariant: a reservation
// is acknowledged only with a readable durable record, and a restart
// neither loses an acknowledged charge nor invents an unacknowledged one.
//
// Two differences from the original, both deliberate:
//   - the first Service is closed before the reopen. The original reopens
//     while the first one is still open; a Service now owns its state
//     directory exclusively, so that open is refused with ErrStateBusy
//     (proven on its own by TestSecondOpenerIsRefusedWhileTheStateIsHeld).
//     Closing first is what a restart is.
//   - after the fault the reopened Service is fenced: the directory in a
//     publication path is state it cannot account for. That stronger
//     refusal at open is asserted explicitly, after and independently of
//     the probe's own assertion.
func TestComparisonReservationRequiresDurableRecordPort(t *testing.T) {
	for _, block := range []bool{false, true} {
		name := "control"
		if block {
			name = "failed-publication"
		}
		t.Run(name, func(t *testing.T) {
			cfg := Config{
				StateDir: t.TempDir(), IDPrefix: "comparison",
				Images:     map[string]Image{"img": {Manifest: "img", BaselineMiB: 1}},
				BudgetCPUs: 2, BudgetMemoryMiB: 8192, MaxGuests: 1,
			}
			s, err := NewService(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			owner := Owner{UID: 1000, Daemon: "comparison"}
			req := ReserveRequest{Attempt: "attempt-a", Image: "img", CPUs: 1,
				MemoryMiB: 128, DiskMiB: 1, DeadlineUnix: time.Now().Add(time.Minute).Unix(), Network: "locked"}
			id := IDFor(cfg.IDPrefix, req.Attempt)
			dst := filepath.Join(cfg.StateDir, "attempts", id+".json")
			if block {
				if err := os.Mkdir(dst+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
				// Require the intended real filesystem fault, not a fake callback.
				if err := os.WriteFile(dst+".tmp", []byte("probe"), 0o600); err == nil {
					t.Fatal("fault setup did not refuse a file write onto a directory")
				}
			}
			reply, reserveErr := s.Reserve(owner, req)
			liveGuests := used(s)[2]
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, openErr := NewService(cfg, nil)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer reopened.Close()
			_, retained := reopened.vms[id]
			reopenedGuests := used(reopened)[2]
			if block {
				// Rename can publish the blocking directory at the JSON name.
				// That is not a readable record; restart must not lose it silently.
				if st, err := os.Stat(dst); err == nil && st.Mode().IsRegular() {
					t.Fatal("fault unexpectedly published a regular record")
				}
				t.Logf("fault reached; reserve_error=%v returned_id=%q retained_after_reopen=%v live_guests=%d reopened_guests=%d", reserveErr, reply.ID, retained, liveGuests, reopenedGuests)
				if reserveErr == nil {
					t.Fatal("reservation acknowledged despite failed record publication; reopen loses its capacity charge")
				}
				// the refusal is the publication's own kernel error; nothing
				// was acknowledged, charged or moved
				if !errors.Is(reserveErr, ErrNotDurable) || !errors.Is(reserveErr, syscall.EISDIR) || reply.ID != "" {
					t.Fatalf("refusal is not the publication's EISDIR: %v (reply %+v)", reserveErr, reply)
				}
				if liveGuests != 0 || retained || reopenedGuests != 0 {
					t.Fatalf("an unacknowledged reservation is charged: live %d, reopened %d, retained %v", liveGuests, reopenedGuests, retained)
				}
				if st, err := os.Lstat(dst + ".tmp"); err != nil || !st.IsDir() {
					t.Fatalf("the blocking directory was moved or removed: %v", err)
				}
				if _, err := os.Lstat(dst); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("something occupies the record path: %v", err)
				}
				// the stronger refusal at open, on its own terms: the reopened
				// service names the directory and admits nothing new
				probs := reopened.Problems()
				if len(probs) != 1 || probs[0].Path != dst+".tmp" || probs[0].Kind != "not-regular" {
					t.Fatalf("expected one not-regular problem at %s, got %+v", dst+".tmp", probs)
				}
				other := req
				other.Attempt = "attempt-b"
				if _, err := reopened.Reserve(owner, other); !errors.Is(err, ErrUnsafeState) || !strings.Contains(err.Error(), dst+".tmp") {
					t.Fatalf("a fenced service admitted or refused for another reason: %v", err)
				}
			} else {
				if reserveErr != nil || reply.ID != id || !retained {
					t.Fatalf("control: reserve=%v reply=%+v retained=%v", reserveErr, reply, retained)
				}
				if liveGuests != 1 || reopenedGuests != 1 || len(reopened.Problems()) != 0 {
					t.Fatalf("control: live %d reopened %d problems %+v", liveGuests, reopenedGuests, reopened.Problems())
				}
				if rec, ok := durable(t, cfg.StateDir, id); !ok || rec.State != StatePreparing || rec.Owner != owner || rec.Attempt != req.Attempt {
					t.Fatalf("control: durable record %+v %v", rec, ok)
				}
			}
		})
	}
}

// One Service owns a state directory; a second opener — in this process
// or another (TestProcessDeath/hold) — is refused until the first stops.
func TestSecondOpenerIsRefusedWhileTheStateIsHeld(t *testing.T) {
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, nil, nil)
	mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if _, err := NewService(cfg, nil); !errors.Is(err, ErrStateBusy) {
		t.Fatalf("a second open of a held state directory: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(owner1, lockedReq("0v2", 1, 128)); !errors.Is(err, ErrClosed) {
		t.Fatalf("a closed service admitted: %v", err)
	}
	s2, err := NewService(cfg, nil)
	if err != nil {
		t.Fatalf("open after close: %v", err)
	}
	defer s2.Close()
	if used(s2)[2] != 1 {
		t.Fatalf("the reservation did not survive the stop: %v", used(s2))
	}
}
