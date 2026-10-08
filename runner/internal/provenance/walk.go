// Package provenance is the bounded, non-executing dependency walk
// (BRIEF-CI-P4 D4; rider 03): every `uses:`, container and service image,
// local and nested composite/JS/container action, reusable workflow
// reference and known download a revision's workflows name, resolved to
// immutable identities through a Fetcher that reads metadata and never
// runs anything. Bounds on nodes, depth and metadata bytes are enforced
// with source-located refusals. The lock is the node list; its digest
// changes with any byte of any node.
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrBound is returned when the walk exceeds a bound; the error text
// names the bound and the source location.
var ErrBound = errors.New("dependency walk exceeds a bound")

// Bounds are the walk's limits (contract §7: 256 nodes, depth 16, 1 MiB).
type Bounds struct {
	MaxNodes int
	MaxDepth int
	MaxBytes int
}

// Node is one dependency: the wire shape the ship stores (contract §7).
type Node struct {
	Uses         string `json:"uses"`
	Kind         string `json:"kind"` // js / composite / container / local / download / reusable / unknown
	Origin       string `json:"origin"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit"`
	Tree         string `json:"tree"`
	Subpath      string `json:"subpath"`
	Mirror       string `json:"mirror"`
	MirrorCommit string `json:"mirror-commit"`
	Digest       string `json:"digest"`
	License      string `json:"license"`
	Workflow     string `json:"workflow"`
	Job          string `json:"job"`
	Step         int    `json:"step"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Path         string `json:"path"`
	Refusal      string `json:"refusal,omitempty"`
	// Via names the composite action whose steps named this node ("" for
	// a workflow step); it is source location, not identity
	Via string `json:"via,omitempty"`
	// Source is where the bytes were read from when an explicit mapping
	// redirected the origin ("" when read from the origin itself)
	Source string `json:"source,omitempty"`
}

// Mapped applies the first mapping whose From prefixes the name.
func Mapped(mappings []Mapping, name string) (string, bool) {
	for _, m := range mappings {
		if m.From != "" && strings.HasPrefix(name, m.From) {
			return m.To + strings.TrimPrefix(name, m.From), true
		}
	}
	return name, false
}

// Script is a `run:` step of the repository's own workflows that invokes
// something from the tree (a candidate-controlled harness unless the
// branch policy classifies its path as harness).
type Script struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
	Step     int    `json:"step"`
	Line     string `json:"line"`
}

// Inventory is the walk's result.
type Inventory struct {
	Nodes    []Node   `json:"nodes"`
	Scripts  []Script `json:"scripts"`
	Depth    int      `json:"depth"`
	Bytes    int      `json:"bytes"`
	Notices  []string `json:"notices"`
	External []string `json:"external"` // run: lines with URLs the walk could not classify as a known download
}

// Mapping is an explicit import-time rewrite the operator declared with
// the resolve (rider 03: explicit immutable dependency import/mappings):
// an origin or download URL that starts with From is fetched from To
// instead. Every applied mapping is a lock notice; the lock keeps the
// canonical name as `uses`/`origin` and records where the bytes came
// from. A mapping never comes from the candidate's tree.
type Mapping struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Resolved is what a Fetcher answers for one origin@ref.
type Resolved struct {
	Commit string
	Tree   string
}

// Downloaded is what a Fetcher measured for one URL.
type Downloaded struct {
	SHA256 string
	Size   int64
}

// Image is what a Fetcher pinned for one OCI reference: the manifest
// digest, the docker-archive of that manifest in the download cache
// (sha256, size) and the image id the archive loads as.
type Image struct {
	Digest string
	SHA256 string
	Size   int64
	ID     string
}

// Fetcher reads remote metadata during an explicit import. It resolves
// a ref to a commit and tree, reads a file at a commit, answers an OCI
// image's digest, and measures a download. It executes nothing.
type Fetcher interface {
	Resolve(origin, ref string) (Resolved, error)
	ReadFile(origin, commit, path string) ([]byte, error)
	Image(ref string) (Image, error)
	Download(url string) (Downloaded, error)
}

const maxActionYAML = 256 * 1024

var (
	remoteUses = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(/[^@]*)?@(.+)$`)
	urlInRun   = regexp.MustCompile(`https?://[^\s"'<>)]+`)
	fetchTool  = regexp.MustCompile(`\b(curl|wget|fetch|git clone|git fetch|pip install|pip3 install|npm install|npm ci|apt-get|go install|go get|docker pull|skopeo copy)\b`)
	scriptRef  = regexp.MustCompile(`(^|\s|\./)(bin/[^\s;&|]+|scripts?/[^\s;&|]+|[^\s;&|]+\.(sh|py|pl|rb))\b`)
)

