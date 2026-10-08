package main

// C1, cgroup delegation (P4-VM-STAGE-A-SOURCE-01): the launcher's cgroups
// live in the service cgroup systemd delegates to it (Delegate=yes), never
// at the cgroup root. The serving process is a leaf of it, the jobs cgroup
// enables cpu, memory and pids, every job cgroup is under that, and check,
// placement and the recovery pass refuse a cgroup outside the delegated
// subtree. Every cgroup here is the private model (host_test.go); nothing
// is written under /sys/fs/cgroup and no systemd unit is touched.

import (
	"bufio"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
)

// unplacedHost is testHost before serve's placement: the model holds what
// setup gives it (systemd's part), and nothing has been written yet.
func unplacedHost(t *testing.T, setup func(cg *cgroupModel)) (*realHost, *cgroupModel) {
	t.Helper()
	base := t.TempDir()
	for _, d := range []string{"netns", "proc"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cg := newCgroupModel()
	setup(cg)
	h := &realHost{
		cfg: &Config{StateDir: filepath.Join(base, "state"), IDPrefix: "t", JailBase: filepath.Join(base, "jail"), BinDir: filepath.Join(base, "bin"),
			CgroupService: testService, CgroupParent: "jobs", NFTTable: "urgit-test", CIDRPool: "10.113.0.0/16",
			BudgetCPUs: 8, BudgetMemoryMiB: 18432, MaxGuests: 2},
		log: log.New(&syncBuffer{}, "", 0), images: map[string]launcher.Image{}, vmUID: os.Getuid(), vmGID: os.Getgid(),
		command: (&recorder{}).runCtx, pid: testPid, cgroups: cg, netnsDir: filepath.Join(base, "netns"), procDir: filepath.Join(base, "proc"),
	}
	return h, cg
}

func enabledIn(cg *cgroupModel, p string) []string {
	data, _ := cg.Read(p, "cgroup.subtree_control")
	return strings.Fields(data)
}

// Placement under both systemds: DelegateSubgroup= starts the launcher in
// <service>/supervisor; an older systemd starts it in <service>, and it
// moves itself. Either way it ends in its leaf, the service cgroup holds no
// process and enables cpu, memory and pids, and so does the jobs cgroup; a
// job's leaf and the jailer's cgroup are under the jobs cgroup, the leaf
// with the three controllers and its limits.
func TestPlacementPutsTheLauncherInItsLeafOfTheDelegatedSubtree(t *testing.T) {
	for _, subgroup := range []bool{true, false} {
		h, cg := unplacedHost(t, func(cg *cgroupModel) { cg.systemdDelegates(testService, testPid, subgroup) })
		if err := h.placeCgroups(); err != nil {
			t.Fatalf("subgroup %v: placement: %v", subgroup, err)
		}
		sup, jobs := filepath.Join(testService, supervisorLeaf), h.jobsCgroup()
		if self, _ := cg.Of(testPid); self != sup {
			t.Fatalf("subgroup %v: THE LAUNCHER IS NOT IN ITS LEAF: %s", subgroup, self)
		}
		if procs, _ := cg.Read(testService, "cgroup.procs"); procs != "" {
			t.Fatalf("subgroup %v: a process left in the service cgroup: %q", subgroup, procs)
		}
		for _, p := range []string{testService, jobs} {
			if got := enabledIn(cg, p); !slices.Equal(got, cgroupControllers) {
				t.Fatalf("subgroup %v: %s enables %v, want %v", subgroup, p, got, cgroupControllers)
			}
		}
		id := launcher.IDFor("t", "0v1")
		if err := h.CreateCgroup(id, 2, 1152); err != nil {
			t.Fatalf("subgroup %v: create: %v", subgroup, err)
		}
		leaf, jailer := h.cgroupPaths(id)
		for _, p := range []string{leaf, jailer} {
			if !strings.HasPrefix(p, jobs+"/") {
				t.Fatalf("subgroup %v: A JOB CGROUP OUTSIDE THE JOBS CGROUP: %s", subgroup, p)
			}
		}
		if ctl, _ := cg.Read(leaf, "cgroup.controllers"); missingControllers(ctl) != nil || !cg.has(leaf) {
			t.Fatalf("subgroup %v: the leaf %s has controllers %q", subgroup, leaf, ctl)
		}
		if v, _ := cg.Read(leaf, "memory.max"); v != "1152M" {
			t.Fatalf("subgroup %v: the leaf's memory.max %q", subgroup, v)
		}
		// a second serve over the same subtree places the same way
		if err := h.placeCgroups(); err != nil {
			t.Fatalf("subgroup %v: a second placement: %v", subgroup, err)
		}
	}
}

// Every job cgroup is under the service cgroup, never at the root: the
// layout rule refuses a jobs cgroup or service cgroup that would put one
// elsewhere, and a job id that is not one plain name is never made, given
// to the jailer or removed.
func TestJobCgroupsAreNeverOutsideTheDelegatedSubtree(t *testing.T) {
	good := &Config{CgroupService: testService, CgroupParent: "jobs"}
	if p := cgroupLayoutProblems(good); len(p) != 0 {
		t.Fatalf("the delegated layout refused: %v", p)
	}
	bad := map[string]*Config{
		"a jobs parent outside the subtree": {CgroupService: testService, CgroupParent: "../escape"},
		"an absolute jobs parent":           {CgroupService: testService, CgroupParent: "/urgit.slice"},
		"a nested jobs parent":              {CgroupService: testService, CgroupParent: "a/b"},
		"the supervisor leaf as jobs":       {CgroupService: testService, CgroupParent: supervisorLeaf},
		"a unit name as jobs":               {CgroupService: testService, CgroupParent: "urgit-ci-p4-opus.slice"},
		"no jobs parent":                    {CgroupService: testService},
		"the cgroup root as the service":    {CgroupService: "", CgroupParent: "jobs"},
		"an absolute service":               {CgroupService: "/system.slice/urgit-vm-launcher.service", CgroupParent: "jobs"},
		"a service climbing out":            {CgroupService: "system.slice/../../x.service", CgroupParent: "jobs"},
		"a slice as the service":            {CgroupService: "urgit-ci-p4-opus.slice", CgroupParent: "jobs"},
	}
	for name, c := range bad {
		if p := cgroupLayoutProblems(c); len(p) == 0 {
			t.Fatalf("%s: A LAYOUT OUTSIDE THE DELEGATED SUBTREE WAS ACCEPTED: %+v", name, c)
		}
	}
	h := testHost(t, &recorder{})
	cg := h.cgroups.(*cgroupModel)
	before := len(cg.writes)
	for _, id := range []string{"../t-escape", "a/b", "..", ""} {
		if err := h.CreateCgroup(id, 1, 1152); !errors.Is(err, launcher.ErrNoEffect) {
			t.Fatalf("create of %q: %v", id, err)
		}
		if err := h.RemoveCgroup(id); err == nil {
			t.Fatalf("A CGROUP OUTSIDE THE JOBS CGROUP WAS REMOVED: %q", id)
		}
	}
	if len(cg.writes) != before || cg.has("t-escape") || cg.has(filepath.Join(testService, "t-escape")) {
		t.Fatalf("a refused id wrote %q", cg.writes[before:])
	}
	// a layout made wrong after start is refused at each step, too
	h.cfg.CgroupParent = "../escape"
	id := launcher.IDFor("t", "0v1")
	if err := h.CreateCgroup(id, 1, 1152); !errors.Is(err, launcher.ErrNoEffect) || cg.has(filepath.Join(testService, "..", "escape", id)) {
		t.Fatalf("A JOB CGROUP WAS MADE OUTSIDE THE DELEGATED SUBTREE: %v", err)
	}
}

// Placement refuses, and writes nothing, for each way the subtree is not
// the delegated one the launcher needs: the launcher running outside its
// service cgroup or a jobs parent outside it; a service cgroup systemd did
// not mark delegated; a controller missing from it; a process left in the
// service cgroup or the jobs cgroup, which are inner nodes.
func TestPlacementRefusesWhatIsNotTheDelegatedSubtree(t *testing.T) {
	cases := map[string]struct {
		setup  func(cg *cgroupModel)
		config func(c *Config)
		want   string
	}{
		"the launcher outside its service cgroup": {setup: func(cg *cgroupModel) {
			cg.systemdDelegates(testService, 1, true)
			cg.MkdirAll("user.slice/session-2.scope")
			cg.run("user.slice/session-2.scope", "4100")
		}, want: "not in its delegated service cgroup"},
		"a jobs parent outside the subtree": {setup: func(cg *cgroupModel) { cg.systemdDelegates(testService, testPid, true) },
			config: func(c *Config) { c.CgroupParent = "../escape" }, want: "cgroup_parent must be"},
		"a service cgroup not delegated": {setup: func(cg *cgroupModel) {
			cg.systemdDelegates(testService, testPid, true)
			cg.delegated[testService] = false
		}, want: "not delegated by systemd"},
		"a missing controller": {setup: func(cg *cgroupModel) {
			cg.systemdDelegates(testService, testPid, true)
			cg.enabled["system.slice"] = []string{"cpu", "memory"}
		}, want: "lacks the pids controller"},
		"a process left in the service cgroup": {setup: func(cg *cgroupModel) {
			cg.systemdDelegates(testService, testPid, true)
			cg.procs[testService] = []string{"5150"}
		}, want: "a process left in the service cgroup"},
		"a process left in the jobs cgroup": {setup: func(cg *cgroupModel) {
			cg.systemdDelegates(testService, testPid, true)
			cg.MkdirAll(filepath.Join(testService, "jobs"))
			cg.procs[filepath.Join(testService, "jobs")] = []string{"5151"}
		}, want: "a process left in the jobs cgroup"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, cg := unplacedHost(t, c.setup)
			if c.config != nil {
				c.config(h.cfg)
			}
			before := len(cg.writes)
			err := h.placeCgroups()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("PLACEMENT ACCEPTED A SUBTREE THAT IS NOT THE DELEGATED ONE: %v (want %q)", err, c.want)
			}
			// the inner-node refusals come after the service cgroup's own
			// controllers are enabled; none before writes anything
			if name != "a process left in the jobs cgroup" && len(cg.writes) != before {
				t.Fatalf("a refused placement wrote %q", cg.writes[before:])
			}
			id := launcher.IDFor("t", "0v1")
			if leaf, _ := h.cgroupPaths(id); h.CreateCgroup(id, 1, 1152) == nil && cg.has(leaf) && name != "a process left in the jobs cgroup" {
				t.Fatalf("a job cgroup was made after a refused placement: %s", leaf)
			}
		})
	}
}

