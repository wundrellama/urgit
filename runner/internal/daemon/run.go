package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"urgit/runner/internal/act"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/provenance"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/sig"
	"urgit/runner/internal/state"

	"gopkg.in/yaml.v3"
)

// teardownBound is rider 04's 120 s: a sandbox that is not gone by then
// is quarantined with the failure visible.
const teardownBound = 120 * time.Second

// verifier is the optional guest-side workspace check a backend offers
// (the microvm backend does; the container backend cannot).
type verifier interface {
	Verify(ctx context.Context, h sandbox.Handle, guestPath, sha256 string) (bool, error)
}

// handle carries out one assignment; it returns whether the slot may be
// reused (false when what the attempt's sandbox leaves — a Prepare that
// could not roll back, a failed teardown — became a new retention, which
// withholds the slot from then on).
func (d *Daemon) handle(ctx context.Context, a *ship.Assignment) (keep bool) {
	logf := func(format string, args ...any) {
		d.log.Printf("[%s %s] "+format, append([]any{a.Kind, a.Attempt}, args...)...)
	}
	mode, sandboxReq, network := "", "", "locked"
	if a.Manifest != nil {
		mode, sandboxReq, network = a.Manifest.Mode, a.Manifest.Sandbox, a.Manifest.Network
		if network == "" {
			network = "locked"
		}
	}
	logf("assignment %s: candidate %s repo %s ref %s oid %s trust %s deadline %ds workflow %q job %q mode %s sandbox %s network %s",
		a.ID, a.Candidate, a.Repo, a.Ref, a.OID, a.Trust, a.DeadlineSeconds, a.Workflow, a.Job, mode, sandboxReq, network)
	deadline := time.Duration(a.DeadlineSeconds) * time.Second
	if deadline <= 0 {
		deadline = time.Hour
	}
	actx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	work := filepath.Join(d.cfg.WorkDir, a.Attempt)
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		d.fail(actx, a, "work dir: "+err.Error(), logf)
		return true
	}
	defer os.RemoveAll(work)

	// a resolve is host-side parsing and fetching (D4): no sandbox, no
	// execution; only a resolver-capable runner takes it (honourable)
	if a.Kind == "resolve" {
		if err := d.resolveAllowed(); err != nil {
			d.fail(actx, a, err.Error(), logf)
			return true
		}
		d.runResolve(actx, a, work, logf)
		return true
	}

	scope, err := d.effectiveScope(a)
	if err != nil {
		d.fail(actx, a, "network: "+err.Error(), logf)
		return true
	}
	spec := sandbox.Spec{
		Image: d.cfg.ActImage, CPUs: d.cfg.CPUs, MemoryMiB: d.cfg.MemoryMiB, DiskMiB: d.cfg.DiskMiB, Network: "ci-" + a.Attempt,
		Attempt: a.Attempt, Deadline: time.Now().Add(deadline), Profile: network, Destinations: scope,
		Label: jobLabel(a),
	}
	// a backend whose reserve requests the launcher settles: the request is
	// recorded durably before it is sent, so that no life of this daemon
	// advertises its slot before it is settled (runner/launcher/
	// INTEGRATION.md §11.10); a record that is not durable sends none
	var admission *state.Quarantine
	if _, settles := d.box.(sandbox.Settler); settles {
		q, err := d.newAdmission(spec)
		if err == nil {
			err = d.admit(q)
		}
		if err != nil {
			d.fail(actx, a, "sandbox prepare: its reserve request could not be recorded durably, so none was sent: "+err.Error(), logf)
			return true
		}
		admission, spec.Request = &q, q.Request
	}
	h, err := d.box.Prepare(actx, spec)
	if admission != nil {
		keep := d.resolveAdmission(*admission, err)
		if err != nil {
			d.fail(actx, a, "sandbox prepare: "+err.Error(), logf)
			return keep
		}
	} else if err != nil {
		// §2: what a failed Prepare could not release stays owned and
		// charged under its exact identity (runner/launcher/INTEGRATION.md
		// §3); the slot passes to its retention
		var kept *sandbox.RetainedError
		added := errors.As(err, &kept) && d.retain(kept.Handle, "sandbox prepare failed and could not be rolled back: "+err.Error())
		d.fail(actx, a, "sandbox prepare: "+err.Error(), logf)
		return !added
	}
	logf("sandbox %s prepared (%s)", h.ID, describeHandle(h))
	keep = true
	jobStopped := false
	defer func() {
		if jobStopped {
			// Cancellation has already spent part of rider 04's budget.
			if err := d.destroyStoppedJob(h); err != nil {
				keep = !d.retain(h, "teardown failed: "+err.Error())
				return
			}
			logf("sandbox %s destroyed", h.ID)
			return
		}
		// teardown uses a fresh context bounded by rider 04's 120 s: the
		// attempt's deadline may have passed, and a teardown that does not
		// finish in the bound is a quarantine, never a silent success
		dctx, dcancel := context.WithTimeout(context.Background(), teardownBound)
		defer dcancel()
		if err := d.box.Destroy(dctx, h); err != nil {
			keep = !d.retain(h, "teardown failed: "+err.Error())
			return
		}
		logf("sandbox %s destroyed", h.ID)
	}()

	env := d.box.ExecEnv(h)
	bundle, err := d.assembleBundle(actx, a, work, env)
	if err != nil {
		d.fail(actx, a, "bundle: "+err.Error(), logf)
		return keep
	}
	logf("bundle assembled: workspace %s, %d harness path(s) replaced from the baseline, %d action(s), %d download(s)", bundle.WorkspaceSHA[:12], len(bundle.Replaced), bundle.Actions, bundle.Downloads)
	for _, served := range bundle.Served {
		logf("action cache: %s", served)
	}
	if err := d.box.Copy(actx, h, bundle.Dir+"/.", env.WorkRoot); err != nil {
		d.fail(actx, a, "copy bundle: "+err.Error(), logf)
		return keep
	}
	if env.CopyAct {
		actBinary, err := exec.LookPath(d.cfg.ActBinary)
		if err != nil {
			d.fail(actx, a, "act_binary: "+err.Error(), logf)
			return keep
		}
		if err := d.box.Copy(actx, h, actBinary, env.ActBinary); err != nil {
			d.fail(actx, a, "copy act: "+err.Error(), logf)
			return keep
		}
	}
	// P07: a guest that can recompute the workspace digest must agree
	// with the host before anything runs on it
	if v, ok := d.box.(verifier); ok {
		agreed, err := v.Verify(actx, h, env.SourceDir, bundle.WorkspaceSHA)
		if err != nil {
			d.fail(actx, a, "workspace verification: "+err.Error(), logf)
			return keep
		}
		if !agreed {
			d.fail(actx, a, "workspace digest differs inside the guest; the bundle was altered in transit", logf)
			return keep
		}
		logf("guest confirmed workspace %s", bundle.WorkspaceSHA[:12])
	}

	// P03: every locked container image is loaded from its verified
	// archive into the sandbox's own Docker, under the lock's name, and
	// must load as the id the lock names; a load that fails or differs
	// refuses before the job, and act (--pull=false, a name no registry
	// serves) never fetches an image
	for _, img := range bundle.Images {
		if a.Kind != "job" {
			break // a plan lists jobs; the archives' presence and bytes were verified above
		}
		// the archive's own identity (its bytes matched the lock's sha256
		// at staging): the config id the lock names, and the rootfs diff
		// ids the loaded image must report whatever its image store calls
		// an id (a containerd-backed store reports the manifest digest)
		id, layers, err := provenance.ArchiveIdentity(filepath.Join(bundle.Dir, "downloads", img.Archive))
		if err != nil {
			d.fail(actx, a, fmt.Sprintf("image %s: the locked archive %s: %v", img.Ref, img.Archive[:12], err), logf)
			return keep
		}
		if id != img.ID {
			d.fail(actx, a, fmt.Sprintf("image %s: the locked archive holds image %s, the lock names %s", img.Ref, id, img.ID), logf)
			return keep
		}
		archive := env.WorkRoot + "/downloads/" + img.Archive
		if out, code, err := d.capture(actx, h, env.WorkRoot, []string{"docker", "load", "-i", archive}, nil); err != nil || code != 0 {
			d.fail(actx, a, fmt.Sprintf("image %s: docker load of the locked archive %s exited %d: %s", img.Ref, img.Archive[:12], code, strings.TrimSpace(out)), logf)
			return keep
		}
		out, code, err := d.capture(actx, h, env.WorkRoot, []string{"docker", "image", "inspect", "--format", "{{.Id}} {{json .RootFS.Layers}}", img.Tag}, nil)
		if err != nil || code != 0 {
			d.fail(actx, a, fmt.Sprintf("image %s: the archive loaded no image named %s: %s", img.Ref, img.Tag, strings.TrimSpace(out)), logf)
			return keep
		}
		gotID, gotLayers, _ := strings.Cut(strings.TrimSpace(out), " ")
		want, _ := json.Marshal(layers)
		if gotID != img.ID && gotLayers != string(want) {
			d.fail(actx, a, fmt.Sprintf("image %s: loaded as %s with layers %s; the lock names %s with layers %s", img.Ref, gotID, gotLayers, img.ID, want), logf)
			return keep
		}
		logf("image %s loaded from the locked archive %s as %s (%s, %d layer(s) verified)", img.Ref, img.Archive[:12], img.Tag, img.ID[:19], len(layers))
	}

	switch a.Kind {
	case "plan":
		d.runPlan(actx, a, h, env, bundle, logf)
	case "job":
		jobStopped = d.runJob(actx, ctx, a, h, env, bundle, logf)
	default:
		d.fail(actx, a, "unknown assignment kind "+a.Kind, logf)
	}
	return keep
}

