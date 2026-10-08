// Package config reads the daemon's TOML configuration.
//
// The keys are the contract from BRIEF-CI-P1 D7 (ship_url, enroll_token
// consumed once, sandbox, docker_host, act_binary, act_image, capacity,
// work_dir, state_file), P3's labels, and P4's microvm keys with rider
// 04's defaults: an omitted sandbox is microvm (Firecracker + jailer via
// the launcher), capacity 1, cpus 2, memory_mib 4096 with 1024 MiB of
// host overhead reserved beside it, disk_mib 20480. A VM runner names
// explicit budgets and the launcher socket and image; every limit is
// validated before any work, never ignored. Docker is an explicit opt-in
// (`sandbox = "docker-rootless"`) that keeps its P1–P3 shape.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"urgit/runner/internal/netname"
)

// Rider 04's product defaults.
const (
	DefaultCapacity  = 1
	DefaultCPUs      = 2
	DefaultMemoryMiB = 4096
	DefaultDiskMiB   = 20480
	OverheadMiB      = 1024
	MinMemoryMiB     = 128
	MinDiskMiB       = 1024
)

// NetworkProfile is a runner-declared profile (rider 03): a name the
// ship may authorize for a job and the runner's ceiling of destinations
// for it, each `proto:host:port` where host is an IP literal or a DNS name
// (CI-P4-NET-1, named destinations). The runner never resolves a name: the
// VM launcher resolves and pins it on the host for one reservation.
type NetworkProfile struct {
	Name         string   `toml:"name"`
	Destinations []string `toml:"destinations"`
}

type Config struct {
	ShipURL     string `toml:"ship_url"`
	EnrollToken string `toml:"enroll_token"`
	Sandbox     string `toml:"sandbox"`
	DockerHost  string `toml:"docker_host"`
	ActBinary   string `toml:"act_binary"`
	ActImage    string `toml:"act_image"`
	Capacity    int    `toml:"capacity"`
	WorkDir     string `toml:"work_dir"`
	StateFile   string `toml:"state_file"`
	// the CI public key to verify assignments and grants with, as an
	// operator-pinned override of the one enrollment recorded (D5)
	CIPublicKey string `toml:"ci_public_key"`
	// the labels this daemon declares (CI-P3-SCHED-A): sent at enrollment
	// and on every poll; the ship matches a job's runs-on against them
	// plus its implicit set. which repositories the daemon may run is the
	// ship's to say (the Runners panel), never a key here. A label is
	// placement only: it grants no network and reserves no resource.
	Labels []string `toml:"labels"`

	// microvm backend (P4): the launcher's control socket, the image
	// directory (manifest.json + kernel + rootfs), the per-guest limits and
	// the runner's explicit budgets; the network profiles it supports
	LauncherSocket  string           `toml:"launcher_socket"`
	ImagePath       string           `toml:"image_path"`
	CPUs            int              `toml:"cpus"`
	MemoryMiB       int              `toml:"memory_mib"`
	DiskMiB         int              `toml:"disk_mib"`
	BudgetCPUs      int              `toml:"budget_cpus"`
	BudgetMemoryMiB int              `toml:"budget_memory_mib"`
	NetworkProfiles []NetworkProfile `toml:"network_profiles"`
	// Resolver marks a runner the operator allows to fetch external
	// dependencies during an explicit import (rider 03: import may fetch;
	// execution never does). Default false.
	Resolver bool `toml:"resolver"`
	// DockerEgressUnrestrictedAck is the docker-rootless operator's
	// explicit acknowledgement that a network profile in the compatibility
	// profile is NAT egress as a whole: the destinations are declared and
	// bound into the manifest but not narrowed here (only the VM
	// launcher's policy narrows). Without it, network_profiles under
	// docker-rootless refuse to load. Default false.
	DockerEgressUnrestrictedAck bool `toml:"docker_egress_unrestricted_ack"`
}

// OverheadMiB is the host VMM/helper reservation beside guest RAM.
func (c *Config) OverheadMiB() int { return OverheadMiB }

// ProfileNames is what the daemon advertises to the ship.
func (c *Config) ProfileNames() []string {
	var out []string
	for _, p := range c.NetworkProfiles {
		out = append(out, p.Name)
	}
	return out
}

// Profile finds a declared profile by name.
func (c *Config) Profile(name string) (NetworkProfile, bool) {
	for _, p := range c.NetworkProfiles {
		if p.Name == name {
			return p, true
		}
	}
	return NetworkProfile{}, false
}