// check names the cgroup problems among its own: a jobs parent outside the
// delegated subtree, a missing controller at the root, and — while the
// service runs — a service cgroup systemd does not mark delegated, or one
// lacking a controller. The model host passes none of them.
func TestCheckRefusesCgroupsOutsideTheDelegatedSubtree(t *testing.T) {
	cgroupLines := func(h *realHost) []string {
		// check's image lines name a manifest digest's first 12 characters:
		// the test host's one-word image is no image here
		h.images = map[string]launcher.Image{}
		var out []string
		if err := h.check(); err != nil {
			for _, l := range strings.Split(err.Error(), "\n") {
				if strings.Contains(l, "cgroup") {
					out = append(out, strings.TrimSpace(l))
				}
			}
		}
		return out
	}
	h := testHost(t, &recorder{})
	if got := cgroupLines(h); len(got) != 0 {
		t.Fatalf("check refused the delegated subtree: %q", got)
	}
	cases := map[string]struct {
		spoil func(h *realHost, cg *cgroupModel)
		want  string
	}{
		"a jobs parent outside the subtree": {func(h *realHost, cg *cgroupModel) { h.cfg.CgroupParent = "../escape" }, "cgroup_parent must be"},
		"a missing root controller":         {func(h *realHost, cg *cgroupModel) { cg.rootCtl = "cpu memory" }, "cpu, memory and pids controllers is required"},
		"a service cgroup not delegated":    {func(h *realHost, cg *cgroupModel) { cg.delegated[testService] = false }, "not delegated by systemd"},
		"a service cgroup missing pids":     {func(h *realHost, cg *cgroupModel) { cg.enabled["system.slice"] = []string{"cpu", "memory"} }, "lacks the pids controller"},
	}
	for name, c := range cases {
		h := testHost(t, &recorder{})
		c.spoil(h, h.cgroups.(*cgroupModel))
		if got := cgroupLines(h); !slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, c.want) }) {
			t.Fatalf("%s: CHECK PASSED A CGROUP OUTSIDE THE DELEGATED SUBTREE: %q (want %q)", name, got, c.want)
		}
	}
}