// jobLabel is the job an assignment runs, for the operator's selection of
// a retention or a launcher incident (recovery ruling A): the ship's
// repository, workflow and job — display only, never identity.
func jobLabel(a *ship.Assignment) string {
	var parts []string
	for _, p := range []string{a.Repo, a.Workflow, a.Job} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return a.Kind + " " + a.Attempt
	}
	return strings.Join(parts, " · ")
}

func describeHandle(h sandbox.Handle) string {
	if h.VM != "" {
		return fmt.Sprintf("vm %s incarnation %s (cid %d created %d)", h.VM, h.Incarnation, h.CID, h.Created)
	}
	return fmt.Sprintf("network %s, volume %s", h.Network, h.Volume)
}

// fail reports that no result is coming. A plan attempt gets the reason
// as its (refused) plan; a job attempt is abandoned (D8).
func (d *Daemon) fail(ctx context.Context, a *ship.Assignment, reason string, logf func(string, ...any)) {
	logf("no result: %s", reason)
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch a.Kind {
	case "plan":
		resp, err := d.client.Plan(rctx, a.Attempt, map[string]string{"error": reason})
		if err != nil {
			logf("plan error POST failed: %v", err)
			return
		}
		logf("plan error POST -> %s", resp.Error())
	case "resolve":
		resp, err := d.client.PostLock(rctx, a.Attempt, map[string]string{"error": reason})
		if err != nil {
			logf("lock error POST failed: %v", err)
			return
		}
		logf("lock error POST -> %s", resp.Error())
	default:
		resp, err := d.client.Abandon(rctx, a.Attempt, reason)
		if err != nil {
			logf("abandon POST failed: %v", err)
			return
		}
		logf("abandon POST -> %s", resp.Error())
	}
}