type walker struct {
	root     string
	fetcher  Fetcher
	bounds   Bounds
	inv      Inventory
	seen     map[string]bool // origin@commit/subpath already inventoried
	stack    []string        // recursion stack for cycles
	depth    int
	bytes    int
	mappings []Mapping
	env      []map[string]string // the workflow, job and step env: maps in scope (innermost last)
	inputs   []map[string]string // the composite inputs in scope (innermost last)
	with     *yaml.Node          // the current step's with: (the inputs of a composite it names)
}

// Walk inventories the workflows under root/.github/workflows.
func Walk(root string, fetcher Fetcher, bounds Bounds) (Inventory, error) {
	return WalkWith(root, fetcher, bounds, nil)
}

// WalkWith is Walk with the operator's explicit mappings.
func WalkWith(root string, fetcher Fetcher, bounds Bounds, mappings []Mapping) (Inventory, error) {
	w := &walker{root: filepath.Clean(root), fetcher: fetcher, bounds: bounds, seen: map[string]bool{}, mappings: mappings}
	files, err := filepath.Glob(filepath.Join(w.root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		return w.inv, err
	}
	sort.Strings(files)
	for _, f := range files {
		name := filepath.Base(f)
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		data, err := w.readBounded(f)
		if err != nil {
			return w.inv, fmt.Errorf("%s: %w", name, err)
		}
		if err := w.workflow(name, data); err != nil {
			return w.inv, err
		}
	}
	if w.inv.Nodes == nil {
		w.inv.Nodes = []Node{}
	}
	if w.inv.Scripts == nil {
		w.inv.Scripts = []Script{}
	}
	w.inv.Bytes = w.bytes
	return w.inv, nil
}

func (w *walker) readBounded(p string) ([]byte, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("symlinked metadata is refused")
	}
	if st.Size() > maxActionYAML {
		return nil, fmt.Errorf("%w: metadata file %s is %d bytes", ErrBound, p, st.Size())
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return data, w.account(len(data), p)
}

func (w *walker) account(n int, where string) error {
	w.bytes += n
	if w.bounds.MaxBytes > 0 && w.bytes > w.bounds.MaxBytes {
		return fmt.Errorf("%w: metadata exceeds %d bytes at %s", ErrBound, w.bounds.MaxBytes, where)
	}
	return nil
}

func (w *walker) add(n Node) error {
	w.inv.Nodes = append(w.inv.Nodes, n)
	if w.bounds.MaxNodes > 0 && len(w.inv.Nodes) > w.bounds.MaxNodes {
		return fmt.Errorf("%w: more than %d dependency nodes at %s %s step %d", ErrBound, w.bounds.MaxNodes, n.Workflow, n.Job, n.Step)
	}
	return nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func (w *walker) workflow(name string, data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	top := &doc
	if top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	jobs := mappingValue(top, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}
	w.env = []map[string]string{envMap(mappingValue(top, "env"))}
	defer func() { w.env = nil }()
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		jobID, job := jobs.Content[i].Value, jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			continue
		}
		w.env = w.env[:1]
		w.env = append(w.env, envMap(mappingValue(job, "env")))
		loc := Node{Workflow: name, Job: jobID}
		if uses := mappingValue(job, "uses"); uses != nil && uses.Kind == yaml.ScalarNode {
			n := loc
			n.Uses, n.Kind, n.Origin = uses.Value, "reusable", uses.Value
			n.Refusal = "reusable workflows are not supported in this release"
			if err := w.add(n); err != nil {
				return err
			}
		}
		if c := mappingValue(job, "container"); c != nil {
			ref := ""
			if c.Kind == yaml.ScalarNode {
				ref = c.Value
			} else if img := mappingValue(c, "image"); img != nil {
				ref = img.Value
			}
			if ref != "" {
				if err := w.image(loc, "container:"+ref, ref); err != nil {
					return err
				}
			}
		}
		if svcs := mappingValue(job, "services"); svcs != nil && svcs.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(svcs.Content); j += 2 {
				sname, svc := svcs.Content[j].Value, svcs.Content[j+1]
				ref := ""
				if svc.Kind == yaml.ScalarNode {
					ref = svc.Value
				} else if img := mappingValue(svc, "image"); img != nil {
					ref = img.Value
				}
				if ref != "" {
					if err := w.image(loc, "service:"+sname+":"+ref, ref); err != nil {
						return err
					}
				}
			}
		}
		if err := w.steps(loc, mappingValue(job, "steps"), ""); err != nil {
			return err
		}
	}
	return nil
}

