package launcher

import (
	"errors"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func netReq(attempt string) ReserveRequest {
	r := lockedReq(attempt, 2, 1024)
	r.Network, r.Destinations = "integration", []string{"tcp:192.0.2.1:8472"}
	return r
}

// holdingOf is the holding each host create operation is recorded under.
var holdingOf = map[string]string{"prepare": "disk", "cgroup": "cgroup", "network": "network", "start": "vmm"}

// Every host effect of a create comes after a publication that names its
// holding, and after that publication's directory fsync; the create is
// acknowledged after the running record is durable.
func TestEveryHostEffectFollowsItsDurableIntent(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, netReq("0v1"))
	pid, err := s.Create(owner1, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	events := tr.all()
	tr.dump(t)
	checked := 0
	for i, e := range events {
		want, ok := holdingOf[e.op]
		if e.kind != "host" || !ok || e.inject != "" {
			continue
		}
		ri := -1
		for j := i - 1; j >= 0; j-- {
			if events[j].kind == "fs" && events[j].op == "rename" && events[j].id == e.id {
				ri = j
				break
			}
		}
		if ri < 0 || events[ri].err != "" || !slices.Contains(events[ri].holds, want) {
			t.Fatalf("host %s at %d: the last publication before it (%d) does not name %s", e.op, i, ri, want)
		}
		synced := false
		for j := ri + 1; j < i; j++ {
			synced = synced || (events[j].kind == "fs" && events[j].op == "syncdir" && events[j].err == "")
		}
		if !synced {
			t.Fatalf("host %s at %d: its intent (%d) was not directory-synced first", e.op, i, ri)
		}
		checked++
	}
	if checked != 4 {
		t.Fatalf("checked %d host effects, want 4 (disk, cgroup, network, vmm)", checked)
	}
	last := events[len(events)-2:]
	if last[0].op != "rename" || last[0].state != string(StateRunning) || last[1].op != "syncdir" {
		t.Fatalf("the create was not acknowledged after a durable running record: %v", last)
	}
	rec, _ := durable(t, cfg.StateDir, r.ID)
	if rec.State != StateRunning || rec.PID != pid || !rec.HasDisk || !rec.HasCgroup || !rec.HasNetwork || !rec.HasVMM {
		t.Fatalf("durable running record %+v", rec)
	}
}

// A create whose intent cannot be made durable never starts the effect
// that intent names; everything created before it is rolled back.
func TestIntentPublicationFailureStopsBeforeTheEffect(t *testing.T) {
	for _, step := range []string{"prepare", "cgroup", "network", "start"} {
		for _, fault := range []string{"sync", "syncdir"} { // certainly not written / uncertain
			t.Run(step+"/"+fault, func(t *testing.T) {
				tr := &tracer{}
				fsys := newFaultFS(tr)
				h := newModelHost(t, tr)
				cfg := testConfig(t.TempDir())
				s := open(t, cfg, h, fsys)
				r := mustReserve(t, s, owner1, netReq("0v1"))
				x := &fsFault{op: fault, mode: "before", err: errInjected, times: 1}
				prev := map[string]string{"cgroup": "prepare", "network": "cgroup", "start": "network"}[step]
				if prev == "" {
					fsys.inject(x)
				} else {
					h.at(prev+"/after", func(string) { fsys.inject(x) })
				}
				_, err := s.Create(owner1, r.ID)
				tr.dump(t)
				if x.hits != 1 {
					t.Fatalf("fault not reached")
				}
				if n := h.count(step); n != 0 {
					t.Fatalf("host %s ran %d times without a durable intent", step, n)
				}
				if !errors.Is(err, ErrNotDurable) || !strings.Contains(err.Error(), "rolled back") {
					t.Fatalf("create: %v", err)
				}
				if leaks := h.leaks(r.ID); len(leaks) != 0 {
					t.Fatalf("rollback left %v", leaks)
				}
				if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
					t.Fatalf("not released: %v", used(s))
				}
				s2 := reopen(t, s, h)
				if _, ok := held(s2, r.ID); ok || len(s2.Problems()) != 0 {
					t.Fatalf("reopen disagrees: %+v %v", s2.All(), s2.Problems())
				}
			})
		}
	}
}