// gitAuth is the extra header git sends for a clone the ship authorized
// with an attempt-bound read capability (A06); nothing when there is none.
func gitAuth(a *ship.Assignment) []string {
	if a.ReadToken == "" {
		return nil
	}
	return []string{"-c", "http.extraHeader=Authorization: Basic " + basicAuth("x", a.ReadToken)}
}

// gitCheckout clones the repository from the ship's Git endpoint and
// checks out the candidate oid, reachable through refs/ci/candidate/<id>
// (D9), on a local branch named for the assignment's ref so act reads
// the same github.ref a push to that ref carries (ERPit's workflows
// filter on `branches: [master]`). The checkout is verified: HEAD is the
// candidate and the tree is clean.
func (d *Daemon) gitCheckout(ctx context.Context, a *ship.Assignment, dir string) error {
	url := strings.TrimRight(d.cfg.ShipURL, "/") + "/git/" + a.Repo
	branch := strings.TrimPrefix(a.Ref, "refs/heads/")
	if branch == "" || branch == a.Ref {
		branch = "ci-candidate"
	}
	// the ship names the scratch ref (every candidate of one head and
	// base shares it); an older ship names it after the candidate
	scratch := a.ScratchRef
	if scratch == "" {
		scratch = "refs/ci/candidate/" + a.Candidate
	}
	auth := gitAuth(a)
	steps := [][]string{
		append(append([]string{"git"}, auth...), "clone", "--quiet", "--no-checkout", url, dir),
		append(append([]string{"git", "-C", dir}, auth...), "fetch", "--quiet", "origin", scratch),
	}
	if a.BaselineRef != "" {
		steps = append(steps, append(append([]string{"git", "-C", dir}, auth...), "fetch", "--quiet", "origin", a.BaselineRef))
	}
	steps = append(steps, []string{"git", "-C", dir, "checkout", "--quiet", "-B", branch, a.OID})
	for _, argv := range steps {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s", shownGit(argv), strings.TrimSpace(scrubToken(string(out), a.ReadToken)))
		}
	}
	head, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(head)); got != a.OID {
		return fmt.Errorf("checked out %s, not the candidate %s", got, a.OID)
	}
	if err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--quiet", "HEAD").Run(); err != nil {
		return fmt.Errorf("checkout of %s is not clean", a.OID)
	}
	return nil
}

