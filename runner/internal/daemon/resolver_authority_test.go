package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"urgit/runner/internal/inputpolicy"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
)

func resolverManifest(mode string) sig.Manifest {
	m := wireManifest("", "", "")
	m.Mode, m.Workflow, m.Event, m.Context, m.Trigger = mode, "", "", "", ""
	m.Sandbox = "container"
	if mode == "resolve-tag" {
		m.NetworkScope = inputpolicy.Prefix
	}
	return m
}

func testResolverAssignment(mode, repo, oid string) *ship.Assignment {
	m := resolverManifest(mode)
	m.Repo, m.OID = repo, oid
	var policy *inputpolicy.Policy
	if mode == "resolve-tag" {
		policy = &inputpolicy.Policy{Version: 1, Sources: []string{}}
	}
	return &ship.Assignment{AutomaticInputs: policy, ID: "0v1.asg", Attempt: wireAttempt, Candidate: wireCandidate, Repo: repo, Ref: m.Ref, OID: oid, WorkflowsOID: oid, Trust: m.Trust, ScratchRef: "refs/ci/run/" + wireCandidate, Kind: "resolve", DeadlineSeconds: 60, Manifest: &m}
}

func TestResolverPurposeIsAuthenticated(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	d := wireDaemonUnder(pub)
	makeBody := func(mode string) map[string]any {
		b := wireBody(t, priv, resolverManifest(mode), "")
		a := b["assignment"].(map[string]any)
		a["kind"], a["event-payload"] = "resolve", ""
		a["scratch-ref"] = "refs/ci/run/" + wireCandidate
		if mode == "resolve-tag" {
			a["automatic-inputs"] = &inputpolicy.Policy{Version: 1, Sources: []string{}}
		}
		return b
	}
	for _, mode := range []string{"resolve", "resolve-tag"} {
		a := pollWire(t, makeBody(mode))
		if err := d.verifyAssignment(a); err != nil {
			t.Fatalf("supported purpose %s: %v", mode, err)
		}
		if mode == "resolve-tag" && a.Sig.Version == sig.ManifestVersion {
			t.Fatal("old v5 runner would accept automatic authority")
		}
		if mode == "resolve" && a.Sig.Version != sig.ManifestVersion {
			t.Fatal("legacy explicit obligation lost its version")
		}
	}
	for _, mode := range []string{"", "event", "plan", "resolve-unknown"} {
		if err := d.verifyAssignment(pollWire(t, makeBody(mode))); err == nil {
			t.Errorf("ambiguous/unsupported signed purpose %q accepted", mode)
		}
	}
	for name, mutate := range map[string]func(map[string]any){
		"downgrade domain": func(a map[string]any) { a["sig"].(map[string]any)["version"] = sig.ManifestVersion },
		"remove purpose":   func(a map[string]any) { delete(a["manifest"].(map[string]any), "mode") },
		"elevate purpose":  func(a map[string]any) { a["manifest"].(map[string]any)["mode"] = "resolve" },
		"unsigned kind":    func(a map[string]any) { a["kind"] = "job" },
		"import mapping": func(a map[string]any) {
			a["mappings"] = []any{map[string]any{"from": "https://github.com/", "to": "http://127.0.0.1/"}}
		},
		"credentials":     func(a map[string]any) { a["grants"] = []any{map[string]any{"name": "X", "value": "fixture"}} },
		"scratch ref":     func(a map[string]any) { a["scratch-ref"] = "refs/heads/master" },
		"workflow source": func(a map[string]any) { a["workflows-oid"] = strings.Repeat("a", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			b := makeBody("resolve-tag")
			mutate(b["assignment"].(map[string]any))
			if err := d.verifyAssignment(pollWire(t, b)); err == nil {
				t.Fatal("altered automatic resolver authority accepted")
			}
		})
	}
}

type resolverTransport func(*http.Request) (*http.Response, error)

