package sandbox

// Stage 01 (runner/launcher/INTEGRATION.md §3): what a failed Prepare
// leaves charged is rolled back by its exact identity or named in a
// RetainedError — for the microvm backend against a scripted launcher on a
// private unix socket (the wire, answering exactly as the script says),
// for the compatibility backend through a docker command recorder that
// runs nothing.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/launcher/launchertest"
)

// scripted is a launcher that answers hello itself and every other
// request as answer says; hangup closes the connection unanswered.
type scripted struct {
	mu     sync.Mutex
	seen   []launcher.Request
	answer func(launcher.Request) (reply launcher.Reply, hangup bool)
}

func serveScripted(t *testing.T, answer func(launcher.Request) (launcher.Reply, bool)) (*scripted, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "s.sock")
	if err != nil {
		t.Fatal(err)
	}
	s := &scripted{answer: answer}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		l.Close()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req launcher.Request
					if json.Unmarshal(line, &req) != nil {
						return
					}
					reply, hangup := launcher.Reply{OK: true, Protocol: launcher.WireProtocol}, false
					if req.Op != "hello" {
						s.mu.Lock()
						s.seen = append(s.seen, req)
						s.mu.Unlock()
						reply, hangup = s.answer(req)
					}
					if hangup {
						return
					}
					data, _ := json.Marshal(reply)
					if _, err := c.Write(append(data, '\n')); err != nil {
						return
					}
				}
			}()
		}
	}()
	return s, "s.sock"
}

func (s *scripted) requests(op string) []launcher.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []launcher.Request
	for _, r := range s.seen {
		if r.Op == op {
			out = append(out, r)
		}
	}
	return out
}

func scriptedMicrovm(t *testing.T, answer func(launcher.Request) (launcher.Reply, bool)) (*Microvm, *scripted) {
	t.Helper()
	dir, _ := launchertest.Image(t, "img")
	s, socket := serveScripted(t, answer)
	box, err := NewMicrovm(&config.Config{Sandbox: "microvm", LauncherSocket: socket, ImagePath: dir, ActImage: "img"})
	if err != nil {
		t.Fatal(err)
	}
	m := box.(*Microvm)
	if err := m.SetOwner("0vd"); err != nil {
		t.Fatal(err)
	}
	return m, s
}

var vmSpec = Spec{Attempt: "0v1.att", CPUs: 1, MemoryMiB: 128, DiskMiB: 1, Deadline: time.Now().Add(time.Hour), Profile: "locked"}

// scriptedToken is the incarnation token the scripted launcher answers.
var scriptedToken = strings.Repeat("7", 32)

// A reservation the launcher could not acknowledge but keeps charged is
// named in its answer; Prepare destroys exactly that incarnation — by its
// token (runner/launcher/INTEGRATION.md §11.1) — and when that destroy
// fails, the error names it (RetainedError), never an empty handle.
func TestRetainedReserveIsRolledBackByItsIncarnation(t *testing.T) {
	for _, released := range []bool{true, false} {
		m, s := scriptedMicrovm(t, func(req launcher.Request) (launcher.Reply, bool) {
			switch req.Op {
			case "reserve":
				return launcher.Reply{Error: "reserve t-x: not acknowledged; it stays charged until its owner destroys it or its deadline passes", Retained: true, ID: "t-x", Incarnation: scriptedToken, CID: 7, Created: 99}, false
			case "destroy":
				if released {
					return launcher.Reply{OK: true}, false
				}
				return launcher.Reply{Error: "t-x is quarantined (cleanup failed); only the operator's clear releases it", Quarantined: true}, false
			}
			return launcher.Reply{Error: "unexpected " + req.Op}, false
		})
		_, err := m.Prepare(context.Background(), vmSpec)
		destroys := s.requests("destroy")
		if len(destroys) != 1 || destroys[0].ID != "t-x" || destroys[0].Incarnation != scriptedToken || destroys[0].CID != 7 || destroys[0].Created != 99 {
			t.Fatalf("THE CHARGED RESERVATION WAS NOT DESTROYED BY ITS INCARNATION: %+v (%v)", destroys, err)
		}
		var kept *RetainedError
		switch {
		case released && (err == nil || errors.As(err, &kept)):
			t.Fatalf("released: %v", err)
		case !released && (!errors.As(err, &kept) || kept.Handle.VM != "t-x" || kept.Handle.Incarnation != scriptedToken || kept.Handle.CID != 7 || kept.Handle.Created != 99 || kept.Handle.ID != "ci-0v1.att"):
			t.Fatalf("A RESERVATION LEFT CHARGED WAS NOT NAMED: %v", err)
		}
	}
}

