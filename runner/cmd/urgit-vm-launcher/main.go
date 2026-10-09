// urgit-vm-launcher is the administrator-owned privileged VM launcher
// (rider 01; specs/ci-execution-contract.md §9). It runs as root under
// systemd, owns its protected root, and does exactly the operations the
// launcher core asks for: a disk copy, a cgroup, an optional network
// namespace with a host-side policy, the jailer starting Firecracker,
// a vsock connection, a kill, and the removals. It accepts no path,
// mount, uid, network or cgroup value from a request; images are named
// by manifest digest and must be installed under its image directory;
// budgets and the network ceiling are its own configuration.
//
//	urgit-vm-launcher serve  -config /etc/urgit-vm-launcher-p4-opus.toml
//	urgit-vm-launcher check  -config …   (the recipe's preflight: binaries, images, kvm, cgroups, tools)
//	urgit-vm-launcher list    -config …   (every record)
//	urgit-vm-launcher recover -config … [-select <id>/<incarnation>/<cid>/<created>/<rev> -action inspect|retry|release]
//	                                     (the incidents: inspect, retry a cleanup, release — recover.go)
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/BurntSushi/toml"

	"urgit/runner/internal/launcher"
	"urgit/runner/internal/pindns"
)

const version = "p4-opus-1"

// Config is the root-owned launcher configuration.
type Config struct {
	Socket            string   `toml:"socket"`
	StateDir          string   `toml:"state_dir"`
	ImageDir          string   `toml:"image_dir"`
	JailBase          string   `toml:"jail_base"`
	BinDir            string   `toml:"bin_dir"`
	FirecrackerSHA256 string   `toml:"firecracker_sha256"`
	JailerSHA256      string   `toml:"jailer_sha256"`
	RunnerUIDs        []int    `toml:"runner_uids"`
	VMUser            string   `toml:"vm_user"`
	IDPrefix          string   `toml:"id_prefix"`
	BudgetCPUs        int      `toml:"budget_cpus"`
	BudgetMemoryMiB   int      `toml:"budget_memory_mib"`
	MaxGuests         int      `toml:"max_guests"`
	CgroupService     string   `toml:"cgroup_service"`
	CgroupParent      string   `toml:"cgroup_parent"`
	CIDRPool          string   `toml:"cidr_pool"`
	EgressInterface   string   `toml:"egress_interface"`
	Ceiling           []string `toml:"ceiling"`
	NFTTable          string   `toml:"nft_table"`
	SocketGroup       string   `toml:"socket_group"`
}

func loadConfig(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, err
	}
	var problems []string
	need := func(v, name string) {
		if v == "" {
			problems = append(problems, name+" is required")
		}
	}
	need(c.Socket, "socket")
	need(c.StateDir, "state_dir")
	need(c.ImageDir, "image_dir")
	need(c.JailBase, "jail_base")
	need(c.BinDir, "bin_dir")
	need(c.FirecrackerSHA256, "firecracker_sha256")
	need(c.JailerSHA256, "jailer_sha256")
	need(c.VMUser, "vm_user")
	need(c.CgroupService, "cgroup_service")
	need(c.CgroupParent, "cgroup_parent")
	need(c.NFTTable, "nft_table")
	if len(c.RunnerUIDs) == 0 {
		problems = append(problems, "runner_uids is required")
	}
	if c.BudgetCPUs < 1 || c.BudgetMemoryMiB < 1 || c.MaxGuests < 1 {
		problems = append(problems, "budget_cpus, budget_memory_mib and max_guests are required")
	}
	// the launcher's cgroups live in its delegated service cgroup, below its
	// jobs cgroup, and nowhere else (cgroupPaths)
	problems = append(problems, cgroupLayoutProblems(&c)...)
	if c.IDPrefix == "" {
		c.IDPrefix = "urgit-p4o"
	}
	if c.CIDRPool == "" {
		c.CIDRPool = "10.113.0.0/16"
	}
	// N2: the masquerade follows the namespace address on whichever
	// interface the routing table picks, so an interface named here would
	// no longer be honoured. A setting that does nothing is refused rather
	// than silently ignored.
	if c.EgressInterface != "" {
		problems = append(problems, "egress_interface is retired: the masquerade follows the route the kernel picks (N2); remove it")
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return &c, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: urgit-vm-launcher serve|check|list|recover -config <toml> [-select <id>/<incarnation>/<cid>/<created>/<rev> -action inspect|retry|release]")
		os.Exit(2)
	}
	sub := os.Args[1]
	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	configPath := fs.String("config", "/etc/urgit-vm-launcher.toml", "root-owned configuration")
	selection := fs.String("select", "", "recover: the incident, as <id>/<incarnation>/<cid>/<created>/<rev> (its inspection shows it; the incarnation is - for a record written before tokens); without it recover is interactive")
	action := fs.String("action", "inspect", "recover with -select: inspect, retry (cleanup only) or release")
	fs.Parse(os.Args[2:])
	logger := log.New(os.Stdout, "urgit-vm-launcher: ", log.LstdFlags|log.Lmicroseconds)
	cfg, err := loadConfig(*configPath)
	if err != nil {
		logger.Printf("config: %v", err)
		os.Exit(2)
	}
	host, err := newHost(cfg, logger)
	if err != nil {
		logger.Printf("%v", err)
		os.Exit(2)
	}
	switch sub {
	case "check":
		if err := host.check(); err != nil {
			logger.Printf("CHECK FAILED: %v", err)
			os.Exit(1)
		}
		logger.Printf("check passed")
	case "serve":
		if err := host.check(); err != nil {
			logger.Printf("refusing to serve: %v", err)
			os.Exit(1)
		}
		// before any record is loaded or recovered: the launcher in its leaf
		// of the delegated subtree, the controllers given to its jobs cgroup
		if err := host.placeCgroups(); err != nil {
			logger.Printf("refusing to serve: %v", err)
			os.Exit(1)
		}
		svc, err := host.service()
		if err != nil {
			logger.Printf("refusing to serve: %v", err)
			os.Exit(1)
		}
		// D1, recovery: a networked VM this launcher may still run is served
		// only with its input containment in place
		if p := host.inputContainmentProblems(svc.All()); len(p) > 0 {
			for _, x := range p {
				logger.Printf("UNCONTAINED %s", x)
			}
			logger.Printf("refusing to serve: %d networked VM(s) without their input containment", len(p))
			if err := svc.Close(); err != nil {
				logger.Printf("close: %v", err)
			}
			os.Exit(1)
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		err = serve(ctx, cfg, svc, host.imageNames(), logger)
		stop()
		if err != nil {
			logger.Printf("%v", err)
			os.Exit(1)
		}
	case "list":
		if err := list(cfg, os.Stdout, logger); err != nil {
			logger.Printf("list: %v", err)
			os.Exit(1)
		}
	case "recover":
		if *selection == "" {
			os.Exit(recoverInteractive(host, os.Stdin, os.Stdout))
		}
		os.Exit(recoverScripted(host, *selection, *action, os.Stdout))
	case "clear":
		// recovery ruling A: a cleanup and a release are separate operator
		// actions now; there is no combined clear
		fmt.Fprintln(os.Stderr, "clear is gone: `urgit-vm-launcher recover` lists the incidents, retries a cleanup (never a release) and releases one separately, once nothing is held")
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand")
		os.Exit(2)
	}
}

// serve answers the socket until ctx ends, then stops in order: no new
// connection, the reaper stopped, no new operation and every create asked
// to roll back (BeginStop), the operations in progress finished and every
// owned VM torn down (Service.Shutdown, which gives the state directory
// back) — every teardown by one allowance after the stop request (its
// publication aside), which the unit's stop timeout, an outer backstop,
// waits past (runner/launcher/INTEGRATION.md §7.2). A teardown that fails is
// returned, so the exit status shows it; its record stays quarantined for
// the next start. Every teardown the launcher starts on its own and cannot
// finish is logged as it ends (§7.4).
func serve(ctx context.Context, cfg *Config, svc *launcher.Service, images []string, logger *log.Logger) error {
	svc.SetReport(func(o launcher.Outcome) { logOutcome(logger, o) })
	n := 0
	for _, p := range svc.Problems() {
		if p.Kind == "uncertified" {
			// not an entry to resolve: the reaper retries the directory's
			// fsync, and the fence lifts once it succeeds (INTEGRATION.md §11.6)
			logger.Printf("RECORDS NOT CERTIFIED %s: no new reservation or boot, and no absence answered as a release, until its fsync succeeds; the loaded records are still enforced", p)
			continue
		}
		logger.Printf("UNSAFE STATE %s", p)
		n++
	}
	if n > 0 {
		logger.Printf("%d state entries could not be accounted for: no new reservation or boot, and no absence answered as a release, until an operator resolves them; cleanup of the loaded records continues", n)
	}
	for _, l := range svc.Leftovers() {
		logger.Printf("leftover publication %s (never authoritative; the next publication of its id overwrites it)", l)
	}
	// an incident is released only by the operator's release (§8), which
	// needs this service stopped: say which ones are held
	for _, r := range svc.All() {
		if r.State == launcher.StateQuarantined {
			logger.Printf("QUARANTINED %s (%s; owner %s, charge %d cpus / %d MiB): %s; the operator's `urgit-vm-launcher recover`, with serve stopped, retries its cleanup and releases it", r.ID, r.Label, r.Owner.Daemon, r.CPUs, r.MemoryMiB+launcher.OverheadMiB, r.Reason)
		}
	}
	l, err := listen(cfg)
	if err != nil {
		return errors.Join(err, svc.Close())
	}
	allowed := map[uint32]bool{}
	for _, u := range cfg.RunnerUIDs {
		allowed[uint32(u)] = true
	}
	reapCtx, stopReaper := context.WithCancel(ctx)
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		// its first pass at once: interrupted work of a previous process,
		// before the first tick; no pass waits for another's teardowns
		svc.Reap(reapCtx, 5*time.Second)
	}()
	sv := &launcher.Server{Service: svc, AllowedUIDs: allowed, Version: version, Log: logger}
	logger.Printf("serving on %s for uids %v; budget %d cpus / %d MiB / %d guests; ceiling %v; images %v", cfg.Socket, cfg.RunnerUIDs, cfg.BudgetCPUs, cfg.BudgetMemoryMiB, cfg.MaxGuests, cfg.Ceiling, images)
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	serr := sv.Serve(l)
	l.Close()
	// the reaper starts nothing more; a teardown it has in progress keeps
	// its own deadline, and Shutdown waits for it before the lock goes
	stopReaper()
	svc.BeginStop("launcher stopping")
	// ownership-scoped shutdown: every VM this launcher owns is torn down
	logger.Printf("stopping: creates rolled back, operations in progress finished, owned vms destroyed")
	derr := svc.Shutdown(context.Background())
	if derr != nil {
		logger.Printf("shutdown: %v", derr)
	}
	<-reaped
	return errors.Join(serr, derr)
}

