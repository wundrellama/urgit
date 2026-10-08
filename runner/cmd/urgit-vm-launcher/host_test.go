package main

// The host adapter on private roots (runner/launcher/INTEGRATION.md §7.3):
// a cgroup-filesystem model with the kernel semantics the adapter relies
// on, a private jail base, named-namespace directory and /proc, and
// commands recorded, never executed.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"urgit/runner/internal/launcher"
)

// cgroupModel is a private cgroup v2 filesystem: a cgroup is a directory
// whose interface files are not contents; one that has processes or child
// cgroups cannot be removed (EBUSY); cgroup.procs lists its own processes.
// C1 adds the kernel's rules the delegated layout relies on: a cgroup's
// cgroup.controllers are those its parent enables (the root's are given);
// cgroup.subtree_control enables only those (ENOENT otherwise), and not in
// a non-root cgroup that has processes (EBUSY: no internal processes);
// writing a pid to cgroup.procs moves it, and not into a non-root cgroup
// whose subtree_control enables a controller (EBUSY); systemd's delegation
// mark (Delegated) is the model's delegated set.
type cgroupModel struct {
	mu        sync.Mutex
	groups    map[string]bool
	files     map[string]map[string]string
	procs     map[string][]string
	writes    []string
	enabled   map[string][]string // each cgroup's subtree_control
	delegated map[string]bool
	rootCtl   string // the root's cgroup.controllers
}

func newCgroupModel() *cgroupModel {
	return &cgroupModel{groups: map[string]bool{"": true}, files: map[string]map[string]string{}, procs: map[string][]string{},
		enabled: map[string][]string{}, delegated: map[string]bool{}, rootCtl: "cpu memory pids"}
}

// controllers is p's cgroup.controllers; the lock is held.
func (c *cgroupModel) controllers(p string) []string {
	if p == "" {
		return strings.Fields(c.rootCtl)
	}
	return c.enabled[cgParent(p)]
}

// systemdDelegates is systemd's part of the layout on the model: the root
// and every ancestor of the service cgroup enable cpu, memory and pids for
// their children, the service cgroup exists, marked delegated, and pid
// runs in its supervisor leaf (DelegateSubgroup=), or in the service
// cgroup itself (a systemd without it) when subgroup is false.
func (c *cgroupModel) systemdDelegates(service string, pid int, subgroup bool) {
	c.MkdirAll(service)
	c.mu.Lock()
	defer c.mu.Unlock()
	for q := cgParent(service); ; q = cgParent(q) {
		c.enabled[q] = []string{"cpu", "memory", "pids"}
		if q == "" {
			break
		}
	}
	c.delegated[service] = true
	where := service
	if subgroup {
		where = filepath.Join(service, supervisorLeaf)
		c.groups[where] = true
	}
	c.procs[where] = append(c.procs[where], fmt.Sprint(pid))
}

func (c *cgroupModel) Delegated(p string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	if !c.groups[p] {
		return false, cgErr("getxattr", p, syscall.ENOENT)
	}
	return c.delegated[p], nil
}

func (c *cgroupModel) Of(pid int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p, pids := range c.procs {
		if slices.Contains(pids, fmt.Sprint(pid)) {
			return p, nil
		}
	}
	return "", cgErr("open", fmt.Sprintf("/proc/%d/cgroup", pid), syscall.ENOENT)
}

func cgClean(p string) string {
	if p = filepath.Clean(p); p == "." {
		return ""
	}
	return p
}

func cgParent(p string) string { return cgClean(filepath.Dir(p)) }

func cgErr(op, p string, errno syscall.Errno) error {
	return &os.PathError{Op: op, Path: p, Err: errno}
}

func (c *cgroupModel) MkdirAll(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for q := cgClean(p); q != ""; q = cgParent(q) {
		c.groups[q] = true
	}
	return nil
}

func (c *cgroupModel) Mkdir(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	switch {
	case c.groups[p]:
		return cgErr("mkdir", p, syscall.EEXIST)
	case !c.groups[cgParent(p)]:
		return cgErr("mkdir", p, syscall.ENOENT)
	}
	c.groups[p] = true
	return nil
}

