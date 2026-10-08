package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"urgit/runner/internal/guest"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/provenance"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
)

// Bundle is the immutable input the guest receives (contract §6),
// assembled and verified on the host: the exact candidate tree with the
// branch policy's harness paths taken from the baseline, the locked
// actions materialized into act's cache, the inventoried downloads, the
// single-job projection, and bundle.json naming every identity.
type Bundle struct {
	Dir          string
	WorkspaceSHA string
	WorkflowsOID string
	Replaced     []string
	Actions      int
	Downloads    int
	// Served lists each action served from a ship mirror, `uses <- mirror @ commit`
	Served []string
	// Tools lists the tool-cache tools the bundle seeded (top-level
	// directories of toolcache/, e.g. "go")
	Tools []string
	// Images lists the locked container images staged as archives, to be
	// loaded into the sandbox's Docker before the job (never pulled)
	Images []LockedImage
}

// LockedImage is one container image the lock pinned: the reference as
// the workflow wrote it, the archive under downloads/ (by sha256), the
// local name the archive loads under and the image id it must load as.
type LockedImage struct {
	Ref     string
	Archive string
	Tag     string
	ID      string
}

type bundleManifest struct {
	Attempt       string   `json:"attempt"`
	Candidate     string   `json:"candidate"`
	OID           string   `json:"oid"`
	Baseline      string   `json:"baseline"`
	WorkflowsOID  string   `json:"workflows_oid"`
	HarnessPaths  []string `json:"harness_paths"`
	ReplacedPaths []string `json:"replaced_paths"`
	Lock          string   `json:"lock"`
	WorkspaceSHA  string   `json:"workspace_sha256"`
	Mode          string   `json:"mode"`
	Actions       []string `json:"actions"`
	Downloads     []string `json:"downloads"`
}