// A host create operation that fails is not taken as proof that nothing
// was created: whatever it may have left is removed by the idempotent
// Remove* before the reservation is released. Only an explicit
// ErrNoEffect lets a failed start be forgotten.
func TestHostCreateFailuresAreRemovedNotAssumedAbsent(t *testing.T) {
	remover := map[string]string{"prepare": "rmjail", "cgroup": "rmcgroup", "network": "rmnet", "start": ""}
	kind := map[string]string{"prepare": "jail", "cgroup": "cgroup", "network": "network"}
	for _, op := range []string{"prepare", "cgroup", "network", "start"} {
		for _, mode := range []string{"before", "after", "noeffect"} {
			if op == "start" && mode != "noeffect" {
				continue // TestUncertainStartIsQuarantinedUntilTheOperatorClears
			}
			t.Run(op+"/"+mode, func(t *testing.T) {
				tr := &tracer{}
				h := newModelHost(t, tr)
				cfg := testConfig(t.TempDir())
				s := open(t, cfg, h, nil)
				r := mustReserve(t, s, owner1, netReq("0v1"))
				h.fail(op, mode, errInjected)
				_, err := s.Create(owner1, r.ID)
				tr.dump(t)
				if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "rolled back") || errors.Is(err, ErrQuarantined) {
					t.Fatalf("create: %v", err)
				}
				if mode == "after" && h.count(remover[op]) == 0 {
					t.Fatalf("the failed %s's %s was never removed", op, kind[op])
				}
				if leaks := h.leaks(r.ID); len(leaks) != 0 {
					t.Fatalf("left behind after a failed %s (%s): %v", op, mode, leaks)
				}
				if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
					t.Fatalf("not released: %v", used(s))
				}
				if _, ok := durable(t, cfg.StateDir, r.ID); ok {
					t.Fatal("record file survived the release")
				}
			})
		}
	}
}

// A start that failed without saying it had no effect may have left a
// VMM whose pid nobody knows. Nothing is removed from under it, the
// owner's destroy, the reaper and a restart cannot release it; only the
// operator's clear can — and even that not while a process remains.
func TestUncertainStartIsQuarantinedUntilTheOperatorClears(t *testing.T) {
	for _, mode := range []string{"after", "before"} { // a VMM runs / none runs, and the host did not say which
		t.Run(mode, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			h.fail("start", mode, errInjected)
			_, err := s.Create(owner1, r.ID)
			if !errors.Is(err, ErrQuarantined) || !errors.Is(err, errInjected) || strings.Contains(err.Error(), "(rolled back)") {
				t.Fatalf("create: %v", err)
			}
			rec, _ := held(s, r.ID)
			if rec.State != StateQuarantined || !rec.HasVMM || rec.PID != 0 || !strings.Contains(rec.Reason, "vmm start outcome unknown") {
				t.Fatalf("record %+v", rec)
			}
			if !h.exists("jail", r.ID) || !h.exists("cgroup", r.ID) {
				t.Fatalf("resources were removed from under a VMM that may run: %v", h.leaks(r.ID))
			}
			if d, _ := durable(t, cfg.StateDir, r.ID); d.State != StateQuarantined || !d.HasVMM {
				t.Fatalf("durable %+v", d)
			}
			removals := h.count("rmcgroup") + h.count("rmjail")
			if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
				t.Fatalf("owner destroy released an unknown VMM: %v", err)
			}
			s.ReapOnce()
			if h.count("rmcgroup")+h.count("rmjail") != removals {
				t.Fatal("a destroy or the reaper removed resources from under an unknown VMM")
			}
			s = reopen(t, s, h)
			s.ReapOnce()
			if rec, ok := held(s, r.ID); !ok || rec.State != StateQuarantined || used(s)[2] != 1 {
				t.Fatalf("a restart freed an unknown VMM's capacity: %+v %v", rec, ok)
			}
			if mode == "after" {
				// the process is still there: the operator's clear cannot
				// release it either, and the failed clear leaves the unknown
				// VMM recorded for the next one. Stage 01 (recovery ruling A):
				// the clear's cleanup retry now looks for the VMM and stops the
				// one it finds; this one survives TERM and KILL
				h.mu.Lock()
				h.hang[r.ID] = true
				h.mu.Unlock()
				if err := s.ClearQuarantine(r.ID); !errors.Is(err, ErrQuarantined) {
					t.Fatalf("clear with a live VMM: %v", err)
				}
				if rec, _ := held(s, r.ID); !rec.HasVMM || rec.PID != 0 {
					t.Fatalf("a failed clear dropped the unknown VMM: %+v", rec)
				}
				if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
					t.Fatalf("after a failed clear the owner released an unknown VMM: %v", err)
				}
				// the operator inspected and stopped it
				if err := os.Remove(h.marker("vmm", r.ID)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ClearQuarantine(r.ID); err != nil {
				t.Fatalf("operator clear: %v", err)
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 {
				t.Fatalf("after the clear: leaks %v used %v", leaks, used(s))
			}
		})
	}
}

