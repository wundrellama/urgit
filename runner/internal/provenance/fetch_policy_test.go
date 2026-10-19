package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// All effectful consumers use the same authority gate, including cache reads
// and mirror operations. No command or DNS lookup is run by these negatives.
func TestFetchPurposeGuardsEveryConsumer(t *testing.T) {
	for _, purpose := range []FetchPurpose{"", AutomaticTag, "unsupported"} {
		t.Run(string(purpose), func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "cache")
			f := &GitFetcher{Purpose: purpose, Cache: cache}
			for _, origin := range []string{"https://github.com/actions/cache", "http://10.0.0.1/a", "file:///fixture", "/fixture", "ssh://git@localhost/a", "ext::helper"} {
				if _, err := f.Resolve(origin, "v1"); err == nil {
					t.Fatal("resolve allowed", origin)
				}
				if _, err := f.ReadFile(origin, "abc", "action.yml"); err == nil {
					t.Fatal("cache read allowed", origin)
				}
				if _, err := f.MirrorCommit(origin, "v1", "abc", "def"); err == nil {
					t.Fatal("mirror commit allowed", origin)
				}
				if _, err := f.PushMirror(origin, "abc", "http://localhost", "mirror", "fixture"); err == nil {
					t.Fatal("mirror push allowed", origin)
				}
			}
			for _, target := range []string{"http://127.1/", "http://2130706433/", "http://10.0.0.1/", "http://172.16.0.1/", "http://192.168.0.1/", "http://169.254.1.1/", "http://[::1]/", "http://[fe80::1%25lo]/", "http://[fd00::1]/", "http://[::ffff:127.0.0.1]/", "https://public.example/", "file:///fixture"} {
				if _, err := f.Download(target); err == nil {
					t.Fatal("download allowed", target)
				}
			}
			for _, ref := range []string{"alpine:3", "ghcr.io/owner/image:tag", "127.0.0.1:5000/image:tag", "[::1]:5000/image:tag", "file:///fixture"} {
				if _, err := f.Image(ref); err == nil {
					t.Fatal("registry allowed", ref)
				}
			}
			if _, err := os.Stat(cache); !os.IsNotExist(err) {
				t.Fatalf("gate touched cache: %v", err)
			}
		})
	}
}

// Explicit maintainer authority remains capable of importing approved public
// dependencies, including their redirects. This is an in-process transport
// control, not a claim about public network reachability.
func TestExplicitImportDownloadAndRedirectPositive(t *testing.T) {
	const content = "approved dependency fixture\n"
	var visited []string
	old := http.DefaultTransport
	http.DefaultTransport = policyTransport(func(r *http.Request) (*http.Response, error) {
		visited = append(visited, r.URL.String())
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(content)), Request: r}
		if r.URL.Host == "approved.example" {
			resp.StatusCode, resp.Header = 302, http.Header{"Location": {"https://cdn.example/input"}}
		}
		return resp, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	f := &GitFetcher{Purpose: PolicyImport, Cache: t.TempDir()}
	got, err := f.Download("https://approved.example/input")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	if len(visited) != 2 || got.SHA256 != hex.EncodeToString(sum[:]) || got.Size != int64(len(content)) {
		t.Fatalf("%v %+v", visited, got)
	}
	data, err := os.ReadFile(f.DownloadPath(got.SHA256))
	if err != nil || string(data) != content {
		t.Fatalf("cache %q: %v", data, err)
	}
}

// Retained real listener control for the parent if this constructor's sandbox
// refuses sockets. Never skip or weaken it merely to make the local run green.
func TestFetchPolicyOwnedLoopback(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); _, _ = io.WriteString(w, "owned fixture") }))
	defer server.Close()
	auto := &GitFetcher{Purpose: AutomaticTag, Cache: t.TempDir()}
	if _, err := auto.Download(server.URL); err == nil || hits.Load() != 0 {
		t.Fatalf("automatic fetch %v, hits %d", err, hits.Load())
	}
	explicit := &GitFetcher{Purpose: PolicyImport, Cache: t.TempDir()}
	if _, err := explicit.Download(server.URL); err != nil || hits.Load() != 1 {
		t.Fatalf("explicit import %v, hits %d", err, hits.Load())
	}
}

func TestMetadataReadsStayInCapturedTree(t *testing.T) {
	root := checkout(t, map[string]string{".github/workflows/ok.yml": "on: push\njobs: {}\n", "local/action.yml": "runs:\n  using: composite\n  steps: []\n"})
	if got, err := ReadTreeFile(root, "local/action.yml"); err != nil || len(got) == 0 {
		t.Fatalf("local positive: %v", err)
	}
	if files, err := WorkflowFiles(root); err != nil || len(files) != 1 {
		t.Fatalf("workflow positive: %v %v", files, err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "action.yml"), []byte("owned outside canary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../action.yml", "escape/action.yml"} {
		if data, err := ReadTreeFile(root, path); err == nil {
			t.Fatalf("escaped %s: %q", path, data)
		}
	}
	if err := os.RemoveAll(filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkflowFiles(root); err == nil {
		t.Fatal("workflow parent symlink escaped")
	}
}
