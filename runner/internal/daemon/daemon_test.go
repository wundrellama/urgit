package daemon

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
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
	// answer, when set, is the output and exit of every Run that is not
	// act (the image loads before the job)
	answer func(argv []string) (string, int)
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
	if f.answer != nil && len(argv) > 0 && !strings.HasSuffix(argv[0], "act") {
		out, code := f.answer(argv)
		done <- code
		return io.NopCloser(strings.NewReader(out)), done, nil
	}
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
func (f *fakeBox) Kind() string                              { return "fake" }
func (f *fakeBox) SetOwner(string) error                     { return nil }
func (f *fakeBox) HandleFor(id string) sandbox.Handle {
	return sandbox.Handle{ID: id, Network: id, Volume: id + "-work", Container: id}
}

// the fake's execution settings are the container backend's shape
func (f *fakeBox) ExecEnv(h sandbox.Handle) sandbox.ExecEnv {
	return sandbox.ExecEnv{
		WorkRoot: "/work", SourceDir: "/work/src", ActBinary: "/usr/local/bin/act", CopyAct: true,
		DockerSocket: "unix:///run/user/1000/test/docker.sock", JobNetwork: h.Network,
		CachePath: "/work/cache", ArtifactPath: "/work/artifacts", ActionCachePath: "/work/actions", ToolCache: "/work/toolcache",
		ServerAddr: "10.9.8.7",
	}
}

type fakeShip struct {
	mu       sync.Mutex
	events   []string
	results  []string
	abandons []string
	plans    []string
	uploads  []string // upload request bodies
	puts     []string // "<key> <bytes> <x-amz-content-sha256>" the store saw
	storeURL string   // where the signed PUT points (a fake store); "" refuses uploads
	// the retention reports and recovery answers the daemon posts (Q11;
	// INTEGRATION.md §11.12): taken, and kept
	reports []string
	answers []string
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
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/retentions"):
			s.reports = append(s.reports, string(body))
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/recovery"):
			s.answers = append(s.answers, string(body))
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/upload"):
			s.uploads = append(s.uploads, string(body))
			if s.storeURL == "" {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":"ship object storage is not configured; CI cannot be enabled"}`))
				return
			}
			var req struct {
				Name   string `json:"name"`
				SHA256 string `json:"sha256"`
			}
			_ = json.Unmarshal(body, &req)
			key := "ci/r/0v1.cand/0v1.att/trusted/" + req.Name
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": s.storeURL + "/bucket/" + key, "method": "PUT", "key": key,
				"headers": map[string]string{"x-amz-content-sha256": req.SHA256, "authorization": "AWS4-HMAC-SHA256 test"},
			})
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

// a store that only records what it is sent, and refuses a PUT without
// the ship's authorization header
func (s *fakeShip) store(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method != http.MethodPut || r.Header.Get("authorization") == "" {
			w.WriteHeader(403)
			return
		}
		s.puts = append(s.puts, strings.TrimPrefix(r.URL.Path, "/bucket/")+" "+string(body)+" "+r.Header.Get("x-amz-content-sha256"))
		w.WriteHeader(200)
	})
}

// newTestDaemon is a daemon on the fake box whose ship (and object store)
// answer in process: no listener (stage 01 fixture adaptation — these
// tests used loopback httptest servers).
func newTestDaemon(t *testing.T, box *fakeBox, sh *fakeShip, capacity int) *Daemon {
	t.Helper()
	work := t.TempDir()
	cfg := &config.Config{ShipURL: shipURL, ActBinary: "/bin/sh", ActImage: "img", Capacity: capacity, WorkDir: work, StateFile: filepath.Join(work, "state.json"), DockerHost: "unix:///run/user/1000/test/docker.sock"}
	client := ship.New(shipURL, "0v1.bearer")
	client.HTTP = &http.Client{Transport: inproc{"ship.test": sh.handler(t), "store.test": sh.store(t)}}
	d := &Daemon{cfg: cfg, box: box, log: log.New(io.Discard, "", 0), capacity: capacity, client: client, daemonID: "0v1.daemon"}
	d.checkout = func(_ context.Context, a *ship.Assignment, dir string) error {
		if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, ".github", "workflows", "fixture-chain.yml"), []byte(chainWorkflow), 0o644)
	}
	return d
}

