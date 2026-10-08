package provenance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a checkout on disk from a map of path -> content; "->target" makes a symlink
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if strings.HasPrefix(c, "->") {
			os.Symlink(strings.TrimPrefix(c, "->"), full)
			continue
		}
		os.WriteFile(full, []byte(c), 0o644)
	}
	return dir
}

// fakeFetcher answers remote action metadata from a table; nothing is
// executed and nothing leaves the process
type fakeFetcher struct {
	repos map[string]fakeRepo // "https://github.com/owner/repo"
	calls []string
}

type fakeRepo struct {
	refs    map[string]string // ref -> commit
	tree    string
	files   map[string]string // path -> content at that commit
	license string
}

func (f *fakeFetcher) Resolve(origin, ref string) (Resolved, error) {
	f.calls = append(f.calls, "resolve "+origin+"@"+ref)
	r, ok := f.repos[origin]
	if !ok {
		return Resolved{}, errors.New("no such repository")
	}
	commit, ok := r.refs[ref]
	if !ok {
		if len(ref) == 40 {
			for _, c := range r.refs {
				if c == ref {
					return Resolved{Commit: ref, Tree: r.tree}, nil
				}
			}
		}
		return Resolved{}, errors.New("no such ref " + ref)
	}
	return Resolved{Commit: commit, Tree: r.tree}, nil
}

func (f *fakeFetcher) ReadFile(origin, commit, path string) ([]byte, error) {
	f.calls = append(f.calls, "read "+origin+" "+commit+" "+path)
	r, ok := f.repos[origin]
	if !ok {
		return nil, os.ErrNotExist
	}
	if path == "LICENSE" {
		if r.license == "" {
			return nil, os.ErrNotExist
		}
		return []byte(r.license), nil
	}
	c, ok := r.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(c), nil
}

func (f *fakeFetcher) Image(ref string) (Image, error) {
	f.calls = append(f.calls, "image "+ref)
	if strings.Contains(ref, "nodigest") {
		return Image{}, errors.New("manifest unknown")
	}
	return Image{Digest: "sha256:" + strings.Repeat("ab", 32), SHA256: strings.Repeat("ef", 32), Size: 4321, ID: "sha256:" + strings.Repeat("12", 32)}, nil
}

func (f *fakeFetcher) Download(url string) (Downloaded, error) {
	f.calls = append(f.calls, "download "+url)
	if strings.Contains(url, "missing") {
		return Downloaded{}, errors.New("404")
	}
	return Downloaded{SHA256: strings.Repeat("cd", 32), Size: 1234}, nil
}

const workflow = `name: w
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    container: node:20-bookworm
    services:
      db:
        image: postgres:16
    steps:
      - uses: actions/checkout@v4
      - uses: actions/cache@v4
        with: {path: x, key: y}
      - uses: ./.github/actions/local-composite
      - uses: docker://alpine:3.20
      - name: fetch tools
        run: |
          curl -fsSL https://bootstrap.urbit.org/urbit-v4.6.pill -o pill
          wget https://example.org/tool.tgz
      - name: structural tests
        run: bin/test.sh structural
  reuse:
    uses: org/shared/.github/workflows/x.yml@main
`

const localComposite = `name: local
runs:
  using: composite
  steps:
    - uses: actions/setup-go@v5
      with: {go-version: '1.27'}
    - run: echo hi
      shell: bash
`

