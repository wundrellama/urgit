package main

// C6 (P4-VM-STAGE-A-SOURCE-01, supplement-15): under ProtectSystem=strict,
// systemd refuses to start a unit whose ReadWritePaths= lists a path that
// does not exist (226/NAMESPACE) unless the entry is optional (`-`). Every
// writable path of the unit is one the install creates before the unit
// starts, or is optional. The unit file is read as written; nothing is
// installed or started.

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installCreated is every writable path that exists before the unit starts:
// the protected root the install creates (rider 01), the unit's own runtime
// directory (RuntimeDirectory=, which systemd creates under /run), and the
// cgroup filesystem.
var installCreated = []string{"/var/lib/urgit-ci-p4-opus", "/run/urgit-ci-p4-opus", "/sys/fs/cgroup"}

// serviceValues is every value of every key of the unit's [Service] section,
// in order (a key may repeat), comments and other sections aside.
func serviceValues(t *testing.T, path string) map[string][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, section := map[string][]string{}, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "["):
			section = line
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || section != "[Service]":
		default:
			if k, v, ok := strings.Cut(line, "="); ok {
				out[strings.TrimSpace(k)] = append(out[strings.TrimSpace(k)], strings.TrimSpace(v))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// writablePathProblems is every ReadWritePaths entry that is neither optional
// (`-`) nor one the install creates; the runtime directory counts only when
// the unit's RuntimeDirectory= names it.
func writablePathProblems(entries, runtimeDirs []string) []string {
	var p []string
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e, "-"):
		case !slices.Contains(installCreated, e):
			p = append(p, e+": neither created by the install nor optional (`-`): systemd would refuse to start the unit while it is missing")
		case strings.HasPrefix(e, "/run/") && !slices.Contains(runtimeDirs, strings.TrimPrefix(e, "/run/")):
			p = append(p, e+": under /run, but not the unit's RuntimeDirectory=")
		}
	}
	return p
}

// The unit lists only writable paths that exist when it starts, or optional
// ones: the protected root, its runtime directory, the cgroup filesystem,
// and -/run/netns (absent until a network namespace exists; Stage B must
// make it writable). The rule itself refuses what C6 found — a plain
// /run/netns — and any other path the install does not create.
func TestUnitWritablePathsExistOrAreOptional(t *testing.T) {
	keys := serviceValues(t, filepath.Join("..", "..", "launcher", "urgit-vm-launcher.service"))
	var entries []string
	for _, v := range keys["ReadWritePaths"] {
		entries = append(entries, strings.Fields(v)...)
	}
	if len(entries) == 0 || keys["ProtectSystem"] == nil {
		t.Fatalf("the unit's ReadWritePaths %q under ProtectSystem %q", entries, keys["ProtectSystem"])
	}
	if p := writablePathProblems(entries, keys["RuntimeDirectory"]); p != nil {
		t.Fatalf("THE UNIT LISTS A WRITABLE PATH THAT MAY BE MISSING WHEN IT STARTS: %q", p)
	}
	if !slices.Contains(entries, "-/run/netns") {
		t.Fatalf("the unit's ReadWritePaths no longer names -/run/netns: %q", entries)
	}
	rules := map[string]struct {
		entries, runtime []string
		refused          bool
	}{
		"C6's plain /run/netns":                {[]string{"/var/lib/urgit-ci-p4-opus", "/run/netns"}, []string{"urgit-ci-p4-opus"}, true},
		"a path the install does not create":   {[]string{"/var/lib/elsewhere"}, nil, true},
		"a runtime dir the unit does not name": {[]string{"/run/urgit-ci-p4-opus"}, nil, true},
		"the unit's own entries":               {[]string{"/var/lib/urgit-ci-p4-opus", "/run/urgit-ci-p4-opus", "/sys/fs/cgroup", "-/run/netns"}, []string{"urgit-ci-p4-opus"}, false},
	}
	for name, r := range rules {
		if got := writablePathProblems(r.entries, r.runtime); (got != nil) != r.refused {
			t.Fatalf("%s: problems %q, want refused %v", name, got, r.refused)
		}
	}
}

// optionalPathProblems is every optional (`-`) ReadWritePaths entry that no
// privileged ExecStartPre (`+/usr/bin/mkdir -p <path>`) creates. systemd
// skips an optional entry that is absent when it sets up the sandbox, so
// under ProtectSystem=strict the path stays read-only and the service
// cannot create it itself (N1, measured with user units under /var/tmp).
func optionalPathProblems(entries, pre []string) []string {
	var p []string
	for _, e := range entries {
		path, optional := strings.CutPrefix(e, "-")
		if optional && !slices.Contains(pre, "+/usr/bin/mkdir -p "+path) {
			p = append(p, path+": optional, but no privileged ExecStartPre creates it, so it stays read-only while absent")
		}
	}
	return p
}

// N1: every optional writable path, /run/netns among them, is created by a
// privileged ExecStartPre before the sandbox is set up, so `ip netns add`
// can create the namespaces under it. The rule refuses the unit as Stage A
// shipped it (the optional entry alone).
func TestUnitCreatesItsOptionalWritablePaths(t *testing.T) {
	keys := serviceValues(t, filepath.Join("..", "..", "launcher", "urgit-vm-launcher.service"))
	var entries []string
	for _, v := range keys["ReadWritePaths"] {
		entries = append(entries, strings.Fields(v)...)
	}
	if p := optionalPathProblems(entries, keys["ExecStartPre"]); p != nil {
		t.Fatalf("AN OPTIONAL WRITABLE PATH STAYS READ-ONLY WHILE ABSENT: %q", p)
	}
	if !slices.Contains(keys["ExecStartPre"], "+/usr/bin/mkdir -p /run/netns") {
		t.Fatalf("no privileged ExecStartPre creates /run/netns: %q", keys["ExecStartPre"])
	}
	stageA := []string{"/var/lib/urgit-ci-p4-opus", "/run/urgit-ci-p4-opus", "/sys/fs/cgroup", "-/run/netns"}
	if optionalPathProblems(stageA, nil) == nil {
		t.Fatal("the rule accepts Stage A's unit, whose -/run/netns stays read-only while absent")
	}
	if optionalPathProblems(stageA, []string{"/usr/bin/mkdir -p /run/netns"}) == nil {
		t.Fatal("the rule accepts an unprivileged ExecStartPre, which runs inside the read-only sandbox")
	}
}
