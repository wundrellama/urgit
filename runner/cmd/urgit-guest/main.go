// urgit-guest is the guest helper (BRIEF-CI-P4 D3): PID 1 of the
// disposable VM. It mounts the pseudo filesystems, names the machine
// after the attempt, starts the guest's own Docker daemon, creates the
// job network, then serves exactly one helper session on vsock port 5000
// and powers the guest off when it ends. It has no host-shell, path-open,
// mint-token or run-host-command operation: every request is one of the
// protocol's frames and every path is under /work.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"urgit/runner/internal/guest"
)

const (
	workRoot   = "/work"
	dockerSock = "/run/docker.sock"
	jobNetwork = "urgit-ci"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.SetPrefix("urgit-guest: ")
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("urgit-guest protocol %d\n", guest.ProtocolVersion)
		return
	}
	// PID 1 starts with no environment: every exec below needs a PATH
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	if os.Getpid() == 1 {
		if err := initSystem(); err != nil {
			log.Printf("init: %v", err)
			powerOff()
		}
	}
	cmdline := kernelCmdline()
	attempt := cmdline["urgit.attempt"]
	hostname := "urgit-guest"
	if attempt != "" {
		hostname = "urgit-" + strings.ReplaceAll(strings.TrimPrefix(attempt, "0v"), ".", "")[:min(12, len(strings.TrimPrefix(attempt, "0v")))]
	}
	if err := configureIdentity(hostname); err != nil {
		log.Printf("identity: %v", err)
	}
	if err := configureNetwork(cmdline["urgit.ip"]); err != nil {
		log.Printf("network: %v", err)
	}
	dockerVersion, err := startDocker()
	if err != nil {
		log.Printf("docker: %v", err)
		powerOff()
	}
	log.Printf("docker %s answers on %s; job network %s", dockerVersion, dockerSock, jobNetwork)
	ready := guest.Ready{
		Helper:    guest.ProtocolVersion,
		Kernel:    kernelRelease(),
		Docker:    dockerVersion,
		MachineID: readTrim("/etc/machine-id"),
		Hostname:  hostname,
		Bridge:    jobBridgeGateway(),
	}
	if os.Getenv("URGIT_GUEST_PROVISION") == "1" || cmdline["urgit.provision"] == "1" {
		// the image build's provisioning boot: load the job image from the
		// second drive into Docker's data root, then stop cleanly
		provision()
		stopDocker()
		powerOff()
	}
	os.MkdirAll(workRoot, 0o755)
	lfd, err := guest.ListenVsock(guest.HelperPort)
	if err != nil {
		log.Printf("%v", err)
		powerOff()
	}
	log.Printf("listening on vsock port %d for one session", guest.HelperPort)
	conn, err := guest.AcceptVsock(lfd)
	if err != nil {
		log.Printf("%v", err)
		powerOff()
	}
	syscall.Close(lfd)
	srv := guest.NewServer(workRoot, ready, guest.ShellRunner{})
	srv.OnShutdown = func() { log.Printf("shutdown requested") }
	if err := srv.Serve(conn); err != nil {
		log.Printf("session ended: %v", err)
	} else {
		log.Printf("session ended")
	}
	stopDocker()
	powerOff()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// initSystem is the PID 1 work: pseudo filesystems, /dev, cgroup2, and a