// shownGit is the argv without the credential header, for messages.
func shownGit(argv []string) string {
	var out []string
	for i := 0; i < len(argv) && len(out) < 4; i++ {
		if argv[i] == "-c" && i+1 < len(argv) && strings.HasPrefix(argv[i+1], "http.extraHeader=") {
			i++
			continue
		}
		out = append(out, argv[i])
	}
	return strings.Join(out, " ")
}

func scrubToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, token, "***"), basicAuth("x", token), "***")
}

// runPlan: act -l per workflow file inside the sandbox, the YAML walk on
// the host bundle, one POST. act refusing a file is the plan's error.
// The workflow files are the bundle's (the baseline's under a required
// mode, the candidate's own under a trial), and the plan says which
// revision they came from.
func (d *Daemon) runPlan(ctx context.Context, a *ship.Assignment, h sandbox.Handle, env sandbox.ExecEnv, bundle *Bundle, logf func(string, ...any)) {
	src := filepath.Join(bundle.Dir, "src")
	files, err := workflowFiles(src)
	if err != nil {
		d.fail(ctx, a, "workflows: "+err.Error(), logf)
		return
	}
	type wireJob struct {
		ID          string     `json:"id"`
		Workflow    string     `json:"workflow"`
		Name        string     `json:"name"`
		Stage       int        `json:"stage"`
		Needs       []string   `json:"needs"`
		Cond        *plan.Cond `json:"cond"`
		Matrix      bool       `json:"matrix"`
		Events      []string   `json:"events"`
		Environment string     `json:"environment,omitempty"`
		// the job's runs-on as a list (a string is one element) and its
		// timeout-minutes; the ship matches labels and bounds the
		// deadline with them (CI-P3-SCHED-A, CI-DELIVERY-1.1)
		RunsOn         []string `json:"runs-on"`
		TimeoutMinutes int      `json:"timeout-minutes,omitempty"`
	}
	body := struct {
		OID          string    `json:"oid"`
		WorkflowsOID string    `json:"workflows-oid"`
		Workspace    string    `json:"workspace-sha256"`
		Workflows    []string  `json:"workflows"`
		Jobs         []wireJob `json:"jobs"`
	}{OID: a.OID, WorkflowsOID: bundle.WorkflowsOID, Workspace: bundle.WorkspaceSHA, Workflows: files, Jobs: []wireJob{}}
	for _, file := range files {
		out, code, err := d.capture(ctx, h, env.SourceDir, []string{"sh", "-c", env.ActBinary + " -l -W '.github/workflows/" + file + "' 2>&1"}, nil)
		if err != nil {
			d.fail(ctx, a, "act -l "+file+": "+err.Error(), logf)
			return
		}
		if code != 0 {
			d.fail(ctx, a, "act -l "+file+" exited "+strconv.Itoa(code)+": "+strings.TrimSpace(out), logf)
			return
		}
		listed, err := act.ParseList(out)
		if err != nil {
			d.fail(ctx, a, "act -l "+file+": "+err.Error(), logf)
			return
		}
		data, err := os.ReadFile(filepath.Join(src, ".github", "workflows", file))
		if err != nil {
			d.fail(ctx, a, err.Error(), logf)
			return
		}
		walked, err := plan.Walk(data)
		if err != nil {
			d.fail(ctx, a, "yaml "+file+": "+err.Error(), logf)
			return
		}
		for _, row := range listed {
			info := walked[row.JobID]
			if info.Needs == nil {
				info.Needs = []string{}
			}
			if info.RunsOn == nil {
				info.RunsOn = []string{}
			}
			body.Jobs = append(body.Jobs, wireJob{
				ID: row.JobID, Workflow: file, Name: row.WorkflowName, Stage: row.Stage, Needs: info.Needs,
				Cond: info.Cond, Matrix: info.Matrix, Events: row.Events, Environment: info.Environment,
				RunsOn: info.RunsOn, TimeoutMinutes: info.TimeoutMinutes,
			})
		}
		logf("act -l %s: %d job(s)", file, len(listed))
	}
	resp, err := d.client.Plan(ctx, a.Attempt, body)
	if err != nil {
		logf("plan POST failed: %v", err)
		return
	}
	logf("plan POST -> %s", resp.Error())
}

