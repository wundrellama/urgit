package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write a config file with the given body plus the always-required keys
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "urgit-runner.toml")
	full := "ship_url = \"http://127.0.0.1:8470\"\nwork_dir = \"" + filepath.Join(dir, "work") + "\"\nstate_file = \"" + filepath.Join(dir, "state.json") + "\"\n" + body
	if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// an image directory with a manifest, as the recipe leaves it
func imageDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":1}`), 0o644)
	return dir
}

func mustFail(t *testing.T, path string, want ...string) {
	t.Helper()
	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected a refusal naming %v", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("refusal %q does not name %q", err.Error(), w)
		}
	}
}

// rider 04: an omitted sandbox is microvm, with its defaults
func TestDefaultsAreMicrovm(t *testing.T) {
	img := imageDir(t)
	c, err := Load(write(t, "launcher_socket = \"/run/x/l.sock\"\nimage_path = \""+img+"\"\nact_image = \"docker.io/catthehacker/ubuntu:act-latest\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sandbox != "microvm" || c.Capacity != 1 || c.CPUs != 2 || c.MemoryMiB != 4096 || c.DiskMiB != 20480 {
		t.Fatalf("defaults: %+v", c)
	}
	if c.OverheadMiB() != 1024 {
		t.Fatalf("overhead %d", c.OverheadMiB())
	}
}

func TestMicrovmRequiresLauncherImageAndBudgets(t *testing.T) {
	img := imageDir(t)
	mustFail(t, write(t, "image_path = \""+img+"\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"), "launcher_socket is required")
	mustFail(t, write(t, "launcher_socket = \"/run/x/l.sock\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"), "image_path is required")
	// a path without a manifest is not an image
	empty := t.TempDir()
	mustFail(t, write(t, "launcher_socket = \"/run/x/l.sock\"\nimage_path = \""+empty+"\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"), "manifest.json")
	mustFail(t, write(t, "launcher_socket = \"/run/x/l.sock\"\nimage_path = \""+img+"\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"), "act_image is required")
	mustFail(t, write(t, "launcher_socket = \"/run/x/l.sock\"\nimage_path = \""+img+"\"\nact_image = \"x\"\n"), "budget_cpus is required", "budget_memory_mib is required")
}

func TestLimitsAreValidated(t *testing.T) {
	img := imageDir(t)
	base := "launcher_socket = \"/run/x/l.sock\"\nimage_path = \"" + img + "\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"
	mustFail(t, write(t, base+"cpus = 0\n"), "cpus must be at least 1")
	mustFail(t, write(t, base+"cpus = -2\n"), "cpus must be at least 1")
	mustFail(t, write(t, base+"memory_mib = 64\n"), "memory_mib must be at least 128")
	mustFail(t, write(t, base+"memory_mib = -1\n"), "memory_mib")
	mustFail(t, write(t, base+"disk_mib = 0\n"), "disk_mib must be at least 1024")
	mustFail(t, write(t, base+"disk_mib = -5\n"), "disk_mib")
	mustFail(t, write(t, base+"capacity = 0\n"), "capacity must be at least 1")
	// over budget: capacity × cpus and capacity × (memory + overhead)
	mustFail(t, write(t, base+"capacity = 5\n"), "capacity 5 × cpus 2 = 10 exceeds budget_cpus 8")
	mustFail(t, write(t, base+"capacity = 4\ncpus = 1\n"), "capacity 4 × (memory_mib 4096 + 1024 overhead) = 20480 exceeds budget_memory_mib 18432")
	// exactly at budget is fine: 2 × 4 cpus = 8; 2 × (8192+1024) = 18432
	if _, err := Load(write(t, base+"capacity = 2\ncpus = 4\nmemory_mib = 8192\n")); err != nil {
		t.Fatalf("at-budget config refused: %v", err)
	}
}

// docker-rootless is an explicit opt-in that keeps its P1–P3 shape and
// refuses the microvm-only keys rather than ignoring them
func TestDockerRootlessIsExplicitAndRefusesMicrovmKeys(t *testing.T) {
	c, err := Load(write(t, "sandbox = \"docker-rootless\"\ndocker_host = \"unix:///run/user/1000/docker.sock\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sandbox != "docker-rootless" || c.ActImage != "catthehacker/ubuntu:act-latest" || c.CPUs != 0 || c.DiskMiB != 0 {
		t.Fatalf("docker defaults changed: %+v", c)
	}
	mustFail(t, write(t, "sandbox = \"docker-rootless\"\n"), "docker_host is required")
	mustFail(t, write(t, "sandbox = \"docker-rootless\"\ndocker_host = \"unix:///x\"\ndisk_mib = 1024\n"), "disk_mib is not enforced by docker-rootless")
	mustFail(t, write(t, "sandbox = \"docker-rootless\"\ndocker_host = \"unix:///x\"\nimage_path = \"/x\"\n"), "image_path is a microvm key")
	mustFail(t, write(t, "sandbox = \"lxc\"\n"), "unknown sandbox")
	// a profile under docker-rootless is NAT egress whole: refused unless
	// the operator acknowledges that in the config
	profile := "sandbox = \"docker-rootless\"\ndocker_host = \"unix:///x\"\n[[network_profiles]]\nname = \"egress\"\ndestinations = [\"tcp:140.82.112.3:443\"]\n"
	mustFail(t, write(t, profile), "docker_egress_unrestricted_ack")
	if _, err := Load(write(t, "docker_egress_unrestricted_ack = true\n"+profile)); err != nil {
		t.Fatalf("acknowledged docker profile refused: %v", err)
	}
}

func TestNetworkProfilesAreValidated(t *testing.T) {
	img := imageDir(t)
	base := "launcher_socket = \"/run/x/l.sock\"\nimage_path = \"" + img + "\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"
	c, err := Load(write(t, base+"[[network_profiles]]\nname = \"integration\"\ndestinations = [\"tcp:198.51.100.20:8472\", \"udp:198.51.100.20:53\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.NetworkProfiles) != 1 || c.NetworkProfiles[0].Name != "integration" || len(c.NetworkProfiles[0].Destinations) != 2 {
		t.Fatalf("profiles: %+v", c.NetworkProfiles)
	}
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"\"\ndestinations = [\"tcp:198.51.100.20:8472\"]\n"), "network profile name is required")
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"locked\"\ndestinations = [\"tcp:198.51.100.20:8472\"]\n"), "locked is reserved")
	// a DNS name is a destination (CI-P4-NET-1, named destinations); the
	// launcher resolves and pins it, the runner never does
	named, err := Load(write(t, base+"[[network_profiles]]\nname = \"apt\"\ndestinations = [\"tcp:archive.ubuntu.com:80\", \"tcp:bootstrap.urbit.org:443\"]\n"))
	if err != nil || len(named.NetworkProfiles[0].Destinations) != 2 {
		t.Fatalf("a profile of DNS names was refused: %v", err)
	}
	for _, d := range []string{"tcp:Store.example:8472", "tcp:*.example.com:443", "tcp:localhost:80", "tcp:store.example.:8472", "tcp:host_1.example:80", "tcp:[store.example]:8472"} {
		mustFail(t, write(t, base+"[[network_profiles]]\nname = \"a\"\ndestinations = [\""+d+"\"]\n"), "lower-case DNS name")
	}
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"a\"\ndestinations = [\"198.51.100.20:8472\"]\n"), "proto:host:port")
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"a\"\ndestinations = [\"tcp:198.51.100.20:70000\"]\n"), "port")
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"a\"\ndestinations = []\n"), "at least one destination")
	mustFail(t, write(t, base+"[[network_profiles]]\nname = \"a\"\ndestinations = [\"tcp:1.2.3.4:1\"]\n[[network_profiles]]\nname = \"a\"\ndestinations = [\"tcp:1.2.3.4:2\"]\n"), "duplicate network profile")
}

func TestLabelsStillValidated(t *testing.T) {
	img := imageDir(t)
	base := "launcher_socket = \"/run/x/l.sock\"\nimage_path = \"" + img + "\"\nact_image = \"x\"\nbudget_cpus = 8\nbudget_memory_mib = 18432\n"
	mustFail(t, write(t, base+"labels = [\"big mem\"]\n"), "labels carry no commas or spaces")
	c, err := Load(write(t, base+"labels = [\"networked\", \"networked\", \" big-mem \"]\n"))
	if err != nil || len(c.Labels) != 2 {
		t.Fatalf("labels: %v %+v", err, c.Labels)
	}
}
