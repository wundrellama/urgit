package launcher

// Recovery ruling A (runner/launcher/INTEGRATION.md §8): a quarantine is an
// incident; cleanup — automatic, or the operator's retry — never releases
// it; release is a separate operator action on the exact inspected
// incarnation and revision, after resolution, with its evidence kept.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// FindVMMs is the model's VMMFinder: the VMM marker of id, if there is
// one (a fault on "find" fails the scan).
func (h *modelHost) FindVMMs(id string) ([]int, error) {
	if err := h.enter("find", id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(h.marker("vmm", id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return nil, err
	}
	return []int{pid}, nil
}

// noFinder is a host that cannot look for a VMM of unknown pid.
type noFinder struct{ Host }

const testLabel = "owner/repo · ci.yml · build"

// selectionOf is the selection an inspection of rec would show.
func selectionOf(rec Record) Selection {
	return rec.Selection()
}

// quarantinedByDisk leaves a created record quarantined: its destroy's disk
// removal fails (the fault is healed afterwards), so it still holds its
// disk and jail.
func quarantinedByDisk(t *testing.T, s *Service, h *modelHost, attempt string) Record {
	t.Helper()
	req := lockedReq(attempt, 1, 128)
	req.Label = testLabel
	r := mustReserve(t, s, owner1, req)
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.fail("rmdisk", "before", errInjected)
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: the destroy quarantines: %v", err)
	}
	h.heal("rmdisk")
	rec, ok := held(s, r.ID)
	if !ok || rec.State != StateQuarantined {
		t.Fatalf("fixture: quarantined %+v %v", rec, ok)
	}
	return rec
}

func inspect(t *testing.T, s *Service, rec Record) Inspection {
	t.Helper()
	in, err := s.InspectIncident(selectionOf(rec))
	if err != nil {
		t.Fatalf("inspect %s: %v", rec.ID, err)
	}
	return in
}

// A cleanup obligation that was missed — here the reaper reaches a record a
// minute past its deadline plus its allowance, as after a launcher that was
// not running — is an incident. The late cleanup still stops and removes
// everything the record owns, but the record stays quarantined and charged:
// its incident records the trigger, the missed deadline and the attempt,
// and only the operator's separate release returns the capacity. An
// owner's destroy asked after the deadline is answered the same way:
// quarantined, never a release its daemon would free a slot for.
func TestMissedObligationStaysChargedAfterLateCleanup(t *testing.T) {
	t.Run("the reaper", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		s := open(t, cfg, h, nil)
		var mu sync.Mutex
		var got []Outcome
		s.SetReport(func(o Outcome) {
			mu.Lock()
			got = append(got, o)
			mu.Unlock()
		})
		r := mustReserve(t, s, owner1, expiring("0v1", 1, 128))
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		clock.advance(expire)
		s.ReapOnce()
		rec, ok := held(s, r.ID)
		if !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
			t.Fatalf("A LATE CLEANUP RELEASED THE CAPACITY: held %v, record %+v, used %v", ok, rec, used(s))
		}
		if len(h.leaks(r.ID)) != 0 || rec.holds() {
			t.Fatalf("the late cleanup did not stop and remove what the record owns: leaks %v, record %+v", h.leaks(r.ID), rec)
		}
		inc := rec.Incident
		if inc == nil || !inc.Missed || inc.Due == 0 || !strings.Contains(inc.Trigger, "deadline") || inc.Count != 1 || len(inc.Attempts) != 1 ||
			inc.Attempts[0].Result != "resolved" || inc.Attempts[0].By != "late recovery" {
			t.Fatalf("THE MISSED OBLIGATION IS NOT RECORDED: %+v", inc)
		}
		if d, ok := durable(t, cfg.StateDir, r.ID); !ok || d.State != StateQuarantined || d.Incident == nil || !d.Incident.Missed {
			t.Fatalf("the incident is not durable: %+v %v", d, ok)
		}
		mu.Lock()
		reports := append([]Outcome(nil), got...)
		mu.Unlock()
		if len(reports) != 1 || reports[0].Kind != "quarantined" || !reports[0].Late {
			t.Fatalf("the late recovery's report: %+v", reports)
		}
		if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("the owner's destroy of the incident: %v", err)
		}
		// a cleanup retry after it is recovery too: the incident keeps the
		// original trigger and missed deadline, the retry one attempt more
		if _, err := s.RetryCleanup(selectionOf(rec)); err != nil {
			t.Fatal(err)
		}
		in := inspect(t, s, rec)
		if got := in.Record.Incident; got == nil || !got.Missed || got.Due != inc.Due || got.Trigger != inc.Trigger || got.Count != 2 || in.Record.CleanupDue != rec.CleanupDue {
			t.Fatalf("A RETRY REWROTE THE MISSED OBLIGATION: %+v, was %+v", got, inc)
		}
		if !in.Releasable || used(s)[2] != 1 {
			t.Fatalf("a resolved incident is not releasable, or the retry released it: %+v used %v", in, used(s))
		}
		if err := s.Release(in.Selection); err != nil {
			t.Fatal(err)
		}
		if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
			t.Fatalf("the release did not return the capacity: %v", used(s))
		}
	})
	t.Run("an owner's destroy", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		clock := withClock(&cfg)
		s := open(t, cfg, h, nil)
		r := mustReserve(t, s, owner1, expiring("0v1", 1, 128))
		if _, err := s.Create(owner1, r.ID); err != nil {
			t.Fatal(err)
		}
		clock.advance(expire)
		err := s.Destroy(owner1, r.ID)
		rec, ok := held(s, r.ID)
		if !errors.Is(err, ErrQuarantined) || !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
			t.Fatalf("A LATE DESTROY WAS REPORTED AS A RELEASE: %v; held %v %+v; used %v", err, ok, rec, used(s))
		}
		if rec.Incident == nil || !rec.Incident.Missed || len(h.leaks(r.ID)) != 0 {
			t.Fatalf("the late destroy's incident %+v, leaks %v", rec.Incident, h.leaks(r.ID))
		}
	})
}