// Load reads and validates a configuration file.
func Load(path string) (*Config, error) {
	var c Config
	raw := map[string]any{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if c.Sandbox == "" {
		c.Sandbox = "microvm"
	}
	if _, explicit := raw["capacity"]; !explicit {
		c.Capacity = DefaultCapacity
	}
	if c.ActBinary == "" {
		c.ActBinary = "act"
	}
	var problems []error
	add := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	if c.ShipURL == "" {
		add("ship_url is required")
	}
	if c.WorkDir == "" {
		add("work_dir is required")
	}
	if c.StateFile == "" {
		add("state_file is required")
	}
	if c.Capacity < 1 {
		add("capacity must be at least 1")
	}
	_, hasDisk := raw["disk_mib"]
	_, hasImage := raw["image_path"]
	_, hasLauncher := raw["launcher_socket"]
	switch c.Sandbox {
	case "microvm":
		if c.CPUs == 0 && raw["cpus"] == nil {
			c.CPUs = DefaultCPUs
		}
		if c.MemoryMiB == 0 && raw["memory_mib"] == nil {
			c.MemoryMiB = DefaultMemoryMiB
		}
		if c.DiskMiB == 0 && !hasDisk {
			c.DiskMiB = DefaultDiskMiB
		}
		if c.LauncherSocket == "" {
			add("launcher_socket is required for the microvm sandbox (the administrator's urgit-vm-launcher control socket)")
		}
		if c.ImagePath == "" {
			add("image_path is required for the microvm sandbox (a directory holding manifest.json, the kernel and the rootfs)")
		} else if st, err := os.Stat(filepath.Join(c.ImagePath, "manifest.json")); err != nil || st.IsDir() {
			add("image_path %s holds no manifest.json: not a guest image directory", c.ImagePath)
		}
		if c.ActImage == "" {
			add("act_image is required for the microvm sandbox: the OCI job image preloaded in the guest (the manifest's act_image.reference)")
		}
		if c.CPUs < 1 {
			add("cpus must be at least 1 (got %d)", c.CPUs)
		}
		if c.MemoryMiB < MinMemoryMiB {
			add("memory_mib must be at least %d (got %d)", MinMemoryMiB, c.MemoryMiB)
		}
		if c.DiskMiB < MinDiskMiB {
			add("disk_mib must be at least %d (got %d)", MinDiskMiB, c.DiskMiB)
		}
		if c.BudgetCPUs < 1 {
			add("budget_cpus is required for the microvm sandbox: the CPUs this runner may hold across every guest")
		}
		if c.BudgetMemoryMiB < 1 {
			add("budget_memory_mib is required for the microvm sandbox: guest RAM plus %d MiB overhead per guest, summed", OverheadMiB)
		}
		if c.CPUs >= 1 && c.BudgetCPUs >= 1 && c.Capacity*c.CPUs > c.BudgetCPUs {
			add("capacity %d × cpus %d = %d exceeds budget_cpus %d", c.Capacity, c.CPUs, c.Capacity*c.CPUs, c.BudgetCPUs)
		}
		if c.MemoryMiB >= MinMemoryMiB && c.BudgetMemoryMiB >= 1 && c.Capacity*(c.MemoryMiB+OverheadMiB) > c.BudgetMemoryMiB {
			add("capacity %d × (memory_mib %d + %d overhead) = %d exceeds budget_memory_mib %d", c.Capacity, c.MemoryMiB, OverheadMiB, c.Capacity*(c.MemoryMiB+OverheadMiB), c.BudgetMemoryMiB)
		}
	case "docker-rootless":
		if c.ActImage == "" {
			c.ActImage = "catthehacker/ubuntu:act-latest"
		}
		if c.DockerHost == "" {
			add("docker_host is required for the docker-rootless sandbox")
		}
		if hasDisk {
			add("disk_mib is not enforced by docker-rootless (containers share the daemon's storage); remove it or use the microvm sandbox")
		}
		if hasImage {
			add("image_path is a microvm key; docker-rootless runs the act_image on the rootless daemon")
		}
		if hasLauncher {
			add("launcher_socket is a microvm key")
		}
		if c.CPUs < 0 || c.MemoryMiB < 0 {
			add("cpus and memory_mib cannot be negative")
		}
		if len(c.NetworkProfiles) > 0 && !c.DockerEgressUnrestrictedAck {
			add("network_profiles under docker-rootless are NAT egress as a whole (destinations are not narrowed); set docker_egress_unrestricted_ack = true to declare them anyway, or use the microvm sandbox")
		}
	default:
		add("unknown sandbox %q: microvm (the default) or docker-rootless", c.Sandbox)
	}
	seen := map[string]bool{}
	labels := c.Labels[:0]
	for _, l := range c.Labels {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		if strings.ContainsAny(l, ", \t") {
			add("label %q: labels carry no commas or spaces", l)
			continue
		}
		seen[l] = true
		labels = append(labels, l)
	}
	c.Labels = labels
	names := map[string]bool{}
	for _, p := range c.NetworkProfiles {
		if p.Name == "" {
			add("network profile name is required")
			continue
		}
		if p.Name == "locked" {
			add("network profile name locked is reserved for the default (no network)")
		}
		if names[p.Name] {
			add("duplicate network profile %q", p.Name)
		}
		names[p.Name] = true
		if len(p.Destinations) == 0 {
			add("network profile %q needs at least one destination", p.Name)
		}
		for _, d := range p.Destinations {
			if err := ValidateDestination(d); err != nil {
				add("network profile %q: %v", p.Name, err)
			}
		}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	if err := os.MkdirAll(c.WorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("work_dir: %w", err)
	}
	return &c, nil
}

// ValidateDestination checks one `proto:host:port` entry: tcp or udp, a
// host that is an IP literal or a DNS name in netname's grammar (the ship
// and the launcher use the same one), a port. Nothing is resolved here.
func ValidateDestination(d string) error {
	parts := strings.SplitN(d, ":", 2)
	if len(parts) != 2 || (parts[0] != "tcp" && parts[0] != "udp") {
		return fmt.Errorf("destination %q must be proto:host:port with proto tcp or udp", d)
	}
	host, port, err := net.SplitHostPort(parts[1])
	if err != nil {
		return fmt.Errorf("destination %q must be proto:host:port", d)
	}
	// SplitHostPort strips brackets; they belong to an IPv6 literal only,
	// so a name has one spelling and the ceiling's exact match sees it
	if net.ParseIP(host) == nil && (!netname.Valid(host) || strings.HasPrefix(parts[1], "[")) {
		return fmt.Errorf("destination %q must name an IP literal or a lower-case DNS name of two or more labels (no wildcard, no trailing dot)", d)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("destination %q: port must be 1–65535", d)
	}
	return nil
}