func fetcherFor(t *testing.T) *fakeFetcher {
	return &fakeFetcher{repos: map[string]fakeRepo{
		"https://github.com/actions/checkout": {refs: map[string]string{"v4": "1111111111111111111111111111111111111111"}, tree: "aaaa000000000000000000000000000000000001", files: map[string]string{"action.yml": "runs:\n  using: node20\n  main: dist/index.js\n"}, license: "MIT License"},
		"https://github.com/actions/cache":    {refs: map[string]string{"v4": "2222222222222222222222222222222222222222"}, tree: "aaaa000000000000000000000000000000000002", files: map[string]string{"action.yml": "runs:\n  using: node20\n  main: dist/restore/index.js\n"}, license: "MIT License"},
		"https://github.com/actions/setup-go": {refs: map[string]string{"v5": "3333333333333333333333333333333333333333"}, tree: "aaaa000000000000000000000000000000000003", files: map[string]string{"action.yml": "runs:\n  using: node20\n  main: dist/setup/index.js\n"}, license: "MIT License"},
	}}
}

func TestWalkInventoriesEveryKindWithoutExecuting(t *testing.T) {
	dir := checkout(t, map[string]string{
		".github/workflows/w.yml":                    workflow,
		".github/actions/local-composite/action.yml": localComposite,
		"bin/test.sh":                                "#!/bin/sh\necho never run\n",
	})
	f := fetcherFor(t)
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	byUses := map[string]Node{}
	for _, n := range inv.Nodes {
		byUses[n.Uses] = n
	}
	want := map[string]string{
		"actions/checkout@v4":                         "js",
		"actions/cache@v4":                            "js",
		"./.github/actions/local-composite":           "local",
		"actions/setup-go@v5":                         "js", // nested in the local composite
		"docker://alpine:3.20":                        "container",
		"container:node:20-bookworm":                  "container",
		"service:db:postgres:16":                      "container",
		"https://bootstrap.urbit.org/urbit-v4.6.pill": "download",
		"https://example.org/tool.tgz":                "download",
		"org/shared/.github/workflows/x.yml@main":     "reusable",
	}
	for uses, kind := range want {
		n, ok := byUses[uses]
		if !ok {
			t.Errorf("missing node %q; have %v", uses, keys(byUses))
			continue
		}
		if n.Kind != kind {
			t.Errorf("%s: kind %s, want %s", uses, n.Kind, kind)
		}
		if n.Workflow == "" || n.Job == "" {
			t.Errorf("%s: no source location: %+v", uses, n)
		}
	}
	// remote actions resolved to immutable identities with a license
	if c := byUses["actions/cache@v4"]; c.Commit != "2222222222222222222222222222222222222222" || c.Tree == "" || c.Origin != "https://github.com/actions/cache" || c.Ref != "v4" || c.License != "MIT License" {
		t.Errorf("cache node: %+v", c)
	}
	// the nested action names the composite as its source
	if g := byUses["actions/setup-go@v5"]; g.Via != "./.github/actions/local-composite" {
		t.Errorf("nested node via %q", g.Via)
	}
	// a reusable workflow is inventoried and refused, never silently dropped
	if r := byUses["org/shared/.github/workflows/x.yml@main"]; r.Refusal == "" {
		t.Errorf("reusable workflow not refused: %+v", r)
	}
	// downloads carry the identity the fetcher measured
	if d := byUses["https://example.org/tool.tgz"]; d.SHA256 != strings.Repeat("cd", 32) || d.Size != 1234 {
		t.Errorf("download node: %+v", d)
	}
	// container images pinned by digest, with the archive the store
	// mirrors (sha256, size) and the id it loads as (P03)
	if n := byUses["container:node:20-bookworm"]; !strings.HasPrefix(n.Digest, "sha256:") || n.SHA256 != strings.Repeat("ef", 32) || n.Size != 4321 || n.Tree != "sha256:"+strings.Repeat("12", 32) {
		t.Errorf("container node: %+v", n)
	}
	// run: scripts of the repository are inventoried as harness candidates
	// (ERPit's bin/test.sh shape), with their source location
	if len(inv.Scripts) != 1 || inv.Scripts[0].Line != "bin/test.sh structural" || inv.Scripts[0].Job != "build" {
		t.Errorf("run: inventory %+v", inv.Scripts)
	}
	// nothing was executed: the fetcher saw resolves and reads only
	for _, c := range f.calls {
		if strings.HasPrefix(c, "exec") {
			t.Fatalf("executed: %s", c)
		}
	}
	if inv.Depth != 1 {
		t.Errorf("depth %d (the local composite nests setup-go one level down)", inv.Depth)
	}
}