func (c *cgroupModel) Write(p, file, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	if !c.groups[p] {
		return cgErr("open", filepath.Join(p, file), syscall.ENOENT)
	}
	switch file {
	case "cgroup.subtree_control":
		on := slices.Clone(c.enabled[p])
		for _, tok := range strings.Fields(value) {
			name := tok[1:]
			if !slices.Contains(c.controllers(p), name) {
				return cgErr("write", filepath.Join(p, file), syscall.ENOENT)
			}
			if tok[0] == '+' && !slices.Contains(on, name) {
				if p != "" && len(c.procs[p]) > 0 {
					return cgErr("write", filepath.Join(p, file), syscall.EBUSY)
				}
				on = append(on, name)
			} else if tok[0] == '-' {
				on = slices.DeleteFunc(on, func(x string) bool { return x == name })
			}
		}
		c.enabled[p] = on
	case "cgroup.procs":
		if p != "" && len(c.enabled[p]) > 0 {
			return cgErr("write", filepath.Join(p, file), syscall.EBUSY)
		}
		for q := range c.procs {
			c.procs[q] = slices.DeleteFunc(c.procs[q], func(x string) bool { return x == value })
		}
		c.procs[p] = append(c.procs[p], value)
	}
	if c.files[p] == nil {
		c.files[p] = map[string]string{}
	}
	c.files[p][file] = value
	c.writes = append(c.writes, filepath.Join(p, file)+"="+value)
	return nil
}

func (c *cgroupModel) Read(p, file string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	if !c.groups[p] {
		return "", cgErr("open", filepath.Join(p, file), syscall.ENOENT)
	}
	switch file {
	case "cgroup.procs":
		return strings.Join(c.procs[p], "\n"), nil
	case "cgroup.controllers":
		return strings.Join(c.controllers(p), " "), nil
	case "cgroup.subtree_control":
		return strings.Join(c.enabled[p], " "), nil
	}
	return c.files[p][file], nil
}

func (c *cgroupModel) Children(p string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	if !c.groups[p] {
		return nil, cgErr("open", p, syscall.ENOENT)
	}
	var kids []string
	for q := range c.groups {
		if q != "" && cgParent(q) == p {
			kids = append(kids, filepath.Base(q))
		}
	}
	sort.Strings(kids)
	return kids, nil
}

func (c *cgroupModel) Rmdir(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p = cgClean(p)
	if !c.groups[p] {
		return cgErr("rmdir", p, syscall.ENOENT)
	}
	if len(c.procs[p]) > 0 {
		return cgErr("rmdir", p, syscall.EBUSY)
	}
	for q := range c.groups {
		if q != p && cgParent(q) == p {
			return cgErr("rmdir", p, syscall.EBUSY)
		}
	}
	delete(c.groups, p)
	delete(c.files, p)
	delete(c.enabled, p)
	return nil
}

func (c *cgroupModel) Exists(p string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.groups[cgClean(p)], nil
}

// run puts processes into a cgroup (what the jailer does for the VMM).
func (c *cgroupModel) run(p string, pids ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.procs[cgClean(p)] = pids
}

func (c *cgroupModel) has(p string) bool {
	ok, _ := c.Exists(p)
	return ok
}

