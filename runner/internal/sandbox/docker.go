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
	"time"
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
	// Owner is the daemon id every sandbox object is labelled with
	// (`urgit-ci-daemon=<id>`, P3 D6 g): two runners on one rootless
	// daemon is a supported deployment, and a runner reconciles its own
	// sandboxes only. Set by the daemon once its identity is known; empty
	// until then, which labels nothing and reconciles as before.
	Owner string
	// exec runs one docker CLI command (the arguments after --host) and
	// returns its standard output; nil runs the docker binary. Tests put a
	// recorder here that runs nothing.
	exec func(ctx context.Context, args ...string) (string, error)
}

// cleanupBound bounds the cleanup of a Prepare that failed half way,
// rider 04's 120 s, in a context of its own: the attempt's may be over.
const cleanupBound = 120 * time.Second

// LabelOwner is the label that names a sandbox's daemon.
const LabelOwner = "urgit-ci-daemon"

// labels are the `--label` arguments every object is created with
func (d *Docker) labels() []string {
	out := []string{"--label", "urgit-ci=1"}
	if d.Owner != "" {
		out = append(out, "--label", LabelOwner+"="+d.Owner)
	}
	return out
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
	return "docker-rootless (container compatibility mode; not a VM boundary; a network profile is NAT egress as a whole, destinations not narrowed)"
}

func (d *Docker) Kind() string { return "docker-rootless" }

// SetOwner labels every sandbox object with the daemon id from here on
// (P3 D6 g).
func (d *Docker) SetOwner(daemonID string) error {
	d.Owner = daemonID
	return nil
}

// HandleFor rebuilds the Docker-shaped handle an orphan id names.
func (d *Docker) HandleFor(id string) Handle {
	return Handle{ID: id, Attempt: strings.TrimPrefix(id, "ci-"), Network: id, Volume: id + "-work", Container: id}
}

// ExecEnv: the rootless socket is bound into job containers
// (CI-SANDBOX-1.1), the attempt's bridge is the job network, act is
// copied in, and the work volume holds everything.
func (d *Docker) ExecEnv(h Handle) ExecEnv {
	return ExecEnv{
		WorkRoot: "/work", SourceDir: "/work/src", ActBinary: "/usr/local/bin/act", CopyAct: true,
		DockerSocket: d.Host, JobNetwork: h.Network,
		CachePath: "/work/cache", ArtifactPath: "/work/artifacts", ActionCachePath: "/work/actions", ToolCache: "/work/toolcache",
		ToolCacheVolume: h.Volume + "-tools",
		WorkVolume:      h.Volume,
		ServerAddr:      h.Address,
		Locked:          false,
	}
}

func (d *Docker) socketPath() string {
	return strings.TrimPrefix(d.Host, "unix://")
}

func (d *Docker) command(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"--host", d.Host}, args...)
	return exec.CommandContext(ctx, "docker", full...)
}

func (d *Docker) docker(ctx context.Context, args ...string) (string, error) {
	if d.exec != nil {
		return d.exec(ctx, args...)
	}
	var out, errb bytes.Buffer
	cmd := d.command(ctx, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker %s: %s", args[0], strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// abandon undoes a Prepare that failed after it created something: every
// object of the attempt is destroyed, in a context of its own. When that
// fails too, the objects stay, and the error names them (*RetainedError,
// the Docker-shaped handle: runner/launcher/INTEGRATION.md §3) — the
// daemon then withholds their slot.
func (d *Docker) abandon(ctx context.Context, h Handle, cause error) (Handle, error) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupBound)
	defer cancel()
	if err := d.Destroy(cctx, h); err != nil {
		return Handle{}, &RetainedError{Handle: h, Err: fmt.Errorf("%w; and the sandbox's objects could not be removed: %v", cause, err)}
	}
	return Handle{}, cause
}