func keys(m map[string]Node) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestWalkRefusesTraversalAndSymlinkedLocalActions(t *testing.T) {
	dir := checkout(t, map[string]string{
		".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: ./../../etc\n      - uses: ./.github/actions/link\n",
		".github/actions/link":    "->../../../../etc",
	})
	inv, err := Walk(dir, fetcherFor(t), Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, n := range inv.Nodes {
		if n.Refusal != "" {
			refused++
			if !strings.Contains(n.Refusal, "traversal") && !strings.Contains(n.Refusal, "symlink") {
				t.Errorf("refusal reason %q", n.Refusal)
			}
		}
	}
	if refused != 2 {
		t.Fatalf("want 2 refusals, got %d: %+v", refused, inv.Nodes)
	}
}

func TestWalkRefusesCyclesAndDepth(t *testing.T) {
	f := fetcherFor(t)
	// a composite that uses itself
	f.repos["https://github.com/org/loop"] = fakeRepo{refs: map[string]string{"v1": "4444444444444444444444444444444444444444"}, tree: "aaaa000000000000000000000000000000000004",
		files: map[string]string{"action.yml": "runs:\n  using: composite\n  steps:\n    - uses: org/loop@v1\n"}}
	dir := checkout(t, map[string]string{".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: org/loop@v1\n"})
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range inv.Nodes {
		if strings.Contains(n.Refusal, "cycle") {
			found = true
		}
	}
	if !found {
		t.Fatalf("cycle not refused: %+v", inv.Nodes)
	}
	// a chain deeper than the bound
	for i := 0; i < 20; i++ {
		next := "org/chain" + string(rune('a'+i+1))
		f.repos["https://github.com/org/chain"+string(rune('a'+i))] = fakeRepo{refs: map[string]string{"v1": strings.Repeat(string(rune('0'+i%10)), 40)}, tree: strings.Repeat("b", 40),
			files: map[string]string{"action.yml": "runs:\n  using: composite\n  steps:\n    - uses: " + next + "@v1\n"}}
	}
	dir2 := checkout(t, map[string]string{".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: org/chaina@v1\n"})
	_, err = Walk(dir2, f, Bounds{MaxNodes: 256, MaxDepth: 4, MaxBytes: 1 << 20})
	if !errors.Is(err, ErrBound) {
		t.Fatalf("depth bound: %v", err)
	}
}

func TestWalkRefusesNodeAndByteBounds(t *testing.T) {
	var steps strings.Builder
	for i := 0; i < 5; i++ {
		steps.WriteString("      - uses: docker://img" + string(rune('a'+i)) + ":1\n")
	}
	dir := checkout(t, map[string]string{".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n" + steps.String()})
	if _, err := Walk(dir, fetcherFor(t), Bounds{MaxNodes: 3, MaxDepth: 16, MaxBytes: 1 << 20}); !errors.Is(err, ErrBound) {
		t.Fatalf("node bound: %v", err)
	}
	big := checkout(t, map[string]string{
		".github/workflows/w.yml":        "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: ./.github/actions/big\n",
		".github/actions/big/action.yml": "runs:\n  using: composite\n  steps: []\n# " + strings.Repeat("x", 5000) + "\n",
	})
	if _, err := Walk(big, fetcherFor(t), Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 4000}); !errors.Is(err, ErrBound) {
		t.Fatalf("byte bound: %v", err)
	}
}