// If the running record cannot be made durable the create is not
// acknowledged: the VMM it started (its pid known in memory) is stopped
// and everything removed.
func TestRunningPublicationFailureStopsTheVMM(t *testing.T) {
	for _, fault := range []string{"sync", "syncdir"} {
		t.Run(fault, func(t *testing.T) {
			tr := &tracer{}
			fsys := newFaultFS(tr)
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, fsys)
			r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
			x := &fsFault{op: fault, mode: "before", err: errInjected, times: 1}
			h.at("start/after", func(string) { fsys.inject(x) })
			_, err := s.Create(owner1, r.ID)
			tr.dump(t)
			if x.hits != 1 || !errors.Is(err, ErrNotDurable) || !strings.Contains(err.Error(), "record running") {
				t.Fatalf("create: %v (fault hits %d)", err, x.hits)
			}
			if h.count("kill") == 0 || h.exists("vmm", r.ID) {
				t.Fatal("the started VMM was not stopped")
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 {
				t.Fatalf("leaks %v used %v", leaks, used(s))
			}
		})
	}
}

// After a restart every loaded record is handled by what its durable
// record says, and by nothing else. An interrupted teardown (`stopping`)
// may have ended in a quarantine whose own publication failed, so it
// loads quarantined: the reaper performs its owed cleanup and keeps it
// quarantined, the owner is refused, and only the operator's clear
// releases it. An interrupted create is torn down (never resumed), an
// unknown VMM stays quarantined, idle and running reservations keep their
// charge, a deadline still reaps.
func TestRestartRecoveryFollowsTheDurableRecord(t *testing.T) {
	type markers struct{ jail, cgroup, network, vmm bool }
	cases := []struct {
		name    string
		rec     func(*Record)
		host    markers
		outcome string // released | retained (cleaned, quarantined, operator only) | kept
		state   State  // when kept
	}{
		{"interrupted teardown", func(r *Record) { r.State, r.HasDisk, r.HasCgroup = StateStopping, true, true }, markers{jail: true, cgroup: true}, "retained", ""},
		{"interrupted teardown of a live vmm", func(r *Record) {
			r.State, r.HasDisk, r.HasCgroup, r.HasVMM, r.PID = StateStopping, true, true, true, 40001
		}, markers{jail: true, cgroup: true, vmm: true}, "retained", ""},
		{"interrupted networked teardown", func(r *Record) {
			r.State, r.HasNetwork, r.NetIndex = StateStopping, true, 3
			r.Network, r.Destinations = "integration", []string{"tcp:192.0.2.1:8472"}
		}, markers{network: true}, "retained", ""},
		{"interrupted create", func(r *Record) { r.HasDisk, r.HasCgroup = true, true }, markers{jail: true}, "released", ""},
		{"interrupted start", func(r *Record) { r.HasDisk, r.HasCgroup, r.HasVMM = true, true, true }, markers{jail: true, cgroup: true, vmm: true}, "kept", StateQuarantined},
		{"running", func(r *Record) {
			r.State, r.HasDisk, r.HasCgroup, r.HasVMM, r.PID = StateRunning, true, true, true, 40001
		}, markers{jail: true, cgroup: true, vmm: true}, "kept", StateRunning},
		{"running past its deadline", func(r *Record) {
			r.State, r.HasDisk, r.HasCgroup, r.HasVMM, r.PID = StateRunning, true, true, true, 40001
			r.DeadlineUnix = time.Now().Add(-time.Second).Unix()
		}, markers{jail: true, cgroup: true, vmm: true}, "released", ""},
		{"idle reservation", func(r *Record) {}, markers{}, "kept", StatePreparing},
		{"quarantined", func(r *Record) { r.State, r.HasCgroup, r.Reason = StateQuarantined, true, "cgroup busy" }, markers{cgroup: true}, "kept", StateQuarantined},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			if c.name == "running past its deadline" {
				// stage 01 (recovery ruling A): its deadline passed a second
				// or two before the reaper's pass; an allowance that covers
				// that keeps the pass in time, as meant (a later one is a
				// missed obligation: recovery_test.go)
				cfg.CleanupBound = 5 * time.Second
			}
			r := goodRecord(cfg, "0v1", 3)
			c.rec(&r)
			writeRecord(t, cfg.StateDir, r)
			put := func(kind, content string) {
				p := h.marker(kind, r.ID)
				if kind == "jail" {
					os.MkdirAll(p, 0o700)
					p += "/disk.ext4"
				}
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.host.jail {
				put("jail", "1")
			}
			if c.host.cgroup {
				put("cgroup", "1")
			}
			if c.host.network {
				put("network", strconv.Itoa(r.NetIndex))
			}
			if c.host.vmm {
				put("vmm", strconv.Itoa(max(r.PID, 1)))
			}
			s := open(t, cfg, h, nil)
			if used(s)[2] != 1 || len(s.Problems()) != 0 {
				t.Fatalf("loaded %v problems %v", used(s), s.Problems())
			}
			// an interrupted create is never resumed
			if r.State == StatePreparing && r.holds() {
				if _, err := s.Create(owner1, r.ID); !errors.Is(err, ErrState) || h.count("prepare") != 0 {
					t.Fatalf("an interrupted create was resumed: %v", err)
				}
			}
			if c.outcome == "retained" {
				// loaded quarantined, and the owner cannot clear it before or
				// after the owed cleanup
				if got, _ := held(s, r.ID); got.State != StateQuarantined || !strings.Contains(got.Reason, "teardown interrupted") {
					t.Fatalf("an interrupted teardown was not loaded quarantined: %+v", got)
				}
				if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || len(tr.all()) != 0 {
					t.Fatalf("owner destroy of an interrupted teardown: %v (host ops %v)", err, tr.all())
				}
			}
			s.ReapOnce()
			tr.dump(t)
			got, ok := held(s, r.ID)
			switch c.outcome {
			case "released":
				if ok || used(s)[2] != 0 || len(h.leaks(r.ID)) != 0 {
					t.Fatalf("not released: %+v leaks %v", got, h.leaks(r.ID))
				}
				if _, ok := durable(t, cfg.StateDir, r.ID); ok {
					t.Fatal("the record file survived its release")
				}
			case "retained":
				if !ok || got.State != StateQuarantined || used(s)[2] != 1 {
					t.Fatalf("an interrupted teardown was released by recovery: %+v %v", got, ok)
				}
				if leaks := h.leaks(r.ID); len(leaks) != 0 {
					t.Fatalf("the owed cleanup was not performed: %v", leaks)
				}
				if d, _ := durable(t, cfg.StateDir, r.ID); d.State != StateQuarantined {
					t.Fatalf("the retained quarantine is not durable: %+v", d)
				}
				ops := len(tr.all())
				s.ReapOnce()
				if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || len(tr.all()) != ops {
					t.Fatalf("after the cleanup the owner or the reaper acted again: %v", err)
				}
				if err := s.ClearQuarantine(r.ID); err != nil {
					t.Fatalf("operator clear: %v", err)
				}
				if _, ok := held(s, r.ID); ok || used(s)[2] != 0 {
					t.Fatal("the operator's clear did not release it")
				}
			case "kept":
				if !ok || got.State != c.state || used(s)[2] != 1 {
					t.Fatalf("kept %+v %v, want %s", got, ok, c.state)
				}
				if c.state == StatePreparing {
					if _, err := s.Create(owner1, r.ID); err != nil {
						t.Fatalf("an idle reservation must still boot: %v", err)
					}
				}
			}
		})
	}
}

