package sandbox

// Docker absence (runner/launcher/INTEGRATION.md §11.4; independent review
// 01, R4): a look proves an object absent only by Docker's own not-found
// reply about that very object — its kind and its exact name, the whole
// reply. No docker command runs here: the recorder answers.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Each kind's not-found replies, and only they, prove its absence; another
// object's reply, a missing driver, another kind's reply, extra words or
// lines, or a transport or API error prove nothing.
func TestDockerAbsenceIsTheExactObjectsReply(t *testing.T) {
	for _, c := range []struct {
		kind, name string
		absent     []string
		not        []string
	}{
		{"container", "ci-0v1.att",
			[]string{"Error: No such container: ci-0v1.att", "Error response from daemon: No such container: ci-0v1.att"},
			[]string{"Error: No such container: ci-0v1.att2", "Error: No such container: ci-0v2.att", "Error: No such object: ci-0v1.att",
				"Error: No such container: ci-0v1.att extra", "Error: No such volume: ci-0v1.att", "error: no such container: ci-0v1.att",
				"Warning: something\nError: No such container: ci-0v1.att", "Error: No such container: ci-0v1.att\nError: No such container: ci-0v2.att",
				"Cannot connect to the Docker daemon at unix:///run/user/1000/docker.sock. Is the docker daemon running?", ""}},
		{"volume", "ci-0v1.att-work",
			[]string{"Error: No such volume: ci-0v1.att-work", "Error response from daemon: No such volume: ci-0v1.att-work",
				"Error response from daemon: get ci-0v1.att-work: no such volume"},
			[]string{"Error: No such volume: ci-unrelated", "Error: No such volume driver: offline-plugin", "Error: No such volume: ci-0v1.att-work-tools",
				"Error response from daemon: get ci-0v1.att-work-tools: no such volume", "Error response from daemon: get ci-0v1.att-work: no such volume driver",
				"Error response from daemon: page not found", "Error: No such network: ci-0v1.att-work"}},
		{"network", "ci-0v1.att",
			[]string{"Error: No such network: ci-0v1.att", "Error response from daemon: No such network: ci-0v1.att",
				"Error response from daemon: network ci-0v1.att not found"},
			[]string{"Error response from daemon: network ci-0v2.att not found", "Error response from daemon: network ci-0v1.att not found (plugin)",
				"Error: No such network: ci-0v1.at", "Error response from daemon: page not found", "Error: No such volume: ci-0v1.att"}},
		// a handle that names no volume: no reply is about it
		{"volume", "", nil, []string{"Error: No such volume: ", "Error response from daemon: No such volume: ", "Error response from daemon: get : no such volume"}},
	} {
		for _, msg := range c.absent {
			if !absentAnswer(msg, c.kind, c.name) {
				t.Errorf("Docker's own reply of %s %s's absence was not taken: %q", c.kind, c.name, msg)
			}
		}
		for _, msg := range c.not {
			if absentAnswer(msg, c.kind, c.name) {
				t.Errorf("A FOREIGN OR UNCERTAIN REPLY CERTIFIED %s %s ABSENT: %q", strings.ToUpper(c.kind), c.name, msg)
			}
		}
	}
}

// answering is a docker command seam that runs nothing: each command whose
// exact words are in replies fails with that reply (as the runner's own
// wrapper reports it); any other is answered, as for an object that exists.
func answering(replies map[string]string) func(ctx context.Context, args ...string) (string, error) {
	return func(ctx context.Context, args ...string) (string, error) {
		if msg, ok := replies[strings.Join(args, " ")]; ok {
			return "", errors.New("docker " + args[0] + ": " + msg)
		}
		return "", nil
	}
}

// Leftovers — the proof a Docker retention's release waits for — is an
// error while any look is answered by anything but the exact object's
// not-found reply, and names none once every look is.
func TestDockerLeftoversNeedTheExactObjectsReply(t *testing.T) {
	h := Handle{ID: "ci-0v1.att", Network: "ci-0v1.att", Volume: "ci-0v1.att-work", Container: "ci-0v1.att"}
	container, volume, tools, network := "inspect --type container -f {{.Id}} ci-0v1.att", "volume inspect -f {{.Name}} ci-0v1.att-work",
		"volume inspect -f {{.Name}} ci-0v1.att-work-tools", "network inspect -f {{.Id}} ci-0v1.att"
	exact := map[string]string{
		container: "Error: No such container: ci-0v1.att",
		volume:    "Error response from daemon: get ci-0v1.att-work: no such volume",
		tools:     "Error: No such volume: ci-0v1.att-work-tools",
		network:   "Error response from daemon: network ci-0v1.att not found",
	}
	look := func(replies map[string]string) ([]string, error) {
		d := &Docker{Host: "unix:///run/user/1000/test/docker.sock", Owner: "0vd", exec: answering(replies)}
		return d.Leftovers(context.Background(), h)
	}
	if left, err := look(exact); err != nil || len(left) != 0 {
		t.Fatalf("every object's own not-found reply: %q %v", left, err)
	}
	for name, change := range map[string][2]string{
		"another volume's reply":                       {volume, "Error: No such volume: ci-unrelated"},
		"a missing driver":                             {volume, "Error: No such volume driver: offline-plugin"},
		"the tools volume's reply for the work volume": {volume, "Error: No such volume: ci-0v1.att-work-tools"},
		"another network's":                            {network, "Error response from daemon: network ci-0v2.att not found"},
		"another kind's":                               {container, "Error: No such volume: ci-0v1.att"},
	} {
		replies := map[string]string{}
		for k, v := range exact {
			replies[k] = v
		}
		replies[change[0]] = change[1]
		if left, err := look(replies); err == nil {
			t.Errorf("A FOREIGN OR UNCERTAIN REPLY CERTIFIED THE OBJECTS ABSENT (%s): %q", name, left)
		}
	}
	// an object that answers is left, by its exact name
	replies := map[string]string{container: exact[container], tools: exact[tools], network: exact[network]}
	if left, err := look(replies); err != nil || strings.Join(left, ", ") != "volume ci-0v1.att-work" {
		t.Fatalf("an object that exists: %q %v", left, err)
	}
}