// steps walks a step list of a job (via == "") or a composite action
// (via names it).
func (w *walker) steps(loc Node, steps *yaml.Node, via string) error {
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return nil
	}
	for i, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		n := loc
		n.Step = i
		n.Via = via
		if uses := mappingValue(step, "uses"); uses != nil && uses.Kind == yaml.ScalarNode {
			w.with = mappingValue(step, "with")
			err := w.uses(n, uses.Value, via)
			w.with = nil
			if err != nil {
				return err
			}
			if err := w.toolchain(n, uses.Value, mappingValue(step, "with")); err != nil {
				return err
			}
			continue
		}
		if run := mappingValue(step, "run"); run != nil && run.Kind == yaml.ScalarNode {
			w.env = append(w.env, envMap(mappingValue(step, "env")))
			err := w.run(n, run.Value)
			w.env = w.env[:len(w.env)-1]
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// compositeInputs is the inputs map a composite's steps see: the
// caller's with: values (themselves expanded in the caller's scope) over
// the action's declared defaults. A value that stays an expression is
// left out, so a URL built from it is listed as external.
func (w *walker) compositeInputs(with *yaml.Node, doc *yaml.Node) map[string]string {
	out := map[string]string{}
	top := doc
	if top != nil && top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	if decl := mappingValue(top, "inputs"); decl != nil && decl.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(decl.Content); i += 2 {
			if def := mappingValue(decl.Content[i+1], "default"); def != nil && def.Kind == yaml.ScalarNode {
				if v := w.substitute(def.Value, nil); !strings.Contains(v, "${{") {
					out[decl.Content[i].Value] = v
				}
			}
		}
	}
	if with != nil && with.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(with.Content); i += 2 {
			if with.Content[i+1].Kind != yaml.ScalarNode {
				continue
			}
			// a value the caller gives shadows the default; one that stays
			// an expression is unknown (never the default)
			if v := w.substitute(with.Content[i+1].Value, nil); !strings.Contains(v, "${{") {
				out[with.Content[i].Value] = v
			} else {
				delete(out, with.Content[i].Value)
			}
		}
	}
	return out
}

// envMap reads an env: mapping of literal scalars (an expression value
// stays unknown, so a URL built from it is listed as external).
func envMap(m *yaml.Node) map[string]string {
	out := map[string]string{}
	if m == nil || m.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i+1].Kind == yaml.ScalarNode && !strings.Contains(m.Content[i+1].Value, "${{") {
			out[m.Content[i].Value] = m.Content[i+1].Value
		}
	}
	return out
}