func TestWalkRefusesUnknownRuntimeAndDockerfileAndMissingRef(t *testing.T) {
	f := fetcherFor(t)
	f.repos["https://github.com/org/weird"] = fakeRepo{refs: map[string]string{"v1": "5555555555555555555555555555555555555555"}, tree: strings.Repeat("c", 40), files: map[string]string{"action.yml": "runs:\n  using: node8\n  main: x.js\n"}}
	f.repos["https://github.com/org/dockerfile"] = fakeRepo{refs: map[string]string{"v1": "6666666666666666666666666666666666666666"}, tree: strings.Repeat("d", 40), files: map[string]string{"action.yml": "runs:\n  using: docker\n  image: Dockerfile\n"}}
	dir := checkout(t, map[string]string{".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: org/weird@v1\n      - uses: org/dockerfile@v1\n      - uses: org/missing@v9\n      - uses: docker://nodigest:1\n      - run: curl https://example.org/missing.tgz\n"})
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, n := range inv.Nodes {
		reasons[n.Uses] = n.Refusal
	}
	for uses, want := range map[string]string{"org/weird@v1": "runtime", "org/dockerfile@v1": "Dockerfile", "org/missing@v9": "no such", "docker://nodigest:1": "manifest", "https://example.org/missing.tgz": "404"} {
		if !strings.Contains(reasons[uses], want) {
			t.Errorf("%s: refusal %q lacks %q", uses, reasons[uses], want)
		}
	}
}

// the mutable tag is resolved ONCE at import: the same walk asked again
// after the fetcher's tag moved yields a different lock, and a lock
// carries the commit, never the tag (P02)
func TestLockBindsTheResolvedCommitNotTheTag(t *testing.T) {
	dir := checkout(t, map[string]string{".github/workflows/w.yml": "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/cache@v4\n"})
	f := fetcherFor(t)
	inv1, _ := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	r := f.repos["https://github.com/actions/cache"]
	r.refs["v4"] = "9999999999999999999999999999999999999999"
	inv2, _ := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if inv1.Nodes[0].Commit == inv2.Nodes[0].Commit {
		t.Fatal("the moved tag did not change the resolved commit (the fetcher is not being asked)")
	}
	if inv1.Nodes[0].Ref != "v4" || inv2.Nodes[0].Ref != "v4" {
		t.Fatal("the original ref text must be kept")
	}
	if Digest(inv1.Nodes) == Digest(inv2.Nodes) {
		t.Fatal("different resolutions gave the same digest")
	}
}

// P06: a `run:` line fetches a URL spelled through the workflow's env
// and the script's own assignments (ERPit's PILL_URL / $base/click
// shape): the walk expands what the tree defines literally and lists
// what it cannot as external, never guessing
func TestWalkExpandsEnvIntoDownloads(t *testing.T) {
	dir := checkout(t, map[string]string{
		".github/workflows/e.yml": `name: e
on: [push]
env:
  PILL_URL: https://bootstrap.urbit.org/urbit-v4.6.pill
  TOOLS_SHA: c9c91ce142cfe85edbed320138f21c7213aceaab
jobs:
  fixtures:
    runs-on: ubuntu-latest
    env:
      VERE_URL: https://bootstrap.urbit.org/vere/live/v4.6/vere-v4.6-linux-x86_64
    steps:
      - name: pill and vere
        env:
          OUT: ~/.cache
        run: |
          curl -fL -o "$OUT/pill" "$PILL_URL"
          curl -fL --retry 3 -o vere "${VERE_URL}"
      - name: tools
        run: |
          set -euo pipefail
          base="https://raw.githubusercontent.com/urbit/tools/$TOOLS_SHA/pkg"
          curl -fsSL -o click "$base/click"
          curl -fsSL -o fmt "${{ env.PILL_URL }}.sig"
      - name: dynamic
        run: |
          curl -o x "https://example.org/$(uname -m)/tool"
          curl -o y "$UNDEFINED_VAR/thing"
`,
	})
	f := fetcherFor(t)
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range inv.Nodes {
		if n.Kind == "download" {
			got[n.Origin] = true
		}
	}
	for _, want := range []string{
		"https://bootstrap.urbit.org/urbit-v4.6.pill",
		"https://bootstrap.urbit.org/vere/live/v4.6/vere-v4.6-linux-x86_64",
		"https://raw.githubusercontent.com/urbit/tools/c9c91ce142cfe85edbed320138f21c7213aceaab/pkg/click",
		"https://bootstrap.urbit.org/urbit-v4.6.pill.sig",
	} {
		if !got[want] {
			t.Errorf("download %s not inventoried; have %v", want, got)
		}
	}
	if len(inv.External) != 2 {
		t.Errorf("external lines %v (the $(uname) and undefined-variable URLs stay external)", inv.External)
	}
	for _, e := range inv.External {
		if !strings.Contains(e, "$(uname -m)") && !strings.Contains(e, "$UNDEFINED_VAR") {
			t.Errorf("unexpected external line %q", e)
		}
	}
}

