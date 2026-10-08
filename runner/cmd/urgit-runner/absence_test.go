package main

// The runner's release of a Docker retention through the real backend
// (runner/launcher/INTEGRATION.md §11.4; independent review 01, R4): the
// retry's removals and the release's proof are the real Docker backend's
// Destroy and Leftovers, over a docker command seam that runs nothing — a
// model of the retention's objects by name.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/state"
)

// dockerModel answers the docker commands of one handle's objects: a look
// at an object that exists succeeds, a removal removes it (a forced volume
// removal of a volume that does not exist succeeds, as Docker's does), and
// a look at or removal of an object that does not exist fails with its
// reply in absent — Docker's own not-found reply unless a case swaps it.
type dockerModel struct {
	mu       sync.Mutex
	objects  map[string]bool   // "<kind> <name>" that exist
	absent   map[string]string // "<kind> <name>" → the reply about it once absent
	commands map[string]struct {
		object string
		remove bool
	}
}

func newDockerModel(h sandbox.Handle, exist ...string) *dockerModel {
	c, v, tv, n := "container "+h.Container, "volume "+h.Volume, "volume "+h.Volume+"-tools", "network "+h.Network
	m := &dockerModel{objects: map[string]bool{}, absent: map[string]string{
		c:  "Error: No such container: " + h.Container,
		v:  "Error response from daemon: get " + h.Volume + ": no such volume",
		tv: "Error: No such volume: " + h.Volume + "-tools",
		n:  "Error response from daemon: network " + h.Network + " not found",
	}, commands: map[string]struct {
		object string
		remove bool
	}{
		"inspect --type container -f {{.Id}} " + h.Container:                     {c, false},
		"volume inspect -f {{.Name}} " + h.Volume:                                {v, false},
		"volume inspect -f {{.Name}} " + h.Volume + "-tools":                     {tv, false},
		"network inspect -f {{.Id}} " + h.Network:                                {n, false},
		"network inspect -f {{range .Containers}}{{.Name}} {{end}} " + h.Network: {n, false},
		"rm -f " + h.Container:                                                   {c, true},
		"volume rm -f " + h.Volume:                                               {v, true},
		"volume rm -f " + h.Volume + "-tools":                                    {tv, true},
		"network rm " + h.Network:                                                {n, true},
	}}
	for _, o := range exist {
		m.objects[o] = true
	}
	return m
}

func (m *dockerModel) run(_ context.Context, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.commands[cmd]
	switch {
	case !ok:
		return "", errors.New("docker " + args[0] + ": the model knows no command " + cmd)
	case m.objects[c.object]:
		if c.remove {
			delete(m.objects, c.object)
		}
		return "", nil
	case c.remove && args[0] == "volume":
		return "", nil
	}
	return "", errors.New("docker " + args[0] + ": " + m.absent[c.object])
}

func (m *dockerModel) exists() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for o := range m.objects {
		out = append(out, o)
	}
	return out
}