// zombie reaper for orphans that reparent to us.
func initSystem() error {
	mounts := []struct{ src, dst, fstype, opts string }{
		{"proc", "/proc", "proc", "nosuid,noexec,nodev"},
		{"sysfs", "/sys", "sysfs", "nosuid,noexec,nodev"},
		{"devtmpfs", "/dev", "devtmpfs", "nosuid,mode=0755"},
		{"devpts", "/dev/pts", "devpts", "nosuid,noexec,gid=5,mode=0620,ptmxmode=0666"},
		{"tmpfs", "/dev/shm", "tmpfs", "nosuid,nodev"},
		{"tmpfs", "/run", "tmpfs", "nosuid,nodev,mode=0755"},
		{"tmpfs", "/tmp", "tmpfs", "nosuid,nodev"},
		{"cgroup2", "/sys/fs/cgroup", "cgroup2", "nosuid,noexec,nodev"},
	}
	for _, m := range mounts {
		os.MkdirAll(m.dst, 0o755)
		if err := mount(m.src, m.dst, m.fstype, m.opts); err != nil {
			return fmt.Errorf("mount %s: %w", m.dst, err)
		}
	}
	// the root drive is mounted read-write by the kernel (rw in the boot
	// args); remount to be sure and drop the shared propagation
	_ = syscall.Mount("", "/", "", syscall.MS_REMOUNT, "")
	go reap()
	return nil
}

func mount(src, dst, fstype, opts string) error {
	var flags uintptr
	var data []string
	for _, o := range strings.Split(opts, ",") {
		switch o {
		case "nosuid":
			flags |= syscall.MS_NOSUID
		case "noexec":
			flags |= syscall.MS_NOEXEC
		case "nodev":
			flags |= syscall.MS_NODEV
		case "":
		default:
			data = append(data, o)
		}
	}
	err := syscall.Mount(src, dst, fstype, flags, strings.Join(data, ","))
	if err == syscall.EBUSY {
		return nil
	}
	return err
}

// reap collects orphans reparented to PID 1. It runs only between the
// helper's own processes (see server.go): a Wait4(-1) racing an active
// exec.Cmd would steal its exit status, so the reaper skips while one
// is running by checking /proc for our direct children being reaped by
// the server — in practice orphans are rare (containerd reaps its own).
func reap() {
	for {
		time.Sleep(2 * time.Second)
		for {
			var ws syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG|syscall.WNOWAIT, nil)
			if err != nil || pid <= 0 {
				break
			}
			// a child the server started is reaped by it; only reap
			// processes whose parent link we do not own
			if ownChild(pid) {
				break
			}
			_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		}
	}
}

var ownPids = map[int]bool{}

func ownChild(pid int) bool { return ownPids[pid] }

func kernelCmdline() map[string]string {
	out := map[string]string{}
	data, _ := os.ReadFile("/proc/cmdline")
	for _, f := range strings.Fields(string(data)) {
		if k, v, ok := strings.Cut(f, "="); ok {
			out[k] = v
		} else {
			out[f] = ""
		}
	}
	return out
}

func kernelRelease() string {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return "?"
	}
	b := make([]byte, 0, 65)
	for _, c := range u.Release {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func readTrim(p string) string {
	d, _ := os.ReadFile(p)
	return strings.TrimSpace(string(d))
}

// configureIdentity: hostname, hosts, a fresh machine-id per boot, an
// empty resolver (locked profile: no DNS), the loopback up.
func configureIdentity(hostname string) error {
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		return err
	}
	os.WriteFile("/etc/hostname", []byte(hostname+"\n"), 0o644)
	os.WriteFile("/etc/hosts", []byte("127.0.0.1 localhost "+hostname+"\n"), 0o644)
	var id [16]byte
	rand.Read(id[:])
	os.WriteFile("/etc/machine-id", []byte(hex.EncodeToString(id[:])+"\n"), 0o444)
	os.WriteFile("/etc/resolv.conf", []byte(""), 0o644)
	return run("ip", "link", "set", "lo", "up")
}

// configureNetwork brings eth0 up only when the host put an address on
// the kernel command line (a networked profile); locked guests have no
// NIC at all.
func configureNetwork(spec string) error {
	if spec == "" {
		return nil
	}
	addr, gw, _ := strings.Cut(spec, ",")
	if err := run("ip", "addr", "add", addr, "dev", "eth0"); err != nil {
		return err
	}
	if err := run("ip", "link", "set", "eth0", "up"); err != nil {
		return err
	}
	if gw != "" {
		return run("ip", "route", "add", "default", "via", gw, "dev", "eth0")
	}
	return nil
}