// lookup finds a variable in the env scopes (innermost first) and the
// script's own literal assignments.
func (w *walker) lookup(name string, script map[string]string) (string, bool) {
	if v, ok := script[name]; ok {
		return v, true
	}
	if strings.HasPrefix(name, "inputs.") && len(w.inputs) > 0 {
		v, ok := w.inputs[len(w.inputs)-1][strings.TrimPrefix(name, "inputs.")]
		return v, ok
	}
	for i := len(w.env) - 1; i >= 0; i-- {
		if v, ok := w.env[i][name]; ok {
			return v, true
		}
	}
	return "", false
}

var (
	shellVar   = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	exprEnvVar = regexp.MustCompile(`\$\{\{\s*(env\.[A-Za-z_][A-Za-z0-9_]*|inputs\.[A-Za-z_][A-Za-z0-9_-]*)\s*\}\}`)
	assignment = regexp.MustCompile(`^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=("([^"]*)"|'([^']*)'|(\S*))$`)
)

// substitute expands $VAR, ${VAR} and ${{ env.VAR }} references from the
// env scopes and the script's literal assignments, a bounded number of
// rounds; a reference it cannot resolve stays in the text.
func (w *walker) substitute(text string, script map[string]string) string {
	for round := 0; round < 4; round++ {
		before := text
		text = exprEnvVar.ReplaceAllStringFunc(text, func(m string) string {
			name := exprEnvVar.FindStringSubmatch(m)[1]
			name = strings.TrimPrefix(name, "env.")
			if v, ok := w.lookup(name, script); ok {
				return v
			}
			return m
		})
		text = shellVar.ReplaceAllStringFunc(text, func(m string) string {
			name := shellVar.FindStringSubmatch(m)[1]
			if v, ok := w.lookup(name, script); ok {
				return v
			}
			return m
		})
		if text == before {
			break
		}
	}
	return text
}

// mapped applies the first explicit mapping whose From prefixes the
// name; the notice records it.
func (w *walker) mapped(name string) (string, bool) {
	to, ok := Mapped(w.mappings, name)
	if ok {
		w.inv.Notices = append(w.inv.Notices, "mapping: "+name+" fetched from "+to)
	}
	return to, ok
}

var goVersionLine = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)

// toolchain inventories the download a well-known setup action performs
// at run time (P06): actions/setup-go fetches the Go release named by
// go-version or go-version-file, which the walk resolves from the tree
// and records as a download node of the official archive, so the job's
// tool cache can be seeded from the mirror instead of go.dev.
func (w *walker) toolchain(n Node, uses string, with *yaml.Node) error {
	if !strings.HasPrefix(uses, "actions/setup-go@") {
		return nil
	}
	version := ""
	if v := mappingValue(with, "go-version"); v != nil && v.Kind == yaml.ScalarNode {
		version = strings.TrimSpace(v.Value)
	}
	if f := mappingValue(with, "go-version-file"); f != nil && f.Kind == yaml.ScalarNode && version == "" {
		rel := path.Clean(f.Value)
		if strings.HasPrefix(rel, "..") {
			n.Uses, n.Kind, n.Origin, n.Refusal = "toolchain:go", "download", f.Value, "go-version-file traverses out of the tree"
			return w.add(n)
		}
		data, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(rel)))
		if err != nil {
			n.Uses, n.Kind, n.Origin, n.Refusal = "toolchain:go", "download", f.Value, "go-version-file "+rel+" is not in the tree"
			return w.add(n)
		}
		if m := goVersionLine.FindStringSubmatch(string(data)); m != nil {
			version = m[1]
		} else if strings.HasSuffix(rel, ".go-version") || strings.HasSuffix(rel, ".tool-versions") {
			version = strings.TrimSpace(strings.TrimPrefix(firstLine(data), "golang "))
		}
	}
	if version == "" || strings.Contains(version, "${{") || strings.ContainsAny(version, "<>=^~x*") || strings.Count(version, ".") != 2 {
		n.Uses, n.Kind, n.Origin, n.Refusal = "toolchain:go", "download", uses, "setup-go version is not an exact release (got '"+version+"'); pin go-version or go-version-file to one"
		return w.add(n)
	}
	url := "https://go.dev/dl/go" + version + ".linux-amd64.tar.gz"
	d := n
	d.Uses, d.Kind, d.Origin = "toolchain:go@"+version, "download", url
	d.Subpath = "go/" + version + "/x64" // where the tool cache expects it
	fetchFrom, mappedURL := w.mapped(url)
	if mappedURL {
		d.Source = fetchFrom
	}
	got, err := w.fetcher.Download(fetchFrom)
	if err != nil {
		d.Refusal = "toolchain download could not be fetched at import: " + err.Error()
	} else {
		d.SHA256, d.Size = got.SHA256, got.Size
	}
	return w.add(d)
}