// logOutcome is serve's line for a teardown the launcher started on its
// own that did not release its record. A late one never releases
// (recovery ruling A): it is logged as the incident it is, its reason
// saying LATE. A release decided in time whose withdrawal was published
// after the deadline is logged too (INTEGRATION.md §11.3).
func logOutcome(logger *log.Logger, o launcher.Outcome) {
	switch o.Kind {
	case "released":
		// on-time cleanup, late bookkeeping: reported apart (INTEGRATION.md §11.8)
		logger.Printf("RELEASED, ACCOUNTING CONFIRMED LATE %s (owner %s, %s): %s", o.ID, o.Owner.Daemon, o.By, o.Reason)
	case "halted":
		logger.Printf("TEARDOWN HALTED %s (owner %s, %s): %s; nothing was removed and it stays charged; the reaper's next pass or its owner's destroy starts it again", o.ID, o.Owner.Daemon, o.By, o.Reason)
	case "quarantined":
		logger.Printf("QUARANTINED %s (owner %s, %s): %s; the operator's `urgit-vm-launcher recover`, with serve stopped, retries its cleanup and releases it", o.ID, o.Owner.Daemon, o.By, o.Reason)
	case "unconfirmed":
		logger.Printf("RELEASE NOT CONFIRMED %s (owner %s, %s): %s; its accounting is pending: it stays charged until a retry confirms it", o.ID, o.Owner.Daemon, o.By, o.Reason)
	default:
		logger.Printf("TEARDOWN %s %s (owner %s, %s): %s: %v", strings.ToUpper(o.Kind), o.ID, o.Owner.Daemon, o.By, o.Reason, o.Err)
	}
}

// listen opens the control socket, reachable by the runner group only; a
// socket whose ownership or mode cannot be set is not served. A socket a
// previous serve left at the path is replaced; anything else there is not
// the launcher's to remove, and is refused.
func listen(cfg *Config) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Socket), 0o755); err != nil {
		return nil, err
	}
	switch fi, err := os.Lstat(cfg.Socket); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	case fi.Mode()&os.ModeSocket == 0:
		return nil, fmt.Errorf("%s is not a socket (%s); it is not the launcher's to remove", cfg.Socket, fi.Mode().Type())
	default:
		if err := os.Remove(cfg.Socket); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Listener, error) {
		l.Close()
		return nil, err
	}
	if cfg.SocketGroup != "" {
		g, err := user.LookupGroup(cfg.SocketGroup)
		if err != nil {
			return fail(fmt.Errorf("socket_group %s: %w", cfg.SocketGroup, err))
		}
		gid, err := strconv.Atoi(g.Gid)
		if err != nil {
			return fail(fmt.Errorf("socket_group %s: gid %q: %w", cfg.SocketGroup, g.Gid, err))
		}
		if err := os.Chown(cfg.Socket, 0, gid); err != nil {
			return fail(err)
		}
	}
	if err := os.Chmod(cfg.Socket, 0o660); err != nil {
		return fail(err)
	}
	return l, nil
}

// list prints every record from the read-only view (no lock, no host
// operation: it works beside a running serve), then any leftover
// publication and any state the launcher cannot account for; the latter
// fails the command.
func list(cfg *Config, out io.Writer, logger *log.Logger) error {
	snap, err := launcher.ReadState(cfg.StateDir, cfg.IDPrefix)
	if err != nil {
		return err
	}
	for _, r := range snap.Records {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(b))
	}
	for _, l := range snap.Leftovers {
		logger.Printf("leftover publication %s", l)
	}
	for _, p := range snap.Problems {
		logger.Printf("UNSAFE STATE %s", p)
	}
	// the releases whose late bookkeeping is kept in their evidence
	// (INTEGRATION.md §11.8): reported apart, never claimed on time
	evs, bad, err := launcher.Evidence(cfg.StateDir)
	if err != nil {
		return err
	}
	for _, ev := range evs {
		if rel := ev.Release; rel != nil && rel.Late() {
			confirmed := "at an instant not recorded (it counts as late)"
			if rel.ConfirmedUnixNano != 0 {
				confirmed = "at " + time.Unix(0, rel.ConfirmedUnixNano).UTC().Format(time.RFC3339Nano)
			}
			logger.Printf("RELEASED, ACCOUNTING CONFIRMED LATE %s (incarnation %s, %s): its cleanup finished at %s, before its obligation's deadline %s; its accounting was confirmed %s",
				ev.ID, incarnationOf(ev.Incarnation), labelOf(launcher.InspectRecord(ev)), time.Unix(0, rel.CleanedUnixNano).UTC().Format(time.RFC3339Nano), time.Unix(0, rel.DueUnixNano).UTC().Format(time.RFC3339Nano), confirmed)
		}
	}
	for _, p := range bad {
		logger.Printf("UNREADABLE EVIDENCE %s", p)
	}
	if n := len(snap.Problems); n > 0 {
		return fmt.Errorf("%d state entries could not be accounted for", n)
	}
	return nil
}

// ---- the privileged host ------------------------------------------------

type realHost struct {
	cfg    *Config
	log    *log.Logger
	images map[string]launcher.Image
	vmUID  int
	vmGID  int
	// pid is the launcher's own process, which placeCgroups puts in its
	// leaf of the delegated subtree (os.Getpid in production)
	pid int
	// command runs one host command under ctx: its combined output,
	// whether a process was started at all, and its error. owned: the
	// command leads its own process group (every command but the jailer,
	// whose --daemonize calls setsid, which a group leader cannot).
	// runCommand in production; tests put a recorder here that executes
	// nothing.
	command func(ctx context.Context, owned bool, name string, args ...string) (out string, started bool, err error)
	// the kernel interfaces, each at its root (tests point them at private
	// models): the cgroup v2 filesystem, the named network namespaces'
	// mount points, /proc
	cgroups  cgroupFS
	netnsDir string
	procDir  string
	// ctx bounds every operation of this view of the host (Bound); nil
	// is no bound
	ctx context.Context
	// named destinations (names.go): the host resolver a granted name is
	// pinned with, the responder starter, and the running responders,
	// shared by every bounded view. Tests put models here.
	lookupIP func(ctx context.Context, name string) ([]netip.Addr, error)
	startDNS func(nsPath, listen string, table pindns.Table) (io.Closer, error)
	dns      *dnsRegistry
	ownAddrs func() ([]netip.Addr, error) // nil: the host's interfaces
}

// Bound is the core's seam (launcher.Bounder): a view of the host whose
// every command and filesystem operation ends by ctx, reporting one cut
// short as possibly acted (runner/launcher/INTEGRATION.md §7.2).
func (h *realHost) Bound(ctx context.Context) launcher.Host {
	v := *h
	v.ctx = ctx
	return &v
}

func (h *realHost) context() context.Context {
	if h.ctx == nil {
		return context.Background()
	}
	return h.ctx
}

// killGrace bounds, after a command's deadline, how long it may take to
// exit once killed — and, after any exit, how long its output may stay
// open — before the call returns anyway (INTEGRATION.md §7.1).
const killGrace = 2 * time.Second

// maxOutput is what a command's output keeps; the rest is dropped.
const maxOutput = 64 << 10