func (f resolverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Actual runResolve -> walker -> GitFetcher, with an in-process HTTP transport.
// This does not measure DNS, a TCP connection or native controller admission.
func TestAutomaticResolverRefusesHostFetchBeforeTransport(t *testing.T) {
	for _, tag := range []string{"v*", "never-this-tag"} {
		for _, target := range []string{
			"http://127.0.0.1/private/export", "http://10.1.2.3/", "http://169.254.169.254/",
			"http://[::1]/", "http://[fd00::1]/", "http://[::ffff:127.0.0.1]/",
			"https://approved.example/redirect-to-loopback", "https://address-changes.example/input",
		} {
			t.Run(tag+"/"+target, func(t *testing.T) {
				calls := 0
				old := http.DefaultTransport
				http.DefaultTransport = resolverTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if strings.Contains(r.URL.Path, "redirect") {
						return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"http://127.0.0.1/private/export"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("owned in-process fixture\n")), Request: r}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = old })
				sh, box := &fakeShip{}, &fakeBox{}
				d := newTestDaemon(t, box, sh, 1)
				d.cfg.Resolver = true
				d.checkout = func(_ context.Context, a *ship.Assignment, dir string) error {
					p := filepath.Join(dir, ".github", "workflows")
					if err := os.MkdirAll(p, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(p, "release.yml"), []byte(fmt.Sprintf("on:\n  push:\n    tags: ['%s']\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -fL %s\n", tag, target)), 0600)
				}
				m := resolverManifest("resolve-tag")
				a := &ship.Assignment{AutomaticInputs: &inputpolicy.Policy{Version: 1, Sources: []string{}}, ID: "0v1.asg", Attempt: wireAttempt, Candidate: wireCandidate, Repo: m.Repo, Ref: m.Ref, OID: wireOID, WorkflowsOID: wireOID, Trust: m.Trust, ScratchRef: "refs/ci/run/" + wireCandidate, Kind: "resolve", DeadlineSeconds: 60, Manifest: &m}
				d.handle(context.Background(), a)
				if calls != 0 {
					t.Errorf("automatic tagged YAML reached HTTP transport %d times", calls)
				}
				if len(box.ops) != 0 {
					t.Fatal("resolver executed a sandbox")
				}
				if len(sh.locks) != 2 {
					t.Fatalf("want inventory and explicit refused lock: %v", sh.locks)
				}
				var body struct {
					Nodes []struct {
						Refusal string `json:"refusal"`
						SHA256  string `json:"sha256"`
					} `json:"nodes"`
				}
				if err := json.Unmarshal([]byte(sh.locks[1]), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Nodes) != 1 || !strings.Contains(body.Nodes[0].Refusal, "automatic tag resolution has no external-input policy") || body.Nodes[0].SHA256 != "" {
					t.Fatalf("missing precise refusal: %+v", body)
				}
				if _, err := os.Stat(filepath.Join(d.cfg.WorkDir, "resolver-cache")); !os.IsNotExist(err) {
					t.Fatalf("automatic fetch touched shared import cache: %v", err)
				}
			})
		}
	}
}

func TestAutomaticCheckoutDropsAmbientAuthority(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL", "GIT_SSH_COMMAND", "GIT_ASKPASS", "GIT_TEMPLATE_DIR", "XDG_CONFIG_HOME", "NETRC"} {
		t.Setenv(key, "untrusted-inherited-fixture")
	}
	a := testResolverAssignment("resolve-tag", "demo", wireOID)
	cmd := checkoutCommand(context.Background(), a, []string{"git", "checkout", "--quiet", wireOID})
	for _, entry := range cmd.Env {
		if strings.Contains(entry, "untrusted-inherited-fixture") {
			t.Fatalf("inherited authority: %s", entry)
		}
	}
	args := strings.Join(cmd.Args, " ")
	for _, guard := range []string{"credential.helper=", "http.proxy=", "http.followRedirects=false", "protocol.allow=never", "protocol.http.allow=always", "protocol.https.allow=always", "core.hooksPath=/dev/null", "submodule.recurse=false", "init.templateDir="} {
		if !strings.Contains(args, guard) {
			t.Errorf("checkout lacks %s", guard)
		}
	}
	// The test inspects the actual command constructor; it does not start Git.
	if cmd.Process != nil {
		t.Fatal("unexpected process")
	}
}