// A reserve whose answer is lost has an unknown outcome: a fresh
// connection asks the launcher; the attempt's record is that reservation
// (destroyed by its incarnation); none, and nothing was reserved; and when
// the launcher cannot be asked either, the attempt is retained without a
// launcher identity.
//
// Policy-dependent (settled-admission ruling 01; INTEGRATION.md §11.10):
// before it, the fresh connection asked for the list, and a list without the
// attempt meant "nothing reserved" ("holds no reservation"). Now it settles
// exactly its request: admitted, that reservation; closed, "never
// admitted"; no answer, retained by its request. No list settles anything.
func TestLostReserveAnswerIsSettled(t *testing.T) {
	for _, c := range []struct {
		name      string
		listed    bool
		listFails bool
	}{{"the launcher reserved it", true, false}, {"the launcher reserved nothing", false, false}, {"the launcher cannot be asked", false, true}} {
		t.Run(c.name, func(t *testing.T) {
			m, s := scriptedMicrovm(t, func(req launcher.Request) (launcher.Reply, bool) {
				switch req.Op {
				case "reserve":
					return launcher.Reply{}, true
				case "settle":
					if c.listFails {
						return launcher.Reply{}, true
					}
					if c.listed {
						return launcher.Reply{OK: true, Settled: launcher.SettledAdmitted, Record: &launcher.Record{ID: "t-y", Incarnation: scriptedToken, Attempt: "0v1.att", CID: 8, Created: 5, State: launcher.StatePreparing, Request: req.Request}}, false
					}
					return launcher.Reply{OK: true, Settled: launcher.SettledClosed, SettledWhy: "never admitted; closed by this settlement"}, false
				case "destroy":
					return launcher.Reply{OK: true}, false
				}
				return launcher.Reply{Error: "unexpected " + req.Op}, false
			})
			_, err := m.Prepare(context.Background(), vmSpec)
			var kept *RetainedError
			destroys := s.requests("destroy")
			switch {
			case c.listed:
				if len(destroys) != 1 || destroys[0].ID != "t-y" || destroys[0].Incarnation != scriptedToken || destroys[0].CID != 8 || destroys[0].Created != 5 || err == nil || errors.As(err, &kept) {
					t.Fatalf("A RESERVE WITH A LOST ANSWER WAS NOT ROLLED BACK: destroys %+v, %v", destroys, err)
				}
			case c.listFails:
				if !errors.As(err, &kept) || kept.Handle.VM != "" || kept.Handle.Attempt != "0v1.att" || kept.Handle.ID != "ci-0v1.att" || len(destroys) != 0 {
					t.Fatalf("AN UNKNOWN RESERVATION WAS NOT RETAINED: %v (destroys %+v)", err, destroys)
				}
			default:
				if err == nil || errors.As(err, &kept) || len(destroys) != 0 || !strings.Contains(err.Error(), "never admitted") {
					t.Fatalf("nothing reserved: %v (destroys %+v)", err, destroys)
				}
			}
			if n := len(s.requests("list")); n != 0 {
				t.Fatalf("A LIST WAS TAKEN FOR A SETTLEMENT: %d list request(s)", n)
			}
			settles := s.requests("settle")
			if len(settles) != 1 || settles[0].Attempt != "0v1.att" || settles[0].Request == "" || settles[0].Request != s.requests("reserve")[0].Request {
				t.Fatalf("THE SETTLEMENT DID NOT NAME THE LOST REQUEST: settles %+v", settles)
			}
		})
	}
}

// The microvm backend never falls back: with no launcher it is
// unavailable, and nothing else is constructed in its place.
func TestMicrovmHasNoFallback(t *testing.T) {
	dir, _ := launchertest.Image(t, "img")
	box, err := New(&config.Config{Sandbox: "microvm", LauncherSocket: t.TempDir() + "/none.sock", ImagePath: dir, ActImage: "img"})
	if box != nil || !errors.Is(err, ErrMicrovmUnavailable) {
		t.Fatalf("microvm with no launcher: %v %v", box, err)
	}
	if box, err := New(&config.Config{}); box != nil || err == nil {
		t.Fatalf("no sandbox named: %v %v", box, err)
	}
}

// dockerRecorder runs no docker command: it records each and fails the
// ones whose first words fail names.
type dockerRecorder struct {
	mu    sync.Mutex
	calls []string
	ctxOK []bool // whether each call's context was still live
	fail  map[string]string
}

func (r *dockerRecorder) exec(ctx context.Context, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, cmd)
	r.ctxOK = append(r.ctxOK, ctx.Err() == nil)
	r.mu.Unlock()
	for prefix, msg := range r.fail {
		if strings.HasPrefix(cmd, prefix) {
			return "", errors.New("docker " + args[0] + ": " + msg)
		}
	}
	if strings.HasPrefix(cmd, "inspect -f") {
		return "10.9.8.7\n", nil
	}
	return "", nil
}