// The recovery pass removes a cgroup only inside the delegated subtree: an
// interrupted teardown found at open, retried through the core, leaves a
// job cgroup whose service cgroup systemd does not mark delegated as it is
// — still held, still quarantined — and a process left in the job's leaf,
// an inner node above the jailer's cgroup, keeps the whole subtree; the
// removal order (the jailer's cgroup, then the leaf) is kept once it can go.
func TestRecoveryPassRefusesACgroupOutsideTheDelegatedSubtree(t *testing.T) {
	h := testHost(t, &recorder{})
	cg := h.cgroups.(*cgroupModel)
	st := interrupted("0vrc", launcher.StateStopping)
	st.HasDisk = false
	writeRecord(t, h.cfg.StateDir, st)
	leaf, jailer := h.cgroupPaths(st.ID)
	if err := cg.MkdirAll(jailer); err != nil {
		t.Fatal(err)
	}
	cg.delegated[testService] = false
	in, err := act(h, st.Selection(), "retry")
	if err == nil && !slices.Contains(in.Remaining, "cgroup") {
		t.Fatalf("THE RECOVERY PASS REMOVED A CGROUP OUTSIDE THE DELEGATED SUBTREE: %v, remaining %v", err, in.Remaining)
	}
	if !cg.has(leaf) || !cg.has(jailer) {
		t.Fatalf("a cgroup of a subtree not delegated was removed: leaf %v, jailer %v", cg.has(leaf), cg.has(jailer))
	}
	cg.delegated[testService] = true
	cg.run(leaf, "6160") // a process left in the leaf, above the jailer's cgroup
	if err := h.RemoveCgroup(st.ID); err == nil || !strings.Contains(err.Error(), "6160") || !cg.has(leaf) || !cg.has(jailer) {
		t.Fatalf("A PROCESS LEFT IN A NON-LEAF CGROUP DID NOT KEEP ITS SUBTREE: %v (leaf %v, jailer %v)", err, cg.has(leaf), cg.has(jailer))
	}
	cg.run(leaf)
	cg.mu.Lock()
	before := len(cg.writes)
	cg.mu.Unlock()
	if err := h.RemoveCgroup(st.ID); err != nil || cg.has(leaf) || cg.has(jailer) || !cg.has(h.jobsCgroup()) {
		t.Fatalf("the delegated subtree's job cgroup: %v (leaf %v, jailer %v, jobs %v)", err, cg.has(leaf), cg.has(jailer), cg.has(h.jobsCgroup()))
	}
	if len(cg.writes) != before {
		t.Fatalf("the removal wrote %q", cg.writes[before:])
	}
}