// assembleBundle builds work/bundle. Nothing from the runner's own
// configuration, the bearer, the CI key or the read capability is
// written under it.
func (d *Daemon) assembleBundle(ctx context.Context, a *ship.Assignment, work string, env sandbox.ExecEnv) (*Bundle, error) {
	dir := filepath.Join(work, "bundle")
	src := filepath.Join(dir, "src")
	for _, sub := range []string{"src", "actions", "downloads", "toolcache", "projected", "cache", "artifacts"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	if err := d.checkout(ctx, a, src); err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}
	b := &Bundle{Dir: dir, WorkflowsOID: a.OID}
	baseline, mode := "", "required"
	if a.Manifest != nil {
		baseline, mode = a.Manifest.Baseline, a.Manifest.Mode
	}
	if a.WorkflowsOID != "" {
		b.WorkflowsOID = a.WorkflowsOID
	}
	// the approved harness (D5): under a required or shadow run every
	// harness path is the baseline's, byte for byte; the candidate keeps
	// everything else exactly. a trial runs its own harness.
	if baseline != "" && baseline != a.OID && mode != "trial" && len(a.HarnessPaths) > 0 {
		replaced, err := overlayHarness(ctx, src, baseline, a.HarnessPaths)
		if err != nil {
			return nil, fmt.Errorf("baseline harness: %w", err)
		}
		b.Replaced = replaced
		b.WorkflowsOID = baseline
	}
	var actions []string
	if a.Lock != nil {
		for _, node := range a.Lock.Nodes {
			if node.Mirror == "" || node.MirrorCommit == "" {
				continue // local actions and downloads live elsewhere; container images are the guest's
			}
			target := filepath.Join(dir, "actions", plan.SafeFilename(node.Uses))
			if err := d.mirror(ctx, a, node, target); err != nil {
				return nil, fmt.Errorf("action %s: %w", node.Uses, err)
			}
			actions = append(actions, node.Uses)
			b.Served = append(b.Served, fmt.Sprintf("%s <- %s @ %s", node.Uses, node.Mirror, node.MirrorCommit))
		}
	}
	b.Actions = len(actions)
	var downloads []string
	index := []map[string]any{}
	images := map[string]string{}
	for _, dl := range a.Downloads {
		target := filepath.Join(dir, "downloads", dl.SHA256)
		if err := d.fetchDownload(ctx, a, dl, target); err != nil {
			return nil, fmt.Errorf("download %s: %w", dl.URL, err)
		}
		downloads = append(downloads, dl.URL)
		index = append(index, map[string]any{"url": dl.URL, "uses": dl.Uses, "kind": dl.Kind, "sha256": dl.SHA256, "size": dl.Size, "file": dl.SHA256})
		// a locked container image (P03): the archive's verified bytes are
		// loaded into the sandbox's Docker under the lock's name before
		// the job, and the projection names that image, so act finds it
		// locally and pulls nothing
		if dl.Kind == "container" {
			if !strings.HasPrefix(dl.Digest, "sha256:") || !strings.HasPrefix(dl.ID, "sha256:") {
				return nil, fmt.Errorf("image %s: the lock carries no digest and id", dl.URL)
			}
			tag := provenance.LockedTag(dl.URL, dl.Digest)
			images[dl.URL] = tag
			if !hasImage(b.Images, tag) {
				b.Images = append(b.Images, LockedImage{Ref: dl.URL, Archive: dl.SHA256, Tag: tag, ID: dl.ID})
				b.Served = append(b.Served, fmt.Sprintf("%s <- %s (image %s, id %s)", dl.URL, dl.SHA256[:12], tag, dl.ID))
			}
			continue
		}
		// a locked toolchain is seeded into the tool cache (P06): the
		// archive's verified bytes unpacked where the setup action looks,
		// with the marker that says the version is complete
		if strings.HasPrefix(dl.Uses, "toolchain:go@") && dl.Subpath != "" {
			if err := seedGoToolchain(target, filepath.Join(dir, "toolcache"), dl.Subpath); err != nil {
				return nil, fmt.Errorf("toolchain %s: %w", dl.Uses, err)
			}
			b.Served = append(b.Served, fmt.Sprintf("%s <- %s (tool cache %s)", dl.Uses, dl.SHA256[:12], dl.Subpath))
			if tool := strings.SplitN(dl.Subpath, "/", 2)[0]; !contains(b.Tools, tool) {
				b.Tools = append(b.Tools, tool)
			}
		}
	}
	b.Downloads = len(downloads)
	idx, _ := json.MarshalIndent(index, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "downloads", "index.json"), idx, 0o644); err != nil {
		return nil, err
	}
	if a.Kind == "job" && a.Workflow != "" && a.Job != "" {
		original, err := os.ReadFile(filepath.Join(src, ".github", "workflows", a.Workflow))
		if err != nil {
			return nil, fmt.Errorf("workflow: %w", err)
		}
		projected, err := plan.ProjectImages(original, a.Job, a.Attempt, a.Workflow, images)
		if err != nil {
			return nil, fmt.Errorf("projection: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "projected", a.Workflow), projected, 0o644); err != nil {
			return nil, err
		}
	}
	sha, err := guest.WorkspaceDigest(src)
	if err != nil {
		return nil, fmt.Errorf("workspace digest: %w", err)
	}
	b.WorkspaceSHA = sha
	lock := ""
	if a.Lock != nil {
		lock = a.Lock.Digest
	}
	if actions == nil {
		actions = []string{}
	}
	if downloads == nil {
		downloads = []string{}
	}
	if b.Replaced == nil {
		b.Replaced = []string{}
	}
	m := bundleManifest{
		Attempt: a.Attempt, Candidate: a.Candidate, OID: a.OID, Baseline: baseline, WorkflowsOID: b.WorkflowsOID,
		HarnessPaths: a.HarnessPaths, ReplacedPaths: b.Replaced, Lock: lock, WorkspaceSHA: sha, Mode: mode, Actions: actions, Downloads: downloads,
	}
	if m.HarnessPaths == nil {
		m.HarnessPaths = []string{}
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), data, 0o644); err != nil {
		return nil, err
	}
	return b, nil
}

// overlayHarness replaces every harness path in the checkout with the
// baseline revision's bytes (a path the baseline lacks is removed) and
// reports which paths ended up different from the candidate's own.
func overlayHarness(ctx context.Context, src, baseline string, paths []string) ([]string, error) {
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", src}, args...)...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := git("cat-file", "-e", baseline+"^{commit}"); err != nil {
		return nil, fmt.Errorf("baseline revision %s is not in the clone (not reachable from the ship's refs)", baseline)
	}
	var replaced []string
	for _, p := range paths {
		p = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(p), "/"), "/")
		if p == "" || p == "." || strings.Contains(p, "..") {
			return nil, fmt.Errorf("harness path %q is not a repository path", p)
		}
		// does the candidate differ from the baseline under this path?
		_, diffErr := git("diff", "--quiet", baseline, "HEAD", "--", p)
		differs := diffErr != nil
		exists, _ := git("ls-tree", "--name-only", baseline, "--", p)
		if err := os.RemoveAll(filepath.Join(src, filepath.FromSlash(p))); err != nil {
			return nil, err
		}
		if exists != "" {
			if out, err := git("checkout", baseline, "--", p); err != nil {
				return nil, fmt.Errorf("checkout %s from %s: %s", p, baseline, out)
			}
		} else {
			_, _ = git("rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", p)
		}
		if differs {
			replaced = append(replaced, p)
		}
	}
	return replaced, nil
}