// projectionNameOf reads the workflow's own name (or the file name when
// it has none) and prefixes it the way the projection did.
func projectionNameOf(original []byte, attempt, fileName string) string {
	var doc struct {
		Name string `yaml:"name"`
	}
	_ = yaml.Unmarshal(original, &doc)
	if doc.Name == "" {
		doc.Name = fileName
	}
	return plan.ProjectionName(attempt, doc.Name)
}

func workflowFiles(src string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(src, ".github", "workflows"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if files == nil {
		files = []string{}
	}
	return files, nil
}

// capture runs argv in the sandbox and returns its combined output.
func (d *Daemon) capture(ctx context.Context, h sandbox.Handle, workDir string, argv, env []string) (string, int, error) {
	stream, done, err := d.box.Run(ctx, h, workDir, argv, env)
	if err != nil {
		return "", 0, err
	}
	out, err := io.ReadAll(stream)
	_ = stream.Close()
	code := <-done
	return string(out), code, err
}

// runJob: run act on the single-job projection the bundle carries
// (CI-PROJECT-1) with the backend's execution settings, relay every
// event line, then claim the jobResult the stream carried or abandon.
func (d *Daemon) runJob(ctx, daemonCtx context.Context, a *ship.Assignment, h sandbox.Handle, env sandbox.ExecEnv, bundle *Bundle, logf func(string, ...any)) (stopped bool) {
	if a.Workflow == "" || a.Job == "" {
		d.fail(ctx, a, "job assignment without workflow and job", logf)
		return
	}
	original, err := os.ReadFile(filepath.Join(bundle.Dir, "src", ".github", "workflows", a.Workflow))
	if err != nil {
		d.fail(ctx, a, "workflow: "+err.Error(), logf)
		return
	}
	argv := []string{
		env.ActBinary, "push",
		"-W", env.WorkRoot + "/projected/" + a.Workflow,
		"-j", a.Job,
		"-P", "ubuntu-latest=" + d.cfg.ActImage,
	}
	// every label of the job's runs-on maps to the runner image (P3 D2b):
	// act picks the first label it has a platform for and skips a job it
	// has none for, so `runs-on: [self-hosted, big-mem]` must find one.
	// the ship already matched these labels against this daemon's own.
	if walked, err := plan.Walk(original); err == nil {
		for _, label := range walked[a.Job].RunsOn {
			if strings.EqualFold(label, "ubuntu-latest") {
				continue
			}
			argv = append(argv, "-P", label+"="+d.cfg.ActImage)
		}
	}
	argv = append(argv,
		"--network", env.JobNetwork,
		"--json", "--pull=false",
		"--cache-server-path", env.CachePath,
		"--artifact-server-path", env.ArtifactPath,
		// the socket act binds into every job container (CI-SANDBOX-1.1):
		// the backend's own — the guest's daemon in a VM, the rootless
		// daemon in compatibility mode — never a path the daemon resolves
		"--container-daemon-socket", env.DockerSocket,
		// remote actions come from the bundle's cache, never from a fetch
		// (D4): offline mode refuses anything the lock did not materialize
		"--action-cache-path", env.ActionCachePath,
		"--action-offline-mode",
	)
	// the seeded tool cache (P06): each tool the bundle seeded is mounted
	// under act's fixed RUNNER_TOOL_CACHE (/opt/hostedtoolcache, act's
	// own volume — a tool is a nested mount below it), so a setup action
	// finds its locked toolchain and downloads nothing
	if opts := jobMounts(env, bundle.Tools); opts != "" {
		argv = append(argv, "--container-options", opts)
	}
	// the mirrored downloads (D4): every job container sees the bundle's
	// downloads directory read-only at one fixed path, named by
	// URGIT_CI_DOWNLOADS, with index.json mapping URL -> sha256 file
	argv = append(argv, "--env", "URGIT_CI_DOWNLOADS="+jobDownloads)
	// act's cache and artifact servers bind the sandbox's own address on
	// the job network (rider 03: a locked sandbox has no route out for
	// act to guess one from); a backend that names none cannot run
	if env.ServerAddr == "" {
		d.fail(ctx, a, "sandbox names no job-network address for act's servers", logf)
		return
	}
	argv = append(argv, "--cache-server-addr", env.ServerAddr, "--artifact-server-addr", env.ServerAddr)
	for _, e := range plan.PrereqEnv(a.PrereqOutputs) {
		argv = append(argv, "--env", e)
	}
	for _, e := range env.Env {
		argv = append(argv, "--env", e)
	}
	// the credentials the ship released (D4): each becomes an act secret,
	// never an argument in the log; a grant past its expiry or outside
	// the manifest is refused here and the job runs without it. the
	// values are scrubbed from every line before the relay and before
	// the saved stream.
	shown := append([]string(nil), argv...)
	var values []string
	for _, g := range d.usableGrants(a, logf) {
		argv = append(argv, "--secret", g.Name+"="+g.Value)
		shown = append(shown, "--secret", g.Name+"=***")
		values = append(values, g.Value)
	}
	logf("projection-name %s (CI-PROJECT-1.1); running: %s", projectionNameOf(original, a.Attempt, a.Workflow), strings.Join(shown, " "))
	stream, done, err := d.box.Run(ctx, h, env.SourceDir, argv, nil)
	if err != nil {
		d.fail(ctx, a, "act start: "+err.Error(), logf)
		return
	}
	// the deadline bounds act's run (ctx kills it); reporting what act
	// did — the relay of its last lines, the upload, the result — gets a
	// short grace past it, so a job that finished right at the deadline
	// is reported and the ship decides (it refuses a closed attempt,
	// CI-DELIVERY-1.1), rather than abandoned for a context that expired
	// between act's last line and the result. the grace never extends
	// execution: the sandbox is torn down at the deadline regardless.
	rctx, rcancel := reportingContext(ctx, daemonCtx)
	defer rcancel()
	ctx = rctx
	streamPath := filepath.Join(d.cfg.WorkDir, a.Attempt+".act.jsonl")
	summary, relayErr, code, stopped := d.relayJob(ctx, a, h, stream, done, streamPath, values, logf)
	if stopped {
		// Do not spend cancellation's cleanup budget on reporting to a
		// closed/unreachable ship. The deferred Destroy owns teardown.
		return true
	}
	logf("act exited %d; relayed %d line(s) (%d accepted, %d refused, %d dropped); jobResult %q",
		code, summary.Lines, summary.Accepted, summary.Refused, summary.Dropped, summary.JobResult)
	if relayErr != nil {
		d.fail(ctx, a, "relay: "+relayErr.Error(), logf)
		return
	}
	if summary.Closed {
		logf("no result: the ship closed the attempt while act ran")
		return
	}
	if summary.JobResult == "" {
		d.fail(ctx, a, fmt.Sprintf("act exited %d without a jobResult", code), logf)
		return
	}
	// the finished stream goes to the store before the result names it
	// (D1): the ship signs the PUT, the daemon sends the bytes, and the
	// result carries the size and hash. an upload that fails leaves the
	// result without a log; the verdict is the stream's, not the store's.
	logRef := d.uploadLog(ctx, a, streamPath, logf)
	resp, err := d.client.Result(ctx, a.Attempt, summary.JobResult, logRef)
	if err != nil {
		logf("result POST failed: %v", err)
		return
	}
	logf("result %s POST -> %s", summary.JobResult, resp.Error())
	return
}

// reportingContext outlives the attempt's own deadline by two minutes
// (and is cancelled with it when the daemon stops): act is bound by the
// deadline; telling the ship what happened is not.
func reportingContext(ctx, daemonCtx context.Context) (context.Context, context.CancelFunc) {
	grace := 2 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(daemonCtx, deadline.Add(grace))
	}
	return context.WithCancel(daemonCtx)
}