// runCommand runs one host command, never past ctx (INTEGRATION.md §7.1):
//   - a command that cannot be started acted not at all (started false);
//   - an owned command leads a new process group. When ctx ends, the group
//     is killed while its leader is not yet reaped, so the group id cannot
//     have been reused; after a normal exit, any member still alive is
//     killed the same way, before the leader is reaped;
//   - the jailer (not owned) is killed by its own pid (Go's pidfd) and by
//     the group its setsid may have made; after a normal exit nothing is
//     signalled — that group is the VMM's;
//   - a descendant that keeps the output open is waited for killGrace at
//     most; a process that does not die of SIGKILL (uninterruptible sleep)
//     is left to a reaper goroutine, and the call returns, saying so.
//
// Every failure but "not started" may have acted.
func runCommand(ctx context.Context, owned bool, name string, args ...string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, fmt.Errorf("%s not started: %w", name, err)
	}
	cmd := exec.Command(name, args...)
	out := &boundedBuffer{max: maxOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = killGrace
	if owned {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		return "", false, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	go func() {
		waitExited(pid)
		close(exited)
	}()
	var cut error
	select {
	case <-exited:
		if owned {
			_ = syscall.Kill(-pid, syscall.SIGKILL) // members left behind; the leader is unreaped
		}
	case <-ctx.Done():
		cut = ctx.Err()
		if !owned {
			_ = cmd.Process.Kill()
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL) // its group, or the jailer's own after setsid
		select {
		case <-exited:
		case <-time.After(killGrace):
			go func() {
				<-exited
				_ = cmd.Wait()
			}()
			return out.String(), true, fmt.Errorf("%s: cut short (%w), and pid %d did not exit after SIGKILL within %s: it may still act", name, cut, pid, killGrace)
		}
	}
	err := cmd.Wait()
	switch {
	case cut != nil:
		return out.String(), true, fmt.Errorf("%s: cut short, killed: %w", name, cut)
	case errors.Is(err, exec.ErrWaitDelay):
		return out.String(), true, fmt.Errorf("%s: exited, but a descendant kept its output open past %s: %w", name, killGrace, err)
	}
	return out.String(), true, err
}

// waitExited returns once pid has exited, leaving it unreaped (waitid
// WEXITED|WNOWAIT): until the command's Wait, neither its pid nor its
// process group id can be reused.
func waitExited(pid int) {
	const pPID = 1
	var info [128]byte // siginfo_t
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno != syscall.EINTR {
			return
		}
	}
}

// boundedBuffer keeps the first max bytes written to it.
type boundedBuffer struct {
	mu  sync.Mutex
	max int
	b   bytes.Buffer
}

