// Package sandbox is the daemon's isolation boundary (CI-SANDBOX-1-B; P4
// D1). The interface is written against a disposable VM's constraints
// and both backends fit them: Prepare takes an image and resource
// limits; the checkout enters by Copy, never a bind mount; Run hands
// back an output stream and an exit code and nothing else; Destroy is
// fallible and a failed teardown quarantines the slot; the daemon
// reconciles orphans by asking the ship, never by trusting local state.
//
// P4 adds the microvm backend (Firecracker + jailer through the
// administrator's launcher) as the default and turns the guest-side
// execution settings act needs — the Docker socket, the job network,
// the work, cache and action paths — into ExecEnv, a typed seam each
// backend answers, so daemon.go builds act's flags from it instead of
// substituting sockets by hand.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
)

// Spec is what a sandbox is built from. The P4 fields (attempt, deadline,
// network profile and scope) are the reservation's identity and bounds;
// a backend honours the fields it can and ignores the rest, and never
// widens a network scope.
type Spec struct {
	Image     string
	CPUs      int
	MemoryMiB int
	DiskMiB   int
	Network   string // the per-attempt isolated network's name (docker) / the attempt's network profile (microvm)
	// P4
	Attempt      string
	Deadline     time.Time
	Profile      string   // "locked" or an authorized profile name
	Destinations []string // the effective scope for the profile (already intersected by the daemon)
	// Label is the job, for the operator's selection only (the ship's
	// repository, workflow and job): never identity
	Label string
	// Request is the reserve's request token (runner/launcher/
	// INTEGRATION.md §11.10), which the daemon records durably before it
	// prepares: a lost reserve answer is settled by it, never by a list
	Request string
}

// Handle names one prepared sandbox. ID is the attempt's sandbox name
// (`ci-<attempt>`) for every backend; the rest is backend-specific and
// opaque to the daemon, but exact: it is what a retention records
// (runner/launcher/INTEGRATION.md §1).
type Handle struct {
	ID      string
	Attempt string
	// Address is the sandbox's own address on the job network (the
	// container's on its bridge; the guest's bridge gateway in a VM)
	Address string
	// docker-rootless
	Network   string
	Volume    string
	Container string
	// microvm: the launcher's record and its incarnation — its token
	// (INTEGRATION.md §11.1), with its cid and creation time; every
	// mutation names it, so none reaches a later incarnation of the same
	// attempt
	VM          string
	Incarnation string
	CID         uint32
	Created     int64
	// Label is the job, for the operator's selection only
	Label string
	// Request is the reserve request that admitted it — and, with no VM,
	// the request whose outcome is not settled yet (INTEGRATION.md §11.10)
	Request string
}

// RetainedError is a Prepare that failed and could not release what it
// had reserved or created (INTEGRATION.md §3): Handle names exactly what
// stays owned and charged — the launcher's record and incarnation, or the
// compatibility mode's objects; a microvm handle with no VM names the
// reserve request whose outcome is not settled (INTEGRATION.md §11.10). The
// daemon withholds a slot for it until it is released — a microvm request by
// its settlement — (INTEGRATION.md §§8.4, 11.10).
type RetainedError struct {
	Handle Handle
	Err    error
}

func (e *RetainedError) Error() string {
	return fmt.Sprintf("%v (retained, still charged: %s)", e.Err, Describe(e.Handle))
}

func (e *RetainedError) Unwrap() error { return e.Err }

// Describe names a handle's exact identity for a log line or a reason.
func Describe(h Handle) string {
	switch {
	case h.VM != "" && h.Incarnation != "":
		return fmt.Sprintf("%s: vm %s incarnation %s (cid %d created %d)", h.ID, h.VM, h.Incarnation, h.CID, h.Created)
	case h.VM != "":
		return fmt.Sprintf("%s: vm %s cid %d created %d (no incarnation token)", h.ID, h.VM, h.CID, h.Created)
	case h.Network != "" || h.Container != "":
		return fmt.Sprintf("%s: network %s, volume %s, container %s", h.ID, h.Network, h.Volume, h.Container)
	case h.Request != "":
		return fmt.Sprintf("%s: reserve request %s, its outcome not settled (attempt %s)", h.ID, h.Request, h.Attempt)
	}
	return fmt.Sprintf("%s: launcher identity unknown (attempt %s)", h.ID, h.Attempt)
}

