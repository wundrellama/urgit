// Package sandbox is the daemon's isolation boundary (CI-SANDBOX-1-B).
//
// The interface is written against a disposable VM's constraints and the
// one backend this release ships, docker-rootless, fits them: Prepare
// takes an image and resource limits; the checkout enters by Copy, never
// a bind mount; Run hands back an output stream and an exit code and
// nothing else; Destroy is fallible and a failed teardown quarantines the
// slot; the daemon reconciles orphans by asking the ship, never by
// trusting local state.
package sandbox

import (
	"context"
	"errors"
	"io"
)

// Spec is what a sandbox is built from. A backend honours the fields it
// can and ignores the rest; it never errors on an unsupported one.
type Spec struct {
	Image     string
	CPUs      int
	MemoryMiB int
	DiskMiB   int
	Network   string // the per-attempt isolated network's name
}

// Handle names one prepared sandbox. Opaque to the daemon beyond ID.
type Handle struct {
	ID        string
	Network   string
	Volume    string
	Container string
}

type Sandbox interface {
	// Prepare boots or creates the sandbox.
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
	// Name is the disclosure string for the banner and README.
	Name() string
}

// ErrMicrovmUnavailable is the microvm constructor's answer in P1.
var ErrMicrovmUnavailable = errors.New("microvm sandbox is not available in this release")

// New selects a backend by the config's sandbox name.
func New(kind, dockerHost string) (Sandbox, error) {
	switch kind {
	case "docker-rootless":
		return NewDocker(dockerHost)
	case "microvm":
		return NewMicrovm()
	default:
		return nil, errors.New("unknown sandbox: " + kind)
	}
}