func TestAutomaticResolverRefusesRemoteActionAndRegistry(t *testing.T) {
	for _, step := range []string{"uses: actions/cache@v4", "uses: docker://127.0.0.1:5000/image:tag", "uses: docker://ghcr.io/example/image:tag"} {
		t.Run(step, func(t *testing.T) {
			sh := &fakeShip{}
			d := newTestDaemon(t, &fakeBox{}, sh, 1)
			d.cfg.Resolver = true
			d.checkout = func(_ context.Context, _ *ship.Assignment, dir string) error {
				path := filepath.Join(dir, ".github", "workflows")
				if err := os.MkdirAll(path, 0700); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(path, "release.yml"), []byte("on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - "+step+"\n"), 0600)
			}
			d.handle(context.Background(), testResolverAssignment("resolve-tag", "demo", wireOID))
			if len(sh.locks) != 2 || !strings.Contains(sh.locks[1], "automatic tag resolution has no external-input policy") {
				t.Fatalf("missing source-controlled sibling refusal: %v", sh.locks)
			}
			if _, err := os.Stat(filepath.Join(d.cfg.WorkDir, "resolver-cache")); !os.IsNotExist(err) {
				t.Fatal("sibling fetch touched cache", err)
			}
		})
	}
}

// Authored wire fixture (native capture has its own required acceptance test).
func TestAutomaticSignedInputBinding(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	d := wireDaemonUnder(pub)
	policy := &inputpolicy.Policy{Version: 1, Sources: []string{"https://inputs.example/tool"}}
	m := resolverManifest("resolve-tag")
	m.NetworkScope, _ = policy.Scope()
	body := wireBody(t, priv, m, "")
	outer := body["assignment"].(map[string]any)
	outer["kind"], outer["event-payload"], outer["scratch-ref"], outer["automatic-inputs"] = "resolve", "", "refs/ci/run/"+wireCandidate, policy
	a := pollWire(t, body)
	if err := d.verifyAssignment(a); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ship.Assignment){
		"missing": func(a *ship.Assignment) { a.AutomaticInputs = nil },
		"empty":   func(a *ship.Assignment) { a.AutomaticInputs = &inputpolicy.Policy{Version: 1, Sources: []string{}} },
		"null":    func(a *ship.Assignment) { a.AutomaticInputs = &inputpolicy.Policy{Version: 1} },
		"version": func(a *ship.Assignment) { a.AutomaticInputs = &inputpolicy.Policy{Version: 2, Sources: policy.Sources} },
		"scope":   func(a *ship.Assignment) { m := *a.Manifest; m.NetworkScope = inputpolicy.Prefix; a.Manifest = &m },
		"legacy unsigned automatic": func(a *ship.Assignment) {
			m := *a.Manifest
			m.NetworkScope = ""
			a.Manifest = &m
			a.AutomaticInputs = nil
		},
		"downgrade": func(a *ship.Assignment) { m := *a.Manifest; m.Mode = "resolve"; a.Manifest = &m },
	} {
		t.Run(name, func(t *testing.T) {
			b := *a
			mutate(&b)
			if err := d.verifyAssignment(&b); err == nil {
				t.Fatal("policy binding bypass")
			}
		})
	}
}

func TestAutomaticLegacySignedObligationKeepsZeroFetchAuthority(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	d := wireDaemonUnder(pub)
	m := resolverManifest("resolve-tag")
	m.NetworkScope = ""
	body := wireBody(t, priv, m, "")
	outer := body["assignment"].(map[string]any)
	outer["kind"], outer["event-payload"], outer["scratch-ref"] = "resolve", "", "refs/ci/run/"+wireCandidate
	a := pollWire(t, body)
	if err := d.verifyAssignment(a); err != nil {
		t.Fatal("persisted zero-authority obligation:", err)
	}
	// A newly configured runner ceiling cannot become a repository grant.
	sh, box := &fakeShip{}, &fakeBox{}
	runner := newTestDaemon(t, box, sh, 1)
	runner.cfg.Resolver = true
	runner.cfg.AutomaticInputs = []string{"https://inputs.example/tool"}
	runner.checkout = func(_ context.Context, _ *ship.Assignment, dir string) error {
		p := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(p, "release.yml"), []byte("on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl https://inputs.example/tool\n"), 0600)
	}
	runner.handle(context.Background(), a)
	if len(sh.locks) != 2 || !strings.Contains(sh.locks[1], "no external-input policy") {
		t.Fatalf("legacy gained grant: %v", sh.locks)
	}
}
