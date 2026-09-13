package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"urgit/runner/internal/config"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
)

// fakeBox records every call. It has no way to read its own filesystem,
// which is the point: the interface offers Copy (in) and Run (a stream
// out) and nothing else, so the daemon cannot read the sandbox after Run.
type fakeBox struct {
	mu         sync.Mutex
	ops        []string
	stream     string
	exit       int
	destroyErr error
	ranAt      int // index in ops where Run happened
}

func (f *fakeBox) record(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, op)
}

func (f *fakeBox) Prepare(_ context.Context, spec sandbox.Spec) (sandbox.Handle, error) {
	f.record("prepare " + spec.Network + " image=" + spec.Image)
	return sandbox.Handle{ID: spec.Network, Network: spec.Network, Volume: spec.Network + "-work", Container: spec.Network}, nil
}
func (f *fakeBox) Copy(_ context.Context, _ sandbox.Handle, host, guest string) error {
	f.record("copy " + filepath.Base(strings.TrimSuffix(host, "/.")) + " -> " + guest)
	return nil
}
func (f *fakeBox) Run(_ context.Context, _ sandbox.Handle, workDir string, argv, env []string) (io.ReadCloser, <-chan int, error) {
	f.record("run " + strings.Join(argv, " ") + " env=" + strings.Join(env, ","))
	f.mu.Lock()
	f.ranAt = len(f.ops) - 1
	f.mu.Unlock()
	done := make(chan int, 1)
	done <- f.exit
	return io.NopCloser(strings.NewReader(f.stream)), done, nil
}
func (f *fakeBox) Signal(context.Context, sandbox.Handle, string, string) error { return nil }
func (f *fakeBox) Destroy(_ context.Context, h sandbox.Handle) error {
	f.record("destroy " + h.ID)
	return f.destroyErr
}
func (f *fakeBox) Orphans(context.Context) ([]string, error) { return nil, nil }
func (f *fakeBox) Name() string                              { return "fake" }

type fakeShip struct {
	mu       sync.Mutex
	events   []string
	results  []string
	abandons []string
	plans    []string
}

func (s *fakeShip) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Header.Get("x-ci-bearer") != "0v1.bearer" {
			w.WriteHeader(401)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/event"):
			var ev map[string]any
			_ = json.Unmarshal(body, &ev)
			if ev["jobID"] != "b" {
				w.WriteHeader(409)
				_, _ = w.Write([]byte(`{"error":"event job does not match the assignment"}`))
				return
			}
			s.events = append(s.events, string(body))
			w.WriteHeader(202)
		case strings.HasSuffix(r.URL.Path, "/result"):
			s.results = append(s.results, string(body))
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/abandon"):
			s.abandons = append(s.abandons, string(body))
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/plan"):
			s.plans = append(s.plans, string(body))
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

const chainWorkflow = `name: fixture-chain
on: [push]
jobs:
  a:
    runs-on: ubuntu-latest
    outputs:
      go: ${{ steps.emit.outputs.go }}
    steps:
      - name: emit go
        id: emit
        run: echo "go=true" >> "$GITHUB_OUTPUT"
  b:
    runs-on: ubuntu-latest
    needs: a
    if: needs.a.outputs.go == 'true'
    steps:
      - name: gated step
        run: echo "b ran because a said go"
`

func newTestDaemon(t *testing.T, box *fakeBox, srv *httptest.Server, capacity int) *Daemon {
	t.Helper()
	work := t.TempDir()
	cfg := &config.Config{ShipURL: srv.URL, ActBinary: "/bin/sh", ActImage: "img", Capacity: capacity, WorkDir: work, StateFile: filepath.Join(work, "state.json")}
	d := &Daemon{cfg: cfg, box: box, log: log.New(io.Discard, "", 0), capacity: capacity, client: ship.New(srv.URL, "0v1.bearer"), daemonID: "0v1.daemon"}
	d.checkout = func(_ context.Context, a *ship.Assignment, dir string) error {
		if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, ".github", "workflows", "fixture-chain.yml"), []byte(chainWorkflow), 0o644)
	}
	return d
}

var jobAssignment = &ship.Assignment{
	ID: "0v1.asg", Attempt: "0v1.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/heads/master",
	OID: "0000000000000000000000000000000000000001", Kind: "job", Workflow: "fixture-chain.yml", Job: "b",
	PrereqOutputs: map[string]map[string]string{"a": {"go": "true"}}, DeadlineSeconds: 60,
}