// the execution manifest the ship signs (P4 D5); the plain fields must
// agree with it, and the fake sandbox is the container shape
var jobManifest = sig.Manifest{
	Incarnation: "0v1.inc", Repo: "r", Ref: "refs/heads/master", Candidate: "0v1.cand", OID: "0000000000000000000000000000000000000001",
	Baseline: "", Lock: "", Generation: 1, Workflow: "fixture-chain.yml", Job: "b", Trust: "trusted", Sandbox: "container", Mode: "required", Network: "locked",
}

var jobAssignment = &ship.Assignment{
	ID: "0v1.asg", Attempt: "0v1.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/heads/master",
	OID: "0000000000000000000000000000000000000001", Kind: "job", Workflow: "fixture-chain.yml", Job: "b",
	PrereqOutputs: map[string]map[string]string{"a": {"go": "true"}}, DeadlineSeconds: 60,
	Manifest: &jobManifest,
}

// a job: prepare, copy the checkout, the act binary and the projection in,
// run act on the projection with the isolated network and the prereq env,
// relay, claim the stream's jobResult, destroy; nothing touches the
// sandbox after Run except Destroy
func TestJobNeverReadsSandboxAfterRun(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{stream: `{"level":"info","msg":"Using docker host"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"step"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	d := newTestDaemon(t, box, sh, 1)
	if keep := d.handle(context.Background(), jobAssignment); !keep {
		t.Fatal("slot must be kept after a clean teardown")
	}
	// the bundle (checkout, projection, action cache, downloads, bundle.json)
	// enters as ONE copy; act follows; nothing else touches the sandbox
	// before Run
	want := []string{
		"prepare ci-0v1.att image=img",
		"copy bundle -> /work",
		"copy sh -> /usr/local/bin/act",
		"run /usr/local/bin/act push -W /work/projected/fixture-chain.yml -j b -P ubuntu-latest=img --network ci-0v1.att --json --pull=false --cache-server-path /work/cache --artifact-server-path /work/artifacts --container-daemon-socket unix:///run/user/1000/test/docker.sock --action-cache-path /work/actions --action-offline-mode --container-options -v /work/downloads:/urgit/downloads:ro --env URGIT_CI_DOWNLOADS=/urgit/downloads --cache-server-addr 10.9.8.7 --artifact-server-addr 10.9.8.7 --env NEEDS_A_OUTPUTS_GO=true env=",
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

const imageWorkflow = `name: fixture-image
on: [push]
jobs:
  b:
    runs-on: ubuntu-latest
    container: docker.io/library/busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
    services:
      db:
        image: "postgres:16"
    steps:
      - uses: docker://alpine:3.19
      - run: echo hi
`

// a locked container image (P03): the daemon stages the lock's archive
// from the store (verified like every download), loads it into the
// sandbox's own Docker under the lock's name before act, verifies the
// loaded id against the lock, and projects the job onto that name so act
// (--pull=false) finds it locally; an image the lock does not name is
// left as written. Nothing is ever pulled.
func TestLockedImageLoadedFromArchiveBeforeAct(t *testing.T) {
	sh := &fakeShip{}
	// a minimal docker-archive: one config blob (two rootfs layers) and
	// the manifest naming it; its id is the config's digest
	config := []byte(`{"architecture":"amd64","os":"linux","config":{},"rootfs":{"type":"layers","diff_ids":["sha256:aaaa","sha256:bbbb"]}}`)
	sum := sha256.Sum256(config)
	id := "sha256:" + hex.EncodeToString(sum[:])
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for name, data := range map[string][]byte{hex.EncodeToString(sum[:]) + ".json": config, "manifest.json": []byte(`[{"Config":"` + hex.EncodeToString(sum[:]) + `.json","RepoTags":["urgit-locked/library/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"],"Layers":[]}]`)} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	archiveSum := sha256.Sum256(tarBuf.Bytes())
	archive := hex.EncodeToString(archiveSum[:])
	box := &fakeBox{stream: `{"job":"fixture-image/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	// a classic image store: .Id is the config digest
	box.answer = func(argv []string) (string, int) {
		switch strings.Join(argv[:2], " ") {
		case "docker load":
			return "Loaded image: urgit-locked/library/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662\n", 0
		case "docker image":
			return id + ` ["sha256:aaaa","sha256:bbbb"]` + "\n", 0
		}
		return "unexpected " + strings.Join(argv, " "), 1
	}
	d := newTestDaemon(t, box, sh, 1)
	d.checkout = func(_ context.Context, a *ship.Assignment, dir string) error {
		if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, ".github", "workflows", "fixture-image.yml"), []byte(imageWorkflow), 0o644)
	}
	var staged []string
	var projected string
	d.fetchDownload = func(_ context.Context, a *ship.Assignment, dl ship.Download, target string) error {
		staged = append(staged, dl.Kind+" "+dl.URL+" "+dl.SHA256)
		return os.WriteFile(target, tarBuf.Bytes(), 0o644)
	}
	a := *jobAssignment
	a.Workflow, a.Job = "fixture-image.yml", "b"
	a.PrereqOutputs = nil
	m := jobManifest
	m.Workflow = "fixture-image.yml"
	a.Manifest = &m
	a.Downloads = []ship.Download{
		{URL: "docker.io/library/busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662", Uses: "container:docker.io/library/busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662", Kind: "container", SHA256: archive, Size: int64(tarBuf.Len()), Mirror: "store", Path: "ci/downloads/" + archive, Digest: "sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662", ID: id},
	}
	// the projection is read back through Copy: the fake records the
	// bundle directory's projected workflow when it is copied in
	box.mu.Lock()
	box.mu.Unlock()
	orig := d.box
	d.box = &projectionReader{fakeBox: box, read: func(host string) {
		if data, err := os.ReadFile(filepath.Join(strings.TrimSuffix(host, "/."), "projected", "fixture-image.yml")); err == nil {
			projected = string(data)
		}
	}}
	defer func() { d.box = orig }()
	if keep := d.handle(context.Background(), &a); !keep {
		t.Fatal("slot must be kept after a clean teardown")
	}
	if len(staged) != 1 || staged[0] != "container docker.io/library/busybox@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 "+archive {
		t.Fatalf("staged: %v", staged)
	}
	ops := strings.Join(box.ops, "\n")
	load := "run docker load -i /work/downloads/" + archive + " env="
	inspect := "run docker image inspect --format {{.Id}} {{json .RootFS.Layers}} urgit-locked/library/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 env="
	li, ii, ai := strings.Index(ops, load), strings.Index(ops, inspect), strings.Index(ops, "run /usr/local/bin/act")
	if li < 0 || ii < li || ai < ii {
		t.Fatalf("the load, the id check and act must run in that order:\n%s", ops)
	}
	if !strings.Contains(projected, "container: urgit-locked/library/busybox:sha256-73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662\n") {
		t.Fatalf("the projection must name the loaded image:\n%s", projected)
	}
	if !strings.Contains(projected, `image: "postgres:16"`) || !strings.Contains(projected, "uses: docker://alpine:3.19") {
		t.Fatalf("images the lock does not name stay as written:\n%s", projected)
	}
	if len(sh.results) != 1 || len(sh.abandons) != 0 {
		t.Fatalf("results=%v abandons=%v", sh.results, sh.abandons)
	}
	// a containerd-backed store: .Id is a manifest digest, but the rootfs
	// layers are the archive's — accepted
	box.answer = func(argv []string) (string, int) {
		if argv[1] == "load" {
			return "Loaded image", 0
		}
		return `sha256:9999999999999999999999999999999999999999999999999999999999999999 ["sha256:aaaa","sha256:bbbb"]` + "\n", 0
	}
	box.ops = nil
	sh.results, sh.abandons = nil, nil
	a.Attempt = "0v4.att"
	d.handle(context.Background(), &a)
	if !strings.Contains(strings.Join(box.ops, "\n"), "run /usr/local/bin/act") || len(sh.abandons) != 0 {
		t.Fatalf("a containerd-store id with the archive's layers must be accepted:\n%s\n%v", strings.Join(box.ops, "\n"), sh.abandons)
	}
	// neither the id nor the layers are the archive's: refused before act
	box.answer = func(argv []string) (string, int) {
		if argv[1] == "load" {
			return "Loaded image", 0
		}
		return `sha256:9999999999999999999999999999999999999999999999999999999999999999 ["sha256:cccc"]` + "\n", 0
	}
	box.ops = nil
	sh.results, sh.abandons = nil, nil
	a.Attempt = "0v2.att"
	d.handle(context.Background(), &a)
	if strings.Contains(strings.Join(box.ops, "\n"), "run /usr/local/bin/act") {
		t.Fatalf("act ran on an image that loaded as another id:\n%s", strings.Join(box.ops, "\n"))
	}
	if len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "loaded as sha256:9999") || !strings.Contains(sh.abandons[0], "the lock names "+id) {
		t.Fatalf("abandons=%v", sh.abandons)
	}
	// the lock names another id than the archive holds: refused before the load
	a.Downloads[0].ID = "sha256:" + strings.Repeat("77", 32)
	box.ops = nil
	sh.abandons = nil
	a.Attempt = "0v5.att"
	d.handle(context.Background(), &a)
	if strings.Contains(strings.Join(box.ops, "\n"), "docker load") || len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "the locked archive holds image "+id) {
		t.Fatalf("ops=%v abandons=%v", box.ops, sh.abandons)
	}
	a.Downloads[0].ID = id
	// the archive missing from the store: refused at the bundle, no load
	d.fetchDownload = func(_ context.Context, a *ship.Assignment, dl ship.Download, target string) error {
		return errors.New("store answered 404")
	}
	box.ops = nil
	sh.abandons = nil
	a.Attempt = "0v3.att"
	d.handle(context.Background(), &a)
	if strings.Contains(strings.Join(box.ops, "\n"), "docker load") || strings.Contains(strings.Join(box.ops, "\n"), "run /usr/local/bin/act") {
		t.Fatalf("nothing may run when the archive is missing:\n%s", strings.Join(box.ops, "\n"))
	}
	if len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "bundle: download docker.io/library/busybox@sha256:73aaf") || !strings.Contains(sh.abandons[0], "store answered 404") {
		t.Fatalf("abandons=%v", sh.abandons)
	}
}

