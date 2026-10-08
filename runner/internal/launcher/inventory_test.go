package launcher

// An inventory is authoritative only when it is complete (INTEGRATION.md
// §11.7; independent review 02). While any entry of the records directory
// is unaccounted for — malformed, unreadable, of an unsupported format, of
// another identity, not a regular file, foreign — or two loaded records
// share a cid, it is reported, and no absence is proof of a release: the
// owners' list, direct and on the wire, is refused; an unknown or stale
// destroy, an inspection, a create, stop or connect, and an operator's
// selection, retry or release are refused, never answered done or unknown;
// nothing is admitted; the open succeeds and the loaded records are still
// enforced; a reaper pass, or a healed barrier, lifts none of it; and every
// unaccounted entry is left exactly as found. The fixtures are private
// files, spoiled after the first life closed; the host is the nonexecuting
// model.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAPartialInventoryProvesNoAbsence(t *testing.T) {
	recordPath := func(cfg Config, id string) string { return filepath.Join(cfg.StateDir, "attempts", id+".json") }
	write := func(t *testing.T, p string, data []byte) {
		t.Helper()
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name  string
		own   bool // the unaccounted entry is the target's own record
		spoil func(t *testing.T, cfg Config, target string) string
	}{
		{"its own record truncated", true, func(t *testing.T, cfg Config, target string) string {
			p := recordPath(cfg, target)
			write(t, p, []byte("{"))
			return p
		}},
		{"its own record of an unsupported format", true, func(t *testing.T, cfg Config, target string) string {
			p := recordPath(cfg, target)
			write(t, p, []byte(`{"format": 99}`))
			return p
		}},
		{"its own record naming another identity", true, func(t *testing.T, cfg Config, target string) string {
			p := recordPath(cfg, target)
			data, err := json.MarshalIndent(recordFile{Format: recordFormat, Record: goodRecord(cfg, "0vimposter", minCID+50)}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			write(t, p, data)
			return p
		}},
		{"its own record not a regular file", true, func(t *testing.T, cfg Config, target string) string {
			p := recordPath(cfg, target)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"its own record unreadable", true, func(t *testing.T, cfg Config, target string) string {
			if os.Geteuid() == 0 {
				t.Skip("root reads a mode-0 file")
			}
			p := recordPath(cfg, target)
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"another record truncated, its own released", false, func(t *testing.T, cfg Config, target string) string {
			p := recordPath(cfg, IDFor(cfg.IDPrefix, "0vother"))
			write(t, p, []byte("{"))
			return p
		}},
		{"a foreign entry, its own released", false, func(t *testing.T, cfg Config, target string) string {
			p := filepath.Join(cfg.StateDir, "attempts", "notes.txt")
			write(t, p, []byte("an operator's note"))
			return p
		}},
		{"two records sharing a cid, its own released", false, func(t *testing.T, cfg Config, target string) string {
			// the later of two loaded records is reported: here the second
			// incarnation of 0vtwice, given the known guest's cid
			read := func(attempt string) Record {
				data, err := os.ReadFile(recordPath(cfg, IDFor(cfg.IDPrefix, attempt)))
				if err != nil {
					t.Fatal(err)
				}
				r, _, err := decodeRecord(data)
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			r := read("0vtwice")
			r.CID = read("0vknown").CID
			data, _, err := encodeRecord(cfg.IDPrefix, r)
			if err != nil {
				t.Fatal(err)
			}
			p := recordPath(cfg, r.ID)
			write(t, p, data)
			return p
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			cfg.MaxGuests = 4 // the target, a known guest and two incarnations of one attempt
			s := open(t, cfg, h, nil)
			target := mustReserve(t, s, owner1, lockedReq("0vtarget", 1, 128))
			known := mustReserve(t, s, owner1, lockedReq("0vknown", 1, 128))
			for _, r := range []ReserveReply{target, known} {
				if _, err := s.CreateOf(owner1, r.Ref()); err != nil {
					t.Fatal(err)
				}
			}
			first := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
			if err := s.DestroyOf(owner1, first.Ref()); err != nil {
				t.Fatal(err)
			}
			second := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
			targetRec, _ := held(s, target.ID)
			if !c.own {
				if err := s.DestroyOf(owner1, target.Ref()); err != nil { // definitely released
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			entry := c.spoil(t, cfg, target.ID)
			before := fileDigest(t, entry)
			s2, err := openService(cfg, h, osFS{})
			if err != nil {
				t.Fatalf("A PARTIAL INVENTORY LEFT THE LOADED RECORDS UNENFORCED: the open failed: %v", err)
			}
			closeAtEnd(t, s2)
			reported := false
			for _, p := range s2.Problems() {
				reported = reported || p.Path == entry
			}
			if !reported {
				t.Fatalf("AN UNACCOUNTED ENTRY WAS NOT REPORTED: %+v", s2.Problems())
			}
			if recs, err := s2.List(owner1); err == nil {
				t.Fatalf("THE OWNERS' LIST OF A PARTIAL INVENTORY WAS SERVED: %+v", recs)
			}
			if recs, err := listOver(t, s2); err == nil {
				t.Fatalf("THE OWNERS' LIST OF A PARTIAL INVENTORY WAS SERVED on the wire: %+v", recs)
			}
			if err := s2.DestroyOf(owner1, target.Ref()); err == nil {
				t.Fatal("AN ABSENCE FROM A PARTIAL INVENTORY WAS ANSWERED DONE")
			}
			if _, err := s2.Inspect(owner1, target.ID); err == nil || errors.Is(err, ErrUnknown) {
				t.Fatalf("AN ABSENCE FROM A PARTIAL INVENTORY WAS ANSWERED NO SUCH VM: %v", err)
			}
			_, createErr := s2.CreateOf(owner1, target.Ref())
			_, connectErr := s2.ConnectOf(owner1, target.Ref(), 1024)
			for op, err := range map[string]error{"create": createErr, "stop": s2.StopOf(owner1, target.Ref()), "connect": connectErr} {
				if err == nil || errors.Is(err, ErrUnknown) {
					t.Fatalf("AN ABSENCE FROM A PARTIAL INVENTORY WAS ANSWERED NO SUCH VM: its %s: %v", op, err)
				}
			}
			if err := s2.DestroyOf(owner1, first.Ref()); err == nil {
				t.Fatal("A STALE DESTROY WAS ANSWERED DONE ON A PARTIAL INVENTORY")
			}
			if _, err := s2.InspectIncident(targetRec.Selection()); err == nil || errors.Is(err, ErrUnknown) {
				t.Fatalf("AN OPERATOR'S SELECTION OF AN UNLOADED RECORD WAS ANSWERED UNKNOWN: %v", err)
			}
			if _, err := s2.RetryCleanup(targetRec.Selection()); err == nil || errors.Is(err, ErrUnknown) {
				t.Fatalf("AN OPERATOR'S SELECTION OF AN UNLOADED RECORD WAS ANSWERED UNKNOWN: its retry: %v", err)
			}
			if err := s2.Release(targetRec.Selection()); err == nil || errors.Is(err, ErrUnknown) {
				t.Fatalf("AN OPERATOR'S SELECTION OF AN UNLOADED RECORD WAS ANSWERED UNKNOWN: its release: %v", err)
			}
			if _, err := s2.Reserve(owner1, lockedReq("0vnew", 1, 128)); err == nil {
				t.Fatal("A RESERVATION WAS ADMITTED ON A PARTIAL INVENTORY")
			}
			// a loaded record is still enforced and answered for
			if r, ok := held(s2, second.ID); !ok || r.Incarnation != second.Incarnation {
				t.Fatalf("the loaded incarnation: %+v %v", r, ok)
			}
			kills := h.count("kill")
			if err := s2.DestroyOf(owner1, known.Ref()); err != nil || h.exists("vmm", known.ID) || h.count("kill") == kills {
				t.Fatalf("A PARTIAL INVENTORY LEFT THE LOADED RECORDS UNENFORCED: %v; vmm %v", err, h.exists("vmm", known.ID))
			}
			s2.ReapOnce()
			if after := fileDigest(t, entry); after != before {
				t.Fatalf("AN UNACCOUNTED ENTRY WAS ALTERED: %s, now %s", before, after)
			}
			// nothing but a new open that accounts for every entry lifts it
			if recs, err := s2.List(owner1); err == nil {
				t.Fatalf("A REAPER PASS LIFTED THE PARTIAL INVENTORY'S FENCE: %+v", recs)
			}
		})
	}
	// the records directory's barrier failing too (§11.6): its retry
	// succeeds, and the partial inventory stays fenced
	t.Run("a healed barrier keeps the fence of a partial inventory", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		fsys := &barrierFS{records: filepath.Join(cfg.StateDir, "attempts")}
		gone, known := withdrawnUncertainly(t, cfg, h, fsys)
		write(t, recordPath(cfg, IDFor(cfg.IDPrefix, "0vother")), []byte("{"))
		s, err := openService(cfg, h, fsys)
		if err != nil || !uncertified(s) {
			t.Fatalf("fixture: an uncertified, partial reopen: %v", err)
		}
		closeAtEnd(t, s)
		fsys.repair()
		s.ReapOnce() // its retry of the barrier succeeds
		if uncertified(s) {
			t.Fatalf("fixture: the barrier healed: %+v", s.Problems())
		}
		if recs, err := s.List(owner1); err == nil {
			t.Fatalf("A HEALED BARRIER LIFTED A PARTIAL INVENTORY'S FENCE: %+v", recs)
		}
		if err := s.DestroyOf(owner1, gone.Ref()); err == nil {
			t.Fatal("A HEALED BARRIER LIFTED A PARTIAL INVENTORY'S FENCE: an absence answered done")
		}
		if _, ok := held(s, known.ID); !ok {
			t.Fatal("the known guest was not loaded")
		}
	})
	// the controls: an authoritative inventory answers a definite absence
	t.Run("an authoritative inventory (control)", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, nil)
		target := mustReserve(t, s, owner1, lockedReq("0vtarget", 1, 128))
		first := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
		targetRec, _ := held(s, target.ID)
		for _, r := range []ReserveReply{target, first} {
			if err := s.DestroyOf(owner1, r.Ref()); err != nil {
				t.Fatal(err)
			}
		}
		second := mustReserve(t, s, owner1, lockedReq("0vtwice", 1, 128))
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s2 := open(t, cfg, h, nil)
		if recs, err := s2.List(owner1); err != nil || len(recs) != 1 || recs[0].Incarnation != second.Incarnation {
			t.Fatalf("the list of an authoritative inventory: %+v %v", recs, err)
		}
		if recs, err := listOver(t, s2); err != nil || len(recs) != 1 {
			t.Fatalf("the wire's list of an authoritative inventory: %+v %v", recs, err)
		}
		if err := s2.DestroyOf(owner1, target.Ref()); err != nil {
			t.Fatalf("a definite absence: %v", err)
		}
		if _, err := s2.Inspect(owner1, target.ID); !errors.Is(err, ErrUnknown) {
			t.Fatalf("a definite absence's inspection: %v", err)
		}
		if err := s2.DestroyOf(owner1, first.Ref()); err != nil {
			t.Fatalf("a stale destroy of a gone incarnation: %v", err)
		}
		if _, err := s2.InspectIncident(targetRec.Selection()); !errors.Is(err, ErrUnknown) {
			t.Fatalf("a released selection: %v", err)
		}
		if err := s2.Release(targetRec.Selection()); !errors.Is(err, ErrUnknown) {
			t.Fatalf("a released selection's release: %v", err)
		}
		if err := s2.StopOf(owner1, target.Ref()); !errors.Is(err, ErrUnknown) {
			t.Fatalf("a definite absence's stop: %v", err)
		}
		s2.ReapOnce()
		if _, err := s2.List(owner1); err != nil {
			t.Fatalf("the list after a reaper pass: %v", err)
		}
	})
}