// ExecEnv is what the daemon needs to invoke act inside a sandbox: where
// the checkout and the act binary live, which Docker socket act must
// bind into job containers, which network the job containers join, and
// the cache/artifact/action/tool paths (all inside the sandbox).
type ExecEnv struct {
	WorkRoot        string // the bundle root
	SourceDir       string
	ActBinary       string
	CopyAct         bool   // true when the backend needs the host's act copied in
	DockerSocket    string // the socket act binds into job containers
	JobNetwork      string // act --network
	CachePath       string
	ArtifactPath    string
	ActionCachePath string
	ToolCache       string
	// ToolCacheVolume names the Docker volume holding ToolCache in
	// compatibility mode ("" in the VM, where ToolCache is a guest path
	// the guest's Docker binds directly); the daemon mounts each seeded
	// tool from it under act's fixed RUNNER_TOOL_CACHE
	ToolCacheVolume string
	// WorkVolume names the Docker volume holding WorkRoot in
	// compatibility mode ("" in the VM); the daemon mounts the bundle's
	// downloads from it into every job container
	WorkVolume string
	// ServerAddr is the address act's cache and artifact servers bind
	// to: the sandbox's own address on the job network, which the job
	// containers reach; act would otherwise guess it from a route out,
	// and a locked sandbox has none
	ServerAddr string
	Env        []string
	// Locked says the sandbox has no network at all; act's action fetches
	// must come from the action cache (offline mode)
	Locked bool
}

type Sandbox interface {
	// Prepare boots or creates the sandbox. A failure that leaves
	// something owned and charged is a *RetainedError naming it.
	Prepare(ctx context.Context, spec Spec) (Handle, error)
	// Copy puts a host file or directory into the sandbox at guestPath.
	Copy(ctx context.Context, h Handle, hostPath, guestPath string) error
	// Run executes argv inside the sandbox with the given environment and
	// working directory. The reader is the process's combined output;
	// the channel delivers its exit code once, after the reader drains.
	Run(ctx context.Context, h Handle, workDir string, argv []string, env []string) (io.ReadCloser, <-chan int, error)
	// Signal delivers a signal to a process inside the sandbox by name
	// match (the harness kills act mid-job with it).
	Signal(ctx context.Context, h Handle, processName string, signal string) error
	// Destroy tears the sandbox down and returns the first error.
	Destroy(ctx context.Context, h Handle) error
	// Orphans lists the ids of sandboxes this backend still holds.
	Orphans(ctx context.Context) ([]string, error)
	// HandleFor rebuilds a handle from an orphan id (reconciliation).
	HandleFor(id string) Handle
	// ExecEnv is the backend's guest execution settings for a handle.
	ExecEnv(h Handle) ExecEnv
	// Name is the disclosure string for the banner and README.
	Name() string
	// Kind is the config name: microvm or docker-rootless.
	Kind() string
	// SetOwner names the daemon whose sandboxes these are (after enrollment).
	SetOwner(daemonID string) error
}

// ReleaseProver is a backend that proves a sandbox's release by the
// durable evidence of whoever released it — the microvm launcher's
// released query (runner/launcher/INTEGRATION.md §11.8): nil only when it
// proves exactly that sandbox's release. Its absence from Orphans is no
// such proof.
type ReleaseProver interface {
	ProveReleased(ctx context.Context, h Handle) error
}

// Settlement is a backend's conclusive answer for one reserve request
// (runner/launcher/INTEGRATION.md §11.10): admitted — Handle names the
// reservation it admitted, which follows its own disposition; released —
// it admitted one that has since been released, on the release's evidence;
// closed — it holds nothing and never will. Detail says how, for the log.
type Settlement struct {
	Outcome string
	Handle  Handle
	Detail  string
}

// The outcomes of a settlement (the launcher's: INTEGRATION.md §11.10).
const (
	SettledAdmitted = launcher.SettledAdmitted
	SettledReleased = launcher.SettledReleased
	SettledClosed   = launcher.SettledClosed
)

// NewRequest is a fresh reserve request token, for the daemon to record
// durably before it prepares (Spec.Request).
func NewRequest() (string, error) { return launcher.NewRequest() }

// Settler is a backend whose reserve requests are settled by the launcher
// that admits them (the microvm backend): the only answer that returns the
// slot of a request whose outcome was not known. An error settles nothing.
type Settler interface {
	Settle(ctx context.Context, h Handle) (Settlement, error)
}

// Inventory is what the launcher answers for a legacy release
// (runner/launcher/INTEGRATION.md §11.12): the socket asked, its version
// and wire protocol from hello, and this daemon's records from a list it
// answers only when its inventory is authoritative.
type Inventory struct {
	Socket   string
	Launcher string
	Protocol int
	Records  []launcher.Record
}

// Inventorier is a backend that can say what the launcher holds for this
// daemon (the microvm backend), and nothing more: an error is no inventory.
type Inventorier interface {
	Inventory(ctx context.Context) (Inventory, error)
}

// ErrMicrovmUnavailable is the microvm constructor's answer when any
// prerequisite is missing: the launcher socket, the image, a digest.
var ErrMicrovmUnavailable = errors.New("microvm sandbox is not available")

// New selects a backend by the config's sandbox name. There is no
// fallback between them (rider 04).
func New(cfg *config.Config) (Sandbox, error) {
	switch cfg.Sandbox {
	case "docker-rootless":
		return NewDocker(cfg.DockerHost)
	case "microvm":
		return NewMicrovm(cfg)
	default:
		return nil, errors.New("unknown sandbox: " + cfg.Sandbox)
	}
}