func (d *Docker) Prepare(ctx context.Context, spec Spec) (Handle, error) {
	if spec.Network == "" {
		return Handle{}, errors.New("prepare: a network name is required")
	}
	h := Handle{ID: spec.Network, Attempt: spec.Attempt, Network: spec.Network, Volume: spec.Network + "-work", Container: spec.Network, Label: spec.Label}
	// a bridge isolated from other attempts' bridges. `locked` (rider 03,
	// the default) is --internal: no egress at all. an authorized profile
	// is a NAT bridge; its destination scope is NOT narrowed here — Docker
	// compatibility mode grants the profile's egress whole, which the
	// compatibility ledger discloses; only the VM launcher's policy
	// enforces destinations
	netArgs := []string{"network", "create", "--driver", "bridge"}
	if spec.Profile == "" || spec.Profile == "locked" {
		netArgs = append(netArgs, "--internal")
	}
	if _, err := d.docker(ctx, append(netArgs, append(d.labels(), h.Network)...)...); err != nil {
		return Handle{}, err
	}
	if _, err := d.docker(ctx, append([]string{"volume", "create"}, append(d.labels(), h.Volume)...)...); err != nil {
		return d.abandon(ctx, h, err)
	}
	// the seeded tool cache is its own volume: mounted under the work
	// root here so the bundle copy fills it, and per tool into every job
	// container under act's tool cache (ExecEnv.ToolCacheVolume)
	if _, err := d.docker(ctx, append([]string{"volume", "create"}, append(d.labels(), h.Volume+"-tools")...)...); err != nil {
		return d.abandon(ctx, h, err)
	}
	args := append([]string{
		"run", "-d", "--name", h.Container, "--network", h.Network,
	}, d.labels()...)
	args = append(args,
		"-v", h.Volume+":/work",
		"-v", h.Volume+"-tools:/work/toolcache",
		"-v", d.socketPath()+":/var/run/docker.sock",
		"-e", "DOCKER_HOST=unix:///var/run/docker.sock",
	)
	if spec.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(spec.CPUs))
	}
	if spec.MemoryMiB > 0 {
		args = append(args, "--memory", strconv.Itoa(spec.MemoryMiB)+"m")
	}
	// DiskMiB: a volume has no size limit on the default driver; ignored
	args = append(args, spec.Image, "tail", "-f", "/dev/null")
	if _, err := d.docker(ctx, args...); err != nil {
		return d.abandon(ctx, h, err)
	}
	// the container's address on the attempt's bridge: act binds its
	// cache and artifact servers there (an --internal bridge has no route
	// out for act to guess an address from)
	addr, err := d.docker(ctx, "inspect", "-f", "{{(index .NetworkSettings.Networks \""+h.Network+"\").IPAddress}}", h.Container)
	if err != nil || strings.TrimSpace(addr) == "" {
		return d.abandon(ctx, h, fmt.Errorf("sandbox address on %s: %v", h.Network, err))
	}
	h.Address = strings.TrimSpace(addr)
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
// error it met after attempting all of them. Its nil is what frees the
// attempt's slot, so a removal that failed counts as done only on Docker's
// exact reply that that very object does not exist (absent;
// runner/launcher/INTEGRATION.md §11.4): any other failure is kept. A
// listing of the network's containers that failed is not kept by itself:
// Docker refuses to remove a network while any container is attached, so
// the network's own removal — or its exact absence — is the proof none is.
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
			keep(d.removeContainer(ctx, name))
		}
	}
	keep(d.removeContainer(ctx, h.Container))
	for _, vol := range []string{h.Volume, h.Volume + "-tools"} {
		if _, err := d.docker(ctx, "volume", "rm", "-f", vol); err != nil && !absent(err, "volume", "volume", vol) {
			keep(err)
		}
	}
	if _, err := d.docker(ctx, "network", "rm", h.Network); err != nil && !absent(err, "network", "network", h.Network) {
		keep(err)
	}
	return first
}