// rider 03: an explicit mapping reads an origin or a download from
// elsewhere; the lock keeps the canonical name, records the source and
// says so in a notice. Nothing is mapped that the operator did not name.
func TestWalkAppliesExplicitMappings(t *testing.T) {
	dir := checkout(t, map[string]string{
		".github/workflows/m.yml": `name: m
on: [push]
jobs:
  j:
    runs-on: ubuntu-latest
    steps:
      - uses: fixture/action@v1
      - uses: actions/checkout@v4
      - run: curl -o pill https://bootstrap.urbit.org/urbit-v4.6.pill
`,
	})
	f := fetcherFor(t)
	f.repos["http://127.0.0.1:8471/git/fixture-action"] = fakeRepo{refs: map[string]string{"v1": "4444444444444444444444444444444444444444"}, tree: "aaaa000000000000000000000000000000000004", files: map[string]string{"action.yml": "runs:\n  using: composite\n  steps:\n    - run: echo hi\n      shell: bash\n"}, license: "ISC"}
	mappings := []Mapping{
		{From: "https://github.com/fixture/action", To: "http://127.0.0.1:8471/git/fixture-action"},
		{From: "https://bootstrap.urbit.org/", To: "http://127.0.0.1:8472/mirror/"},
	}
	inv, err := WalkWith(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20}, mappings)
	if err != nil {
		t.Fatal(err)
	}
	byUses := map[string]Node{}
	for _, n := range inv.Nodes {
		byUses[n.Uses] = n
	}
	fx := byUses["fixture/action@v1"]
	if fx.Kind != "composite" || fx.Origin != "https://github.com/fixture/action" || fx.Source != "http://127.0.0.1:8471/git/fixture-action" || fx.Commit != "4444444444444444444444444444444444444444" || fx.License != "ISC" {
		t.Errorf("mapped action: %+v", fx)
	}
	if co := byUses["actions/checkout@v4"]; co.Source != "" || co.Refusal != "" {
		t.Errorf("unmapped action touched: %+v", co)
	}
	pill := byUses["https://bootstrap.urbit.org/urbit-v4.6.pill"]
	if pill.Source != "http://127.0.0.1:8472/mirror/urbit-v4.6.pill" || pill.SHA256 == "" {
		t.Errorf("mapped download: %+v", pill)
	}
	joined := strings.Join(inv.Notices, "\n")
	for _, want := range []string{
		"mapping: https://github.com/fixture/action fetched from http://127.0.0.1:8471/git/fixture-action",
		"mapping: https://bootstrap.urbit.org/urbit-v4.6.pill fetched from http://127.0.0.1:8472/mirror/urbit-v4.6.pill",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("notice %q missing in %v", want, inv.Notices)
		}
	}
	// the fetcher read the mapped sources, never the canonical names
	for _, c := range f.calls {
		if strings.Contains(c, "github.com/fixture") || strings.Contains(c, "bootstrap.urbit.org") {
			t.Errorf("canonical name fetched: %s", c)
		}
	}
	// the mapping changes the node's identity: a lock with it and one
	// without are different locks
	plain, _ := Walk(dir, fetcherFor(t), Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if Digest(plain.Nodes) == Digest(inv.Nodes) {
		t.Errorf("mapped and unmapped walks share a digest")
	}
}