func (w *walker) image(loc Node, uses, ref string) error {
	n := loc
	n.Uses, n.Kind, n.Origin = uses, "container", ref
	if strings.Contains(ref, "${{") {
		n.Refusal = "image reference is an expression; only literal references can be pinned"
		return w.add(n)
	}
	// the pin and the bytes: the manifest digest, and the archive of that
	// manifest measured for the ship's store (the runner loads it; the
	// id it loads as is the node's tree, verified before the job)
	img, err := w.fetcher.Image(ref)
	if err != nil {
		n.Refusal = "image digest could not be resolved: " + err.Error()
	} else {
		n.Digest, n.SHA256, n.Size, n.Tree = img.Digest, img.SHA256, img.Size, img.ID
	}
	return w.add(n)
}

func (w *walker) uses(loc Node, uses, via string) error {
	n := loc
	n.Uses = uses
	switch {
	case strings.HasPrefix(uses, "docker://"):
		return w.image(loc, uses, strings.TrimPrefix(uses, "docker://"))
	case strings.HasPrefix(uses, "./"):
		return w.local(n, uses)
	case strings.Contains(uses, "${{"):
		n.Kind, n.Refusal = "unknown", "uses: is an expression; only literal references can be pinned"
		return w.add(n)
	}
	m := remoteUses.FindStringSubmatch(uses)
	if m == nil {
		n.Kind, n.Refusal = "unknown", "uses: is not owner/repo[/path]@ref, ./path or docker://image"
		return w.add(n)
	}
	owner, repo, sub, ref := m[1], m[2], strings.TrimPrefix(m[3], "/"), m[4]
	n.Origin = "https://github.com/" + owner + "/" + repo
	n.Ref = ref
	n.Subpath = sub
	if strings.Contains(sub, "..") {
		n.Kind, n.Refusal = "unknown", "action subpath traversal is refused"
		return w.add(n)
	}
	// the canonical origin names the node; an explicit mapping says
	// where its bytes are read from (the same source for every read)
	source, mappedOrigin := w.mapped(n.Origin)
	if mappedOrigin {
		n.Source = source
	}
	resolved, err := w.fetcher.Resolve(source, ref)
	if err != nil {
		n.Kind, n.Refusal = "unknown", "could not resolve "+n.Origin+"@"+ref+": "+err.Error()
		return w.add(n)
	}
	n.Commit, n.Tree = resolved.Commit, resolved.Tree
	key := n.Origin + "@" + n.Commit + "/" + sub
	for _, s := range w.stack {
		if s == key {
			n.Kind, n.Refusal = "unknown", "dependency cycle: "+key+" reaches itself"
			return w.add(n)
		}
	}
	if w.seen[key] {
		// the same immutable action used again is one node already
		return nil
	}
	w.seen[key] = true
	meta, metaErr := w.readRemoteMeta(source, n.Commit, sub)
	if metaErr != nil {
		n.Kind, n.Refusal = "unknown", metaErr.Error()
		return w.add(n)
	}
	if lic, err := w.fetcher.ReadFile(source, n.Commit, path.Join(sub, "LICENSE")); err == nil {
		n.License = firstLine(lic)
	} else if lic, err := w.fetcher.ReadFile(source, n.Commit, "LICENSE"); err == nil {
		n.License = firstLine(lic)
	}
	kind, steps, refusal := classify(meta)
	n.Kind = kind
	n.Refusal = refusal
	if err := w.add(n); err != nil {
		return err
	}
	if kind == "composite" && refusal == "" {
		w.stack = append(w.stack, key)
		w.depth++
		if w.depth > w.inv.Depth {
			w.inv.Depth = w.depth
		}
		if w.bounds.MaxDepth > 0 && w.depth > w.bounds.MaxDepth {
			return fmt.Errorf("%w: nesting deeper than %d at %s (%s %s step %d)", ErrBound, w.bounds.MaxDepth, uses, n.Workflow, n.Job, n.Step)
		}
		w.inputs = append(w.inputs, w.compositeInputs(w.with, meta))
		err := w.steps(loc, steps, uses)
		w.inputs = w.inputs[:len(w.inputs)-1]
		w.depth--
		w.stack = w.stack[:len(w.stack)-1]
		return err
	}
	return nil
}