// Cleanup — the operator's retry, or the recovery a restart owes an
// interrupted teardown — stops and removes what the record owns and records
// the attempt, and never releases: the record stays quarantined and
// charged whatever it achieved.
func TestCleanupAttemptsNeverRelease(t *testing.T) {
	t.Run("the operator's retry", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := quarantinedByDisk(t, s, h, "0v1")
		in, err := s.RetryCleanup(selectionOf(rec))
		after, ok := held(s, rec.ID)
		if !ok || after.State != StateQuarantined || used(s)[2] != 1 {
			t.Fatalf("A CLEANUP RETRY RELEASED THE INCIDENT: %v; held %v %+v; used %v", err, ok, after, used(s))
		}
		if err != nil || len(h.leaks(rec.ID)) != 0 || after.holds() {
			t.Fatalf("the retry did not clean up: %v; leaks %v; record %+v", err, h.leaks(rec.ID), after)
		}
		inc := after.Incident
		if inc == nil || inc.Count != 2 || inc.Attempts[1].By != "operator retry" || inc.Attempts[1].Result != "resolved" || in.Selection.Rev != after.Rev {
			t.Fatalf("THE RETRY IS NOT RECORDED: %+v; returned %+v", inc, in.Selection)
		}
	})
	// a restart's recovery of an interrupted teardown: in time, or after
	// the deadline it began under — then the incident says it was missed
	for _, c := range []struct {
		name string
		due  time.Duration
	}{{"the recovery a restart owes", time.Minute}, {"the recovery a restart owes past its deadline", -time.Minute}} {
		t.Run(c.name, func(t *testing.T) {
			h := newModelHost(t, &tracer{})
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			due := interruptDurably(t, cfg.StateDir, r.ID, c.due)
			s2 := reopen(t, s, h)
			s2.ReapOnce()
			after, ok := held(s2, r.ID)
			if !ok || after.State != StateQuarantined || used(s2)[2] != 1 {
				t.Fatalf("A RECOVERY RELEASED THE INCIDENT: held %v %+v; used %v", ok, after, used(s2))
			}
			inc := after.Incident
			if inc == nil || len(inc.Attempts) == 0 || inc.Attempts[len(inc.Attempts)-1].By != "recovery" || inc.Attempts[len(inc.Attempts)-1].Result != "resolved" {
				t.Fatalf("THE RECOVERY IS NOT RECORDED: %+v", inc)
			}
			if inc.Due != due || inc.Trigger != "destroyed by owner" || inc.Missed != (c.due < 0) {
				t.Fatalf("THE RECOVERY DOES NOT RECORD ITS OBLIGATION AS IT WAS (due %d, missed %v): %+v", due, c.due < 0, inc)
			}
		})
	}
}