// Destroy — whose nil frees the attempt's slot — counts a removal that
// failed as done only on Docker's exact reply that that very object does
// not exist, the daemon's or the CLI's: another object's reply, another
// kind's, a missing driver or a plugin's "not found" is the error that
// keeps the slot.
func TestDockerDestroyNeedsTheExactObjectsReply(t *testing.T) {
	h := Handle{ID: "ci-0v1.att", Network: "ci-0v1.att", Volume: "ci-0v1.att-work", Container: "ci-0v1.att"}
	rm, volume, tools, network := "rm -f ci-0v1.att", "volume rm -f ci-0v1.att-work", "volume rm -f ci-0v1.att-work-tools", "network rm ci-0v1.att"
	exact := map[string]string{ // the daemon's replies
		rm:      "Error response from daemon: No such container: ci-0v1.att",
		volume:  "Error response from daemon: get ci-0v1.att-work: no such volume",
		tools:   "Error response from daemon: get ci-0v1.att-work-tools: no such volume",
		network: "Error response from daemon: network ci-0v1.att not found",
	}
	destroy := func(replies map[string]string) error {
		d := &Docker{Host: "unix:///run/user/1000/test/docker.sock", Owner: "0vd", exec: answering(replies)}
		return d.Destroy(context.Background(), h)
	}
	if err := destroy(exact); err != nil {
		t.Fatalf("every object's own not-found reply: %v", err)
	}
	for name, change := range map[string][2]string{
		"another container's reply":                    {rm, "Error: No such container: ci-0v1.att2"},
		"another kind's reply":                         {rm, "Error: No such network: ci-0v1.att"},
		"another volume's reply":                       {volume, "Error: No such volume: ci-unrelated"},
		"the tools volume's reply for the work volume": {volume, "Error: No such volume: ci-0v1.att-work-tools"},
		"a missing volume driver":                      {tools, "Error response from daemon: get ci-0v1.att-work-tools: no such volume driver"},
		"another network's reply":                      {network, "Error response from daemon: network ci-0v2.att not found"},
		"a plugin's not found":                         {network, `Error response from daemon: plugin "weave" not found`},
	} {
		replies := map[string]string{}
		for k, v := range exact {
			replies[k] = v
		}
		replies[change[0]] = change[1]
		if err := destroy(replies); err == nil {
			t.Errorf("A FOREIGN OR UNCERTAIN REPLY FREED THE SLOT (%s)", name)
		}
	}
	// the CLI's own not-found replies are exact too
	cli := map[string]string{
		rm:      "Error: No such container: ci-0v1.att",
		volume:  "Error: No such volume: ci-0v1.att-work",
		tools:   "Error: No such volume: ci-0v1.att-work-tools",
		network: "Error: No such network: ci-0v1.att",
	}
	if err := destroy(cli); err != nil {
		t.Fatalf("every object's own not-found reply, the CLI's: %v", err)
	}
}

// A container whose removal another party has in progress is gone only
// once its inspection answers Docker's exact reply that it does not exist;
// any other answer, until the wait is cut, is the error that keeps the
// slot.
func TestDockerRemovalInProgressNeedsTheExactReply(t *testing.T) {
	inspect := "inspect --type container -f {{.Id}} ci-0v1.att"
	for _, c := range []struct {
		name, answer string
		gone         bool
	}{
		{"its own reply", "Error: No such container: ci-0v1.att", true},
		{"another container's", "Error: No such container: ci-0v1.att2", false},
		{"an untyped object's", "Error: No such object: ci-0v1.att", false},
		{"no daemon", "Cannot connect to the Docker daemon at unix:///run/user/1000/test/docker.sock. Is the docker daemon running?", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := &Docker{Host: "unix:///run/user/1000/test/docker.sock", exec: answering(map[string]string{
				"rm -f ci-0v1.att": "Error response from daemon: removal of container ci-0v1.att is already in progress",
				inspect:            c.answer,
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
			defer cancel()
			if err := d.removeContainer(ctx, "ci-0v1.att"); (err == nil) != c.gone {
				t.Fatalf("A REMOVAL IN PROGRESS WAS TAKEN FOR DONE, OR ITS OWN REPLY WAS NOT: %v", err)
			}
		})
	}
}