// A quarantine whose own publication fails stays charged in memory, and
// the durable `stopping` its teardown wrote first holds it: a restart
// loads it quarantined, the reaper performs its owed cleanup but does not
// release it, the owner is refused, and only the operator's clear does.
func TestQuarantinePublicationFailureStaysChargedAndRecoverable(t *testing.T) {
	tr := &tracer{}
	fsys := newFaultFS(tr)
	h := newModelHost(t, tr)
	cfg := testConfig(t.TempDir())
	s := open(t, cfg, h, fsys)
	r := mustReserve(t, s, owner1, lockedReq("0v1", 2, 1024))
	if _, err := s.Create(owner1, r.ID); err != nil {
		t.Fatal(err)
	}
	h.fail("rmcgroup", "before", errInjected)
	x := &fsFault{op: "sync", mode: "before", err: errInjected, times: 1}
	h.at("rmcgroup/before", func(string) { fsys.inject(x) })
	err := s.Destroy(owner1, r.ID)
	if !errors.Is(err, ErrQuarantined) || x.hits != 1 {
		t.Fatalf("destroy: %v (hits %d)", err, x.hits)
	}
	rec, _ := held(s, r.ID)
	if rec.State != StateQuarantined || !strings.Contains(rec.Reason, "quarantine not recorded durably") || used(s)[2] != 1 {
		t.Fatalf("record %+v", rec)
	}
	if d, _ := durable(t, cfg.StateDir, r.ID); d.State != StateStopping {
		t.Fatalf("durable %+v", d)
	}
	if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("the owner cleared an unrecorded quarantine: %v", err)
	}
	h.heal("rmcgroup")
	h.at("rmcgroup/before", nil)
	s2 := reopen(t, s, h)
	if got, _ := held(s2, r.ID); got.State != StateQuarantined {
		t.Fatalf("the durable stopping record was taken as proof of no quarantine: %+v", got)
	}
	s2.ReapOnce()
	got, ok := held(s2, r.ID)
	if !ok || got.State != StateQuarantined || len(h.leaks(r.ID)) != 0 {
		t.Fatalf("after the owed cleanup: %+v %v leaks %v", got, ok, h.leaks(r.ID))
	}
	if err := s2.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("the owner cleared it after the restart: %v", err)
	}
	if err := s2.ClearQuarantine(r.ID); err != nil {
		t.Fatalf("operator clear: %v", err)
	}
	if _, ok := held(s2, r.ID); ok || used(s2)[2] != 0 {
		t.Fatal("the operator's clear did not release it")
	}
}