// interruptDurably rewrites id's durable record as a teardown a crash cut
// short: `stopping`, with the obligation it began under, due in; it
// answers that deadline.
func interruptDurably(t *testing.T, state, id string, in time.Duration) int64 {
	t.Helper()
	p := filepath.Join(state, "attempts", id+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var rf recordFile
	if err := json.Unmarshal(data, &rf); err != nil {
		t.Fatal(err)
	}
	rf.Record.State = StateStopping
	rf.Record.CleanupTrigger = "destroyed by owner"
	rf.Record.CleanupDue = time.Now().Add(in).Unix()
	out, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return rf.Record.CleanupDue
}

// The inspection shows the incident as the operator must see it: the job
// label and the exact incarnation, the obligation it was quarantined under,
// every attempt and what it left, what the record still holds, the charge
// it withholds, and why release is refused — then allowed once a retry
// resolved it.
func TestInspectionShowsTheIncident(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	rec := quarantinedByDisk(t, s, h, "0v1")
	list := s.Incidents()
	if len(list) != 1 {
		t.Fatalf("incidents: %+v", list)
	}
	in := list[0]
	inc := in.Record.Incident
	if in.Record.Label != testLabel || in.Selection != selectionOf(rec) || in.CPUs != 1 || in.MemoryMiB != 128+OverheadMiB ||
		!slices.Contains(in.Remaining, "disk") || in.Releasable || !strings.Contains(in.Why, "disk") ||
		inc == nil || inc.Missed || inc.Due == 0 || !strings.Contains(inc.Trigger, "destroyed by owner") || len(inc.Attempts) != 1 ||
		inc.Attempts[0].By != "teardown" || inc.Attempts[0].Result != "unresolved" || !slices.Contains(inc.Attempts[0].Left, "disk") ||
		!strings.Contains(inc.Attempts[0].Detail, "injected") {
		t.Fatalf("THE INSPECTION DOES NOT SHOW THE INCIDENT: %+v; incident %+v", in, inc)
	}
	if _, err := s.RetryCleanup(in.Selection); err != nil {
		t.Fatal(err)
	}
	again := inspect(t, s, rec)
	if len(again.Remaining) != 0 || !again.Releasable || again.Selection.Rev == in.Selection.Rev || len(again.Record.Incident.Attempts) != 2 {
		t.Fatalf("THE INSPECTION DOES NOT SHOW THE INCIDENT resolved: %+v", again)
	}
}

// A release names the exact inspected incarnation and revision: another
// incarnation's selection, a revision the record has moved past, or a
// replay after the release releases nothing — not even a later
// incarnation of the same attempt.
func TestReleaseNamesTheExactInspectedIncarnation(t *testing.T) {
	h := newModelHost(t, &tracer{})
	s := open(t, testConfig(t.TempDir()), h, nil)
	rec := quarantinedByDisk(t, s, h, "0v1")
	if _, err := s.RetryCleanup(selectionOf(rec)); err != nil {
		t.Fatal(err)
	}
	in := inspect(t, s, rec)
	vary := func(change func(*Selection)) Selection {
		sel := in.Selection
		change(&sel)
		return sel
	}
	for _, c := range []struct {
		name string
		sel  Selection
	}{
		{"another cid", vary(func(s *Selection) { s.CID++ })},
		{"another creation", vary(func(s *Selection) { s.Created-- })},
		{"another token", vary(func(s *Selection) { s.Incarnation = strings.Repeat("0", 32) })},
		{"no token", vary(func(s *Selection) { s.Incarnation = "" })},
		{"an older rev", vary(func(s *Selection) { s.Rev-- })},
	} {
		if err := s.Release(c.sel); !errors.Is(err, ErrStale) {
			t.Fatalf("A STALE SELECTION WAS RELEASED (%s): %v", c.name, err)
		}
		if _, ok := held(s, rec.ID); !ok {
			t.Fatalf("A STALE SELECTION WAS RELEASED (%s)", c.name)
		}
	}
	if err := s.Release(in.Selection); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(in.Selection); !errors.Is(err, ErrUnknown) {
		t.Fatalf("a replayed release: %v", err)
	}
	again := mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	if again.ID != rec.ID {
		t.Fatalf("fixture: the same attempt, the same id: %s %s", again.ID, rec.ID)
	}
	if err := s.Release(in.Selection); !errors.Is(err, ErrStale) {
		t.Fatalf("A RELEASE REACHED ANOTHER INCARNATION: %v", err)
	}
	if r, ok := held(s, again.ID); !ok || r.State != StatePreparing || r.Incarnation != again.Incarnation {
		t.Fatalf("A RELEASE REACHED ANOTHER INCARNATION: %+v %v", r, ok)
	}
}

// While a cleanup holds the record, a release is refused; once it has
// ended, the inspection taken before it is stale. That holds to the
// cleanup's last step: while the retry publishes its outcome — the record
// quarantined again in memory, at its new revision, and nothing held — a
// release of what an inspection shows then is refused too (its withdrawal
// would race the publication, which would put the file back).
func TestReleaseIsRefusedWhileACleanupRuns(t *testing.T) {
	t.Run("while the retry removes", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := quarantinedByDisk(t, s, h, "0v1")
		before := inspect(t, s, rec)
		g := newGate()
		t.Cleanup(g.open)
		h.at("rmdisk/before", g.wait)
		retried := make(chan error, 1)
		go func() {
			_, err := s.RetryCleanup(before.Selection)
			retried <- err
		}()
		<-g.reached
		if err := s.Release(before.Selection); !errors.Is(err, ErrState) {
			t.Fatalf("A RELEASE WAS ALLOWED WHILE A CLEANUP RAN: %v", err)
		}
		g.open()
		if err := <-retried; err != nil {
			t.Fatal(err)
		}
		if err := s.Release(before.Selection); !errors.Is(err, ErrStale) {
			t.Fatalf("A RELEASE WAS ALLOWED ON AN INSPECTION A CLEANUP OUTDATED: %v", err)
		}
		if err := s.Release(inspect(t, s, rec).Selection); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("while the retry publishes its outcome", func(t *testing.T) {
		tr := &tracer{}
		fsys := newFaultFS(tr)
		h := newModelHost(t, tr)
		cfg := testConfig(t.TempDir())
		s := open(t, cfg, h, fsys)
		rec := quarantinedByDisk(t, s, h, "0v1")
		g := newGate()
		t.Cleanup(g.open)
		fsys.inject(&fsFault{op: "rename", path: rec.ID + ".json", block: func() { g.wait("") }, times: 1})
		retried := make(chan error, 1)
		go func() {
			_, err := s.RetryCleanup(selectionOf(rec))
			retried <- err
		}()
		<-g.reached
		during := inspect(t, s, rec)
		if during.Busy == "" || during.Releasable || len(during.Remaining) != 0 || during.Record.State != StateQuarantined {
			t.Fatalf("fixture: the retry publishing its outcome: %+v", during)
		}
		if err := s.Release(during.Selection); !errors.Is(err, ErrState) {
			t.Fatalf("A RELEASE WAS ALLOWED WHILE A CLEANUP RAN: %v", err)
		}
		g.open()
		if err := <-retried; err != nil {
			t.Fatal(err)
		}
		if err := s.Release(inspect(t, s, rec).Selection); err != nil {
			t.Fatal(err)
		}
		if _, ok := durable(t, cfg.StateDir, rec.ID); ok || used(s)[2] != 0 {
			t.Fatalf("after the release: record file %v, used %v", ok, used(s))
		}
	})
}

// startUncertain leaves a record whose VMM start outcome is unknown: the
// start fails after it may have acted (mode "after": the model's VMM runs,
// its pid lost; mode "before": none runs), so the rollback quarantines it
// with its disk and cgroup kept.
func startUncertain(t *testing.T, s *Service, h *modelHost, attempt, mode string) Record {
	t.Helper()
	r := mustReserve(t, s, owner1, lockedReq(attempt, 1, 128))
	h.fail("start", mode, errInjected)
	if _, err := s.Create(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: an uncertain start quarantines: %v", err)
	}
	h.heal("start")
	rec, ok := held(s, r.ID)
	if !ok || !rec.HasVMM || rec.PID != 0 || !h.exists("cgroup", r.ID) || !h.exists("jail", r.ID) {
		t.Fatalf("fixture: %+v %v", rec, ok)
	}
	return rec
}

// A VMM of unknown pid is never taken for gone: release refuses it, and no
// cleanup removes anything from underneath it. Automatic cleanup does not
// look for it; the operator's retry does, when the host can: it stops the
// VMM it finds (verified), or resolves the holding when it finds none — a
// scan that fails resolves nothing.
func TestUnknownCustodyIsResolvedOnlyByTheRetrysScan(t *testing.T) {
	t.Run("release is no proof", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := startUncertain(t, s, h, "0v1", "after")
		if err := s.Release(inspect(t, s, rec).Selection); !errors.Is(err, ErrUnresolved) {
			t.Fatalf("RELEASE TOOK A VMM OF UNKNOWN PID FOR GONE: %v", err)
		}
		if _, ok := held(s, rec.ID); !ok || !h.exists("vmm", rec.ID) || !h.exists("cgroup", rec.ID) {
			t.Fatalf("RELEASE TOOK A VMM OF UNKNOWN PID FOR GONE: %v", h.leaks(rec.ID))
		}
		if h.count("find") != 0 {
			t.Fatalf("automatic cleanup looked for the VMM by scan (%d)", h.count("find"))
		}
	})
	t.Run("a host that cannot look", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), noFinder{h}, nil)
		rec := startUncertain(t, s, h, "0v1", "after")
		_, err := s.RetryCleanup(inspect(t, s, rec).Selection)
		after, _ := held(s, rec.ID)
		if !h.exists("cgroup", rec.ID) || !h.exists("jail", rec.ID) || !after.HasVMM || !after.HasCgroup {
			t.Fatalf("RESOURCES WERE REMOVED FROM UNDER A VMM OF UNKNOWN CUSTODY: %v; leaks %v; record %+v", err, h.leaks(rec.ID), after)
		}
		if !errors.Is(err, ErrQuarantined) || inspect(t, s, rec).Releasable {
			t.Fatalf("an unresolved retry: %v", err)
		}
	})
	t.Run("the scan fails", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := startUncertain(t, s, h, "0v1", "before")
		h.fail("find", "before", errInjected)
		_, err := s.RetryCleanup(inspect(t, s, rec).Selection)
		if after, _ := held(s, rec.ID); !h.exists("cgroup", rec.ID) || !after.HasVMM || !errors.Is(err, ErrQuarantined) {
			t.Fatalf("RESOURCES WERE REMOVED FROM UNDER A VMM OF UNKNOWN CUSTODY: %v; record %+v", err, after)
		}
	})
	t.Run("the scan finds none", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := startUncertain(t, s, h, "0v1", "before")
		if _, err := s.RetryCleanup(inspect(t, s, rec).Selection); err != nil || len(h.leaks(rec.ID)) != 0 {
			t.Fatalf("the retry after a scan that found none: %v; leaks %v", err, h.leaks(rec.ID))
		}
		in := inspect(t, s, rec)
		last := in.Record.Incident.Attempts[len(in.Record.Incident.Attempts)-1]
		if !in.Releasable || !strings.Contains(last.Detail, "no process") {
			t.Fatalf("the scan's finding is not recorded: %+v", last)
		}
	})
	t.Run("the scan finds it", func(t *testing.T) {
		h := newModelHost(t, &tracer{})
		s := open(t, testConfig(t.TempDir()), h, nil)
		rec := startUncertain(t, s, h, "0v1", "after")
		if _, err := s.RetryCleanup(inspect(t, s, rec).Selection); err != nil || len(h.leaks(rec.ID)) != 0 || h.count("kill") == 0 {
			t.Fatalf("the retry that found the VMM: %v; leaks %v; kills %d", err, h.leaks(rec.ID), h.count("kill"))
		}
		if err := s.Release(inspect(t, s, rec).Selection); err != nil {
			t.Fatal(err)
		}
	})
}