// usableGrants is the assignment's grants minus the ones the daemon
// refuses: an expired grant, or one whose signature does not verify
// against the pinned CI key over THIS attempt's manifest, is never
// passed to act (D4/D5).
func (d *Daemon) usableGrants(a *ship.Assignment, logf func(string, ...any)) []ship.Grant {
	var out []ship.Grant
	now := time.Now().Unix()
	for _, g := range a.Grants {
		if g.Expiry <= now {
			logf("grant %s refused: expired at %s (now %s); not passed to act", g.Name, time.Unix(g.Expiry, 0).UTC().Format(time.RFC3339), time.Unix(now, 0).UTC().Format(time.RFC3339))
			continue
		}
		if a.Manifest == nil {
			logf("grant %s refused: no manifest to bind it to; not passed to act", g.Name)
			continue
		}
		m := sig.ManifestMessage{Recipient: d.daemonID, Attempt: a.Attempt, Operation: "grant:" + g.Name, Expiry: g.Expiry, Nonce: g.Nonce, Manifest: *a.Manifest}
		if err := sig.VerifyManifest(d.ciKey, m, g.Sig); err != nil {
			logf("grant %s refused: %v; not passed to act", g.Name, err)
			continue
		}
		out = append(out, g)
	}
	names := make([]string, 0, len(out))
	for _, g := range out {
		names = append(names, g.Name)
	}
	logf("grants %d (%s) of %d offered", len(out), strings.Join(names, " "), len(a.Grants))
	return out
}

