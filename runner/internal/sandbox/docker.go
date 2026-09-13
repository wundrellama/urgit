package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// Docker is the docker-rootless backend: one user-defined bridge network,
// one work volume and one runner container per attempt on a dedicated
// rootless daemon. The rootless daemon's socket is mounted into the
// runner container because act needs a Docker API to create job
// containers; it belongs to the rootless daemon, never to the host's.
// Nothing else from the host is mounted: the checkout and the act binary
// enter through Copy.
type Docker struct {
	Host string
}

func NewDocker(host string) (Sandbox, error) {
	if host == "" {
		return nil, errors.New("docker-rootless sandbox needs docker_host")
	}
	d := &Docker{Host: host}
	out, err := d.docker(context.Background(), "info", "--format", "{{.SecurityOptions}}")
	if err != nil {
		return nil, fmt.Errorf("docker-rootless: daemon at %s not reachable: %w", host, err)
	}
	if !strings.Contains(out, "name=rootless") {
		return nil, fmt.Errorf("docker-rootless: daemon at %s is not rootless (%s)", host, strings.TrimSpace(out))
	}
	return d, nil
}

func (d *Docker) Name() string {
	return "docker-rootless (container isolation; microvm backend pending)"
}

func (d *Docker) socketPath() string {
	return strings.TrimPrefix(d.Host, "unix://")
}

func (d *Docker) command(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"--host", d.Host}, args...)
	return exec.CommandContext(ctx, "docker", full...)
}

func (d *Docker) docker(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := d.command(ctx, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker %s: %s", args[0], strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func (d *Docker) Prepare(ctx context.Context, spec Spec) (Handle, error) {
	if spec.Network == "" {
		return Handle{}, errors.New("prepare: a network name is required")
	}
	h := Handle{ID: spec.Network, Network: spec.Network, Volume: spec.Network + "-work", Container: spec.Network}
	// a bridge with NAT egress, isolated from other attempts' bridges and
	// from the host's own services; not --internal (D10 network policy)
	if _, err := d.docker(ctx, "network", "create", "--driver", "bridge", "--label", "urgit-ci=1", h.Network); err != nil {
		return Handle{}, err
	}
	if _, err := d.docker(ctx, "volume", "create", "--label", "urgit-ci=1", h.Volume); err != nil {
		_ = d.Destroy(ctx, h)
		return Handle{}, err
	}
	args := []string{
		"run", "-d", "--name", h.Container, "--network", h.Network,
		"--label", "urgit-ci=1",
		"-v", h.Volume + ":/work",
		"-v", d.socketPath() + ":/var/run/docker.sock",
		"-e", "DOCKER_HOST=unix:///var/run/docker.sock",
	}
	if spec.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(spec.CPUs))
	}
	if spec.MemoryMiB > 0 {
		args = append(args, "--memory", strconv.Itoa(spec.MemoryMiB)+"m")
	}
	// DiskMiB: a volume has no size limit on the default driver; ignored
	args = append(args, spec.Image, "tail", "-f", "/dev/null")
	if _, err := d.docker(ctx, args...); err != nil {
		_ = d.Destroy(ctx, h)
		return Handle{}, err
	}
	return h, nil
}

func (d *Docker) Copy(ctx context.Context, h Handle, hostPath, guestPath string) error {
	if _, err := d.docker(ctx, "exec", h.Container, "mkdir", "-p", parentDir(guestPath)); err != nil {
		return err
	}
	_, err := d.docker(ctx, "cp", hostPath, h.Container+":"+guestPath)
	return err
}

func parentDir(p string) string {
	i := strings.LastIndex(strings.TrimRight(p, "/"), "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

// Run: docker exec with combined output; the exit code is the command's.
func (d *Docker) Run(ctx context.Context, h Handle, workDir string, argv []string, env []string) (io.ReadCloser, <-chan int, error) {
	args := []string{"exec"}
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, h.Container)
	args = append(args, argv...)
	cmd := d.command(ctx, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan int, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
				if code < 0 {
					code = 128
				}
			} else {
				code = 127
			}
		}
		done <- code
	}()
	return pr, done, nil
}

func (d *Docker) Signal(ctx context.Context, h Handle, processName, signal string) error {
	_, err := d.docker(ctx, "exec", h.Container, "pkill", "-"+signal, "-x", processName)
	return err
}

// Destroy removes every container on the attempt's network (act's job
// containers included, when act was killed before its own cleanup), the
// runner container, the volume and the network, and returns the first
// error it met after attempting all of them.
func (d *Docker) Destroy(ctx context.Context, h Handle) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	attached, err := d.docker(ctx, "network", "inspect", "-f", "{{range .Containers}}{{.Name}} {{end}}", h.Network)
	if err == nil {
		for _, name := range strings.Fields(attached) {
			_, err := d.docker(ctx, "rm", "-f", name)
			keep(err)
		}
	}
	if _, err := d.docker(ctx, "rm", "-f", h.Container); err != nil && !strings.Contains(err.Error(), "No such container") {
		keep(err)
	}
	if _, err := d.docker(ctx, "volume", "rm", "-f", h.Volume); err != nil && !strings.Contains(err.Error(), "no such volume") {
		keep(err)
	}
	if _, err := d.docker(ctx, "network", "rm", h.Network); err != nil && !strings.Contains(err.Error(), "not found") {
		keep(err)
	}
	return first
}

// Orphans: the ci-* networks this daemon still holds; each name is an
// attempt id.
func (d *Docker) Orphans(ctx context.Context) ([]string, error) {
	out, err := d.docker(ctx, "network", "ls", "--filter", "label=urgit-ci=1", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, name := range strings.Fields(out) {
		if strings.HasPrefix(name, "ci-") {
			ids = append(ids, name)
		}
	}
	return ids, nil
}