// A Docker retention's retry removes its objects through the real
// backend, and its release is allowed only once every look at them is
// answered by Docker's exact reply that that very object does not exist:
// a look answered by another object's reply, another kind's, a missing
// driver or no daemon at all leaves the retry unresolved and the release
// refused, the retention and its slot kept.
func TestRecoverReleasesADockerRetentionOnlyOnTheExactObjectsReply(t *testing.T) {
	for _, c := range []struct {
		name   string
		object string // the object whose look is answered otherwise
		reply  string
	}{
		{"every look answered by its object's own reply (control)", "", ""},
		{"another volume's reply", "volume ci-0v8.att-work", "Error: No such volume: ci-unrelated"},
		{"a missing volume driver", "volume ci-0v8.att-work-tools", "Error response from daemon: get ci-0v8.att-work-tools: no such volume driver"},
		{"another network's reply", "network ci-0v8.att", "Error response from daemon: network ci-0v9.att not found"},
		{"another kind's reply", "container ci-0v8.att", "Error: No such volume: ci-0v8.att"},
		{"no daemon", "network ci-0v8.att", "Cannot connect to the Docker daemon at unix:///run/user/1000/test/docker.sock. Is the docker daemon running?"},
	} {
		t.Run(c.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			q := dockerRetention()
			saveState(t, statePath, q)
			m := newDockerModel(handleOf(q), "container ci-0v8.att", "network ci-0v8.att")
			if c.object != "" {
				m.absent[c.object] = c.reply
			}
			docker := sandbox.NewDockerCommand("unix:///run/user/1000/test/docker.sock", daemonID, m.run)
			rc := recovery{cfg: &config.Config{Sandbox: "docker-rootless", StateFile: statePath}, docker: docker, now: time.Now}
			var out strings.Builder
			if code := rc.act(q.Selection(), "release", &out); code != 1 || len(handles(t, statePath)) != 1 {
				t.Fatalf("A DOCKER RETENTION WITH OBJECTS LEFT WAS RELEASED: exit %d\n%s", code, out.String())
			}
			out.Reset()
			retry := rc.act(q.Selection(), "retry", &out)
			if left := m.exists(); len(left) != 0 {
				t.Fatalf("the retry did not remove the retention's objects: %v\n%s", left, out.String())
			}
			st := loaded(t, statePath)
			if len(st.Quarantined) != 1 || len(st.Quarantined[0].Attempts) != 1 {
				t.Fatalf("the retry was not recorded, or released the retention: %+v", st.Quarantined)
			}
			result := st.Quarantined[0].Attempts[0].Result
			out.Reset()
			release := rc.act(st.Quarantined[0].Selection(), "release", &out)
			if c.object == "" {
				if retry != 0 || result != "resolved" || release != 0 || len(handles(t, statePath)) != 0 {
					t.Fatalf("Docker's own replies of absence: retry exit %d (%s), release exit %d\n%s", retry, result, release, out.String())
				}
				return
			}
			if retry != 1 || result != "unresolved" || release != 1 || len(handles(t, statePath)) != 1 || !strings.Contains(out.String(), "could not all be looked at") {
				t.Fatalf("A FOREIGN OR UNCERTAIN REPLY PROVED THE RETENTION RELEASED: retry exit %d (%s), release exit %d, kept %v\n%s", retry, result, release, handles(t, statePath), out.String())
			}
		})
	}
}

// A Docker entry written before retentions named their objects (the handle
// only) is looked at, retried and released by the names every Docker
// sandbox of its handle has — the network and container the handle, the
// volume its -work — through the real backend: its objects are found, its
// retry removes them by those names, and its release follows their exact
// absence.
func TestRecoverLooksAtAnOlderDockerEntryByItsHandlesNames(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	older := state.Quarantine{Handle: "ci-0v8.att", Reason: "teardown failed", At: time.Now().Unix()}
	saveState(t, statePath, older)
	m := newDockerModel(sandbox.Handle{Network: "ci-0v8.att", Volume: "ci-0v8.att-work", Container: "ci-0v8.att"}, "container ci-0v8.att", "volume ci-0v8.att-work")
	docker := sandbox.NewDockerCommand("unix:///run/user/1000/test/docker.sock", daemonID, m.run)
	rc := recovery{cfg: &config.Config{Sandbox: "docker-rootless", StateFile: statePath}, docker: docker, now: time.Now}
	v := rc.inspect(older, daemonID)
	if v.Proven || strings.Join(v.Held, ", ") != "container ci-0v8.att, volume ci-0v8.att-work" {
		t.Fatalf("AN OLDER DOCKER ENTRY'S OBJECTS WERE NOT LOOKED AT BY ITS HANDLE'S NAMES: %+v", v)
	}
	var out strings.Builder
	if code := rc.act(older.Selection(), "retry", &out); code != 0 || len(m.exists()) != 0 {
		t.Fatalf("the retry by the handle's names: exit %d, left %v\n%s", code, m.exists(), out.String())
	}
	st := loaded(t, statePath)
	out.Reset()
	if code := rc.act(st.Quarantined[0].Selection(), "release", &out); code != 0 || len(handles(t, statePath)) != 0 {
		t.Fatalf("the release by the handle's names: exit %d\n%s", code, out.String())
	}
}
