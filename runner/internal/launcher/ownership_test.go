package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// plantForeign puts, where the model host keeps op's object for id, an
// object this reservation did not create, and returns the path whose
// content proves it untouched.
func plantForeign(t *testing.T, h *modelHost, op, id string, index int) string {
	t.Helper()
	switch op {
	case "prepare", "jail":
		dir := h.marker("jail", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "foreign")
		if err := os.WriteFile(p, []byte("not this reservation's"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	case "cgroup":
		p := h.marker("cgroup", id)
		if err := os.WriteFile(p, []byte("not this reservation's"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	case "network":
		// the model's removal checks the index; the planted object carries
		// the one the reservation is given, so only ownership can protect it
		p := h.marker("network", id)
		if err := os.WriteFile(p, []byte(strconv.Itoa(index)), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Fatalf("no such object %s", op)
	return ""
}

// A create step that reports it had no effect — the real adapter does
// when the attempt's jail, cgroup or network objects already exist
// (INTEGRATION.md §7.3) — created nothing the rollback may remove: the
// objects stay, the reservation is released. And a teardown of a record
// that holds nothing removes nothing at all.
func TestNoEffectCreateStepsLeavePreexistingObjectsAlone(t *testing.T) {
	for _, op := range []string{"prepare", "cgroup", "network"} {
		t.Run(op, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, netReq("0v1"))
			sentinel := plantForeign(t, h, op, r.ID, MinNetIndex)
			h.fail(op, "noeffect", errInjected)
			_, err := s.Create(owner1, r.ID)
			if !errors.Is(err, errInjected) || errors.Is(err, ErrQuarantined) {
				t.Fatalf("create: %v", err)
			}
			if _, serr := os.Stat(sentinel); serr != nil {
				tr.dump(t)
				t.Fatalf("A PREEXISTING %s WAS REMOVED by the rollback of a step that created nothing: %v", op, serr)
			}
			if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
				t.Fatalf("not released: %v", used(s))
			}
		})
	}
	t.Run("nothing held", func(t *testing.T) {
		tr := &tracer{}
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
		sentinel := plantForeign(t, h, "jail", r.ID, 0)
		if err := s.Destroy(owner1, r.ID); err != nil {
			t.Fatalf("destroy: %v", err)
		}
		if _, err := os.Stat(sentinel); err != nil {
			tr.dump(t)
			t.Fatalf("A TEARDOWN OF A RECORD THAT HOLDS NOTHING REMOVED A JAIL: %v", err)
		}
		if _, ok := held(s, r.ID); ok {
			t.Fatal("not released")
		}
	})
}
