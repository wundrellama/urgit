package daemon

// The daemon's release of a Docker sandbox's slot (runner/launcher/
// INTEGRATION.md §11.4; independent review 01, R4): a teardown frees the
// slot only on the real backend's Destroy, whose removals count as done
// only on Docker's exact reply that that very object does not exist. The
// docker command seam here runs nothing: it answers.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"urgit/runner/internal/sandbox"
)

// dockerTeardownBox is the fake box whose teardown is the real Docker
// backend's Destroy over a nonexecuting docker command seam.
type dockerTeardownBox struct {
	*fakeBox
	docker *sandbox.Docker
}

func (dockerTeardownBox) Kind() string { return "docker-rootless" }
func (b dockerTeardownBox) Destroy(ctx context.Context, h sandbox.Handle) error {
	b.record("destroy " + h.ID)
	return b.docker.Destroy(ctx, h)
}

// A removal answered by Docker's exact reply that its very object does not
// exist is done — the slot is kept for the next attempt; another object's
// reply, another kind's, a missing driver or a plugin's "not found" proves
// nothing gone, and the teardown failure withholds the slot.
func TestDockerTeardownFreesTheSlotOnlyOnTheExactObjectsReply(t *testing.T) {
	for _, c := range []struct {
		name    string
		command string // the exact docker command that fails
		reply   string // and its reply
		freed   bool
	}{
		{"the container is already gone: its own reply", "rm -f ci-0v1.att", "Error response from daemon: No such container: ci-0v1.att", true},
		{"the network is already gone: its own reply", "network rm ci-0v1.att", "Error response from daemon: network ci-0v1.att not found", true},
		{"another container's reply", "rm -f ci-0v1.att", "Error response from daemon: No such container: ci-0v1.att2", false},
		{"a plugin's not found", "network rm ci-0v1.att", `Error response from daemon: plugin "weave" not found`, false},
		{"another network's reply", "network rm ci-0v1.att", "Error response from daemon: network ci-0v2.att not found", false},
		{"a missing volume driver", "volume rm -f ci-0v1.att-work", "Error response from daemon: get ci-0v1.att-work: no such volume driver", false},
		{"another kind's reply", "volume rm -f ci-0v1.att-work-tools", "Error: No such network: ci-0v1.att-work-tools", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls []string
			run := func(_ context.Context, args ...string) (string, error) {
				cmd := strings.Join(args, " ")
				calls = append(calls, cmd)
				if cmd == c.command {
					return "", errors.New("docker " + args[0] + ": " + c.reply)
				}
				return "", nil
			}
			fb := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
			d := newTestDaemon(t, fb, &fakeShip{}, 2)
			d.box = dockerTeardownBox{fb, sandbox.NewDockerCommand("unix:///run/user/1000/test/docker.sock", d.daemonID, run)}
			keep := d.handle(context.Background(), jobAssignment)
			ran := false
			for _, cmd := range calls {
				ran = ran || cmd == c.command
			}
			if !ran {
				t.Fatalf("fixture: the teardown never ran %q: %q", c.command, calls)
			}
			if c.freed {
				if !keep || d.remainingCapacity() != 2 || len(d.retained) != 0 {
					t.Fatalf("Docker's own reply of the object's absence withheld the slot: keep %v capacity %d retained %+v", keep, d.remainingCapacity(), d.retained)
				}
				return
			}
			if keep || d.remainingCapacity() != 1 || len(d.retained) != 1 || d.retained[0].Container != "ci-0v1.att" {
				t.Fatalf("A FOREIGN OR UNCERTAIN REPLY FREED THE SLOT: keep %v capacity %d retained %+v", keep, d.remainingCapacity(), d.retained)
			}
		})
	}
}