// P06: actions/setup-go's run-time download is inventoried from the
// tree's own go.mod as a download node the bundle seeds into the tool
// cache; a version that is not one exact release is refused, and a
// go-version-file outside the tree is refused
func TestWalkInventoriesSetupGoToolchain(t *testing.T) {
	dir := checkout(t, map[string]string{
		"runner/go.mod": "module x\n\ngo 1.27.1\n",
		".github/workflows/g.yml": `name: g
on: [push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version-file: runner/go.mod
          cache: false
  loose:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.27'
  escape:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version-file: ../outside/go.mod
`,
	})
	f := fetcherFor(t)
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var pinned, loose, escape Node
	for _, n := range inv.Nodes {
		switch {
		case n.Uses == "toolchain:go@1.27.1":
			pinned = n
		case n.Job == "loose" && strings.HasPrefix(n.Uses, "toolchain:go"):
			loose = n
		case n.Job == "escape" && strings.HasPrefix(n.Uses, "toolchain:go"):
			escape = n
		}
	}
	if pinned.Kind != "download" || pinned.Origin != "https://go.dev/dl/go1.27.1.linux-amd64.tar.gz" || pinned.Subpath != "go/1.27.1/x64" || pinned.SHA256 == "" || pinned.Refusal != "" {
		t.Errorf("pinned toolchain: %+v", pinned)
	}
	if loose.Refusal == "" || !strings.Contains(loose.Refusal, "exact release") {
		t.Errorf("loose version not refused: %+v", loose)
	}
	if escape.Refusal == "" || !strings.Contains(escape.Refusal, "traverses") {
		t.Errorf("escaping go-version-file not refused: %+v", escape)
	}
}

// P06 (ERPit's shape): a local composite fetches the URL its caller
// passed as an input (itself the workflow's env), and a second input
// falls back to the action's declared default; an input left an
// expression stays external
func TestWalkExpandsCompositeInputs(t *testing.T) {
	dir := checkout(t, map[string]string{
		".github/workflows/t.yml": `name: t
on: [push]
env:
  PILL_URL: https://bootstrap.urbit.org/urbit-v4.6.pill
jobs:
  fixtures:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/toolchain
        with:
          pill-url: ${{ env.PILL_URL }}
          dynamic: ${{ github.sha }}
`,
		".github/actions/toolchain/action.yml": `name: toolchain
inputs:
  pill-url:
    required: true
  vere-url:
    default: https://bootstrap.urbit.org/vere/live/v4.6/vere-v4.6-linux-x86_64
  dynamic:
    default: ""
runs:
  using: composite
  steps:
    - run: |
        curl -fL -o pill "${{ inputs.pill-url }}"
        curl -fL -o vere "${{ inputs.vere-url }}"
        curl -fL -o x "https://example.org/${{ inputs.dynamic }}"
      shell: bash
`,
	})
	f := fetcherFor(t)
	inv, err := Walk(dir, f, Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range inv.Nodes {
		if n.Kind == "download" {
			got[n.Origin] = true
		}
	}
	for _, want := range []string{"https://bootstrap.urbit.org/urbit-v4.6.pill", "https://bootstrap.urbit.org/vere/live/v4.6/vere-v4.6-linux-x86_64"} {
		if !got[want] {
			t.Errorf("download %s not inventoried; have %v", want, got)
		}
	}
	if len(inv.External) != 1 || !strings.Contains(inv.External[0], "inputs.dynamic") {
		t.Errorf("external %v", inv.External)
	}
}