// uploadLog puts the saved act stream in the store under the attempt's
// log.jsonl and returns its handle, or nil with the reason logged.
func (d *Daemon) uploadLog(ctx context.Context, a *ship.Assignment, path string, logf func(string, ...any)) *ship.ObjectRef {
	size, sum, err := fileDigest(path)
	if err != nil {
		logf("log upload skipped: %v", err)
		return nil
	}
	up, err := d.client.RequestUpload(ctx, a.Attempt, "log.jsonl", "application/x-ndjson", sum, size)
	if err != nil {
		logf("log upload refused: %v", err)
		return nil
	}
	if err := d.client.Put(ctx, up, path, size); err != nil {
		logf("log upload to %s failed: %v", up.Key, err)
		return nil
	}
	logf("log uploaded: %s (%d bytes, sha256 %s)", up.Key, size, sum)
	return &ship.ObjectRef{Size: size, SHA256: sum}
}

// fileDigest is the size and lowercase hex sha-256 of a file.
func fileDigest(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// jobDownloads is where every job container sees the bundle's mirrored
// downloads (read-only).
const jobDownloads = "/urgit/downloads"

// jobMounts is the docker run option string for every job container:
// the mirrored downloads at jobDownloads, and each seeded tool (a
// top-level directory of the bundle's toolcache) at
// /opt/hostedtoolcache/<tool> — from the compatibility mode's volumes by
// subpath, or from the guest's paths by bind.
func jobMounts(env sandbox.ExecEnv, tools []string) string {
	var opts []string
	if env.WorkVolume != "" {
		opts = append(opts, fmt.Sprintf("--mount type=volume,src=%s,dst=%s,volume-subpath=downloads,readonly", env.WorkVolume, jobDownloads))
	} else if env.WorkRoot != "" {
		opts = append(opts, fmt.Sprintf("-v %s/downloads:%s:ro", env.WorkRoot, jobDownloads))
	}
	for _, tool := range tools {
		if env.ToolCacheVolume != "" {
			opts = append(opts, fmt.Sprintf("--mount type=volume,src=%s,dst=/opt/hostedtoolcache/%s,volume-subpath=%s", env.ToolCacheVolume, tool, tool))
		} else if env.ToolCache != "" {
			opts = append(opts, fmt.Sprintf("-v %s/%s:/opt/hostedtoolcache/%s", env.ToolCache, tool, tool))
		}
	}
	return strings.Join(opts, " ")
}
