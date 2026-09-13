// Package config reads the daemon's TOML configuration.
//
// The keys are the contract from BRIEF-CI-P1 D7: ship_url, enroll_token
// (consumed once), sandbox, docker_host, act_binary, act_image, capacity,
// work_dir, state_file. The microvm keys (image_path, cpus, memory_mib,
// disk_mib) are parsed into the sandbox Spec even though that backend is
// not available in this release.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

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

	// microvm backend (P2): parsed, never used in this release
	ImagePath string `toml:"image_path"`
	CPUs      int    `toml:"cpus"`
	MemoryMiB int    `toml:"memory_mib"`
	DiskMiB   int    `toml:"disk_mib"`
}

// Load reads and validates a configuration file. Defaults: sandbox
// docker-rootless, capacity 1, act_binary "act", act_image
// catthehacker/ubuntu:act-latest.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if c.Sandbox == "" {
		c.Sandbox = "docker-rootless"
	}
	if c.Capacity == 0 {
		c.Capacity = 1
	}
	if c.ActBinary == "" {
		c.ActBinary = "act"
	}
	if c.ActImage == "" {
		c.ActImage = "catthehacker/ubuntu:act-latest"
	}
	var problems []error
	if c.ShipURL == "" {
		problems = append(problems, errors.New("ship_url is required"))
	}
	if c.WorkDir == "" {
		problems = append(problems, errors.New("work_dir is required"))
	}
	if c.StateFile == "" {
		problems = append(problems, errors.New("state_file is required"))
	}
	if c.Capacity < 1 {
		problems = append(problems, errors.New("capacity must be at least 1"))
	}
	if c.Sandbox == "docker-rootless" && c.DockerHost == "" {
		problems = append(problems, errors.New("docker_host is required for the docker-rootless sandbox"))
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	if err := os.MkdirAll(c.WorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("work_dir: %w", err)
	}
	return &c, nil
}