// gitMirror materializes one locked action into act's cache layout: a
// clone of the ship's mirror repository at the mirror commit, whose tree
// must be the locked upstream tree, with the original ref names pointing
// at it and `origin` set to the upstream URL act computes — so act's
// offline mode resolves `owner/repo@ref` here and fetches nothing.
func (d *Daemon) gitMirror(ctx context.Context, a *ship.Assignment, node ship.LockNode, dir string) error {
	url := strings.TrimRight(d.cfg.ShipURL, "/") + "/git/" + node.Mirror
	auth := gitAuth(a)
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(scrubToken(string(out), a.ReadToken)), err
	}
	if out, err := run(append(append([]string{"git"}, auth...), "clone", "--quiet", "--no-checkout", url, dir)...); err != nil {
		return fmt.Errorf("mirror %s: %s", node.Mirror, out)
	}
	if _, err := run("git", "-C", dir, "cat-file", "-e", node.MirrorCommit+"^{commit}"); err != nil {
		return fmt.Errorf("mirror %s does not hold commit %s", node.Mirror, node.MirrorCommit)
	}
	tree, err := run("git", "-C", dir, "rev-parse", node.MirrorCommit+"^{tree}")
	if err != nil {
		return err
	}
	if node.Tree != "" && tree != node.Tree {
		return fmt.Errorf("mirror commit %s has tree %s, not the locked tree %s (tampered or wrong mirror)", node.MirrorCommit, tree, node.Tree)
	}
	if out, err := run("git", "-C", dir, "checkout", "--quiet", node.MirrorCommit); err != nil {
		return fmt.Errorf("checkout: %s", out)
	}
	ref := node.Ref
	if ref == "" {
		ref = node.Commit
	}
	for _, name := range []string{"refs/tags/" + ref, "refs/heads/" + ref, "refs/action-cache-offline/" + ref} {
		if _, err := run("git", "-C", dir, "update-ref", name, node.MirrorCommit); err != nil {
			return err
		}
	}
	if node.Commit != "" && node.Commit != node.MirrorCommit {
		// act may resolve a full-sha `uses:`; make the upstream sha name the mirror commit too
		_, _ = run("git", "-C", dir, "update-ref", "refs/tags/"+node.Commit, node.MirrorCommit)
	}
	if node.Origin != "" {
		if _, err := run("git", "-C", dir, "remote", "set-url", "origin", node.Origin); err != nil {
			return err
		}
	}
	return nil
}

// storeDownload stages one inventoried download from the ship's object
// store (the content-addressed key the lock names, read through the
// presigned URL the assignment carries) and verifies the sha256 before
// it may be used. Nothing is fetched from the origin.
func (d *Daemon) storeDownload(ctx context.Context, a *ship.Assignment, dl ship.Download, target string) error {
	if dl.Mirror != "store" || dl.Store == "" || dl.SHA256 == "" {
		return errors.New("download is not mirrored in the ship's store (no presigned object)")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", dl.Store, nil)
	if err != nil {
		return err
	}
	resp, err := d.storeClient().Do(req)
	if err != nil {
		return fmt.Errorf("store: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("store answered %d for %s", resp.StatusCode, dl.Path)
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	h := sha256.New()
	limit := dl.Size + 1
	if limit <= 0 {
		limit = 2 << 30
	}
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit))
	f.Close()
	if err != nil {
		os.Remove(target)
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != dl.SHA256 || (dl.Size > 0 && n != dl.Size) {
		os.Remove(target)
		return fmt.Errorf("download %s: sha256 %s / %d bytes is not the inventoried %s / %d", dl.URL, got, n, dl.SHA256, dl.Size)
	}
	return nil
}

func (d *Daemon) storeClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// seedGoToolchain unpacks a go<version>.linux-amd64.tar.gz (whose
// sha256 was verified by the fetch) into <toolcache>/<subpath> so that
// <subpath>/bin/go exists, and writes the <subpath>.complete marker
// actions/setup-go checks before it would download.
func seedGoToolchain(archive, toolcache, subpath string) error {
	dest := filepath.Join(toolcache, filepath.FromSlash(subpath))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	// the archive's single top-level directory is go/; strip it
	cmd := exec.Command("tar", "-xzf", archive, "-C", dest, "--strip-components=1", "--no-same-owner", "--no-same-permissions")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unpack: %s", strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "go")); err != nil {
		return errors.New("the archive holds no bin/go")
	}
	return os.WriteFile(dest+".complete", []byte(""), 0o644)
}

func hasImage(images []LockedImage, tag string) bool {
	for _, i := range images {
		if i.Tag == tag {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