// evidenceName is the file name of rec's evidence: by its token (a record
// written before tokens: by its cid and creation time).
func evidenceName(rec Record) string {
	if rec.Incarnation != "" {
		return fmt.Sprintf("%s.%s.json", rec.ID, rec.Incarnation)
	}
	return fmt.Sprintf("%s.%d.%d.json", rec.ID, rec.CID, rec.Created)
}

// archived reads the evidence a release of rec left.
func archived(t *testing.T, state string, rec Record) (Record, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "released", evidenceName(rec)))
	if err != nil {
		return Record{}, false
	}
	var rf recordFile
	if err := json.Unmarshal(data, &rf); err != nil {
		t.Fatalf("the evidence is unparseable: %v", err)
	}
	return rf.Record, true
}

// A release is reported only after its durable transition — the evidence
// first, then the withdrawal — and keeps the charge whenever either is not
// proven; the evidence stays after it, and a restart finds the release.
func TestReleaseIsDurableAndKeepsItsEvidence(t *testing.T) {
	resolved := func(t *testing.T, faults *faultFS) (*Service, *modelHost, Config, Record) {
		h := newModelHost(t, &tracer{})
		cfg := testConfig(t.TempDir())
		var fsys fileSystem // nil: the production adapter (a nil *faultFS is not)
		if faults != nil {
			fsys = faults
		}
		s := open(t, cfg, h, fsys)
		rec := quarantinedByDisk(t, s, h, "0v1")
		if _, err := s.RetryCleanup(selectionOf(rec)); err != nil {
			t.Fatal(err)
		}
		return s, h, cfg, rec
	}
	t.Run("released", func(t *testing.T) {
		s, h, cfg, rec := resolved(t, nil)
		if err := s.Release(inspect(t, s, rec).Selection); err != nil {
			t.Fatal(err)
		}
		ev, ok := archived(t, cfg.StateDir, rec)
		if _, still := durable(t, cfg.StateDir, rec.ID); !ok || still || ev.State != StateReleased || ev.Incident == nil ||
			ev.Incident.Attempts[len(ev.Incident.Attempts)-1].Result != "released" {
			t.Fatalf("THE RELEASE DID NOT KEEP ITS EVIDENCE: %+v %v (record still in attempts: %v)", ev, ok, still)
		}
		next := reopen(t, s, h)
		if _, held := held(next, rec.ID); held || used(next)[2] != 0 {
			t.Fatal("a restart found the released record")
		}
		if _, ok := archived(t, cfg.StateDir, rec); !ok {
			t.Fatal("the evidence did not survive a restart")
		}
	})
	for _, c := range []struct {
		name  string
		fault func(rec Record) *fsFault
	}{
		{"the evidence cannot be written", func(rec Record) *fsFault {
			return &fsFault{op: "open", path: evidenceName(rec) + ".tmp", mode: "before", err: errInjected, times: 1}
		}},
		{"the evidence's directory fsync fails", func(rec Record) *fsFault {
			return &fsFault{op: "syncdir", path: "released", mode: "before", err: errInjected, times: 1}
		}},
		{"the withdrawal's unlink fails", func(rec Record) *fsFault {
			return &fsFault{op: "unlink", path: rec.ID + ".json", mode: "before", err: errInjected, times: 1}
		}},
		{"the withdrawal's directory fsync fails", func(rec Record) *fsFault {
			return &fsFault{op: "syncdir", path: "attempts", mode: "before", err: errInjected, times: 1}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			s, _, cfg, rec := resolved(t, fsys)
			in := inspect(t, s, rec)
			fsys.inject(c.fault(rec))
			err := s.Release(in.Selection)
			if err == nil || used(s)[2] != 1 {
				t.Fatalf("A RELEASE REPORTED WITHOUT ITS DURABLE TRANSITION: %v; used %v", err, used(s))
			}
			if _, ok := held(s, rec.ID); !ok {
				t.Fatal("A RELEASE REPORTED WITHOUT ITS DURABLE TRANSITION: dropped from memory")
			}
			// a retried release finishes it, the evidence written once
			if err := s.Release(inspect(t, s, rec).Selection); err != nil {
				t.Fatalf("the retried release: %v", err)
			}
			if _, ok := archived(t, cfg.StateDir, rec); !ok || used(s)[2] != 0 {
				t.Fatalf("after the retried release: evidence %v, used %v", ok, used(s))
			}
		})
	}
	t.Run("another incarnation's evidence at its path", func(t *testing.T) {
		// the same record, cid and creation time — a restart that repeated
		// the tuple — but another token: not this incarnation's evidence
		s, _, cfg, rec := resolved(t, nil)
		dir := filepath.Join(cfg.StateDir, "released")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		other := rec
		other.Incarnation, other.State = strings.Repeat("0", 32), StateReleased
		data, _, err := encodeRecord(cfg.IDPrefix, other)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, evidenceName(rec))
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		err = s.Release(inspect(t, s, rec).Selection)
		now, _ := os.ReadFile(p)
		if err == nil || string(now) != string(data) {
			t.Fatalf("ANOTHER INCARNATION'S EVIDENCE WAS TAKEN FOR THIS ONE'S: %v", err)
		}
		if _, ok := held(s, rec.ID); !ok || used(s)[2] != 1 {
			t.Fatal("the refused release dropped the charge")
		}
	})
	t.Run("something else at the evidence's path", func(t *testing.T) {
		s, _, cfg, rec := resolved(t, nil)
		dir := filepath.Join(cfg.StateDir, "released")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, evidenceName(rec))
		if err := os.WriteFile(p, []byte("not this launcher's evidence"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := s.Release(inspect(t, s, rec).Selection)
		data, _ := os.ReadFile(p)
		if err == nil || string(data) != "not this launcher's evidence" {
			t.Fatalf("A RELEASE OVERWROTE WHAT OCCUPIED ITS EVIDENCE'S PATH: %v, now %q", err, data)
		}
		if _, ok := held(s, rec.ID); !ok || used(s)[2] != 1 {
			t.Fatal("the refused release dropped the charge")
		}
	})
}