func (w *boundedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := w.max - w.b.Len(); room > 0 {
		w.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (w *boundedBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// bounded runs a filesystem operation within the view's bound: one whose
// bound has already passed is not started; one the bound cuts short is
// reported as possibly acted (it may still finish).
func (h *realHost) bounded(what string, op func() error) error {
	if err := h.context().Err(); err != nil {
		return fmt.Errorf("%s not started: %w", what, err)
	}
	done := make(chan error, 1)
	go func() { done <- op() }()
	select {
	case err := <-done:
		return err
	case <-h.context().Done():
		return fmt.Errorf("%s: cut short (%w); it may still act", what, h.context().Err())
	}
}

func newHost(cfg *Config, logger *log.Logger) (*realHost, error) {
	h := &realHost{cfg: cfg, log: logger, images: map[string]launcher.Image{}, command: runCommand, pid: os.Getpid(),
		cgroups: osCgroups{root: "/sys/fs/cgroup", proc: "/proc"}, netnsDir: "/run/netns", procDir: "/proc",
		lookupIP: hostLookup, startDNS: startPinnedDNS, dns: newDNSRegistry()}
	u, err := user.Lookup(cfg.VMUser)
	if err != nil {
		return nil, fmt.Errorf("vm_user %s: %w", cfg.VMUser, err)
	}
	h.vmUID, _ = strconv.Atoi(u.Uid)
	h.vmGID, _ = strconv.Atoi(u.Gid)
	entries, _ := os.ReadDir(cfg.ImageDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(cfg.ImageDir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		if digest != e.Name() {
			logger.Printf("image %s: manifest digest %s does not match the directory name; ignored", e.Name(), digest[:12])
			continue
		}
		var m struct {
			Kernel struct {
				Name, SHA256 string
			}
			Rootfs struct {
				Name        string
				SHA256      string
				BaselineMiB int `json:"baseline_mib"`
			}
			BootArgs string `json:"boot_args"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		h.images[digest] = launcher.Image{Manifest: digest, Kernel: filepath.Join(dir, m.Kernel.Name), Rootfs: filepath.Join(dir, m.Rootfs.Name), BaselineMiB: m.Rootfs.BaselineMiB, BootArgs: m.BootArgs}
	}
	return h, nil
}

func (h *realHost) imageNames() []string {
	var out []string
	for k := range h.images {
		out = append(out, k[:12])
	}
	return out
}

// service opens the launcher core on this host: it takes the state
// directory's lock (refused while another launcher holds it) and loads
// the records; its error is the caller's to report.
func (h *realHost) service() (*launcher.Service, error) {
	return launcher.NewService(launcher.Config{
		StateDir: h.cfg.StateDir, Images: h.images, BudgetCPUs: h.cfg.BudgetCPUs, BudgetMemoryMiB: h.cfg.BudgetMemoryMiB,
		MaxGuests: h.cfg.MaxGuests, Ceiling: h.cfg.Ceiling, IDPrefix: h.cfg.IDPrefix,
	}, h)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := sha256.New()
	if _, err := io.Copy(s, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(s.Sum(nil)), nil
}

// check is the recipe's preflight: pinned binaries, root ownership of
// every trusted path, KVM, cgroup v2 controllers, the tools, images.
func (h *realHost) check() error {
	var problems []string
	for _, bin := range []struct{ name, sha string }{{"firecracker", h.cfg.FirecrackerSHA256}, {"jailer", h.cfg.JailerSHA256}} {
		p := filepath.Join(h.cfg.BinDir, bin.name)
		got, err := fileSHA256(p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		if got != bin.sha {
			problems = append(problems, fmt.Sprintf("%s: sha256 %s is not the pinned %s", p, got, bin.sha))
		}
		if st, err := os.Stat(p); err == nil {
			if st.Sys().(*syscall.Stat_t).Uid != 0 || st.Mode()&0o022 != 0 {
				problems = append(problems, fmt.Sprintf("%s: must be root-owned and not group/world writable", p))
			}
		}
	}
	for _, dir := range []string{h.cfg.ImageDir, h.cfg.JailBase, h.cfg.StateDir, h.cfg.BinDir} {
		st, err := os.Stat(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		if st.Sys().(*syscall.Stat_t).Uid != 0 || st.Mode()&0o022 != 0 {
			problems = append(problems, fmt.Sprintf("%s: must be root-owned and not group/world writable", dir))
		}
	}
	if len(h.images) == 0 {
		problems = append(problems, "no image with a matching manifest digest under "+h.cfg.ImageDir)
	}
	for _, img := range h.images {
		for _, f := range []string{img.Kernel, img.Rootfs} {
			if _, err := os.Stat(f); err != nil {
				problems = append(problems, fmt.Sprintf("image %s: %v", img.Manifest[:12], err))
			}
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		problems = append(problems, "/dev/kvm: "+err.Error())
	}
	if data, err := h.cgroups.Read("", "cgroup.controllers"); err != nil || len(missingControllers(data)) > 0 {
		problems = append(problems, "cgroup v2 with the cpu, memory and pids controllers is required")
	}
	// C1: every job cgroup lies inside the delegated service cgroup; while
	// the service runs, that cgroup is systemd's delegation to it (serve
	// places itself there: placeCgroups)
	problems = append(problems, cgroupLayoutProblems(h.cfg)...)
	if found, err := h.cgroups.Exists(h.cfg.CgroupService); err != nil || found {
		problems = append(problems, h.delegationProblems()...)
	}
	for _, tool := range []string{"ip", "nft", "cp", "resize2fs", "tune2fs", "e2fsck"} {
		if _, err := exec.LookPath(tool); err != nil {
			problems = append(problems, tool+" is not on the PATH")
		}
	}
	for _, d := range h.cfg.Ceiling {
		// D1: a ceiling entry is one exact destination — a literal becomes
		// a forward accept and, for a host address, the input chain's only
		// exception; a DNS name is pinned per reservation to public
		// addresses (names.go) — never a network, a port range or a list
		if _, err := exactDestination(d); err != nil {
			if _, _, _, named := namedDestination(d); !named {
				problems = append(problems, "ceiling entry "+d+": "+err.Error())
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n  "))
	}
	return nil
}

func (h *realHost) jailRoot(id string) string {
	return filepath.Join(h.cfg.JailBase, "firecracker", id, "root")
}

// run runs one owned host command under the view's bound.
func (h *realHost) run(name string, args ...string) (string, error) {
	out, _, err := h.command(h.context(), true, name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return out, nil
}

// PrepareDisk: the jail root with a fresh writable copy of the golden
// rootfs grown to baseline+disk_mib, an independent copy of the kernel, both owned
// by the vm user. The attempt's jail directory is created here and never
// reused: one that already exists was not made for this reservation, and
// the step refuses without effect (runner/launcher/INTEGRATION.md §7.3).
func (h *realHost) PrepareDisk(id string, img launcher.Image, totalMiB int) (string, error) {
	jails := filepath.Join(h.cfg.JailBase, "firecracker")
	dir, root := filepath.Join(jails, id), h.jailRoot(id)
	if err := os.MkdirAll(jails, 0o750); err != nil {
		return "", fmt.Errorf("%w: %v", launcher.ErrNoEffect, err)
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("%w: jail %s already exists and is not this reservation's", launcher.ErrNoEffect, dir)
		}
		return "", fmt.Errorf("%w: jail %s: %v", launcher.ErrNoEffect, dir, err)
	}
	if err := os.Mkdir(root, 0o750); err != nil {
		return "", err
	}
	disk := filepath.Join(root, "disk.ext4")
	if out, err := h.run("cp", "--reflink=auto", "--sparse=always", img.Rootfs, disk); err != nil {
		return "", errors.New(out)
	}
	if _, err := h.run("resize2fs", disk, strconv.Itoa(totalMiB)+"M"); err != nil {
		return "", err
	}
	if _, err := h.run("tune2fs", "-U", "random", disk); err != nil {
		return "", err
	}
	kernel := filepath.Join(root, "vmlinux")
	// C7: never share the installed kernel's inode with a VM. Chown and
	// chmod below must affect only this jail's independent copy.
	if out, err := h.run("cp", img.Kernel, kernel); err != nil {
		return "", errors.New(out)
	}
	for _, p := range []string{root, disk, kernel} {
		if err := os.Chown(p, h.vmUID, h.vmGID); err != nil {
			return "", err
		}
	}
	_ = os.Chmod(disk, 0o600)
	_ = os.Chmod(kernel, 0o400)
	return disk, nil
}

// cgroupFS is the cgroup v2 filesystem as the adapter uses it, every path
// relative to its root: osCgroups is the kernel's (/sys/fs/cgroup); tests
// use a private model with cgroupfs semantics (interface files are not
// directory contents; a cgroup with processes or children cannot be
// removed).
type cgroupFS interface {
	MkdirAll(path string) error
	Mkdir(path string) error // fs.ErrExist when present
	Write(path, file, value string) error
	Read(path, file string) (string, error)
	Children(path string) ([]string, error) // the child cgroups' names
	Rmdir(path string) error
	Exists(path string) (bool, error)
	// Delegated says whether systemd marks the cgroup as delegated to its
	// unit (Delegate=yes sets the trusted.delegate attribute to 1)
	Delegated(path string) (bool, error)
	// Of is pid's cgroup, relative to the root (/proc/<pid>/cgroup)
	Of(pid int) (string, error)
}

type osCgroups struct{ root, proc string }

func (c osCgroups) Delegated(path string) (bool, error) {
	buf := make([]byte, 16)
	n, err := syscall.Getxattr(c.at(path), "trusted.delegate", buf)
	switch {
	case errors.Is(err, syscall.ENODATA):
		return false, nil
	case err != nil:
		return false, &os.PathError{Op: "getxattr trusted.delegate", Path: c.at(path), Err: err}
	}
	return string(buf[:n]) == "1", nil
}

func (c osCgroups) Of(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join(c.proc, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	return cgroupV2Path(string(data))
}

// cgroupV2Path reads a /proc/<pid>/cgroup listing on a cgroup v2 host: its
// one line is 0::/<path>; the path comes back relative to the root.
func cgroupV2Path(listing string) (string, error) {
	lines := strings.Split(strings.TrimSpace(listing), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "0::/") {
		return "", fmt.Errorf("not one cgroup v2 membership: %q", listing)
	}
	return strings.TrimPrefix(filepath.Clean(strings.TrimPrefix(lines[0], "0::")), "/"), nil
}

func (c osCgroups) at(parts ...string) string {
	return filepath.Join(append([]string{c.root}, parts...)...)
}
func (c osCgroups) MkdirAll(path string) error { return os.MkdirAll(c.at(path), 0o755) }
func (c osCgroups) Mkdir(path string) error    { return os.Mkdir(c.at(path), 0o755) }
func (c osCgroups) Write(path, file, value string) error {
	return os.WriteFile(c.at(path, file), []byte(value), 0o644)
}
func (c osCgroups) Read(path, file string) (string, error) {
	data, err := os.ReadFile(c.at(path, file))
	return string(data), err
}
func (c osCgroups) Children(path string) ([]string, error) {
	entries, err := os.ReadDir(c.at(path))
	if err != nil {
		return nil, err
	}
	var kids []string
	for _, e := range entries {
		if e.IsDir() {
			kids = append(kids, e.Name())
		}
	}
	return kids, nil
}
func (c osCgroups) Rmdir(path string) error {
	if err := syscall.Rmdir(c.at(path)); err != nil {
		return &os.PathError{Op: "rmdir", Path: c.at(path), Err: err}
	}
	return nil
}
func (c osCgroups) Exists(path string) (bool, error) {
	_, err := os.Lstat(c.at(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// The cgroup layout under systemd's delegation (C1). The unit (Delegate=yes)
// gets its own service cgroup, cgroup_service, which systemd delegates to the
// launcher: systemd is the only writer above it, the launcher the only one
// below it. In it:
//
//	<service>/supervisor              the serving process, a leaf (the unit's
//	                                  DelegateSubgroup=, or placeCgroups' move)
//	<service>/<cgroup_parent>         the jobs cgroup, no process, with cpu,
//	                                  memory and pids enabled for its children
//	<service>/<cgroup_parent>/<id>    a job's leaf: its limits
//	<service>/<cgroup_parent>/<id>/<id>  the jailer's cgroup: the VMM
//
// No job cgroup is ever at the cgroup root or outside the service cgroup.
const supervisorLeaf = "supervisor"

// cgroupControllers are the controllers the launcher enables in the
// subtree it owns; each job's limits use them.
var cgroupControllers = []string{"cpu", "memory", "pids"}

// unitSuffixes are the names systemd gives its own cgroups; none is a name
// inside the launcher's delegated subtree.
var unitSuffixes = []string{".service", ".slice", ".scope", ".socket", ".mount", ".swap", ".target", ".timer", ".path", ".device", ".automount"}

// cgroupLayoutProblems: the service cgroup is a clean relative path below
// the cgroup root, a service's (it ends in .service); the jobs cgroup is one
// plain name inside it, neither the supervisor leaf nor a unit's name.
func cgroupLayoutProblems(c *Config) []string {
	var p []string
	s := c.CgroupService
	if s == "" || s == "." || filepath.IsAbs(s) || filepath.Clean(s) != s || slices.Contains(strings.Split(s, "/"), "..") || !strings.HasSuffix(s, ".service") {
		p = append(p, "cgroup_service must be the launcher's own systemd service cgroup, a clean relative path under the cgroup root ending in .service")
	}
	j := c.CgroupParent
	if j == "" || j == "." || j == ".." || strings.Contains(j, "/") || j == supervisorLeaf || slices.ContainsFunc(unitSuffixes, func(x string) bool { return strings.HasSuffix(j, x) }) {
		p = append(p, "cgroup_parent must be one plain name, the jobs cgroup inside the delegated service cgroup (not "+supervisorLeaf+", no systemd unit suffix)")
	}
	return p
}

// missingControllers: those of cgroupControllers a cgroup.controllers
// listing lacks.
func missingControllers(listing string) []string {
	have := strings.Fields(listing)
	var out []string
	for _, c := range cgroupControllers {
		if !slices.Contains(have, c) {
			out = append(out, c)
		}
	}
	return out
}

// delegationProblems: the service cgroup is systemd's delegation to the
// launcher — marked delegated, with cpu, memory and pids among its
// controllers.
func (h *realHost) delegationProblems() []string {
	s := h.cfg.CgroupService
	var p []string
	switch ok, err := h.cgroups.Delegated(s); {
	case err != nil:
		p = append(p, fmt.Sprintf("the service cgroup %s: its delegation cannot be read: %v", s, err))
	case !ok:
		p = append(p, fmt.Sprintf("the service cgroup %s is not delegated by systemd (no trusted.delegate; the unit needs Delegate=yes)", s))
	}
	switch data, err := h.cgroups.Read(s, "cgroup.controllers"); {
	case err != nil:
		p = append(p, fmt.Sprintf("the service cgroup %s: %v", s, err))
	case len(missingControllers(data)) > 0:
		p = append(p, fmt.Sprintf("the service cgroup %s lacks the %s controller(s): systemd did not delegate them", s, strings.Join(missingControllers(data), ", ")))
	}
	return p
}

// placeCgroups is serve's start in its delegated subtree (C1), before any
// record is loaded or any job cgroup made: the launcher runs in its service
// cgroup S, which systemd marks delegated and whose controllers include
// cpu, memory and pids; the serving process is in the leaf S/supervisor —
// the unit's DelegateSubgroup= starts it there; started in S itself (a
// systemd without it), it moves itself — and no other process is in S; S
// then enables the controllers for its children, and the jobs cgroup
// S/<cgroup_parent>, holding no process, enables them for the jobs. Anything
// else refuses to serve; a refusal found before the first write writes
// nothing.
func (h *realHost) placeCgroups() error {
	if p := cgroupLayoutProblems(h.cfg); len(p) > 0 {
		return errors.New(strings.Join(p, "; "))
	}
	s, jobs := h.cfg.CgroupService, h.jobsCgroup()
	sup := filepath.Join(s, supervisorLeaf)
	self, err := h.cgroups.Of(h.pid)
	if err != nil {
		return fmt.Errorf("the launcher's own cgroup: %w", err)
	}
	if self != s && self != sup {
		return fmt.Errorf("the launcher runs in cgroup /%s, not in its delegated service cgroup /%s (the unit's Delegate=yes): its job cgroups would be outside the delegated subtree", self, s)
	}
	if p := h.delegationProblems(); len(p) > 0 {
		return errors.New(strings.Join(p, "; "))
	}
	if self == s {
		if err := h.cgroups.Mkdir(sup); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("the supervisor leaf %s: %w", sup, err)
		}
		if err := h.cgroups.Write(sup, "cgroup.procs", strconv.Itoa(h.pid)); err != nil {
			return fmt.Errorf("moving the launcher into its leaf %s: %w", sup, err)
		}
		if now, err := h.cgroups.Of(h.pid); err != nil || now != sup {
			return fmt.Errorf("the launcher is not in its leaf %s after the move (it is in %s): %v", sup, now, err)
		}
	}
	// the service cgroup and the jobs cgroup are inner nodes: a process left
	// in either is refused, never moved — it is not the launcher's
	if procs, err := h.cgroups.Read(s, "cgroup.procs"); err != nil || len(strings.Fields(procs)) > 0 {
		return fmt.Errorf("a process left in the service cgroup %s, an inner node (%q, %v): its controllers cannot be given to the jobs", s, strings.Fields(procs), err)
	}
	if err := h.cgroups.Write(s, "cgroup.subtree_control", "+cpu +memory +pids"); err != nil {
		return fmt.Errorf("enable cpu, memory and pids under %s: %w", s, err)
	}
	if err := h.cgroups.Mkdir(jobs); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("the jobs cgroup %s: %w", jobs, err)
	}
	if procs, err := h.cgroups.Read(jobs, "cgroup.procs"); err != nil || len(strings.Fields(procs)) > 0 {
		return fmt.Errorf("a process left in the jobs cgroup %s, an inner node (%q, %v)", jobs, strings.Fields(procs), err)
	}
	if data, err := h.cgroups.Read(jobs, "cgroup.controllers"); err != nil || len(missingControllers(data)) > 0 {
		return fmt.Errorf("the jobs cgroup %s lacks the %s controller(s) (%v)", jobs, strings.Join(missingControllers(data), ", "), err)
	}
	if err := h.cgroups.Write(jobs, "cgroup.subtree_control", "+cpu +memory +pids"); err != nil {
		return fmt.Errorf("enable cpu, memory and pids under %s: %w", jobs, err)
	}
	return nil
}

// jobsCgroup is the jobs cgroup inside the delegated service cgroup.
func (h *realHost) jobsCgroup() string { return filepath.Join(h.cfg.CgroupService, h.cfg.CgroupParent) }

// cgroupPaths names the VM's leaf relative to the cgroup root
// (runner/launcher/INTEGRATION.md §7.3), plus a legacy child path unused
// when starting the VMM. With Firecracker v1.17, --cgroup-version 2 and
// --parent-cgroup <leaf> but no --cgroup arguments, the jailer creates no
// child cgroup: the VMM runs in the leaf itself, directly under its limits.
// Both returned paths are under the delegated subtree's jobs cgroup (C1);
// the second return value is retained without changing behaviour.
func (h *realHost) cgroupPaths(id string) (leaf, jailer string) {
	leaf = filepath.Join(h.jobsCgroup(), id)
	return leaf, filepath.Join(leaf, id)
}

// ownedCgroup is cgroupPaths only for a layout inside the delegated subtree
// and a leaf exactly one level below the jobs cgroup: a cgroup outside the
// delegated subtree is never made, given to the jailer or removed (C1).
func (h *realHost) ownedCgroup(id string) (leaf, jailer string, err error) {
	if p := cgroupLayoutProblems(h.cfg); len(p) > 0 {
		return "", "", fmt.Errorf("the cgroup layout is not inside the delegated subtree: %s", strings.Join(p, "; "))
	}
	leaf, jailer = h.cgroupPaths(id)
	if id == "" || id == "." || id == ".." || strings.Contains(id, "/") || filepath.Dir(leaf) != h.jobsCgroup() {
		return "", "", fmt.Errorf("cgroup %s for %q is not a job cgroup of %s, the delegated subtree's jobs cgroup", leaf, id, h.jobsCgroup())
	}
	return leaf, jailer, nil
}

// CreateCgroup: a leaf under the jobs cgroup with cpu.max, memory.max
// (guest RAM + overhead), no swap, a pids bound. The jobs cgroup, with its
// controllers, is placed at serve's start (placeCgroups); one not in place
// refuses without effect, as does a leaf that already exists, which is not
// this reservation's.
func (h *realHost) CreateCgroup(id string, cpus, memMiB int) error {
	leaf, _, err := h.ownedCgroup(id)
	if err != nil {
		return fmt.Errorf("%w: %v", launcher.ErrNoEffect, err)
	}
	jobs := h.jobsCgroup()
	if found, err := h.cgroups.Exists(jobs); err != nil || !found {
		return fmt.Errorf("%w: the jobs cgroup %s is not in place (serve places it at its start): present %v, %v", launcher.ErrNoEffect, jobs, found, err)
	}
	if err := h.cgroups.Mkdir(leaf); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: cgroup %s already exists and is not this reservation's", launcher.ErrNoEffect, leaf)
		}
		return fmt.Errorf("%w: cgroup %s: %v", launcher.ErrNoEffect, leaf, err)
	}
	writes := map[string]string{
		"cpu.max":         fmt.Sprintf("%d 100000", cpus*100000),
		"memory.max":      fmt.Sprintf("%dM", memMiB),
		"memory.swap.max": "0",
		"pids.max":        "512",
	}
	for _, f := range slices.Sorted(maps.Keys(writes)) {
		if err := h.cgroups.Write(leaf, f, writes[f]); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

// network numbering from the index: veth host 10.113.<i>.1/30 ↔ ns
// 10.113.<i>.2/30, tap 172.16.<i>.1/30 ↔ guest 172.16.<i>.2/30 (i ≥ 1,
// two octets from the index)
func netAddrs(index int) (vhost, vns, tap, guest string) {
	hi, lo := (index>>6)&0xff, (index&0x3f)*4
	vhost = fmt.Sprintf("10.113.%d.%d", hi, lo+1)
	vns = fmt.Sprintf("10.113.%d.%d", hi, lo+2)
	tap = fmt.Sprintf("172.16.%d.%d", hi, lo+1)
	guest = fmt.Sprintf("172.16.%d.%d", hi, lo+2)
	return
}

// CreateNetwork: the VM's network namespace with its TAP (owned by the
// vm user) and a veth to the host; inside the namespace forwarding and
// masquerade from the TAP; on the host, in the launcher's own nft table,
// a per-VM chain that accepts exactly the allowed destinations from this
// VM's veth and drops everything else, plus the return path. Nothing
// else on the host changes: no default route, no firewall flush.
//
// Before any of it, every object of this VM is proven absent — the
// namespace, the veth, the per-VM chain, any rule naming the veth or the
// namespace address — and the step refuses without effect if one exists:
// it was made by something else, and is neither reused (a chain's stale
// rules would become this VM's policy) nor, later, removed as this
// reservation's (runner/launcher/INTEGRATION.md §7.3).
func (h *realHost) CreateNetwork(id string, index int, allow []string) (launcher.NetInfo, error) {
	var info launcher.NetInfo
	vhost, vns, tapIP, guestIP := netAddrs(index)
	vh, vg := fmt.Sprintf("vh%d", index), fmt.Sprintf("vg%d", index)
	refuse := func(format string, a ...any) (launcher.NetInfo, error) {
		return info, fmt.Errorf("%w: %s", launcher.ErrNoEffect, fmt.Sprintf(format, a...))
	}
	// D1: each destination is one exact entry — no network, port range or
	// list — so neither its forward accept nor its input exception is
	// broader than the entry. A literal gets both; a named destination
	// (names.go) is pinned now, on the host, to the public IPv4 addresses
	// it resolves to: each gets a forward accept at the entry's proto and
	// port, and no input exception. Every name is pinned before anything
	// is created, so a name that cannot be pinned refuses without effect.
	var accepts, forward [][]string
	table := pindns.Table{}
	for _, d := range allow {
		if dst, err := exactDestination(d); err == nil {
			accepts = append(accepts, dst)
			forward = append(forward, dst)
			continue
		}
		proto, name, port, ok := namedDestination(d)
		if !ok {
			return refuse("destination %s: neither one IPv4 literal nor one DNS name, with tcp or udp and one port", d)
		}
		addrs, ok := table[name]
		if !ok {
			pinned, err := h.pin(name)
			if err != nil {
				return refuse("destination %s: %v", d, err)
			}
			addrs, table[name] = pinned, pinned
		}
		for _, a := range addrs {
			forward = append(forward, []string{a.String(), proto, port})
		}
	}
	if found, err := h.netnsExists(id); err != nil || found {
		return refuse("network namespace %s: present %v (%v); not this reservation's", id, found, err)
	}
	if found, err := h.linkExists(vh); err != nil || found {
		return refuse("host link %s: present %v (%v); not this reservation's", vh, found, err)
	}
	st, err := h.tableObjects(index)
	if err != nil || st.chain || st.inChain || len(st.rules) > 0 {
		return refuse("table %s holds VM index %d's objects already (chain vm-%d: %v, chain in-%d: %v, rules %q, %v); not this reservation's", h.cfg.NFTTable, index, index, st.chain, index, st.inChain, st.rules, err)
	}
	steps := [][]string{
		{"ip", "netns", "add", id},
		{"ip", "link", "add", vh, "type", "veth", "peer", "name", vg, "netns", id},
		{"ip", "addr", "add", vhost + "/30", "dev", vh},
		{"ip", "link", "set", vh, "up"},
		{"ip", "-n", id, "addr", "add", vns + "/30", "dev", vg},
		{"ip", "-n", id, "link", "set", vg, "up"},
		{"ip", "-n", id, "link", "set", "lo", "up"},
		{"ip", "-n", id, "tuntap", "add", "tap0", "mode", "tap", "user", strconv.Itoa(h.vmUID)},
		{"ip", "-n", id, "addr", "add", tapIP + "/30", "dev", "tap0"},
		{"ip", "-n", id, "link", "set", "tap0", "up"},
		{"ip", "-n", id, "route", "add", "default", "via", vhost},
		{"ip", "netns", "exec", id, "sysctl", "-q", "-w", "net.ipv4.ip_forward=1"},
		{"ip", "netns", "exec", id, "sysctl", "-q", "-w", "net.ipv6.conf.all.disable_ipv6=1"},
		{"ip", "netns", "exec", id, "nft", "add", "table", "ip", "guest"},
		{"ip", "netns", "exec", id, "nft", "add", "chain", "ip", "guest", "post", "{ type nat hook postrouting priority 100; }"},
		{"ip", "netns", "exec", id, "nft", "add", "rule", "ip", "guest", "post", "oifname", vg, "masquerade"},
		{"sysctl", "-q", "-w", "net.ipv4.conf." + vh + ".forwarding=1"},
	}
	for _, s := range steps {
		if out, err := h.run(s[0], s[1:]...); err != nil {
			return info, errors.New(strings.TrimSpace(out))
		}
	}
	// the host policy: the launcher's inet table and its forward and post
	// chains (shared: `add` is idempotent for them), and a chain per VM,
	// jumped to from the forward hook by this VM's veth, created new —
	// `create` fails if it exists — and never taken over
	t := h.cfg.NFTTable
	chain := "vm-" + strconv.Itoa(index)
	in := inChain(index)
	rules := [][]string{
		{"add", "table", "inet", t},
		{"add", "chain", "inet", t, "forward", "{ type filter hook forward priority 0; policy accept; }"},
		{"add", "chain", "inet", t, "post", "{ type nat hook postrouting priority 100; }"},
		{"create", "chain", "inet", t, chain},
		{"add", "rule", "inet", t, "forward", "iifname", vh, "jump", chain},
		{"add", "rule", "inet", t, "forward", "oifname", vh, "ct", "state", "established,related", "accept"},
		{"add", "rule", "inet", t, "forward", "oifname", vh, "drop"},
		{"add", "rule", "inet", t, chain, "meta", "nfproto", "ipv6", "drop"},
		{"add", "rule", "inet", t, chain, "ct", "state", "invalid", "drop"},
	}
	for _, a := range dedupe(forward) {
		rules = append(rules, []string{"add", "rule", "inet", t, chain, "ip", "daddr", a[0], a[1], "dport", a[2], "accept"})
	}
	// N2: the masquerade follows the namespace address, not an interface
	// name. The kernel's routing table picks the outgoing interface, and on a
	// host with two default routes, or a NIC that unplugs, a rule bound to one
	// name would not match, so the namespace address would leave unmasked.
	// Only traffic toward the launcher's own veths is not masqueraded.
	rules = append(rules,
		[]string{"add", "rule", "inet", t, chain, "drop"},
		[]string{"add", "rule", "inet", t, "post", "ip", "saddr", vns, "oifname", "!=", "vh*", "masquerade"},
	)
	// D1, the input hook: traffic from a VM's veth to any of the host's own
	// addresses takes the input hook, never forward. The launcher's input
	// chain (shared, `add`) drops the whole veth range; each VM's veth jumps
	// first — `insert`, ahead of that drop — to its own chain in-<index>,
	// created new, which accepts exactly its exact destinations (on this
	// hook, only a host address can match one) and drops the rest. The
	// host's own firewall is not touched: an exception here is no accept
	// past it, only the launcher's own drop withheld.
	rules = append(rules,
		[]string{"add", "chain", "inet", t, "input", "{ type filter hook input priority 0; policy accept; }"},
	)
	if !st.rangeDrop {
		rules = append(rules, []string{"add", "rule", "inet", t, "input", "ip", "saddr", vethRange, "drop"})
	}
	rules = append(rules,
		[]string{"create", "chain", "inet", t, in},
		[]string{"insert", "rule", "inet", t, "input", "iifname", vh, "jump", in},
	)
	for _, a := range accepts {
		rules = append(rules, []string{"add", "rule", "inet", t, in, "ip", "daddr", a[0], a[1], "dport", a[2], "accept"})
	}
	rules = append(rules, []string{"add", "rule", "inet", t, in, "drop"})
	for _, r := range rules {
		if out, err := h.run("nft", r...); err != nil {
			return info, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
		}
	}
	// what the table holds now is what is enforced: a missing input rule,
	// chain or drop, or an exception broader than its entry, fails the
	// create (it acted: the core tears it down)
	out, err := h.run("nft", "-a", "list", "table", "inet", t)
	if err != nil {
		return info, fmt.Errorf("the input containment cannot be verified: %v", err)
	}
	if p := inputContainment(out, index, allow); len(p) > 0 {
		return info, fmt.Errorf("the input containment of %s is not in place: %s", vh, strings.Join(p, "; "))
	}
	info = launcher.NetInfo{TAP: "tap0", GuestIP: guestIP + "/30", Gateway: tapIP, Index: index}
	// a VM granted a name gets its responder, in its own namespace, on its
	// gateway: the only resolver the guest is told of. A VM granted only
	// literals gets none, and keeps an empty resolver.
	if len(table) > 0 {
		if h.startDNS == nil || h.dns == nil {
			return info, errors.New("named destinations need the name responder, which is not configured")
		}
		listen := net.JoinHostPort(tapIP, "53")
		srv, err := h.startDNS(filepath.Join(h.netnsDir, id), listen, table)
		if err != nil {
			return info, fmt.Errorf("the name responder for %s: %v", id, err)
		}
		h.dns.put(id, srv)
		info.DNS = tapIP
		// what was pinned, by name, for the create's answer (CI-P4-NET-1,
		// pinned addresses shown per run)
		for _, name := range slices.Sorted(maps.Keys(table)) {
			p := launcher.Pin{Name: name}
			for _, a := range table[name] {
				p.Addrs = append(p.Addrs, a.String())
			}
			info.Pinned = append(info.Pinned, p)
		}
	}
	return info, nil
}

// dedupe keeps the first of each identical forward accept: two granted
// names that pin the same address and port need one rule.
func dedupe(accepts [][]string) [][]string {
	var out [][]string
	for _, a := range accepts {
		if !slices.ContainsFunc(out, func(b []string) bool { return slices.Equal(a, b) }) {
			out = append(out, a)
		}
	}
	return out
}

// vethRange is every address netAddrs gives a veth or a namespace: the
// launcher's veth range, which its input chain drops (D1).
const vethRange = "10.113.0.0/16"

// inChain is VM index's own input chain (D1).
func inChain(index int) string { return "in-" + strconv.Itoa(index) }

// exactDestination reads one destination entry, proto:ip:port, as exactly
// one destination (D1): tcp or udp, one IPv4 address, one port 1-65535 in
// its plain decimal form — never a network, a range, a name or a list,
// which would make its accept broader than the entry. It answers the
// entry's address, proto and port as the rules write them.
func exactDestination(d string) ([]string, error) {
	proto, rest, ok := strings.Cut(d, ":")
	if !ok || (proto != "tcp" && proto != "udp") {
		return nil, errors.New("not proto:ip:port with proto tcp or udp")
	}
	ip, port, err := net.SplitHostPort(rest)
	if err != nil {
		return nil, fmt.Errorf("not proto:ip:port: %v", err)
	}
	if a := net.ParseIP(ip); a == nil || a.To4() == nil || a.String() != ip {
		return nil, errors.New("its address is not one IPv4 address in its plain form")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return nil, errors.New("its port is not one port 1-65535 in plain decimal")
	}
	return []string{ip, proto, port}, nil
}

// nftChain is one chain of an `nft -a list table` listing: its name, its
// base-chain line (`type … hook …`; empty for a regular chain) and its
// rules in order, each as its fields without the trailing `# handle N`.
type nftChain struct {
	name, hook string
	rules      []nftRule
}

type nftRule struct {
	fields []string
	handle string
}

func parseTable(listing string) []nftChain {
	var out []nftChain
	cur := -1
	for _, line := range strings.Split(listing, "\n") {
		text, handle := line, ""
		if i := strings.LastIndex(line, "# handle "); i >= 0 {
			text, handle = line[:i], strings.TrimSpace(line[i+len("# handle "):])
		}
		f := strings.Fields(text)
		switch {
		case len(f) == 0 || f[0] == "table":
		case f[0] == "chain" && len(f) >= 2:
			out = append(out, nftChain{name: f[1]})
			cur = len(out) - 1
		case f[0] == "}":
			cur = -1
		case cur < 0:
		case f[0] == "type" && slices.Contains(f, "hook"):
			out[cur].hook = strings.Join(f, " ")
		default:
			out[cur].rules = append(out[cur].rules, nftRule{fields: f, handle: handle})
		}
	}
	return out
}

// inputContainment is every way the listing does not contain VM index's
// traffic to the host's own addresses (D1): the input chain is a base chain
// on the input hook and drops the veth range; the veth jumps to in-<index>
// ahead of that drop; in-<index> holds exactly one exact exception per
// allowed destination, then its drop, and nothing else — an exception
// broader than its entry, or any other rule, is a problem.
func inputContainment(listing string, index int, allow []string) []string {
	var p []string
	chains := map[string]nftChain{}
	for _, c := range parseTable(listing) {
		chains[c.name] = c
	}
	vh, in := fmt.Sprintf("%q", fmt.Sprintf("vh%d", index)), inChain(index)
	input, ok := chains["input"]
	if !ok || !strings.Contains(input.hook, "hook input") {
		p = append(p, "the launcher's input chain is missing or not on the input hook")
	}
	jump, drop := -1, -1
	for i, r := range input.rules {
		switch {
		case jump < 0 && slices.Equal(r.fields, []string{"iifname", vh, "jump", in}):
			jump = i
		case drop < 0 && slices.Equal(r.fields, []string{"ip", "saddr", vethRange, "drop"}):
			drop = i
		}
	}
	switch {
	case drop < 0:
		p = append(p, "the input chain's drop of the veth range "+vethRange+" is missing")
	case jump < 0:
		p = append(p, "the input rule sending "+vh+" to "+in+" is missing")
	case jump > drop:
		p = append(p, "the input rule sending "+vh+" to "+in+" comes after the veth range's drop")
	}
	c, ok := chains[in]
	if !ok {
		return append(p, "the chain "+in+" is missing")
	}
	var want [][]string
	for _, d := range allow {
		dst, err := exactDestination(d)
		if err != nil {
			// a named destination never has an input exception (names.go):
			// it is pinned to public addresses, never to the host
			if _, _, _, named := namedDestination(d); !named {
				p = append(p, fmt.Sprintf("destination %s: %v", d, err))
			}
			continue
		}
		want = append(want, []string{"ip", "daddr", dst[0], dst[1], "dport", dst[2], "accept"})
	}
	want = append(want, []string{"drop"})
	got := make([][]string, len(c.rules))
	for i, r := range c.rules {
		got[i] = r.fields
	}
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		p = append(p, fmt.Sprintf("the chain %s is not exactly its exact exceptions and its drop: %q, want %q", in, got, want))
	}
	return p
}

// inputContainmentProblems is the recovery check of D1, at serve's start
// once the records are loaded: every record holding a network whose VMM
// may still run — verified running, or not verifiably gone — has its input
// containment in the launcher's table. A problem refuses to serve.
func (h *realHost) inputContainmentProblems(records []launcher.Record) []string {
	var live []launcher.Record
	for _, r := range records {
		if !r.HasNetwork || !r.HasVMM {
			continue
		}
		if r.PID > 0 {
			if l, _ := h.Liveness(r.ID, r.PID); l == launcher.Gone {
				continue
			}
		}
		live = append(live, r)
	}
	if len(live) == 0 {
		return nil
	}
	out, err := h.run("nft", "-a", "list", "table", "inet", h.cfg.NFTTable)
	if err != nil {
		return []string{fmt.Sprintf("the launcher's table cannot be listed: %v", err)}
	}
	var p []string
	for _, r := range live {
		for _, x := range inputContainment(out, r.NetIndex, r.Destinations) {
			p = append(p, fmt.Sprintf("%s (index %d): %s", r.ID, r.NetIndex, x))
		}
	}
	return p
}

// netnsExists says whether a named network namespace id is mounted.
func (h *realHost) netnsExists(id string) (bool, error) {
	_, err := os.Lstat(filepath.Join(h.netnsDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// linkExists asks ip whether the host namespace has a link of that name;
// any answer but "exists" or "does not exist" proves nothing.
func (h *realHost) linkExists(name string) (bool, error) {
	out, started, err := h.command(h.context(), true, "ip", "link", "show", name)
	switch {
	case err == nil:
		return true, nil
	case started && strings.Contains(out, "does not exist"):
		return false, nil
	}
	return false, fmt.Errorf("ip link show %s: %v: %s", name, err, strings.TrimSpace(out))
}

// tableState is what the launcher's table holds of VM index before a
// create: its per-VM chains (vm-<index>, and D1's in-<index>), every rule
// naming its veth or its namespace address or sending traffic to one of its
// chains (exact tokens, as RemoveNetwork matches them), and whether the
// shared input chain already drops the veth range (D1).
type tableState struct {
	chain, inChain bool
	rules          []string
	rangeDrop      bool
}

// tableObjects reads tableState from the launcher's table. A table that does
// not exist holds none of it.
func (h *realHost) tableObjects(index int) (tableState, error) {
	var st tableState
	t := h.cfg.NFTTable
	out, started, err := h.command(h.context(), true, "nft", "-a", "list", "table", "inet", t)
	if err != nil {
		if started && strings.Contains(out, "No such file") {
			return st, nil
		}
		return st, fmt.Errorf("nft -a list table inet %s: %v: %s", t, err, strings.TrimSpace(out))
	}
	vethToken, addr := fmt.Sprintf("%q", fmt.Sprintf("vh%d", index)), netnsSaddr(index)
	names := []string{"vm-" + strconv.Itoa(index), inChain(index)}
	for _, c := range parseTable(out) {
		st.chain = st.chain || c.name == names[0]
		st.inChain = st.inChain || c.name == names[1]
		for _, r := range c.rules {
			jumps := slices.ContainsFunc(r.fields, func(f string) bool { return slices.Contains(names, f) }) // a rule sending traffic to its chains
			if slices.Contains(r.fields, vethToken) || slices.Contains(r.fields, addr) || jumps {
				st.rules = append(st.rules, c.name+": "+strings.Join(r.fields, " "))
			}
			if c.name == "input" && slices.Equal(r.fields, []string{"ip", "saddr", vethRange, "drop"}) {
				st.rangeDrop = true
			}
		}
	}
	return st, nil
}

// vmBootArgs is the guest kernel's command line: the image's own, the
// attempt, and for a networked VM its address and gateway and, when it was
// granted a name, its pinned-name responder (names.go). Without one the
// guest keeps an empty resolver.
func vmBootArgs(spec launcher.VMSpec) string {
	args := spec.Image.BootArgs + " urgit.attempt=" + spec.Attempt
	if spec.Net != nil {
		args += " urgit.ip=" + spec.Net.GuestIP + "," + spec.Net.Gateway
		if spec.Net.DNS != "" {
			args += " urgit.dns=" + spec.Net.DNS
		}
	}
	return args
}

// startBound bounds a start: the jailer and the wait for its pid file
// (the core passes the same bound; the adapter keeps it without one).
const startBound = 30 * time.Second

// StartVM writes the VM configuration into the jail root and runs the
// jailer, which chroots, joins the namespace, moves into its cgroup under
// the VM's leaf (cgroupPaths), drops to the vm user and execs Firecracker.
// The jailed pid comes back from the pid file the jailer writes. A failure
// before the jailer process exists wraps launcher.ErrNoEffect; once it
// ran, a failure — its bound included — may have left a VMM behind and is
// reported as such (the core quarantines).
func (h *realHost) StartVM(id string, spec launcher.VMSpec) (int, error) {
	ctx, cancel := context.WithTimeout(h.context(), startBound)
	defer cancel()
	root := h.jailRoot(id)
	cfg := map[string]any{
		"boot-source":    map[string]any{"kernel_image_path": "/vmlinux", "boot_args": vmBootArgs(spec)},
		"drives":         []map[string]any{{"drive_id": "rootfs", "path_on_host": "/disk.ext4", "is_root_device": true, "is_read_only": false}},
		"machine-config": map[string]any{"vcpu_count": spec.CPUs, "mem_size_mib": spec.MemoryMiB, "smt": false},
		"vsock":          map[string]any{"guest_cid": spec.CID, "uds_path": "/v.sock"},
	}
	if spec.Net != nil {
		cfg["network-interfaces"] = []map[string]any{{"iface_id": "eth0", "host_dev_name": spec.Net.TAP}}
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	vmConfig := filepath.Join(root, "vm.json")
	if err := os.WriteFile(vmConfig, data, 0o640); err != nil {
		return 0, fmt.Errorf("%w: %v", launcher.ErrNoEffect, err)
	}
	if err := os.Chown(vmConfig, h.vmUID, h.vmGID); err != nil {
		return 0, fmt.Errorf("%w: %v", launcher.ErrNoEffect, err)
	}
	leaf, _, err := h.ownedCgroup(id)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", launcher.ErrNoEffect, err)
	}
	args := []string{
		"--id", id, "--exec-file", filepath.Join(h.cfg.BinDir, "firecracker"),
		"--uid", strconv.Itoa(h.vmUID), "--gid", strconv.Itoa(h.vmGID),
		"--chroot-base-dir", h.cfg.JailBase,
		"--cgroup-version", "2", "--parent-cgroup", leaf,
		"--new-pid-ns", "--daemonize",
	}
	if spec.Net != nil {
		args = append(args, "--netns", filepath.Join(h.netnsDir, id))
	}
	args = append(args, "--", "--no-api", "--config-file", "/vm.json")
	// not an owned process group: the jailer's --daemonize calls setsid
	if out, started, err := h.command(ctx, false, filepath.Join(h.cfg.BinDir, "jailer"), args...); err != nil {
		if !started {
			return 0, fmt.Errorf("%w: jailer: %v", launcher.ErrNoEffect, err)
		}
		return 0, fmt.Errorf("jailer: %v: %s", err, strings.TrimSpace(out))
	}
	// the jailer writes <root>/firecracker.pid with the child's host pid
	pidFile := filepath.Join(root, "firecracker.pid")
	wait := time.NewTimer(10 * time.Second)
	defer wait.Stop()
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			// only a pid verified as the id's VMM (§11.2): one the host
			// cannot verify, or a foreign one, is waited past
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				if l, _ := h.Liveness(id, pid); l == launcher.Running {
					return pid, nil
				}
			}
		}
		select {
		case <-wait.C:
			return 0, errors.New("the jailer wrote no live pid within 10 s")
		case <-ctx.Done():
			return 0, fmt.Errorf("the jailer's pid was not seen before the start's bound: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (h *realHost) Connect(id string, port uint32) (*os.File, error) {
	timeout := 5 * time.Second
	if d, ok := h.context().Deadline(); ok {
		timeout = min(timeout, time.Until(d))
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("connect %s: %w", id, context.DeadlineExceeded)
	}
	return launcher.ConnectVsock(filepath.Join(h.jailRoot(id), "v.sock"), port, timeout)
}

// FindVMMs is the operator retry's look for a VMM whose pid was never
// recorded (launcher.VMMFinder; INTEGRATION.md §§8.2, 11.2): every process
// under the proc root verified Running as the id's VMM (Liveness). The
// look is complete only if the proc root was listed, every numeric entry
// was classified Running or Gone, and it was not cut short; otherwise the
// error names the entries it could not classify (the first few) — an
// incomplete look proves nothing gone — and the pids it did verify are
// still answered.
func (h *realHost) FindVMMs(id string) ([]int, error) {
	entries, err := os.ReadDir(h.procDir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", h.procDir, err)
	}
	var pids []int
	var uncertain []string
	for _, e := range entries {
		if err := h.context().Err(); err != nil {
			return pids, fmt.Errorf("the look for %s's vmm was cut short: %w", id, err)
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		switch l, why := h.Liveness(id, pid); l {
		case launcher.Running:
			pids = append(pids, pid)
		case launcher.Unknown:
			uncertain = append(uncertain, fmt.Sprintf("pid %d: %v", pid, why))
		}
	}
	if len(uncertain) > 0 {
		shown := uncertain[:min(len(uncertain), 4)]
		return pids, fmt.Errorf("%d process(es) could not be classified, so the look is incomplete: %s", len(uncertain), strings.Join(shown, "; "))
	}
	return pids, nil
}

// Liveness classifies pid for id's VMM by its command line (§11.2):
// Running when it carries exactly `--id <id>`; Gone when there is no such
// process (its entry is missing from a proc root that is there: it exited)
// or its readable command line does not mention the id at all (a foreign
// process, a zombie, a kernel thread); Unknown — with the reason — when the
// command line cannot be read for any other reason, the proc root itself
// is not there to say that the process is missing, or the command line
// mentions the id otherwise than as `--id <id>`: that proves neither.
func (h *realHost) Liveness(id string, pid int) (launcher.Liveness, error) {
	data, err := os.ReadFile(filepath.Join(h.procDir, strconv.Itoa(pid), "cmdline"))
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH):
		if _, serr := os.Stat(h.procDir); serr != nil {
			return launcher.Unknown, fmt.Errorf("the proc root %s is not there to say the process is missing: %v", h.procDir, serr)
		}
		return launcher.Gone, nil
	case err != nil:
		return launcher.Unknown, fmt.Errorf("its command line cannot be read: %w", err)
	}
	args := strings.Split(string(data), "\x00")
	for i, a := range args {
		if a == "--id" && i+1 < len(args) && args[i+1] == id {
			return launcher.Running, nil
		}
	}
	if strings.Contains(string(data), id) {
		return launcher.Unknown, fmt.Errorf("its command line mentions %s, but not as --id %s", id, id)
	}
	return launcher.Gone, nil
}

// Kill signals this VM's VMM only: the process is pinned first (a pidfd,
// os.FindProcess), then verified by its command line, and the signal goes
// to the pinned process — never to one that took its pid since the check
// (runner/launcher/INTEGRATION.md §7.1).
func (h *realHost) Kill(id string, pid int, sig string) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil // no such process
	}
	defer p.Release()
	switch l, why := h.Liveness(id, pid); l {
	case launcher.Gone:
		return nil
	case launcher.Unknown:
		// ownership is not verified: nothing is signalled (§11.2)
		return fmt.Errorf("pid %d is not signalled: it cannot be verified as %s's vmm: %v", pid, id, why)
	}
	s := syscall.SIGTERM
	if sig == "KILL" {
		s = syscall.SIGKILL
	}
	if err := p.Signal(s); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// RemoveNetwork removes, in the launcher's table, the rules naming this
// VM's veth or its namespace address — each in the chain the listing shows
// it in: forward, post, and D1's input jump — then its per-VM chains,
// vm-<index> and D1's in-<index>, once nothing jumps to them, then the
// namespace (which takes the veth pair and the tap with it). The input
// chain and its drop of the veth range are shared and stay. The index is
// the one the record holds — never re-read from the namespace, which a
// partial removal may already have deleted — and rules match on exact
// tokens, so vh1 is not vh10 and 10.113.0.6 is not 10.113.0.62.
func (h *realHost) RemoveNetwork(id string, index int) error {
	t := h.cfg.NFTTable
	veth := fmt.Sprintf("%q", fmt.Sprintf("vh%d", index))
	saddr := netnsSaddr(index)
	var problems []string
	// the VM's name responder first: its sockets would keep the namespace,
	// and with it the veth and the TAP, alive after `ip netns del`
	if err := h.dns.stop(id); err != nil {
		problems = append(problems, err.Error())
	}
	listing, err := h.run("nft", "-a", "list", "table", "inet", t)
	if err != nil && !strings.Contains(err.Error(), "No such file") {
		problems = append(problems, err.Error())
	}
	for _, c := range parseTable(listing) {
		for _, r := range c.rules {
			if r.handle == "" || (!slices.Contains(r.fields, veth) && !slices.Contains(r.fields, saddr)) {
				continue
			}
			if out, err := h.run("nft", "delete", "rule", "inet", t, c.name, "handle", r.handle); err != nil {
				problems = append(problems, strings.TrimSpace(out))
			}
		}
	}
	for _, chain := range []string{"vm-" + strconv.Itoa(index), inChain(index)} {
		if out, err := h.run("nft", "delete", "chain", "inet", t, chain); err != nil && !strings.Contains(out, "No such") {
			problems = append(problems, strings.TrimSpace(out))
		}
	}
	if out, err := h.run("ip", "netns", "del", id); err != nil && !strings.Contains(out, "No such file") {
		problems = append(problems, strings.TrimSpace(out))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func netnsSaddr(index int) string {
	_, vns, _, _ := netAddrs(index)
	return vns
}

// RemoveCgroup removes the VM's cgroup subtree — the jailer's cgroup and
// anything under it, then the leaf (cgroupPaths) — deepest first, and
// only once no cgroup of it has a process: one that has is a visible
// failure, and nothing is removed. It acts only inside the delegated
// subtree (C1): a cgroup that is not a job cgroup of it, or one whose
// service cgroup systemd does not mark delegated, is refused, and nothing
// is removed — the recovery pass and the operator's retry included.
func (h *realHost) RemoveCgroup(id string) error {
	return h.bounded("remove cgroup of "+id, func() error {
		leaf, _, err := h.ownedCgroup(id)
		if err != nil {
			return err
		}
		found, err := h.cgroups.Exists(leaf)
		if err != nil || !found {
			return err
		}
		if ok, err := h.cgroups.Delegated(h.cfg.CgroupService); err != nil || !ok {
			return fmt.Errorf("cgroup %s is not removed: its service cgroup %s is not a delegated subtree (delegated %v, %v)", leaf, h.cfg.CgroupService, ok, err)
		}
		var order []string // deepest first
		var walk func(path string) error
		walk = func(path string) error {
			kids, err := h.cgroups.Children(path)
			if err != nil {
				return fmt.Errorf("cgroup %s: %w", path, err)
			}
			for _, k := range kids {
				if err := walk(filepath.Join(path, k)); err != nil {
					return err
				}
			}
			order = append(order, path)
			return nil
		}
		if err := walk(leaf); err != nil {
			return err
		}
		for _, path := range order {
			procs, err := h.cgroups.Read(path, "cgroup.procs")
			if err != nil {
				return fmt.Errorf("cgroup %s: %w", path, err)
			}
			if pids := strings.Fields(procs); len(pids) > 0 {
				return fmt.Errorf("cgroup %s still has processes: %s", path, pids)
			}
		}
		for _, path := range order {
			if err := h.cgroups.Rmdir(path); err != nil {
				return fmt.Errorf("cgroup %s: %w", path, err)
			}
		}
		return nil
	})
}

func (h *realHost) RemoveDisk(id string) error {
	return h.bounded("remove disk of "+id, func() error {
		err := os.Remove(filepath.Join(h.jailRoot(id), "disk.ext4"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

func (h *realHost) RemoveJail(id string) error {
	return h.bounded("remove jail of "+id, func() error {
		return os.RemoveAll(filepath.Join(h.cfg.JailBase, "firecracker", id))
	})
}