// a job: prepare, copy the checkout, the act binary and the projection in,
// run act on the projection with the isolated network and the prereq env,
// relay, claim the stream's jobResult, destroy; nothing touches the
// sandbox after Run except Destroy
func TestJobNeverReadsSandboxAfterRun(t *testing.T) {
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	box := &fakeBox{stream: `{"level":"info","msg":"Using docker host"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"step"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	d := newTestDaemon(t, box, srv, 1)
	if keep := d.handle(context.Background(), jobAssignment); !keep {
		t.Fatal("slot must be kept after a clean teardown")
	}
	want := []string{
		"prepare ci-0v1.att image=img",
		"copy src -> /work/src",
		"copy sh -> /usr/local/bin/act",
		"copy fixture-chain.yml -> /work/projected/fixture-chain.yml",
		"run act push -W /work/projected/fixture-chain.yml -j b -P ubuntu-latest=img --network ci-0v1.att --json --pull=false --cache-server-path /work/cache --artifact-server-path /work/artifacts --env NEEDS_A_OUTPUTS_GO=true env=",
		"destroy ci-0v1.att",
	}
	if strings.Join(box.ops, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ops:\n%s\nwant:\n%s", strings.Join(box.ops, "\n"), strings.Join(want, "\n"))
	}
	for _, op := range box.ops[box.ranAt+1:] {
		if !strings.HasPrefix(op, "destroy") {
			t.Fatalf("sandbox touched after Run: %s", op)
		}
	}
	if len(sh.events) != 2 || len(sh.results) != 1 || !strings.Contains(sh.results[0], `"success"`) || len(sh.abandons) != 0 {
		t.Fatalf("ship saw events=%d results=%v abandons=%v", len(sh.events), sh.results, sh.abandons)
	}
	// the projection the daemon wrote had one job, no needs, no if
	// (it was removed with the work dir; the ship's 409 tripwire covers
	// the live case; the plan package's test covers the bytes)
}

// act exiting without a jobResult is an abandon, never a result; a
// refused line (the tripwire) is never claimed either
func TestJobWithoutJobResultAbandons(t *testing.T) {
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	box := &fakeBox{exit: 137, stream: `{"job":"fixture-chain/a","jobID":"a","jobResult":"success","time":"t","msg":"a leaked"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"step"}` + "\n"}
	d := newTestDaemon(t, box, srv, 1)
	d.handle(context.Background(), jobAssignment)
	if len(sh.results) != 0 || len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "exited 137 without a jobResult") {
		t.Fatalf("results=%v abandons=%v", sh.results, sh.abandons)
	}
}

// a failed teardown quarantines the slot: capacity drops, the handle is
// logged, and the slot is not returned
func TestDestroyFailureQuarantines(t *testing.T) {
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	box := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n", destroyErr: io.ErrUnexpectedEOF}
	d := newTestDaemon(t, box, srv, 2)
	if keep := d.handle(context.Background(), jobAssignment); keep {
		t.Fatal("slot must not be reused after a failed teardown")
	}
	if d.remainingCapacity() != 1 || len(d.quarantined) != 1 || d.quarantined[0] != "ci-0v1.att" {
		t.Fatalf("capacity %d quarantined %v", d.remainingCapacity(), d.quarantined)
	}
}

// a plan: act -l per workflow file in the sandbox, the walk on the host,
// one POST carrying needs, cond, matrix and events
func TestPlanAssignment(t *testing.T) {
	sh := &fakeShip{}
	srv := httptest.NewServer(sh.handler(t))
	defer srv.Close()
	box := &fakeBox{stream: "Stage  Job ID  Job name  Workflow name  Workflow file      Events\n0      a       a         fixture-chain  fixture-chain.yml  push  \n1      b       b         fixture-chain  fixture-chain.yml  push  \n"}
	d := newTestDaemon(t, box, srv, 1)
	d.handle(context.Background(), &ship.Assignment{ID: "0v2", Attempt: "0v2.att", Candidate: "0v1.cand", Repo: "r", OID: "0000000000000000000000000000000000000001", Kind: "plan", DeadlineSeconds: 300})
	if len(sh.plans) != 1 {
		t.Fatalf("plans=%v abandons=%v", sh.plans, sh.abandons)
	}
	var posted struct {
		OID       string   `json:"oid"`
		Workflows []string `json:"workflows"`
		Jobs      []struct {
			ID     string          `json:"id"`
			Needs  []string        `json:"needs"`
			Cond   json.RawMessage `json:"cond"`
			Matrix bool            `json:"matrix"`
			Events []string        `json:"events"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(sh.plans[0]), &posted); err != nil {
		t.Fatal(err)
	}
	if posted.OID != "0000000000000000000000000000000000000001" || len(posted.Workflows) != 1 || len(posted.Jobs) != 2 {
		t.Fatalf("%s", sh.plans[0])
	}
	if string(posted.Jobs[0].Cond) != "null" || posted.Jobs[1].Needs[0] != "a" ||
		!strings.Contains(string(posted.Jobs[1].Cond), `"kind":"output-eq"`) || !strings.Contains(string(posted.Jobs[1].Cond), `"v":1`) {
		t.Fatalf("%s", sh.plans[0])
	}
	if !strings.Contains(strings.Join(box.ops, "\n"), "run sh -c act -l -W '.github/workflows/fixture-chain.yml' 2>&1") {
		t.Fatalf("ops: %v", box.ops)
	}
}
