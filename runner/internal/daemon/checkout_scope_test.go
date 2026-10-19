package daemon

import (
	"context"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"urgit/runner/internal/config"
	"urgit/runner/internal/ship"
)

// a repository served over real Smart HTTP by git-http-backend, with a
// candidate scratch ref, a baseline ref, a pull request base, and an
// unrelated branch and tag carrying a large blob the job never needs
type scopeRepo struct {
	url, candidate, baseline, base, unrelated string
	mu                                        sync.Mutex
	wants                                     []string
}

func scopeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_AUTHOR_DATE=1790700000 +0000", "GIT_COMMITTER_DATE=1790700000 +0000")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newScopeRepo(t *testing.T) *scopeRepo {
	t.Helper()
	backend := filepath.Join(strings.TrimSpace(func() string {
		out, err := exec.Command("git", "--exec-path").Output()
		if err != nil {
			t.Skipf("git unavailable: %v", err)
		}
		return string(out)
	}()), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend unavailable: %v", err)
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	scopeGit(t, root, "init", "--quiet", "-b", "master", src)
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "base\n")
	scopeGit(t, src, "add", ".")
	scopeGit(t, src, "commit", "--quiet", "-m", "base")
	r := &scopeRepo{base: scopeGit(t, src, "rev-parse", "HEAD")}
	write("README.md", "baseline\n")
	scopeGit(t, src, "commit", "--quiet", "-am", "baseline")
	r.baseline = scopeGit(t, src, "rev-parse", "HEAD")
	write("code.txt", "candidate\n")
	scopeGit(t, src, "add", ".")
	scopeGit(t, src, "commit", "--quiet", "-m", "candidate")
	r.candidate = scopeGit(t, src, "rev-parse", "HEAD")
	scopeGit(t, src, "checkout", "--quiet", "-b", "unrelated", r.base)
	write("big.bin", strings.Repeat("unrelated history the job never needs\n", 4096))
	scopeGit(t, src, "add", ".")
	scopeGit(t, src, "commit", "--quiet", "-m", "unrelated")
	r.unrelated = scopeGit(t, src, "rev-parse", "HEAD")
	scopeGit(t, src, "tag", "v-unrelated", r.unrelated)
	scopeGit(t, src, "update-ref", "refs/ci/run/0v1.cand", r.candidate)
	scopeGit(t, src, "update-ref", "refs/ci/baseline/master", r.baseline)
	// the base is only reachable from master's history, like a PR base
	scopeGit(t, src, "checkout", "--quiet", "master")
	bare := filepath.Join(root, "repo.git")
	scopeGit(t, root, "clone", "--quiet", "--bare", "--mirror", src, bare)
	scopeGit(t, bare, "config", "http.uploadpack", "true")
	scopeGit(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	handler := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			body, _ := readAllLimited(req, 1<<20)
			r.mu.Lock()
			for _, line := range strings.Split(body, "\n") {
				if i := strings.Index(line, "want "); i >= 0 {
					r.wants = append(r.wants, strings.Fields(line[i+5:])[0])
				}
			}
			r.mu.Unlock()
			req.Body = readCloser(body)
		}
		req.URL.Path = strings.Replace(req.URL.Path, "/git/demo", "/repo.git", 1)
		handler.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	r.url = srv.URL
	return r
}

// readAllLimited reads a request body (protocol v2 fetch requests are
// small) so its want lines can be recorded before git-http-backend
// reads it; gzip-encoded bodies are left unparsed
func readAllLimited(req *http.Request, limit int64) (string, error) {
	b, err := io.ReadAll(io.LimitReader(req.Body, limit))
	_ = req.Body.Close()
	return string(b), err
}

func readCloser(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func TestCheckoutFetchesOnlyWhatTheJobNeeds(t *testing.T) {
	r := newScopeRepo(t)
	d := &Daemon{cfg: &config.Config{ShipURL: r.url}}
	a := &ship.Assignment{Repo: "demo", Ref: "refs/heads/master", OID: r.candidate, Base: r.base,
		Candidate: "0v1.cand", ScratchRef: "refs/ci/run/0v1.cand", BaselineRef: "refs/ci/baseline/master"}
	dir := filepath.Join(t.TempDir(), "src")
	if err := d.gitCheckout(context.Background(), a, dir); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got := scopeGit(t, dir, "rev-parse", "HEAD"); got != r.candidate {
		t.Fatalf("HEAD %s, want candidate %s", got, r.candidate)
	}
	if got := scopeGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "master" {
		t.Fatalf("branch %q, want master", got)
	}
	for _, need := range []string{r.candidate, r.baseline, r.base} {
		scopeGit(t, dir, "cat-file", "-e", need+"^{commit}")
	}
	// the job can diff against the base, as a pull-request workflow does
	if got := scopeGit(t, dir, "diff", "--name-only", r.base, r.candidate); got != "README.md\ncode.txt" {
		t.Fatalf("diff against base: %q", got)
	}
	// nothing of the unrelated branch or tag was transferred
	if err := exec.Command("git", "-C", dir, "cat-file", "-e", r.unrelated).Run(); err == nil {
		t.Fatalf("unrelated commit %s was fetched", r.unrelated)
	}
	if tags := scopeGit(t, dir, "tag", "--list"); tags != "" {
		t.Fatalf("tags were fetched: %q", tags)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.wants {
		if w == r.unrelated {
			t.Fatalf("the runner asked for the unrelated commit")
		}
	}
	if len(r.wants) == 0 {
		t.Fatalf("no upload-pack wants observed; the test did not see the fetch")
	}
}

func TestCheckoutWithoutBaseOrBaseline(t *testing.T) {
	r := newScopeRepo(t)
	d := &Daemon{cfg: &config.Config{ShipURL: r.url}}
	a := &ship.Assignment{Repo: "demo", Ref: "refs/tags/v1", OID: r.candidate,
		Candidate: "0v1.cand", ScratchRef: "refs/ci/run/0v1.cand"}
	dir := filepath.Join(t.TempDir(), "src")
	if err := d.gitCheckout(context.Background(), a, dir); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got := scopeGit(t, dir, "rev-parse", "HEAD"); got != r.candidate {
		t.Fatalf("HEAD %s, want candidate %s", got, r.candidate)
	}
	if got := scopeGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "ci-candidate" {
		t.Fatalf("branch %q, want ci-candidate", got)
	}
}

func TestCheckoutRefusesMissingCandidate(t *testing.T) {
	r := newScopeRepo(t)
	d := &Daemon{cfg: &config.Config{ShipURL: r.url}}
	a := &ship.Assignment{Repo: "demo", Ref: "refs/heads/master", OID: r.unrelated,
		Candidate: "0v1.cand", ScratchRef: "refs/ci/run/0v1.cand"}
	if err := d.gitCheckout(context.Background(), a, filepath.Join(t.TempDir(), "src")); err == nil {
		t.Fatalf("checkout of a commit not under the scratch ref succeeded")
	}
}
