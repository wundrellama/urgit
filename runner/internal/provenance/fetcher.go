package provenance

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GitFetcher is the import-time Fetcher (rider 03: an explicit import may
// fetch): Git metadata through `git` into a bare cache per origin, OCI
// digests through skopeo, downloads through HTTP into a content-addressed
// cache. Every operation reads; nothing fetched is executed. The cache
// is the resolver's working set, never an execution input: execution
// reads the ship's mirrors.
type GitFetcher struct {
	Cache   string
	Timeout time.Duration
	// Log receives one line per fetch (the record of what the import
	// touched; P05's tripwire compares it with the deny log)
	Log func(string)
	// images memoizes the archives this import already copied, by digest
	images map[string]Image
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (g *GitFetcher) logf(format string, args ...any) {
	if g.Log != nil {
		g.Log(fmt.Sprintf(format, args...))
	}
}

func (g *GitFetcher) repoDir(origin string) string {
	return filepath.Join(g.Cache, "git", safeName.ReplaceAllString(strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://"), "_")+".git")
}

func (g *GitFetcher) git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *GitFetcher) timeout() time.Duration {
	if g.Timeout == 0 {
		return 5 * time.Minute
	}
	return g.Timeout
}

func (g *GitFetcher) ensureRepo(origin string) (string, error) {
	dir := g.repoDir(origin)
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if _, err := g.git(dir, "init", "--bare", "--quiet"); err != nil {
		return "", err
	}
	return dir, nil
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Resolve turns origin@ref into a commit and its tree: a tag or branch
// is listed on the remote and fetched shallowly; a full sha is fetched
// directly. Both go through the cache so the same commit is fetched once.
func (g *GitFetcher) Resolve(origin, ref string) (Resolved, error) {
	dir, err := g.ensureRepo(origin)
	if err != nil {
		return Resolved{}, err
	}
	target := ref
	if !shaRe.MatchString(ref) {
		out, err := g.git(dir, "ls-remote", origin, "refs/tags/"+ref+"^{}", "refs/tags/"+ref, "refs/heads/"+ref)
		g.logf("ls-remote %s %s", origin, ref)
		if err != nil {
			return Resolved{}, fmt.Errorf("ls-remote %s: %v", origin, err)
		}
		// prefer the peeled tag, then the tag, then the branch
		lines := strings.Split(out, "\n")
		pick := ""
		for _, want := range []string{"refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref} {
			for _, l := range lines {
				f := strings.Fields(l)
				if len(f) == 2 && f[1] == want {
					pick = f[0]
					break
				}
			}
			if pick != "" {
				break
			}
		}
		if pick == "" {
			return Resolved{}, fmt.Errorf("no such ref %s at %s", ref, origin)
		}
		target = pick
	}
	if _, err := g.git(dir, "cat-file", "-e", target+"^{commit}"); err != nil {
		g.logf("fetch %s %s", origin, target)
		if _, err := g.git(dir, "fetch", "--quiet", "--depth=1", origin, target); err != nil {
			return Resolved{}, fmt.Errorf("fetch %s@%s: %v", origin, target, err)
		}
	}
	commit, err := g.git(dir, "rev-parse", target+"^{commit}")
	if err != nil {
		return Resolved{}, err
	}
	tree, err := g.git(dir, "rev-parse", commit+"^{tree}")
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Commit: commit, Tree: tree}, nil
}

// ReadFile reads one file at a commit from the cache.
func (g *GitFetcher) ReadFile(origin, commit, path string) ([]byte, error) {
	dir := g.repoDir(origin)
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "-p", commit+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return nil, os.ErrNotExist
	}
	return out, nil
}

// Image pins an OCI image reference through skopeo: its manifest digest,
// then a copy of exactly that manifest (linux/amd64) into a docker-archive
// in the content-addressed cache, measured like a download. The archive
// is what the ship's store mirrors and what a runner `docker load`s
// before the job (BRIEF-CI-P4: "OCI images … may use a digest-addressed
// … object store"); its RepoTags carry LockedTag, so the loaded image
// answers to the lock's name and never to a registry pull. The image id
// (the sha256 of the config blob, what Docker reports as .Id after the
// load) is measured from the archive here and verified by the runner.
func (g *GitFetcher) Image(ref string) (Image, error) {
	if _, err := exec.LookPath("skopeo"); err != nil {
		return Image{}, errors.New("skopeo is not installed on the resolver")
	}
	g.logf("skopeo inspect %s", ref)
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	out, err := exec.CommandContext(ctx, "skopeo", "inspect", "--no-tags", "--override-os", "linux", "--override-arch", "amd64", "docker://"+ref).Output()
	if err != nil {
		return Image{}, fmt.Errorf("skopeo inspect %s: %v", ref, err)
	}
	var m struct {
		Digest string `json:"Digest"`
	}
	if err := json.Unmarshal(out, &m); err != nil || !strings.HasPrefix(m.Digest, "sha256:") {
		return Image{}, fmt.Errorf("skopeo inspect %s: no digest", ref)
	}
	if img, ok := g.images[m.Digest]; ok {
		return img, nil
	}
	if err := os.MkdirAll(filepath.Join(g.Cache, "downloads"), 0o755); err != nil {
		return Image{}, err
	}
	tmp, err := os.MkdirTemp(filepath.Join(g.Cache, "downloads"), "img-")
	if err != nil {
		return Image{}, err
	}
	defer os.RemoveAll(tmp)
	pinned := ImageName(ref) + "@" + m.Digest
	tag := LockedTag(ref, m.Digest)
	archive := filepath.Join(tmp, "image.tar")
	g.logf("skopeo copy %s -> docker-archive (%s)", pinned, tag)
	cctx, ccancel := context.WithTimeout(context.Background(), g.timeout())
	defer ccancel()
	if out, err := exec.CommandContext(cctx, "skopeo", "copy", "--override-os", "linux", "--override-arch", "amd64", "docker://"+pinned, "docker-archive:"+archive+":"+tag).CombinedOutput(); err != nil {
		return Image{}, fmt.Errorf("skopeo copy %s: %s", pinned, strings.TrimSpace(string(out)))
	}
	id, err := ArchiveImageID(archive)
	if err != nil {
		return Image{}, fmt.Errorf("image archive %s: %w", pinned, err)
	}
	f, err := os.Open(archive)
	if err != nil {
		return Image{}, err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	f.Close()
	if err != nil {
		return Image{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(archive, g.DownloadPath(sum)); err != nil {
		return Image{}, err
	}
	img := Image{Digest: m.Digest, SHA256: sum, Size: n, ID: id}
	if g.images == nil {
		g.images = map[string]Image{}
	}
	g.images[m.Digest] = img
	return img, nil
}

// ImageName is a reference without its tag or digest: what a digest pins.
func ImageName(ref string) string {
	name := ref
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	// a tag follows the last colon after the last slash (a registry port
	// sits before the first slash)
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i]
	}
	return name
}

// LockedTag is the local name a locked image is loaded under on a
// runner: the reference's path without its registry, under urgit-locked,
// tagged with the manifest digest — a name no registry serves, so a
// missing load is a missing image, never a pull.
func LockedTag(ref, digest string) string {
	path := strings.ToLower(ImageName(ref))
	if i := strings.Index(path, "/"); i >= 0 {
		host := path[:i]
		if strings.ContainsAny(host, ".:") || host == "localhost" {
			path = path[i+1:]
		}
	}
	return "urgit-locked/" + path + ":" + strings.Replace(digest, ":", "-", 1)
}

// ArchiveImageID reads a docker-archive's manifest and returns the id
// of the image it holds: sha256 of its config blob (what a classic
// Docker image store reports as .Id on load).
func ArchiveImageID(archive string) (string, error) {
	id, _, err := ArchiveIdentity(archive)
	return id, err
}

// ArchiveIdentity reads a docker-archive's single image: the config
// blob's digest (the classic image id) and the config's rootfs diff ids
// (the uncompressed layers' digests, which every Docker image store
// reports as .RootFS.Layers). A runner verifies a loaded image by
// either, since a containerd-backed store reports the manifest digest
// as .Id instead of the config's.
func ArchiveIdentity(archive string) (string, []string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	configs := map[string][]byte{}
	var manifest []struct {
		Config string `json:"Config"`
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		switch {
		case name == "manifest.json":
			data, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return "", nil, err
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return "", nil, fmt.Errorf("manifest.json: %w", err)
			}
		case strings.HasSuffix(name, ".json") || strings.HasPrefix(name, "blobs/sha256/"):
			data, err := io.ReadAll(io.LimitReader(tr, 16<<20))
			if err != nil {
				return "", nil, err
			}
			configs[name] = data
		}
	}
	if len(manifest) != 1 {
		return "", nil, fmt.Errorf("manifest.json names %d images, not one", len(manifest))
	}
	data, ok := configs[strings.TrimPrefix(manifest[0].Config, "./")]
	if !ok {
		return "", nil, fmt.Errorf("config %s is not in the archive", manifest[0].Config)
	}
	sum := sha256.Sum256(data)
	var cfg struct {
		RootFS struct {
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", nil, fmt.Errorf("config %s: %w", manifest[0].Config, err)
	}
	if len(cfg.RootFS.DiffIDs) == 0 {
		return "", nil, fmt.Errorf("config %s names no rootfs layers", manifest[0].Config)
	}
	return "sha256:" + hex.EncodeToString(sum[:]), cfg.RootFS.DiffIDs, nil
}

// Download fetches a URL into the content-addressed cache and reports
// its sha256 and size; the bytes stay at DownloadPath for mirroring.
func (g *GitFetcher) Download(url string) (Downloaded, error) {
	g.logf("download %s", url)
	if err := os.MkdirAll(filepath.Join(g.Cache, "downloads"), 0o755); err != nil {
		return Downloaded{}, err
	}
	tmp, err := os.CreateTemp(filepath.Join(g.Cache, "downloads"), "dl-")
	if err != nil {
		return Downloaded{}, err
	}
	defer os.Remove(tmp.Name())
	client := &http.Client{Timeout: g.timeout()}
	resp, err := client.Get(url)
	if err != nil {
		tmp.Close()
		return Downloaded{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tmp.Close()
		return Downloaded{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 2<<30))
	tmp.Close()
	if err != nil {
		return Downloaded{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(tmp.Name(), g.DownloadPath(sum)); err != nil {
		return Downloaded{}, err
	}
	return Downloaded{SHA256: sum, Size: n}, nil
}

// DownloadPath is where a downloaded object's bytes sit in the cache.
func (g *GitFetcher) DownloadPath(sha string) string {
	return filepath.Join(g.Cache, "downloads", sha)
}

// MirrorCommit makes the deterministic mirror commit for a resolved
// action: a parentless commit over the locked tree with fixed author,
// committer and date, so the same tree always mirrors to the same
// commit id. It is created in the origin's cache repository.
func (g *GitFetcher) MirrorCommit(origin, ref, commit, tree string) (string, error) {
	dir := g.repoDir(origin)
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout())
	defer cancel()
	msg := fmt.Sprintf("urgit-ci mirror of %s@%s\n\ncommit %s\ntree %s\n", origin, ref, commit, tree)
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "commit-tree", tree, "-m", msg)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=urgit-ci", "GIT_AUTHOR_EMAIL=ci@urgit", "GIT_AUTHOR_DATE=1000000000 +0000",
		"GIT_COMMITTER_NAME=urgit-ci", "GIT_COMMITTER_EMAIL=ci@urgit", "GIT_COMMITTER_DATE=1000000000 +0000")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit-tree: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// PushMirror pushes a mirror commit to the ship's mirror repository as
// refs/heads/master, authenticating with the write token the ship
// minted for this resolve. The cache repository is shallow (the origin
// was fetched at depth 1), and a push from a shallow repository
// advertises `shallow` lines that %urgit's receive-pack does not accept;
// the mirror commit is parentless, so it is pushed from a throwaway
// non-shallow repository that borrows the cache's objects (alternates):
// a self-contained pack, no shallow lines.
func (g *GitFetcher) PushMirror(origin, mirrorCommit, shipURL, repo, token string) (string, error) {
	dir := g.repoDir(origin)
	stage, err := os.MkdirTemp(g.Cache, "mirror-stage-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if _, err := g.git(stage, "init", "--bare", "--quiet"); err != nil {
		return "", err
	}
	objects, err := filepath.Abs(filepath.Join(dir, "objects"))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, "objects", "info", "alternates"), []byte(objects+"\n"), 0o644); err != nil {
		return "", err
	}
	url := strings.TrimRight(shipURL, "/") + "/git/" + repo
	g.logf("push mirror %s -> %s", mirrorCommit[:12], repo)
	auth := "http.extraHeader=Authorization: Basic " + basicAuth("x", token)
	// master follows the newest resolve; every mirror commit also keeps a
	// branch of its own (m-<commit>), so an older lock's action stays
	// reachable after an explicit update re-mirrors the same origin
	out, err := g.git(stage, "-c", auth, "push", "--quiet", url, mirrorCommit+":refs/heads/master", mirrorCommit+":refs/heads/m-"+mirrorCommit, "--force")
	if err != nil {
		return "", errors.New(strings.ReplaceAll(out, token, "***"))
	}
	return mirrorCommit, nil
}

func basicAuth(user, pass string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(user + ":" + pass)
	var out strings.Builder
	for i := 0; i < len(src); i += 3 {
		var b [3]byte
		n := copy(b[:], src[i:])
		v := uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2])
		out.WriteByte(alphabet[v>>18&63])
		out.WriteByte(alphabet[v>>12&63])
		if n > 1 {
			out.WriteByte(alphabet[v>>6&63])
		} else {
			out.WriteByte('=')
		}
		if n > 2 {
			out.WriteByte(alphabet[v&63])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}