func run(argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

var dockerd *exec.Cmd

// startDocker runs the guest's own dockerd on /run/docker.sock and waits
// until it answers, then creates the job network act uses.
func startDocker() (string, error) {
	os.MkdirAll("/var/lib/docker", 0o710)
	os.MkdirAll("/run/docker", 0o755)
	logf, _ := os.Create("/run/dockerd.log")
	dockerd = exec.Command("/usr/local/bin/dockerd",
		"--host", "unix://"+dockerSock,
		"--data-root", "/var/lib/docker",
		"--exec-root", "/run/docker",
		"--pidfile", "/run/dockerd.pid",
		"--iptables=true", "--ip6tables=false",
		"--default-address-pool", "base=172.20.0.0/16,size=24",
		"--log-level", "warn",
	)
	dockerd.Env = append(os.Environ(), "PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	dockerd.Stdout, dockerd.Stderr = logf, logf
	if err := dockerd.Start(); err != nil {
		return "", err
	}
	ownPids[dockerd.Process.Pid] = true
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var version string
	for ctx.Err() == nil {
		out, err := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "version", "--format", "{{.Server.Version}}").Output()
		if err == nil {
			version = strings.TrimSpace(string(out))
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if version == "" {
		tail, _ := os.ReadFile("/run/dockerd.log")
		return "", fmt.Errorf("dockerd did not answer within 60 s: %s", lastLines(string(tail), 8))
	}
	if err := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "network", "create", "--driver", "bridge", jobNetwork).Run(); err != nil {
		// an existing network (a provisioned image) is fine
		out, _ := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "network", "inspect", jobNetwork).Output()
		if len(out) == 0 {
			return version, fmt.Errorf("job network: %v", err)
		}
	}
	return version, nil
}

// jobBridgeGateway is the guest's own address on the job network: the
// bridge's gateway as Docker reports it ("" if it cannot be read; the
// daemon then refuses to run act, never guesses an address)
func jobBridgeGateway() string {
	out, err := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "network", "inspect", "-f", "{{(index .IPAM.Config 0).Gateway}}", jobNetwork).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func stopDocker() {
	if dockerd == nil || dockerd.Process == nil {
		return
	}
	_ = dockerd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { dockerd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = dockerd.Process.Kill()
	}
}

// provision loads the job image from /dev/vdb (a docker-archive tar) into
// the data root so no boot ever pulls anything.
func provision() {
	os.MkdirAll("/mnt/provision", 0o755)
	if err := syscall.Mount("/dev/vdb", "/mnt/provision", "ext4", syscall.MS_RDONLY, ""); err != nil {
		log.Printf("provision: mount /dev/vdb: %v", err)
		return
	}
	entries, _ := filepath.Glob("/mnt/provision/*.tar")
	for _, e := range entries {
		log.Printf("provision: docker load %s", e)
		cmd := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "load", "-i", e)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("provision: load %s: %v", e, err)
		}
	}
	out, _ := exec.Command("/usr/local/bin/docker", "--host", "unix://"+dockerSock, "images", "--digests", "--format", "{{.Repository}}:{{.Tag}} {{.Digest}} {{.ID}}").Output()
	log.Printf("provision: images:\n%s", out)
	os.WriteFile("/var/lib/urgit-guest-provisioned", out, 0o644)
	_ = syscall.Unmount("/mnt/provision", 0)
}

// powerOff ends the guest. The microvm kernel has no ACPI, so POWER_OFF
// would only halt the vCPUs and leave the VMM waiting; a reboot with
// `reboot=k` on the command line resets through the i8042 controller,
// which Firecracker takes as the guest's exit and stops the VMM.
func powerOff() {
	syscall.Sync()
	if os.Getpid() != 1 {
		os.Exit(0)
	}
	_ = syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
	select {}
}