// The unit asks systemd for the delegation (Delegate=yes) and starts the
// launcher in its leaf (DelegateSubgroup=supervisor); the example
// configuration names the unit's own service cgroup and a jobs cgroup the
// layout rule accepts, and loads.
func TestUnitDelegatesTheServiceCgroup(t *testing.T) {
	unit := filepath.Join("..", "..", "launcher", "urgit-vm-launcher.service")
	f, err := os.Open(unit)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	keys, section := map[string]string{}, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && section == "[Service]" && !strings.HasPrefix(line, "#") {
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if keys["Delegate"] != "yes" || keys["DelegateSubgroup"] != supervisorLeaf {
		t.Fatalf("THE UNIT DOES NOT DELEGATE ITS CGROUP: Delegate=%q DelegateSubgroup=%q", keys["Delegate"], keys["DelegateSubgroup"])
	}
	cfg, err := loadConfig(filepath.Join("..", "..", "launcher", "urgit-vm-launcher.toml.example"))
	if err != nil {
		t.Fatalf("the example configuration: %v", err)
	}
	if cfg.CgroupService != "system.slice/"+filepath.Base(unit) {
		t.Fatalf("the example's cgroup_service %q is not the unit's own service cgroup system.slice/%s", cfg.CgroupService, filepath.Base(unit))
	}
}