// An incident outlives a restart as it was: every attempt and its revision
// are durable, so an operator whose retry's answer was lost inspects again
// after the restart — and the selection taken before the retry is stale.
func TestIncidentSurvivesARestartAndALostAnswer(t *testing.T) {
	h := newModelHost(t, &tracer{})
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, nil)
	rec := quarantinedByDisk(t, s, h, "0v1")
	before := inspect(t, s, rec)
	_, _ = s.RetryCleanup(before.Selection) // its answer is lost
	next := reopen(t, s, h)
	in, err := next.InspectIncident(before.Selection)
	if err != nil || in.Record.Incident == nil || in.Record.Incident.Count != 2 || in.Selection.Rev == before.Selection.Rev || !in.Releasable {
		t.Fatalf("THE INCIDENT DID NOT SURVIVE THE RESTART: %v %+v", err, in)
	}
	if err := next.Release(before.Selection); !errors.Is(err, ErrStale) {
		t.Fatalf("A SELECTION FROM BEFORE THE LOST ANSWER WAS RELEASED: %v", err)
	}
	if err := next.Release(in.Selection); err != nil {
		t.Fatal(err)
	}
}

// An incident's history is bounded: a record admitted at the admission
// limit keeps room for everything its lifecycle adds — a worst-case reason,
// the obligation, and every cleanup attempt of its incident (the latest
// maxAttempts kept, each of bounded text) — so however often its cleanup is
// retried, every retry is published and reloads as it was.
func TestIncidentHistoryStaysWithinTheRecordsRoom(t *testing.T) {
	cfg := testConfig(t.TempDir())
	// JSON escapes '<' as six bytes: the worst case of every bounded text
	h := messyHost{newModelHost(t, &tracer{}), strings.Repeat("<", 4<<10)}
	s := open(t, cfg, h, nil)
	now := time.Now().Unix()
	probe := Record{ID: IDFor("t", "a"), Incarnation: strings.Repeat("0", 32), Attempt: "a", Owner: owner1, Image: "img", CPUs: 1, MemoryMiB: 128, DiskMiB: 10,
		DeadlineUnix: now + 3600, Network: "locked", CID: minCID, State: StatePreparing, Created: now, Updated: now}
	data, _, err := encodeRecord("t", probe)
	if err != nil {
		t.Fatal(err)
	}
	room := maxRecordBytes - recordGrowthBytes
	r := mustReserve(t, s, owner1, lockedReq(strings.Repeat("a", room-len(data)+1), 1, 128))
	if d, _ := os.ReadFile(filepath.Join(cfg.StateDir, "attempts", r.ID+".json")); len(d) != room {
		t.Fatalf("fixture: the record at the limit is %d bytes, want %d", len(d), room)
	}
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("fixture: the destroy quarantines: %v", err)
	}
	rec, _ := held(s, r.ID)
	const retries = 40
	for i := 0; i < retries; i++ {
		in, err := s.RetryCleanup(selectionOf(rec))
		if !errors.Is(err, ErrQuarantined) {
			t.Fatalf("retry %d: %v", i, err)
		}
		rec = in.Record
	}
	d, ok := durable(t, cfg.StateDir, r.ID)
	if !ok || d.Rev != rec.Rev || d.Incident == nil || d.Incident.Count != retries+1 || len(d.Incident.Attempts) != maxAttempts {
		t.Fatalf("AN INCIDENT'S HISTORY OUTGREW THE RECORD'S ROOM: durable %v rev %d (memory %d), incident %d attempts, %d kept; reason %.200q",
			ok, d.Rev, rec.Rev, rec.Incident.Count, len(rec.Incident.Attempts), rec.Reason)
	}
	s2 := reopen(t, s, h)
	if got, ok := held(s2, r.ID); !ok || len(s2.Problems()) != 0 || got.Incident == nil || got.Incident.Count != retries+1 {
		t.Fatalf("the grown incident does not reload: %v %v", ok, s2.Problems())
	}
}