// Docker compatibility: a Prepare that fails half way removes what it
// created, in a context of its own; when that removal fails too, the
// error names the attempt's Docker objects (RetainedError, the
// compatibility mode's own shape) instead of dropping them.
func TestDockerPrepareCleanupIsReportedNotDropped(t *testing.T) {
	spec := Spec{Image: "img", Network: "ci-0v1.att", Attempt: "0v1.att", Profile: "locked"}
	rec := &dockerRecorder{fail: map[string]string{"run -d": "no space left on device"}}
	d := &Docker{Host: "unix:///run/user/1000/test/docker.sock", Owner: "0vd", exec: rec.exec}
	// the attempt's context is over (the recorder answers anyway): the
	// cleanup must not inherit that end
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.Prepare(ctx, spec)
	var kept *RetainedError
	if err == nil || errors.As(err, &kept) {
		t.Fatalf("a cleanup that succeeded: %v", err)
	}
	var cleanup []string
	for i, c := range rec.calls {
		if strings.HasPrefix(c, "rm -f") || strings.HasPrefix(c, "volume rm") || strings.HasPrefix(c, "network rm") {
			cleanup = append(cleanup, c)
			if !rec.ctxOK[i] {
				t.Fatalf("THE CLEANUP RAN UNDER THE ATTEMPT'S ENDED CONTEXT: %s", c)
			}
		}
	}
	if strings.Join(cleanup, "; ") != "rm -f ci-0v1.att; volume rm -f ci-0v1.att-work; volume rm -f ci-0v1.att-work-tools; network rm ci-0v1.att" {
		t.Fatalf("cleanup: %q (all calls %q)", cleanup, rec.calls)
	}
	rec2 := &dockerRecorder{fail: map[string]string{"run -d": "no space left on device", "network rm": "network has active endpoints"}}
	d.exec = rec2.exec
	_, err = d.Prepare(context.Background(), spec)
	if !errors.As(err, &kept) || kept.Handle.Network != "ci-0v1.att" || kept.Handle.Volume != "ci-0v1.att-work" || kept.Handle.Container != "ci-0v1.att" || kept.Handle.VM != "" || kept.Handle.Attempt != "0v1.att" {
		t.Fatalf("A FAILED DOCKER CLEANUP WAS DROPPED: %v", err)
	}
}

// Leftovers names exactly the retention's Docker objects that exist; an
// answer that is neither "exists" nor Docker's not-found reply about that
// very object is a failed look, never taken for an absence (recovery
// ruling A: a Docker retention is released only once none of its objects
// remain). Stage 01, independent review 01 (R4): the recorder answers each
// exact command — a prefix match here had fed the work volume's reply to
// the tools volume's look, which the exact parser now refuses.
func TestDockerLeftoversAreExact(t *testing.T) {
	h := Handle{ID: "ci-0v1.att", Network: "ci-0v1.att", Volume: "ci-0v1.att-work", Container: "ci-0v1.att"}
	container, volume, tools, network := "inspect --type container -f {{.Id}} ci-0v1.att", "volume inspect -f {{.Name}} ci-0v1.att-work",
		"volume inspect -f {{.Name}} ci-0v1.att-work-tools", "network inspect -f {{.Id}} ci-0v1.att"
	d := &Docker{Host: "unix:///run/user/1000/test/docker.sock", Owner: "0vd", exec: answering(map[string]string{
		tools:   "Error: No such volume: ci-0v1.att-work-tools",
		network: "Error: No such network: ci-0v1.att",
	})}
	left, err := d.Leftovers(context.Background(), h)
	if err != nil || strings.Join(left, ", ") != "container ci-0v1.att, volume ci-0v1.att-work" {
		t.Fatalf("leftovers %q %v", left, err)
	}
	absent := map[string]string{container: "Error: No such container: ci-0v1.att", volume: "Error response from daemon: get ci-0v1.att-work: no such volume",
		tools: "Error response from daemon: get ci-0v1.att-work-tools: no such volume", network: "Error response from daemon: network ci-0v1.att not found"}
	d.exec = answering(absent)
	if left, err := d.Leftovers(context.Background(), h); err != nil || len(left) != 0 {
		t.Fatalf("Docker's own answers of absence: %q %v", left, err)
	}
	// a look that failed proves nothing absent: no daemon, an API error,
	// or another kind's answer
	for name, answer := range map[string][2]string{
		"no daemon":         {container, "Cannot connect to the Docker daemon at unix:///run/user/1000/test/docker.sock. Is the docker daemon running?"},
		"an API error":      {network, "Error response from daemon: page not found"},
		"another kind's":    {volume, "Error: No such container: ci-0v1.att-work"},
		"another network's": {network, "Error response from daemon: network ci-0v2.att not found"},
	} {
		replies := map[string]string{}
		for k, v := range absent {
			replies[k] = v
		}
		replies[answer[0]] = answer[1]
		d.exec = answering(replies)
		if left, err := d.Leftovers(context.Background(), h); err == nil {
			t.Fatalf("A FAILED LOOK WAS TAKEN FOR ABSENCE (%s): %q", name, left)
		}
	}
}