// A removal that acted and still reported an error keeps its holding, and
// the record is quarantined. The owner's retry is refused and touches
// nothing even though the removal would now succeed: proof of absence is
// not the operator's clearance (§9). The operator's clear retries the
// idempotent removal and releases.
func TestRemovalFailureKeepsTheHoldingUntilARetryProvesAbsence(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			tr := &tracer{}
			h := newModelHost(t, tr)
			cfg := testConfig(t.TempDir())
			s := open(t, cfg, h, nil)
			r := mustReserve(t, s, owner1, netReq("0v1"))
			if _, err := s.Create(owner1, r.ID); err != nil {
				t.Fatal(err)
			}
			h.fail("rmnet", mode, errInjected)
			if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || !errors.Is(err, errInjected) {
				t.Fatalf("destroy: %v", err)
			}
			rec, _ := held(s, r.ID)
			d, _ := durable(t, cfg.StateDir, r.ID)
			if !rec.HasNetwork || !d.HasNetwork || d.State != StateQuarantined || rec.HasCgroup || rec.HasDisk {
				t.Fatalf("memory %+v durable %+v", rec, d)
			}
			h.heal("rmnet")
			removals := h.count("rmnet")
			if err := s.Destroy(owner1, r.ID); !errors.Is(err, ErrQuarantined) || h.count("rmnet") != removals {
				t.Fatalf("the owner's retry of a quarantine: %v (rmnet %d -> %d)", err, removals, h.count("rmnet"))
			}
			if err := s.ClearQuarantine(r.ID); err != nil {
				t.Fatalf("operator clear: %v", err)
			}
			if leaks := h.leaks(r.ID); len(leaks) != 0 || used(s)[2] != 0 {
				t.Fatalf("leaks %v used %v", leaks, used(s))
			}
		})
	}
}

// Admission compares each request with what is left of the budget; a
// request large enough to wrap the old `used + requested` sum is refused
// and changes nothing.
func TestAdmissionCannotOverflowTheBudget(t *testing.T) {
	s := open(t, testConfig(t.TempDir()), nil, nil)
	mustReserve(t, s, owner1, lockedReq("0v1", 1, 128))
	before := used(s)
	huge := lockedReq("0v2", math.MaxInt, 128)
	huge.MemoryMiB = math.MaxInt - OverheadMiB
	if _, err := s.Reserve(owner1, huge); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a wrapping request: %v", err)
	}
	atBound := lockedReq("0v3", maxUnit, 128)
	if _, err := s.Reserve(owner1, atBound); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("a request over the whole budget: %v", err)
	}
	if used(s) != before {
		t.Fatalf("refusals changed the charge: %v -> %v", before, used(s))
	}
	mustReserve(t, s, owner1, lockedReq("0v4", 7, 128))
	if _, err := s.Reserve(owner1, lockedReq("0v5", 1, 128)); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("a third guest over max_guests: %v", err)
	}
}