// Retry and release act on the record they name only: another record —
// running, and another incident — is not touched.
func TestRecoveryLeavesOtherRecordsAlone(t *testing.T) {
	tr := &tracer{}
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	cfg.MaxGuests = 3
	s := open(t, cfg, h, nil)
	rec := quarantinedByDisk(t, s, h, "0v1")
	other := quarantinedByDisk(t, s, h, "0v2")
	running := mustReserve(t, s, owner2, lockedReq("0v3", 1, 128))
	if _, err := s.Create(owner2, running.ID); err != nil {
		t.Fatal(err)
	}
	mark := len(tr.all())
	if _, err := s.RetryCleanup(selectionOf(rec)); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(inspect(t, s, rec).Selection); err != nil {
		t.Fatal(err)
	}
	for _, e := range tr.all()[mark:] {
		if e.kind == "host" && (e.id == other.ID || e.id == running.ID) {
			t.Fatalf("A RECOVERY TOUCHED ANOTHER RECORD: %s", e)
		}
	}
	if r, ok := held(s, other.ID); !ok || r.State != StateQuarantined || r.Rev != other.Rev {
		t.Fatalf("A RECOVERY TOUCHED ANOTHER RECORD: %+v", r)
	}
	if r, ok := held(s, running.ID); !ok || r.State != StateRunning || !h.exists("vmm", running.ID) {
		t.Fatalf("A RECOVERY TOUCHED ANOTHER RECORD: %+v", r)
	}
}