// removeContainer force-removes a container and answers nil once it is
// gone: a container already removed — Docker's exact reply that it does
// not exist — or whose removal another party has in progress (a harness
// cleaning up under the daemon, a concurrent `docker rm`), is not a failed
// teardown — Destroy's obligation is that the resource is gone, and it
// waits (bounded) to see that it is, by the same exact reply. Any other
// refusal, or a container still present — or not proven absent — after
// the wait, is the error that quarantines the slot (rider 04).
func (d *Docker) removeContainer(ctx context.Context, name string) error {
	_, err := d.docker(ctx, "rm", "-f", name)
	if err == nil || absent(err, "rm", "container", name) {
		return nil
	}
	if !strings.Contains(err.Error(), "already in progress") {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, ierr := d.docker(ctx, "inspect", "--type", "container", "-f", "{{.Id}}", name); absent(ierr, "inspect", "container", name) {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("container %s: removal in progress elsewhere did not finish within 15 s: %v", name, err)
}

// Leftovers says which of h's Docker objects exist now — its container,
// its volumes and its network, by their exact names — the proof a
// retention's release waits for to be none (recovery ruling A;
// runner/launcher/INTEGRATION.md §§8.4, 11.4). A look proves an object
// absent only by Docker's own not-found reply about that very object
// (absentAnswer); any other failed look is an error — nothing is proven
// absent — and no reply proves an object the handle gives no name absent.
func (d *Docker) Leftovers(ctx context.Context, h Handle) ([]string, error) {
	var left []string
	var errs []error
	look := func(kind, name string, args ...string) {
		what := kind + " " + name
		_, err := d.docker(ctx, args...)
		switch {
		case err == nil:
			left = append(left, what)
		case absent(err, args[0], kind, name):
		default:
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
		}
	}
	look("container", h.Container, "inspect", "--type", "container", "-f", "{{.Id}}", h.Container)
	look("volume", h.Volume, "volume", "inspect", "-f", "{{.Name}}", h.Volume)
	look("volume", h.Volume+"-tools", "volume", "inspect", "-f", "{{.Name}}", h.Volume+"-tools")
	look("network", h.Network, "network", "inspect", "-f", "{{.Id}}", h.Network)
	return left, errors.Join(errs...)
}

// absent says whether err, the failure of the docker command whose first
// word is sub as the runner's own wrapper reports it, is Docker's exact
// reply that the object of kind named name does not exist (absentAnswer).
// No error proves nothing.
func absent(err error, sub, kind, name string) bool {
	return err != nil && absentAnswer(strings.TrimPrefix(err.Error(), "docker "+sub+": "), kind, name)
}

// absentAnswer says whether msg — a failed look's whole reply, the runner's
// own "docker <subcommand>: " prefix removed — is Docker's answer that the
// object of kind named name does not exist (INTEGRATION.md §11.4): exactly
// one of its not-found replies, about that kind and that name, and nothing
// else. Another object's reply, a missing driver, an API or transport
// error, extra words or lines prove no absence, and nothing proves a
// nameless object absent.
func absentAnswer(msg, kind, name string) bool {
	if name == "" {
		return false
	}
	msg = strings.TrimSpace(msg)
	switch msg {
	case "Error: No such " + kind + ": " + name, "Error response from daemon: No such " + kind + ": " + name:
		return true
	}
	switch kind {
	case "volume":
		return msg == "Error response from daemon: get "+name+": no such volume"
	case "network":
		return msg == "Error response from daemon: network "+name+" not found"
	}
	return false
}

// NewDockerCommand is a Docker backend whose docker commands go through run
// instead of the docker CLI: the tests' nonexecuting recorder, which lets
// the runner's own release path drive the real Leftovers (INTEGRATION.md
// §11.4). It checks nothing at construction.
func NewDockerCommand(host, owner string, run func(ctx context.Context, args ...string) (string, error)) *Docker {
	return &Docker{Host: host, Owner: owner, exec: run}
}

// Orphans: the ci-* networks THIS daemon still holds (its owner label),
// plus any ci-* network with no owner label at all — a leftover from a
// daemon older than the label, reaped by whoever finds it so an upgrade
// never orphans a stuck sandbox. Another daemon's networks are never
// listed (P3 D6 g): a runner inspects and destroys its own sandboxes
// only. Each name is an attempt id.
func (d *Docker) Orphans(ctx context.Context) ([]string, error) {
	out, err := d.docker(ctx, "network", "ls", "--filter", "label=urgit-ci=1", "--format", "{{.Name}} {{.Labels}}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "ci-") {
			continue
		}
		owner := ""
		for _, kv := range strings.Split(strings.Join(fields[1:], " "), ",") {
			if strings.HasPrefix(kv, LabelOwner+"=") {
				owner = strings.TrimPrefix(kv, LabelOwner+"=")
			}
		}
		if owner == "" || (d.Owner != "" && owner == d.Owner) {
			ids = append(ids, fields[0])
		}
	}
	return ids, nil
}
