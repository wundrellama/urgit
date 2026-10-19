package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"urgit/runner/internal/inputpolicy"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/provenance"
	"urgit/runner/internal/ship"
)

// runResolve handles explicit imports and automatic tag resolution: the bounded
// dependency walk over the revision's workflows on the host (parsing
// only — no sandbox, no execution), the fetch of every approved external
// input through the fetcher, the mirror pushes into the repositories the
// ship created for this resolve, and the lock proposal. It runs only on
// a runner the operator marked resolver = true, and only for a resolve
// assignment the ship signed.
//
// Two phases against the ship's lock route: `inventory` (the resolved
// nodes; the ship answers with the mirror repositories and their
// one-resolve write tokens) and `lock` (the nodes with their mirror
// identities; the ship validates, digests and stores it).
func (d *Daemon) runResolve(ctx context.Context, a *ship.Assignment, work string, logf func(string, ...any)) {
	purpose, err := resolverPurpose(a)
	if err != nil {
		d.fail(ctx, a, err.Error(), logf)
		return
	}
	src := filepath.Join(work, "src")
	if err := d.checkout(ctx, a, src); err != nil {
		d.fail(ctx, a, "checkout: "+err.Error(), logf)
		return
	}
	cache := filepath.Join(d.cfg.WorkDir, "resolver-cache")
	policy := a.AutomaticInputs
	// Persisted pre-allowlist domain6 obligations signed an empty scope. That
	// signature authorizes only the original zero-fetch behavior, never today's
	// repository policy or runner ceiling.
	if purpose == provenance.AutomaticTag && policy == nil {
		policy = &inputpolicy.Policy{Version: 1, Sources: []string{}}
	}
	fetcher := &provenance.GitFetcher{Purpose: purpose, Context: ctx, AutomaticPolicy: policy, AutomaticCeiling: d.cfg.AutomaticInputs, Cache: cache, Log: func(line string) { logf("fetch: %s", line) }}
	bounds := provenance.Bounds{MaxNodes: 256, MaxDepth: 16, MaxBytes: 1 << 20}
	var mappings []provenance.Mapping
	for _, m := range a.Mappings {
		mappings = append(mappings, provenance.Mapping{From: m.From, To: m.To})
	}
	if len(mappings) > 0 {
		logf("explicit mappings: %d (recorded in the lock's notices)", len(mappings))
	}
	inv, err := provenance.WalkWith(src, fetcher, bounds, mappings)
	if err != nil {
		d.fail(ctx, a, "dependency walk: "+err.Error(), logf)
		return
	}
	logf("walk: %d node(s), depth %d, %d bytes, %d script reference(s), %d external line(s)", len(inv.Nodes), inv.Depth, inv.Bytes, len(inv.Scripts), len(inv.External))
	// what each workflow declares for a manual run (Delivery 1A), as the
	// pinned compiler reads it at this revision: the ship lists it in the
	// Run workflow form and admits a run's inputs against it
	dispatch, err := workflowDispatches(src)
	if err != nil {
		d.fail(ctx, a, "manual-run inventory: "+err.Error(), logf)
		return
	}
	logf("manual workflows: %d", len(dispatch))
	// every workflow file's compiled `on:` (Delivery 1C): the events, branch,
	// tag and path filters and schedules the ship selects from once the
	// revision is promoted, read by the same pinned compiler; never run here
	triggers, err := workflowTriggers(src)
	if err != nil {
		d.fail(ctx, a, "trigger catalog: "+err.Error(), logf)
		return
	}
	logf("trigger catalog: %d workflow(s), %d timezone table(s)", len(triggers.Workflows), len(triggers.Zones))
	// phase 1: the inventory; the ship creates the mirrors
	type mirrorGrant struct {
		Repo  string `json:"repo"`
		Token string `json:"token"`
	}
	type storePut struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Key     string            `json:"key"`
		Headers map[string]string `json:"headers"`
	}
	var answer struct {
		Mirrors   map[string]mirrorGrant `json:"mirrors"`
		Downloads map[string]storePut    `json:"downloads"` // by sha256
		Error     string                 `json:"error"`
	}
	var evidence string
	if purpose == provenance.AutomaticTag {
		measured, err := fetcher.InputEvidence()
		if err != nil {
			d.fail(ctx, a, err.Error(), logf)
			return
		}
		data, err := json.Marshal(measured)
		if err != nil {
			d.fail(ctx, a, err.Error(), logf)
			return
		}
		evidence = string(data)
	}
	body := map[string]any{"phase": "inventory", "nodes": inv.Nodes, "scripts": inv.Scripts, "external": inv.External, "depth": inv.Depth, "bytes": inv.Bytes, "notices": inv.Notices}
	if purpose == provenance.AutomaticTag {
		body["input-evidence"] = evidence
	}
	resp, err := d.client.PostLock(ctx, a.Attempt, body)
	if err != nil {
		logf("inventory POST failed: %v", err)
		return
	}
	if resp.Status != 200 {
		logf("inventory POST -> %s", resp.Error())
		return
	}
	if err := json.Unmarshal(resp.Body, &answer); err != nil {
		d.fail(ctx, a, "inventory answer: "+err.Error(), logf)
		return
	}
	// phase 2: mirror every resolvable Git action and every download
	nodes := inv.Nodes
	var downloads []string
	for i := range nodes {
		n := &nodes[i]
		if n.Refusal != "" {
			continue
		}
		switch n.Kind {
		case "js", "composite":
			grant, ok := answer.Mirrors[n.Origin]
			if !ok {
				n.Refusal = "the ship created no mirror repository for " + n.Origin
				continue
			}
			// the bytes live in the cache under the source they were read
			// from (the origin itself, or its explicit mapping)
			source := n.Origin
			if n.Source != "" {
				source = n.Source
			}
			mc, err := fetcher.MirrorCommit(source, n.Ref, n.Commit, n.Tree)
			if err != nil {
				n.Refusal = "mirror commit: " + err.Error()
				continue
			}
			if _, err := fetcher.PushMirror(source, mc, d.cfg.ShipURL, grant.Repo, grant.Token); err != nil {
				n.Refusal = "mirror push to " + grant.Repo + ": " + err.Error()
				continue
			}
			n.Mirror, n.MirrorCommit = grant.Repo, mc
			logf("mirrored %s (%s, tree %s) -> %s @ %s", n.Uses, n.Commit[:12], n.Tree[:12], grant.Repo, mc[:12])
		case "download", "container":
			if n.SHA256 == "" {
				n.Refusal = n.Kind + " has no measured identity"
				continue
			}
			// the bytes go to the ship's object store, content-addressed,
			// through the header-signed PUT the ship issued for exactly
			// this sha256 (the store verifies the payload hash)
			put, ok := answer.Downloads[n.SHA256]
			if !ok {
				n.Refusal = "the ship issued no store upload for " + n.SHA256[:12]
				continue
			}
			if !contains(downloads, n.SHA256) {
				if err := uploadToStore(ctx, fetcher.DownloadPath(n.SHA256), n.Size, put.URL, put.Headers); err != nil {
					n.Refusal = "store upload: " + err.Error()
					continue
				}
				downloads = append(downloads, n.SHA256)
				logf("mirrored %s %s (%d bytes) -> %s", n.Kind, n.SHA256[:12], n.Size, put.Key)
			}
			n.Mirror, n.Path = "store", put.Key
		}
	}
	lock := map[string]any{"phase": "lock", "nodes": nodes, "scripts": inv.Scripts, "external": inv.External, "depth": inv.Depth, "bytes": inv.Bytes, "notices": inv.Notices, "digest": provenance.Digest(nodes), "dispatch": dispatch, "triggers": triggers}
	if purpose == provenance.AutomaticTag {
		lock["input-evidence"] = evidence
	}
	// the resolver's own record of what it walked and fetched, for the
	// import preview and the record (rider 03: native replacements and
	// mappings appear in evidence)
	if data, err := json.MarshalIndent(lock, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(d.cfg.WorkDir, a.Attempt+".lock.json"), data, 0o644)
	}
	resp, err = d.client.PostLock(ctx, a.Attempt, lock)
	if err != nil {
		logf("lock POST failed: %v", err)
		return
	}
	logf("lock POST -> %s", resp.Error())
}

