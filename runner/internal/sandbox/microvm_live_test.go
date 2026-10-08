//go:build live

package sandbox

// The S0 substrate preflight and the S1 boot check (BRIEF-CI-P4 S0.3,
// S1.3), run against the image the recipe built:
//
//	URGIT_LIVE_FC=<firecracker binary> URGIT_LIVE_IMAGE=<image dir> \
//	  go test -tags live -run TestLiveGuest -v ./internal/sandbox/
//
// DISCLOSURE: this boots Firecracker as the invoking user with NO jailer,
// NO network interface and NO cgroup, from a reflink copy of the golden
// rootfs. It proves the kernel, the rootfs, the helper protocol over
// vsock, the guest's own Docker daemon, a real act job and the guest's
// exit — the image and protocol half of the substrate proof. It is NOT
// the execution path (that is the launcher through the jailer, held for
// review) and it qualifies nothing about confinement. The record cites
// it as "unjailed preflight boot".

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"urgit/runner/internal/guest"
	"urgit/runner/internal/launcher"
)

const liveWorkflow = `name: preflight
on: [push]
jobs:
  hello:
    runs-on: ubuntu-latest
    steps:
      - name: identity
        run: |
          echo "guest-hostname=$(hostname)"
          echo "in-job-uid=$(id -u)"
          cat /proc/1/cgroup | head -1
          echo "docker-sock=$(ls -la /var/run/docker.sock 2>&1)"
      - name: docker inside the job
        run: docker info --format '{{.Name}} {{.ServerVersion}} {{.SecurityOptions}}' 2>&1 || true
      - name: no network
        run: |
          set +e
          ip -o link 2>/dev/null | wc -l
          timeout 5 curl -sS -m 4 https://api.github.com/ >/dev/null 2>&1; echo "egress-exit=$?"
`

type liveVM struct {
	dir     string
	cmd     *exec.Cmd
	console *os.File
	vsock   string
}