// fakeProcess makes pid, in the host's private /proc, the jailed VMM of id.
func fakeProcess(t *testing.T, h *realHost, pid int, id string) {
	t.Helper()
	dir := filepath.Join(h.procDir, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte("firecracker\x00--id\x00"+id+"\x00--no-api\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The jailer is given the VM's leaf as its --parent-cgroup and puts the
// VMM in <leaf>/<id> (INTEGRATION.md §7.3): RemoveCgroup sees that
// cgroup's processes and refuses while there are any; once there are none
// it removes the jailer's cgroup and then the leaf — the leaf alone could
// never go while the jailer's cgroup exists under it.
func TestJailerCgroupIsTheOneRemoved(t *testing.T) {
	rec := &recorder{}
	h := testHost(t, rec)
	cg := h.cgroups.(*cgroupModel)
	id := launcher.IDFor("t", "0v1")
	leaf, jailer := h.cgroupPaths(id)
	if err := h.CreateCgroup(id, 2, 1152); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{leaf + "/cpu.max=200000 100000", leaf + "/memory.max=1152M", leaf + "/memory.swap.max=0", leaf + "/pids.max=512"} {
		if !slices.Contains(cg.writes, want) {
			t.Fatalf("the leaf's limits: %q, want %q", cg.writes, want)
		}
	}
	if err := os.MkdirAll(h.jailRoot(id), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.jailRoot(id), "firecracker.pid"), []byte("4242\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeProcess(t, h, 4242, id)
	pid, err := h.StartVM(id, launcher.VMSpec{Image: launcher.Image{BootArgs: "console=ttyS0"}, CPUs: 2, MemoryMiB: 128, CID: 3, Attempt: "0v1"})
	if err != nil || pid != 4242 {
		t.Fatalf("start: %d %v", pid, err)
	}
	if len(rec.unowned) != 1 || !strings.Contains(rec.unowned[0], "/jailer ") {
		t.Fatalf("the jailer must run outside an owned process group (its --daemonize calls setsid): unowned %q", rec.unowned)
	}
	f := strings.Fields(rec.unowned[0])
	i := slices.Index(f, "--parent-cgroup")
	if i < 0 || filepath.Join(f[i+1], id) != jailer {
		t.Fatalf("THE JAILER'S CGROUP IS NOT THE ONE REMOVECGROUP REMOVES: --parent-cgroup %v, jailer cgroup %s", f, jailer)
	}
	// the jailer's cgroup, where the VMM runs
	if err := cg.MkdirAll(jailer); err != nil {
		t.Fatal(err)
	}
	cg.run(jailer, "4242")
	err = h.RemoveCgroup(id)
	if err == nil || !strings.Contains(err.Error(), "4242") || !cg.has(jailer) || !cg.has(leaf) {
		t.Fatalf("a cgroup whose VMM still runs: %v (jailer cgroup there %v, leaf %v)", err, cg.has(jailer), cg.has(leaf))
	}
	cg.run(jailer) // the VMM is gone
	if err := h.RemoveCgroup(id); err != nil || cg.has(jailer) || cg.has(leaf) {
		t.Fatalf("an empty subtree: %v (jailer cgroup there %v, leaf %v)", err, cg.has(jailer), cg.has(leaf))
	}
	if !cg.has(h.jobsCgroup()) {
		t.Fatal("the launcher's jobs cgroup was removed")
	}
	if err := h.RemoveCgroup(id); err != nil {
		t.Fatalf("a second removal (nothing there): %v", err)
	}
}

// A create step whose per-attempt object already exists refuses without
// effect and leaves the object as it found it: the jail directory, the
// cgroup leaf (INTEGRATION.md §7.3).
func TestCreateStepsRefusePreexistingObjects(t *testing.T) {
	id := launcher.IDFor("t", "0v1")
	t.Run("jail", func(t *testing.T) {
		rec := &recorder{}
		h := testHost(t, rec)
		dir := filepath.Join(h.cfg.JailBase, "firecracker", id)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		sentinel := filepath.Join(dir, "foreign")
		if err := os.WriteFile(sentinel, []byte("not this reservation's"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := h.PrepareDisk(id, launcher.Image{Rootfs: "/model/rootfs", Kernel: "/model/vmlinux"}, 2)
		entries, _ := os.ReadDir(dir)
		if !errors.Is(err, launcher.ErrNoEffect) || len(rec.calls) != 0 || len(entries) != 1 {
			t.Fatalf("A PREEXISTING JAIL WAS TAKEN OVER: %v; commands %q; entries now %d", err, rec.calls, len(entries))
		}
	})
	t.Run("cgroup", func(t *testing.T) {
		h := testHost(t, &recorder{})
		cg := h.cgroups.(*cgroupModel)
		leaf, _ := h.cgroupPaths(id)
		if err := cg.MkdirAll(leaf); err != nil {
			t.Fatal(err)
		}
		cg.run(leaf, "777")
		err := h.CreateCgroup(id, 1, 1152)
		if !errors.Is(err, launcher.ErrNoEffect) || slices.ContainsFunc(cg.writes, func(w string) bool { return strings.HasPrefix(w, leaf+"/") }) {
			t.Fatalf("A PREEXISTING CGROUP WAS TAKEN OVER: %v; writes %q", err, cg.writes)
		}
	})
}

// Every removal acts on its own id and index only; another VM's jail,
// cgroup and per-VM chain — unrelated sentinels — stay.
func TestRemovalsLeaveUnrelatedSentinels(t *testing.T) {
	listing := "table inet urgit-test { # handle 1\n\tchain vm-1 { # handle 4\n\t\tdrop # handle 5\n\t}\n\tchain vm-15 { # handle 6\n\t\tdrop # handle 7\n\t}\n}\n"
	rec := &recorder{reply: func(cmd string) (string, bool, error) {
		if cmd == "nft -a list table inet urgit-test" {
			return listing, true, nil
		}
		return "", true, nil
	}}
	h := testHost(t, rec)
	cg := h.cgroups.(*cgroupModel)
	mine, other := launcher.IDFor("t", "0v1"), launcher.IDFor("t", "0v15")
	for _, id := range []string{mine, other} {
		if err := os.MkdirAll(h.jailRoot(id), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.jailRoot(id), "disk.ext4"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, jailer := h.cgroupPaths(id)
		if err := cg.MkdirAll(jailer); err != nil {
			t.Fatal(err)
		}
	}
	for _, err := range []error{h.RemoveNetwork(mine, 1), h.RemoveCgroup(mine), h.RemoveDisk(mine), h.RemoveJail(mine)} {
		if err != nil {
			t.Fatalf("removal: %v", err)
		}
	}
	otherLeaf, otherJailer := h.cgroupPaths(other)
	if _, err := os.Stat(filepath.Join(h.jailRoot(other), "disk.ext4")); err != nil || !cg.has(otherLeaf) || !cg.has(otherJailer) {
		t.Fatalf("ANOTHER VM'S OBJECTS WERE REMOVED: its disk %v, cgroups %v %v", err, cg.has(otherLeaf), cg.has(otherJailer))
	}
	for _, c := range rec.calls {
		if strings.Contains(c, "vm-15") || strings.Contains(c, other) {
			t.Fatalf("a command touched another VM: %s", c)
		}
	}
	if _, err := os.Stat(filepath.Join(h.cfg.JailBase, "firecracker", mine)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("its own jail stayed: %v", err)
	}
}

// A view of the host from Bound carries its one deadline into every
// command, and a view whose context has ended starts nothing: the step
// reports that it had no effect, since nothing ran (INTEGRATION.md §7.2).
func TestBoundViewCarriesItsDeadline(t *testing.T) {
	rec := &recorder{reply: networkHost("")}
	h := testHost(t, rec)
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	v := h.Bound(ctx)
	id := launcher.IDFor("t", "0v1")
	if _, err := v.CreateNetwork(id, 1, []string{"tcp:192.0.2.1:8472"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := v.RemoveNetwork(id, 1); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(rec.deadlines) == 0 {
		t.Fatal("no command recorded")
	}
	for i, d := range rec.deadlines {
		if !d.Equal(deadline) {
			t.Fatalf("COMMAND %d (%s) NOT BOUNDED BY ITS VIEW'S DEADLINE: %v, want %v", i, rec.calls[i], d, deadline)
		}
	}
	ended, stop := context.WithCancel(context.Background())
	stop()
	quiet := &recorder{reply: networkHost("")}
	h2 := testHost(t, quiet)
	_, err := h2.Bound(ended).CreateNetwork(id, 1, []string{"tcp:192.0.2.1:8472"})
	if !errors.Is(err, launcher.ErrNoEffect) || len(quiet.calls) != 0 {
		t.Fatalf("an ended view ran %q: %v", quiet.calls, err)
	}
	if err := h2.Bound(ended).RemoveJail(id); err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("an ended view's removal: %v", err)
	}
}