// projectionReader wraps the fake box to read the bundle's projection
// when it is copied in (the daemon removes the work dir afterwards)
type projectionReader struct {
	*fakeBox
	read func(host string)
}

func (p *projectionReader) Copy(ctx context.Context, h sandbox.Handle, host, guest string) error {
	if strings.HasSuffix(host, "/bundle/.") {
		p.read(host)
	}
	return p.fakeBox.Copy(ctx, h, host, guest)
}

// the finished stream is uploaded before the result names it (D1/D2):
// the daemon asks the ship for the PUT, sends the bytes with the ship's
// headers, and the result carries the size and sha-256 of exactly the
// saved stream; the key is the ship's, in the attempt's trust class
func TestJobUploadsLogBeforeResult(t *testing.T) {
	sh := &fakeShip{}
	sh.storeURL = storeURL
	stream := `{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"step"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"
	box := &fakeBox{stream: stream}
	d := newTestDaemon(t, box, sh, 1)
	d.handle(context.Background(), jobAssignment)
	if len(sh.uploads) != 1 || !strings.Contains(sh.uploads[0], `"name":"log.jsonl"`) {
		t.Fatalf("uploads=%v", sh.uploads)
	}
	sum := sha256.Sum256([]byte(stream))
	want := "ci/r/0v1.cand/0v1.att/trusted/log.jsonl " + stream + " " + hex.EncodeToString(sum[:])
	if len(sh.puts) != 1 || sh.puts[0] != want {
		t.Fatalf("store saw %q, want %q", sh.puts, want)
	}
	if len(sh.results) != 1 || !strings.Contains(sh.results[0], `"log":{"size":`+strconv.Itoa(len(stream))+`,"sha256":"`+hex.EncodeToString(sum[:])+`"}`) {
		t.Fatalf("result must name the uploaded log: %v", sh.results)
	}
}

// a store the ship cannot sign for (503) leaves the result without a log;
// the verdict is still claimed
func TestJobResultWithoutStore(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	d := newTestDaemon(t, box, sh, 1)
	d.handle(context.Background(), jobAssignment)
	if len(sh.puts) != 0 || len(sh.results) != 1 || strings.Contains(sh.results[0], `"log"`) {
		t.Fatalf("puts=%v results=%v", sh.puts, sh.results)
	}
}

// act exiting without a jobResult is an abandon, never a result; a
// refused line (the tripwire) is never claimed either
func TestJobWithoutJobResultAbandons(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{exit: 137, stream: `{"job":"fixture-chain/a","jobID":"a","jobResult":"success","time":"t","msg":"a leaked"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"step"}` + "\n"}
	d := newTestDaemon(t, box, sh, 1)
	d.handle(context.Background(), jobAssignment)
	if len(sh.results) != 0 || len(sh.abandons) != 1 || !strings.Contains(sh.abandons[0], "exited 137 without a jobResult") {
		t.Fatalf("results=%v abandons=%v", sh.results, sh.abandons)
	}
}

// a failed teardown quarantines the slot: capacity drops, the handle is
// logged, and the slot is not returned
func TestDestroyFailureQuarantines(t *testing.T) {
	sh := &fakeShip{}
	box := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n", destroyErr: io.ErrUnexpectedEOF}
	d := newTestDaemon(t, box, sh, 2)
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
	box := &fakeBox{stream: "Stage  Job ID  Job name  Workflow name  Workflow file      Events\n0      a       a         fixture-chain  fixture-chain.yml  push  \n1      b       b         fixture-chain  fixture-chain.yml  push  \n"}
	d := newTestDaemon(t, box, sh, 1)
	planManifest := jobManifest
	planManifest.Workflow, planManifest.Job, planManifest.Mode = "", "", "plan"
	d.handle(context.Background(), &ship.Assignment{ID: "0v2", Attempt: "0v2.att", Candidate: "0v1.cand", Repo: "r", Ref: "refs/heads/master", OID: "0000000000000000000000000000000000000001", Kind: "plan", DeadlineSeconds: 300, Manifest: &planManifest})
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
	if !strings.Contains(sh.plans[0], `"name":"fixture-chain"`) {
		t.Fatalf("the plan must carry the real workflow name: %s", sh.plans[0])
	}
	if string(posted.Jobs[0].Cond) != "null" || posted.Jobs[1].Needs[0] != "a" ||
		!strings.Contains(string(posted.Jobs[1].Cond), `"kind":"output-eq"`) || !strings.Contains(string(posted.Jobs[1].Cond), `"v":1`) {
		t.Fatalf("%s", sh.plans[0])
	}
	if !strings.Contains(strings.Join(box.ops, "\n"), "run sh -c /usr/local/bin/act -l -W '.github/workflows/fixture-chain.yml' 2>&1") {
		t.Fatalf("ops: %v", box.ops)
	}
}

// the ship offers a delivered assignment again when its attempt shows no
// activity; an attempt this process is already running is claimed once
func TestClaimIgnoresInFlight(t *testing.T) {
	d := &Daemon{inFlight: map[string]bool{}}
	if !d.claim("0v1.att") {
		t.Fatal("first claim must succeed")
	}
	if d.claim("0v1.att") {
		t.Fatal("a second claim of a running attempt must be ignored")
	}
	d.release("0v1.att")
	if !d.claim("0v1.att") {
		t.Fatal("after release the attempt may be claimed again")
	}
}

// signGrant is the ship's side in miniature: a grant signed with a key
// the test daemon pins
func signGrant(t *testing.T, priv ed25519.PrivateKey, daemonID, attempt, name, value string, expiry int64) ship.Grant {
	t.Helper()
	m := sig.ManifestMessage{Recipient: daemonID, Attempt: attempt, Operation: "grant:" + name, Expiry: expiry, Nonce: "0v1.nonce", Manifest: jobManifest}
	b, err := m.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return ship.Grant{Name: name, Value: value, Expiry: expiry, Nonce: m.Nonce, Sig: hex.EncodeToString(ed25519.Sign(priv, b))}
}

// grants (D4/D5): a verified, unexpired grant becomes an act secret; an
// expired one and one signed with another key never reach act; the
// daemon's own log redacts the value; the relayed lines and the saved
// stream carry *** where act printed the value
func TestGrantsToActSecretsAndScrub(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	sh := &fakeShip{}
	box := &fakeBox{stream: `{"job":"fixture-chain/b","jobID":"b","time":"t","msg":"token is hunter2hunter2 twice hunter2hunter2"}` + "\n" +
		`{"job":"fixture-chain/b","jobID":"b","jobResult":"success","time":"t","msg":"done"}` + "\n"}
	var logged bytes.Buffer
	d := newTestDaemon(t, box, sh, 1)
	d.log = log.New(&logged, "", 0)
	d.ciKey = hex.EncodeToString(pub)
	now := time.Now().Unix()
	a := *jobAssignment
	a.Grants = []ship.Grant{
		signGrant(t, priv, d.daemonID, a.Attempt, "TOKEN", "hunter2hunter2", now+600),
		signGrant(t, priv, d.daemonID, a.Attempt, "STALE", "stalevalue1234", now-1),
		signGrant(t, otherPriv, d.daemonID, a.Attempt, "FORGED", "forgedvalue123", now+600),
	}
	// a grant signed over a different manifest (another job of the same
	// attempt id) is not this attempt's grant
	other := jobManifest
	other.Job = "a"
	om := sig.ManifestMessage{Recipient: d.daemonID, Attempt: a.Attempt, Operation: "grant:REBOUND", Expiry: now + 600, Nonce: "0v1.nonce", Manifest: other}
	ob, _ := om.Bytes()
	a.Grants = append(a.Grants, ship.Grant{Name: "REBOUND", Value: "reboundvalue12", Expiry: now + 600, Nonce: om.Nonce, Sig: hex.EncodeToString(ed25519.Sign(priv, ob))})
	d.handle(context.Background(), &a)
	run := ""
	for _, op := range box.ops {
		if strings.HasPrefix(op, "run ") {
			run = op
		}
	}
	if !strings.Contains(run, "--secret TOKEN=hunter2hunter2") || strings.Contains(run, "STALE") || strings.Contains(run, "FORGED") || strings.Contains(run, "REBOUND") {
		t.Fatalf("act argv: %s", run)
	}
	text := logged.String()
	if strings.Contains(text, "hunter2hunter2") || !strings.Contains(text, "--secret TOKEN=***") {
		t.Fatalf("the daemon log must redact the secret: %s", text)
	}
	if !strings.Contains(text, "grant STALE refused: expired") || !strings.Contains(text, "grant FORGED refused: signature does not verify") || !strings.Contains(text, "grants 1 (TOKEN) of 4 offered") {
		t.Fatalf("refusals must be logged: %s", text)
	}
	if len(sh.events) != 2 || !strings.Contains(sh.events[0], "token is *** twice ***") {
		t.Fatalf("relayed lines must be scrubbed: %v", sh.events)
	}
	saved, err := os.ReadFile(filepath.Join(d.cfg.WorkDir, a.Attempt+".act.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "hunter2hunter2") || !strings.Contains(string(saved), "token is *** twice ***") {
		t.Fatalf("the saved stream must be scrubbed: %s", saved)
	}
}

// the assignment signature (D5): unsigned, signed for another daemon,
// signed with another key or expired is refused; the ship's own is not
func TestVerifyAssignment(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	d := &Daemon{daemonID: "0v1.daemon", ciKey: hex.EncodeToString(pub), log: log.New(io.Discard, "", 0)}
	sign := func(priv ed25519.PrivateKey, recipient, attempt, op string, expiry int64, m sig.Manifest) *ship.Signature {
		msg := sig.ManifestMessage{Recipient: recipient, Attempt: attempt, Operation: op, Expiry: expiry, Nonce: "0v7", Manifest: m}
		b, _ := msg.Bytes()
		return &ship.Signature{Version: 2, Recipient: recipient, Attempt: attempt, Operation: op, Expiry: expiry, Nonce: msg.Nonce, Sig: hex.EncodeToString(ed25519.Sign(priv, b))}
	}
	later := time.Now().Unix() + 300
	good := *jobAssignment
	good.Sig = sign(priv, "0v1.daemon", good.Attempt, "assign", later, jobManifest)
	if err := d.verifyAssignment(&good); err != nil {
		t.Fatalf("the ship's own signature must verify: %v", err)
	}
	cases := map[string]*ship.Signature{
		"unsigned":        nil,
		"other daemon":    sign(priv, "0v2.other", good.Attempt, "assign", later, jobManifest),
		"other attempt":   sign(priv, "0v1.daemon", "0v9.att", "assign", later, jobManifest),
		"other operation": sign(priv, "0v1.daemon", good.Attempt, "grant:X", later, jobManifest),
		"other key":       sign(otherPriv, "0v1.daemon", good.Attempt, "assign", later, jobManifest),
		"expired":         sign(priv, "0v1.daemon", good.Attempt, "assign", time.Now().Unix()-1, jobManifest),
	}
	for name, s := range cases {
		a := *jobAssignment
		a.Sig = s
		if err := d.verifyAssignment(&a); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	// a v1 signature (no manifest) fails closed with the version reason
	v1 := *jobAssignment
	v1.Sig = &ship.Signature{Version: 1, Recipient: "0v1.daemon", Attempt: good.Attempt, Operation: "assign", Expiry: later, Nonce: "0v7", Sig: good.Sig.Sig}
	if err := d.verifyAssignment(&v1); err == nil || !strings.Contains(err.Error(), "manifest version 1") {
		t.Fatalf("v1 must fail closed naming the version: %v", err)
	}
	// every manifest field is bound (P10): a signature over one manifest
	// never verifies another, and a plain field that disagrees with the
	// signed manifest is a tampered assignment
	mutations := map[string]func(m *sig.Manifest){
		"sandbox":     func(m *sig.Manifest) { m.Sandbox = "vm" },
		"trust":       func(m *sig.Manifest) { m.Trust = "untrusted" },
		"lock":        func(m *sig.Manifest) { m.Lock = "ff" },
		"baseline":    func(m *sig.Manifest) { m.Baseline = "0000000000000000000000000000000000000002" },
		"generation":  func(m *sig.Manifest) { m.Generation = 2 },
		"mode":        func(m *sig.Manifest) { m.Mode = "trial" },
		"network":     func(m *sig.Manifest) { m.Network = "integration" },
		"incarnation": func(m *sig.Manifest) { m.Incarnation = "0v2.inc" },
	}
	for name, mutate := range mutations {
		a := *jobAssignment
		m := jobManifest
		mutate(&m)
		a.Manifest = &m
		a.Sig = good.Sig
		if err := d.verifyAssignment(&a); err == nil {
			t.Fatalf("manifest mutation %s verified with the original signature", name)
		}
	}
	tampered := *jobAssignment
	tampered.Sig = good.Sig
	tampered.OID = "0000000000000000000000000000000000000009"
	if err := d.verifyAssignment(&tampered); err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Fatalf("a plain field differing from the manifest must be refused: %v", err)
	}
	unpinned := &Daemon{daemonID: "0v1.daemon", log: log.New(io.Discard, "", 0)}
	if err := unpinned.verifyAssignment(&good); err == nil {
		t.Fatal("no pinned key must refuse")
	}
}

// the sandbox requirement (M10): a VM-required manifest on a container
// runner is abandoned before any work; the container backend never
// receives it, and a locked manifest that names destinations, or a
// profile this runner does not declare, is refused the same way
func TestHonourableRefusesWhatThisRunnerCannotDo(t *testing.T) {
	d := &Daemon{cfg: &config.Config{Sandbox: "docker-rootless", NetworkProfiles: []config.NetworkProfile{{Name: "integration", Destinations: []string{"tcp:192.168.1.229:8472"}}}}, box: &fakeBox{}, log: log.New(io.Discard, "", 0)}
	vm := *jobAssignment
	m := jobManifest
	m.Sandbox = "vm"
	vm.Manifest = &m
	if err := d.honourable(&vm); err == nil || !strings.Contains(err.Error(), "sandbox requirement vm") {
		t.Fatalf("vm-required on a container runner: %v", err)
	}
	ok := *jobAssignment
	if err := d.honourable(&ok); err != nil {
		t.Fatalf("container-ok manifest refused: %v", err)
	}
	unknown := *jobAssignment
	mu := jobManifest
	mu.Network, mu.NetworkScope = "staging", "tcp:10.0.0.1:443"
	unknown.Manifest = &mu
	if err := d.honourable(&unknown); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared profile: %v", err)
	}
	wider := *jobAssignment
	mw := jobManifest
	mw.Network, mw.NetworkScope = "integration", "tcp:192.168.1.229:8472,tcp:10.0.0.1:443"
	wider.Manifest = &mw
	if err := d.honourable(&wider); err == nil || !strings.Contains(err.Error(), "outside this runner's ceiling") {
		t.Fatalf("scope beyond the ceiling: %v", err)
	}
	inside := *jobAssignment
	mi := jobManifest
	mi.Network, mi.NetworkScope = "integration", "tcp:192.168.1.229:8472"
	inside.Manifest = &mi
	if err := d.honourable(&inside); err != nil {
		t.Fatalf("scope inside the ceiling refused: %v", err)
	}
	lockedWithScope := *jobAssignment
	ml := jobManifest
	ml.NetworkScope = "tcp:192.168.1.229:8472"
	lockedWithScope.Manifest = &ml
	if err := d.honourable(&lockedWithScope); err == nil {
		t.Fatal("a locked manifest naming destinations must be refused")
	}
	resolve := *jobAssignment
	resolve.Kind = "resolve"
	if err := d.honourable(&resolve); err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("resolve on a non-resolver: %v", err)
	}
}