func (w *walker) readRemoteMeta(origin, commit, sub string) (*yaml.Node, error) {
	var data []byte
	var err error
	for _, name := range []string{"action.yml", "action.yaml"} {
		data, err = w.fetcher.ReadFile(origin, commit, path.Join(sub, name))
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("no action.yml under %s at %s", sub, commit)
	}
	if len(data) > maxActionYAML {
		return nil, fmt.Errorf("%w: action metadata is %d bytes", ErrBound, len(data))
	}
	if err := w.account(len(data), origin+"@"+commit); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("action.yml does not parse: %v", err)
	}
	return &doc, nil
}

// local walks `./path`: the action lives in the checkout; the path may
// not traverse out and may not pass through a symlink (the candidate
// controls both, and a required run takes the harness from the baseline)
func (w *walker) local(n Node, uses string) error {
	n.Kind = "local"
	n.Origin = uses
	rel := path.Clean(strings.TrimPrefix(uses, "./"))
	if rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, "/../") {
		n.Refusal = "local action path traversal is refused"
		return w.add(n)
	}
	n.Subpath = rel
	full := filepath.Join(w.root, filepath.FromSlash(rel))
	// every component must be a real directory, never a symlink
	cur := w.root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if err != nil {
			n.Refusal = "local action " + rel + " is missing"
			return w.add(n)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			n.Refusal = "local action path passes through a symlink (" + part + ")"
			return w.add(n)
		}
	}
	var data []byte
	var err error
	for _, name := range []string{"action.yml", "action.yaml"} {
		data, err = w.readBounded(filepath.Join(full, name))
		if err == nil {
			break
		}
		if errors.Is(err, ErrBound) {
			return err
		}
	}
	if err != nil {
		n.Refusal = "local action " + rel + " has no action.yml"
		return w.add(n)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		n.Refusal = "local action.yml does not parse: " + err.Error()
		return w.add(n)
	}
	kind, steps, refusal := classify(&doc)
	n.Refusal = refusal
	if kind == "container" {
		n.Kind = "container"
	}
	if err := w.add(n); err != nil {
		return err
	}
	if kind == "composite" && refusal == "" {
		key := "local:" + rel
		for _, s := range w.stack {
			if s == key {
				// the composite reaches itself through its own steps: a
				// refusal node, never a silent stop
				n.Refusal = "dependency cycle: " + key + " reaches itself"
				return w.add(n)
			}
		}
		w.stack = append(w.stack, key)
		w.depth++
		if w.depth > w.inv.Depth {
			w.inv.Depth = w.depth
		}
		if w.bounds.MaxDepth > 0 && w.depth > w.bounds.MaxDepth {
			return fmt.Errorf("%w: nesting deeper than %d at %s", ErrBound, w.bounds.MaxDepth, uses)
		}
		loc := Node{Workflow: n.Workflow, Job: n.Job}
		w.inputs = append(w.inputs, w.compositeInputs(w.with, &doc))
		err := w.steps(loc, steps, uses)
		w.inputs = w.inputs[:len(w.inputs)-1]
		w.depth--
		w.stack = w.stack[:len(w.stack)-1]
		return err
	}
	return nil
}