func bootLive(t *testing.T, fc, image, attempt string, mem, cpus int) *liveVM {
	t.Helper()
	dir := t.TempDir()
	mf, err := os.ReadFile(filepath.Join(image, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m ImageManifest
	if err := json.Unmarshal(mf, &m); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "disk.ext4")
	// a fresh writable copy per boot (reflink on btrfs; a plain copy elsewhere)
	if out, err := exec.Command("cp", "--reflink=auto", filepath.Join(image, m.Rootfs.Name), disk).CombinedOutput(); err != nil {
		t.Fatalf("copy rootfs: %s", out)
	}
	if out, err := exec.Command("tune2fs", "-U", "random", disk).CombinedOutput(); err != nil {
		t.Logf("tune2fs: %s", out)
	}
	// a unix socket path is at most 108 bytes: the vsock socket gets a short
	// directory of its own (the launcher's jail paths respect the same bound)
	short, err := os.MkdirTemp(os.Getenv("TMPDIR"), "vs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	vs := filepath.Join(short, "v.sock")
	cfg := map[string]any{
		"boot-source":    map[string]any{"kernel_image_path": filepath.Join(image, m.Kernel.Name), "boot_args": m.BootArgs + " urgit.attempt=" + attempt},
		"drives":         []map[string]any{{"drive_id": "rootfs", "path_on_host": disk, "is_root_device": true, "is_read_only": false}},
		"machine-config": map[string]any{"vcpu_count": cpus, "mem_size_mib": mem, "smt": false},
		"vsock":          map[string]any{"guest_cid": 3, "uds_path": vs},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "vm.json")
	os.WriteFile(cfgPath, data, 0o644)
	console, _ := os.Create(filepath.Join(dir, "console.log"))
	cmd := exec.Command(fc, "--no-api", "--config-file", cfgPath)
	cmd.Stdout, cmd.Stderr = console, console
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Logf("firecracker pid %d, dir %s", cmd.Process.Pid, dir)
	return &liveVM{dir: dir, cmd: cmd, console: console, vsock: vs}
}

func (v *liveVM) stop(t *testing.T) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- v.cmd.Wait() }()
	select {
	case err := <-done:
		t.Logf("firecracker exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Logf("firecracker did not exit within 30 s after SHUTDOWN; killing (recorded as a teardown fault)")
		v.cmd.Process.Kill()
		<-done
	}
	v.console.Close()
	tail, _ := os.ReadFile(filepath.Join(v.dir, "console.log"))
	lines := strings.Split(strings.TrimSpace(string(tail)), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	t.Logf("console tail:\n%s", strings.Join(lines, "\n"))
}

func connectLive(t *testing.T, v *liveVM, attempt string) (*guest.Session, guest.Ready) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		fd, err := launcher.ConnectVsock(v.vsock, guest.HelperPort, 3*time.Second)
		if err == nil {
			s := guest.NewSession(fd)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			ready, err := s.Hello(ctx, guest.Hello{Attempt: attempt, Nonce: "0v1"})
			cancel()
			if err == nil {
				return s, ready
			}
			lastErr = err
			s.Close()
		} else {
			lastErr = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("no READY within 90 s: %v", lastErr)
	return nil, guest.Ready{}
}

func TestLiveGuest(t *testing.T) {
	fc, image := os.Getenv("URGIT_LIVE_FC"), os.Getenv("URGIT_LIVE_IMAGE")
	if fc == "" || image == "" {
		t.Skip("URGIT_LIVE_FC and URGIT_LIVE_IMAGE name the firecracker binary and the image directory")
	}
	// the image digests must match the manifest before any boot (M10)
	m := &Microvm{imageDir: image}
	img, err := m.verifyImage()
	if err != nil {
		t.Fatalf("image verification: %v", err)
	}
	t.Logf("image manifest %s: kernel %s rootfs %s", img.digest[:12], img.manifest.Kernel.SHA256[:12], img.manifest.Rootfs.SHA256[:12])
	attempt := fmt.Sprintf("0v%d", time.Now().Unix()%100000)
	started := time.Now()
	vm := bootLive(t, fc, image, attempt, 2048, 2)
	sess, ready := connectLive(t, vm, attempt)
	t.Logf("READY after %s: helper %d kernel %s docker %s machine-id %s hostname %s", time.Since(started).Round(time.Millisecond), ready.Helper, ready.Kernel, ready.Docker, ready.MachineID, ready.Hostname)
	if ready.Docker == "" || ready.Kernel == "" {
		t.Fatalf("READY incomplete: %+v", ready)
	}
	// a second HELLO is refused: one session per boot
	if _, err := sess.Hello(context.Background(), guest.Hello{Attempt: attempt}); err == nil {
		t.Fatal("second HELLO accepted")
	}
	// the bundle: a workspace with one workflow and a projection of it
	work := t.TempDir()
	os.MkdirAll(filepath.Join(work, "src", ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(work, "src", ".github", "workflows", "preflight.yml"), []byte(liveWorkflow), 0o644)
	os.MkdirAll(filepath.Join(work, "projected"), 0o755)
	os.WriteFile(filepath.Join(work, "projected", "preflight.yml"), []byte(strings.Replace(liveWorkflow, "name: preflight", "name: "+attempt+"/preflight", 1)), 0o644)
	os.MkdirAll(filepath.Join(work, "cache"), 0o755)
	os.MkdirAll(filepath.Join(work, "artifacts"), 0o755)
	os.MkdirAll(filepath.Join(work, "actions"), 0o755)
	ctx := context.Background()
	if _, err := sess.Put(ctx, work, ".", guest.Limits{MaxBytes: 1 << 30, MaxEntries: 10000}); err != nil {
		t.Fatalf("put: %v", err)
	}
	want, _ := guest.WorkspaceDigest(filepath.Join(work, "src"))
	if ok, err := sess.Verify(ctx, "src", want); err != nil || !ok {
		t.Fatalf("workspace verify: %v %v", err, ok)
	}
	t.Logf("bundle copied and verified (%s)", want[:12])
	// act -l on the guest
	out, done, err := sess.Exec(ctx, []string{"/usr/local/bin/act", "-l", "-W", ".github/workflows/preflight.yml"}, nil, "src")
	if err != nil {
		t.Fatal(err)
	}
	listing, _ := io.ReadAll(out)
	if code := <-done; code != 0 || !strings.Contains(string(listing), "hello") {
		t.Fatalf("act -l exited %d: %s", code, listing)
	}
	t.Logf("act -l:\n%s", listing)
	// the real job: act push on the projection against the guest's Docker
	argv := []string{"/usr/local/bin/act", "push", "-W", "/work/projected/preflight.yml", "-j", "hello",
		"-P", "ubuntu-latest=" + img.manifest.ActImage.Reference, "--network", "urgit-ci", "--json", "--pull=false",
		"--cache-server-path", "/work/cache", "--artifact-server-path", "/work/artifacts",
		"--container-daemon-socket", "unix:///run/docker.sock", "--action-cache-path", "/work/actions", "--action-offline-mode"}
	jobStart := time.Now()
	out, done, err = sess.Exec(ctx, argv, nil, "src")
	if err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	io.Copy(&stream, out)
	code := <-done
	t.Logf("act push exited %d after %s", code, time.Since(jobStart).Round(time.Millisecond))
	text := stream.String()
	os.WriteFile(filepath.Join(vm.dir, "act.jsonl"), stream.Bytes(), 0o644)
	var jobResult string
	var msgs []string
	for _, line := range strings.Split(text, "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if r, ok := ev["jobResult"].(string); ok && r != "" {
			jobResult = r
		}
		if msg, ok := ev["msg"].(string); ok {
			msgs = append(msgs, msg)
		}
	}
	joined := strings.Join(msgs, "\n")
	t.Logf("job output lines:\n%s", joined)
	if jobResult != "success" {
		t.Fatalf("jobResult %q; stream tail: %s", jobResult, lastN(text, 1500))
	}
	// what the job saw, proven from INSIDE the job container
	// (CI-SANDBOX-1.1's rule): the docker it talks to names the GUEST
	// (its Name is the guest's hostname from READY, its version the
	// guest's), not the host's rootful daemon (the host's hostname) and
	// not the host's rootless one (name=rootless); and no egress exists
	for _, want := range []string{"in-job-uid=", "docker-sock=", "egress-exit=", ready.Hostname + " " + ready.Docker} {
		if !strings.Contains(joined, want) {
			t.Errorf("job output lacks %q", want)
		}
	}
	if host, _ := os.Hostname(); host != "" && strings.Contains(joined, host+" ") {
		t.Errorf("the job's docker names the HOST %s", host)
	}
	if strings.Contains(joined, "egress-exit=0") {
		t.Errorf("the job reached the Internet from a NIC-less guest")
	}
	if strings.Contains(joined, "name=rootless") {
		t.Errorf("the job's docker is a rootless daemon: that is the host's shape, not the guest's")
	}
	// bounded export of the artifacts directory
	var sink bytes.Buffer
	end, err := sess.Export(ctx, "artifacts", 1<<20, &sink)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	t.Logf("export: %d bytes, truncated=%v", end.Bytes, end.Truncated)
	// shutdown: the guest reboots and firecracker exits
	if err := sess.Shutdown(ctx); err != nil {
		t.Logf("shutdown send: %v", err)
	}
	vm.stop(t)
	t.Logf("total %s", time.Since(started).Round(time.Millisecond))
}

// two boots from clean storage in one run (S1.3): each gets a different
// machine-id and a fresh disk copy
func TestLiveGuestBootsTwiceWithDistinctIdentity(t *testing.T) {
	fc, image := os.Getenv("URGIT_LIVE_FC"), os.Getenv("URGIT_LIVE_IMAGE")
	if fc == "" || image == "" {
		t.Skip("URGIT_LIVE_FC and URGIT_LIVE_IMAGE name the firecracker binary and the image directory")
	}
	var ids []string
	for i := 0; i < 2; i++ {
		attempt := fmt.Sprintf("0v%d", time.Now().UnixNano()%1000000)
		vm := bootLive(t, fc, image, attempt, 1024, 1)
		sess, ready := connectLive(t, vm, attempt)
		ids = append(ids, ready.MachineID)
		t.Logf("boot %d: machine-id %s hostname %s", i+1, ready.MachineID, ready.Hostname)
		sess.Shutdown(context.Background())
		vm.stop(t)
	}
	if ids[0] == ids[1] || ids[0] == "" {
		t.Fatalf("two boots share an identity: %v", ids)
	}
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