// The signed mode is the purpose. Automatic resolution uses domain 6 so
// version-5 runners refuse it. No unsigned selector or version fallback can
// elevate it; existing explicit imports keep their version-5 resolve mode.
func resolverPurpose(a *ship.Assignment) (provenance.FetchPurpose, error) {
	if a.Manifest == nil || a.Kind != "resolve" {
		return "", errors.New("missing authenticated resolver purpose")
	}
	m := a.Manifest
	if m.Ref != "refs/ci/resolve" || a.Ref != m.Ref || m.OID != a.OID ||
		m.Workflow != "" || m.Job != "" || m.Event != "" || m.Context != "" ||
		m.Needs != "" || m.Prerequisites != "" || m.Trigger != "" ||
		m.Network != "locked" || len(a.Grants) != 0 {
		return "", errors.New("resolver purpose has inconsistent source, event, credentials or network policy")
	}
	switch m.Mode {
	case "resolve":
		if m.NetworkScope != "" || a.AutomaticInputs != nil {
			return "", errors.New("explicit import carries automatic policy or resolver network scope")
		}
		return provenance.PolicyImport, nil
	case "resolve-tag":
		if m.NetworkScope != "" || a.AutomaticInputs != nil {
			scope, err := a.AutomaticInputs.Scope()
			if err != nil {
				return "", err
			}
			if scope != m.NetworkScope {
				return "", errors.New("automatic input policy disagrees with signed network-scope")
			}
		} // else: signed legacy domain6, zero fetch authority only
		if len(a.Mappings) != 0 || a.WorkflowsOID != m.OID || a.Trust != m.Trust ||
			a.ScratchRef != "refs/ci/run/"+uvKey(m.Candidate) || a.BaselineRef != "" ||
			m.Baseline != "" || m.Lock != "" || a.Lock != nil || len(a.Downloads) != 0 {
			return "", errors.New("automatic resolver carries an import mapping or inconsistent captured input")
		}
		return provenance.AutomaticTag, nil
	default:
		return "", errors.New("missing or unsupported authenticated resolver purpose")
	}
}