// classify reads `runs.using`: node20/node16 → js, composite → composite
// (with its steps), docker → container (a docker:// image only; a
// Dockerfile build is refused), anything else → unknown runtime.
func classify(doc *yaml.Node) (kind string, steps *yaml.Node, refusal string) {
	top := doc
	if top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	runs := mappingValue(top, "runs")
	if runs == nil {
		return "unknown", nil, "action.yml has no runs:"
	}
	using := mappingValue(runs, "using")
	if using == nil {
		return "unknown", nil, "action.yml has no runs.using"
	}
	switch using.Value {
	case "node20", "node16", "node24":
		return "js", nil, ""
	case "composite":
		return "composite", mappingValue(runs, "steps"), ""
	case "docker":
		img := mappingValue(runs, "image")
		if img == nil || !strings.HasPrefix(img.Value, "docker://") {
			return "container", nil, "a Dockerfile-built container action is not supported in this release (only docker:// images pinned by digest)"
		}
		return "container", nil, ""
	default:
		return "unknown", nil, "unknown action runtime " + using.Value
	}
}

// run inventories a `run:` step: URLs fetched by a known tool become
// download nodes (measured by the fetcher), other URLs are listed as
// external operations, and script invocations of the repository are
// listed as harness candidates. Nothing is executed.
func (w *walker) run(n Node, script string) error {
	// the script's own literal assignments (name=value) feed the
	// substitution, so `base=https://…/$SHA` then `curl $base/tool` is
	// inventoried as the literal URL it fetches
	assigned := map[string]string{}
	for _, line := range strings.Split(script, "\n") {
		if m := assignment.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			assigned[m[1]] = m[3] + m[4] + m[5]
		}
	}
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || assignment.MatchString(trimmed) {
			continue
		}
		expanded := w.substitute(trimmed, assigned)
		urls := urlInRun.FindAllString(expanded, -1)
		if len(urls) > 0 && fetchTool.MatchString(expanded) {
			for _, u := range urls {
				if strings.Contains(u, "${{") || strings.Contains(u, "$") {
					w.inv.External = append(w.inv.External, n.Workflow+"/"+n.Job+"#"+fmt.Sprint(n.Step)+": "+trimmed)
					continue
				}
				if w.seen["download:"+u] {
					// the same URL fetched again elsewhere is one node already
					continue
				}
				w.seen["download:"+u] = true
				d := n
				d.Uses, d.Kind, d.Origin = u, "download", u
				fetchFrom, mappedURL := w.mapped(u)
				if mappedURL {
					d.Source = fetchFrom
				}
				got, err := w.fetcher.Download(fetchFrom)
				if err != nil {
					d.Refusal = "download could not be fetched at import: " + err.Error()
				} else {
					d.SHA256, d.Size = got.SHA256, got.Size
				}
				if err := w.add(d); err != nil {
					return err
				}
			}
		} else if fetchTool.MatchString(expanded) && (len(urls) > 0 || strings.Contains(expanded, "$")) {
			// a fetch whose target is a reference the tree does not define
			// literally, or a fetch of a URL the walk cannot mirror: listed
			// as external, never guessed. a URL in an echo is not a fetch.
			w.inv.External = append(w.inv.External, n.Workflow+"/"+n.Job+"#"+fmt.Sprint(n.Step)+": "+trimmed)
		}
		if scriptRef.MatchString(trimmed) {
			w.inv.Scripts = append(w.inv.Scripts, Script{Workflow: n.Workflow, Job: n.Job, Step: n.Step, Line: trimmed})
		}
	}
	return nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return strings.TrimSpace(s)
}

// Digest is the sha256 of the canonical JSON of the node list: the
// runner's view of the lock identity for its own comparisons; the ship
// computes the authoritative lock digest over the noun it stores.
func Digest(nodes []Node) string {
	data, _ := json.Marshal(nodes)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