// workflowDispatches reads every workflow file of a checkout, in name
// order, and lists those that declare workflow_dispatch (Delivery 1A).
func workflowDispatches(src string) ([]plan.Dispatch, error) {
	files, err := workflowFiles(src)
	if err != nil {
		return nil, err
	}
	out := []plan.Dispatch{}
	for _, file := range files {
		data, err := provenance.ReadTreeFile(src, filepath.Join(".github", "workflows", file))
		if err != nil {
			return nil, err
		}
		declared, err := plan.DispatchOf(data, file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if declared != nil {
			out = append(out, *declared)
		}
	}
	return out, nil
}

// workflowTriggers compiles every workflow file of a checkout, in name
// order, into the trigger catalog (Delivery 1C).
func workflowTriggers(src string) (plan.TriggerCatalog, error) {
	files, err := workflowFiles(src)
	if err != nil {
		return plan.TriggerCatalog{}, err
	}
	return plan.CatalogOf(files, func(file string) ([]byte, error) {
		return provenance.ReadTreeFile(src, filepath.Join(".github", "workflows", file))
	})
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// resolveDeadline bounds the whole import.
const resolveDeadline = 30 * time.Minute

var errNotResolver = errors.New("this runner is not a resolver (resolver = true)")

func (d *Daemon) resolveAllowed() error {
	if !d.cfg.Resolver {
		return errNotResolver
	}
	return nil
}

var _ = fmt.Sprintf
var _ = strings.TrimSpace

// uploadToStore PUTs a cached download to the ship's object store with
// the headers the ship signed (the payload hash is the sha256 the
// inventory measured; other bytes are refused by the store itself).
func uploadToStore(ctx context.Context, path string, size int64, url string, headers map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, "PUT", url, f)
	if err != nil {
		return err
	}
	req.ContentLength = size
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	// A signed store PUT is an exact scoped output capability. Never follow
	// a redirect with its headers or inherit a host proxy.
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	resp, err := (&http.Client{Timeout: 30 * time.Minute, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("scoped store upload refuses redirects") }}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("store answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
