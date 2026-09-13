// Package daemon is the runner loop (D7): enroll once, reconcile orphans,
// then long-poll the ship and carry out each assignment in a sandbox.
// Every branch that chooses between outcomes here traces to a field the
// ship sent or a line act emitted; the daemon decides nothing itself.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"urgit/runner/internal/act"
	"urgit/runner/internal/config"
	"urgit/runner/internal/plan"
	"urgit/runner/internal/relay"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

// ExitEnrollmentLost is the exit status after the ship answers 401.
const ExitEnrollmentLost = 3

// ExitNoCapacity is the exit status once every slot is quarantined.
const ExitNoCapacity = 4

type Daemon struct {
	cfg      *config.Config
	box      sandbox.Sandbox
	client   *ship.Client
	daemonID string
	log      *log.Logger

	mu          sync.Mutex
	capacity    int
	quarantined []string
	slots       chan struct{}

	// checkout materializes the candidate on the host; tests replace it
	checkout func(ctx context.Context, a *ship.Assignment, dir string) error
}

// New loads or performs enrollment and constructs the sandbox backend.
func New(ctx context.Context, cfg *config.Config, logger *log.Logger) (*Daemon, error) {
	box, err := sandbox.New(cfg.Sandbox, cfg.DockerHost)
	if err != nil {
		return nil, err
	}
	d := &Daemon{cfg: cfg, box: box, log: logger, capacity: cfg.Capacity}
	d.checkout = d.gitCheckout
	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	if st == nil {
		if cfg.EnrollToken == "" {
			return nil, errors.New("no state file and no enroll_token: mint one on the ship (:urgit-ci|mint-enroll-token) and put it in the config")
		}
		d.log.Printf("enrolling with the ship at %s", cfg.ShipURL)
		client := ship.New(cfg.ShipURL, "")
		id, bearer, err := client.Enroll(ctx, cfg.EnrollToken, cfg.Capacity, cfg.Sandbox)
		if err != nil {
			return nil, err
		}
		st = &state.State{DaemonID: id, Bearer: bearer, ShipURL: cfg.ShipURL}
		if err := state.Save(cfg.StateFile, st); err != nil {
			return nil, fmt.Errorf("state file: %w", err)
		}
		d.log.Printf("enrolled as daemon %s; state written to %s (mode 0600)", id, cfg.StateFile)
	} else {
		d.log.Printf("state file %s present: daemon %s, no re-enrollment", cfg.StateFile, st.DaemonID)
	}
	// the token is consumed: forget it so it is never logged or written
	cfg.EnrollToken = ""
	d.client = ship.New(cfg.ShipURL, st.Bearer)
	d.daemonID = st.DaemonID
	d.slots = make(chan struct{}, cfg.Capacity)
	for i := 0; i < cfg.Capacity; i++ {
		d.slots <- struct{}{}
	}
	return d, nil
}

// Banner is the startup disclosure (CI-SANDBOX-1-B).
func (d *Daemon) Banner() string {
	return fmt.Sprintf("urgit-runner: daemon %s, ship %s, capacity %d, act %s, sandbox: %s",
		d.daemonID, d.cfg.ShipURL, d.cfg.Capacity, act.Version, d.box.Name())
}

// Reconcile (D7 c): every sandbox left from a previous life is destroyed
// when the ship says its attempt is terminal or unknown; a running one is
// left to its deadline, never resumed.
func (d *Daemon) Reconcile(ctx context.Context) error {
	orphans, err := d.box.Orphans(ctx)
	if err != nil {
		return err
	}
	for _, id := range orphans {
		attempt := strings.TrimPrefix(id, "ci-")
		status, found, err := d.client.AttemptStatus(ctx, attempt)
		if errors.Is(err, ship.ErrUnauthorized) {
			return err
		}
		if err != nil {
			d.log.Printf("reconcile %s: ship unreachable: %v (kept)", id, err)
			continue
		}
		if found && status == "running" {
			d.log.Printf("reconcile %s: attempt still running on the ship; left for its deadline, not resumed", id)
			continue
		}
		reason := "unknown to the ship"
		if found {
			reason = "attempt " + status
		}
		h := sandbox.Handle{ID: id, Network: id, Volume: id + "-work", Container: id}
		if err := d.box.Destroy(ctx, h); err != nil {
			d.log.Printf("reconcile %s (%s): destroy failed: %v", id, reason, err)
			d.quarantine(id, err)
			continue
		}
		d.log.Printf("reconcile %s (%s): destroyed", id, reason)
	}
	return nil
}

// Run polls until the context ends, the enrollment is lost, or every
// slot is quarantined. The exit status is the caller's to use.
func (d *Daemon) Run(ctx context.Context) int {
	var wg sync.WaitGroup
	for {
		if d.remainingCapacity() == 0 {
			d.log.Printf("capacity 0: every slot is quarantined (%s); stopping", strings.Join(d.quarantined, ", "))
			wg.Wait()
			return ExitNoCapacity
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return 0
		case <-d.slots:
		}
		assignment, err := d.client.Poll(ctx, d.daemonID)
		if errors.Is(err, ship.ErrUnauthorized) {
			d.log.Printf("enrollment lost; re-enroll with a fresh token")
			wg.Wait()
			return ExitEnrollmentLost
		}
		if err != nil {
			if ctx.Err() != nil {
				d.slots <- struct{}{}
				wg.Wait()
				return 0
			}
			d.log.Printf("poll: %v; retrying in 5 s", err)
			d.slots <- struct{}{}
			time.Sleep(5 * time.Second)
			continue
		}
		if assignment == nil {
			d.slots <- struct{}{}
			continue
		}
		wg.Add(1)
		go func(a *ship.Assignment) {
			defer wg.Done()
			keep := d.handle(ctx, a)
			if keep {
				d.slots <- struct{}{}
			}
		}(assignment)
	}
}

func (d *Daemon) remainingCapacity() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.capacity
}

func (d *Daemon) quarantine(handle string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capacity--
	d.quarantined = append(d.quarantined, handle)
	d.log.Printf("QUARANTINED slot %s: teardown failed: %v; advertised capacity now %d", handle, err, d.capacity)
}

// handle carries out one assignment; it returns whether the slot may be
// reused (false after a failed teardown).
func (d *Daemon) handle(ctx context.Context, a *ship.Assignment) (keep bool) {
	logf := func(format string, args ...any) {
		d.log.Printf("[%s %s] "+format, append([]any{a.Kind, a.Attempt}, args...)...)
	}
	logf("assignment %s: candidate %s repo %s ref %s oid %s deadline %ds workflow %q job %q",
		a.ID, a.Candidate, a.Repo, a.Ref, a.OID, a.DeadlineSeconds, a.Workflow, a.Job)
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

	spec := sandbox.Spec{Image: d.cfg.ActImage, CPUs: d.cfg.CPUs, MemoryMiB: d.cfg.MemoryMiB, DiskMiB: d.cfg.DiskMiB, Network: "ci-" + a.Attempt}
	h, err := d.box.Prepare(actx, spec)
	if err != nil {
		d.fail(actx, a, "sandbox prepare: "+err.Error(), logf)
		return true
	}
	logf("sandbox %s prepared (network %s, volume %s)", h.ID, h.Network, h.Volume)
	keep = true
	defer func() {
		// teardown uses a fresh context: the deadline may have passed
		dctx, dcancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer dcancel()
		if err := d.box.Destroy(dctx, h); err != nil {
			d.quarantine(h.ID, err)
			keep = false
			return
		}
		logf("sandbox %s destroyed", h.ID)
	}()

	src := filepath.Join(work, "src")
	if err := d.checkout(actx, a, src); err != nil {
		d.fail(actx, a, "checkout: "+err.Error(), logf)
		return keep
	}
	if err := d.box.Copy(actx, h, src+"/.", "/work/src"); err != nil {
		d.fail(actx, a, "copy checkout: "+err.Error(), logf)
		return keep
	}
	actBinary, err := exec.LookPath(d.cfg.ActBinary)
	if err != nil {
		d.fail(actx, a, "act_binary: "+err.Error(), logf)
		return keep
	}
	if err := d.box.Copy(actx, h, actBinary, "/usr/local/bin/act"); err != nil {
		d.fail(actx, a, "copy act: "+err.Error(), logf)
		return keep
	}

	switch a.Kind {
	case "plan":
		d.runPlan(actx, a, h, src, logf)
	case "job":
		d.runJob(actx, a, h, work, src, logf)
	default:
		d.fail(actx, a, "unknown assignment kind "+a.Kind, logf)
	}
	return keep
}

// fail reports that no result is coming. A plan attempt gets the reason
// as its (refused) plan; a job attempt is abandoned (D8).
func (d *Daemon) fail(ctx context.Context, a *ship.Assignment, reason string, logf func(string, ...any)) {
	logf("no result: %s", reason)
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if a.Kind == "plan" {
		resp, err := d.client.Plan(rctx, a.Attempt, map[string]string{"error": reason})
		if err != nil {
			logf("plan error POST failed: %v", err)
			return
		}
		logf("plan error POST -> %d", resp.Status)
		return
	}
	resp, err := d.client.Abandon(rctx, a.Attempt, reason)
	if err != nil {
		logf("abandon POST failed: %v", err)
		return
	}
	logf("abandon POST -> %d", resp.Status)
}

// gitCheckout clones the repository from the ship's Git endpoint and
// checks out the candidate oid, reachable through refs/ci/candidate/<id>
// (D9).
func (d *Daemon) gitCheckout(ctx context.Context, a *ship.Assignment, dir string) error {
	url := strings.TrimRight(d.cfg.ShipURL, "/") + "/git/" + a.Repo
	steps := [][]string{
		{"git", "clone", "--quiet", "--no-checkout", url, dir},
		{"git", "-C", dir, "fetch", "--quiet", "origin", "refs/ci/candidate/" + a.Candidate},
		{"git", "-C", dir, "checkout", "--quiet", a.OID},
	}
	for _, argv := range steps {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s", strings.Join(argv[:3], " "), strings.TrimSpace(string(out)))
		}
	}
	head, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(head)); got != a.OID {
		return fmt.Errorf("checked out %s, not the candidate %s", got, a.OID)
	}
	return nil
}

// runPlan: act -l per workflow file inside the sandbox, the YAML walk on
// the host checkout, one POST. act refusing a file is the plan's error.
func (d *Daemon) runPlan(ctx context.Context, a *ship.Assignment, h sandbox.Handle, src string, logf func(string, ...any)) {
	files, err := workflowFiles(src)
	if err != nil {
		d.fail(ctx, a, "workflows: "+err.Error(), logf)
		return
	}
	type wireJob struct {
		ID       string     `json:"id"`
		Workflow string     `json:"workflow"`
		Stage    int        `json:"stage"`
		Needs    []string   `json:"needs"`
		Cond     *plan.Cond `json:"cond"`
		Matrix   bool       `json:"matrix"`
		Events   []string   `json:"events"`
	}
	body := struct {
		OID       string    `json:"oid"`
		Workflows []string  `json:"workflows"`
		Jobs      []wireJob `json:"jobs"`
	}{OID: a.OID, Workflows: files, Jobs: []wireJob{}}
	for _, file := range files {
		out, code, err := d.capture(ctx, h, "/work/src", []string{"sh", "-c", "act -l -W '.github/workflows/" + file + "' 2>&1"}, nil)
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
			body.Jobs = append(body.Jobs, wireJob{
				ID: row.JobID, Workflow: file, Stage: row.Stage, Needs: info.Needs,
				Cond: info.Cond, Matrix: info.Matrix, Events: row.Events,
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

func workflowFiles(src string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(src, ".github", "workflows"))
	if errors.Is(err, os.ErrNotExist) {
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

// runJob: project the workflow to the assigned job (CI-PROJECT-1), run
// act on the projection with the isolated network, relay every event
// line, then claim the jobResult the stream carried or abandon.
func (d *Daemon) runJob(ctx context.Context, a *ship.Assignment, h sandbox.Handle, work, src string, logf func(string, ...any)) {
	if a.Workflow == "" || a.Job == "" {
		d.fail(ctx, a, "job assignment without workflow and job", logf)
		return
	}
	original, err := os.ReadFile(filepath.Join(src, ".github", "workflows", a.Workflow))
	if err != nil {
		d.fail(ctx, a, "workflow: "+err.Error(), logf)
		return
	}
	projected, err := plan.Project(original, a.Job)
	if err != nil {
		d.fail(ctx, a, "projection: "+err.Error(), logf)
		return
	}
	projectedPath := filepath.Join(work, "projected", a.Workflow)
	if err := os.MkdirAll(filepath.Dir(projectedPath), 0o755); err != nil {
		d.fail(ctx, a, err.Error(), logf)
		return
	}
	if err := os.WriteFile(projectedPath, projected, 0o644); err != nil {
		d.fail(ctx, a, err.Error(), logf)
		return
	}
	if err := d.box.Copy(ctx, h, projectedPath, "/work/projected/"+a.Workflow); err != nil {
		d.fail(ctx, a, "copy projection: "+err.Error(), logf)
		return
	}
	argv := []string{
		"act", "push",
		"-W", "/work/projected/" + a.Workflow,
		"-j", a.Job,
		"-P", "ubuntu-latest=" + d.cfg.ActImage,
		"--network", h.Network,
		"--json", "--pull=false",
		"--cache-server-path", "/work/cache",
		"--artifact-server-path", "/work/artifacts",
	}
	for _, e := range plan.PrereqEnv(a.PrereqOutputs) {
		argv = append(argv, "--env", e)
	}
	logf("running: %s", strings.Join(argv, " "))
	stream, done, err := d.box.Run(ctx, h, "/work/src", argv, nil)
	if err != nil {
		d.fail(ctx, a, "act start: "+err.Error(), logf)
		return
	}
	streamLog, _ := os.Create(filepath.Join(d.cfg.WorkDir, a.Attempt+".act.jsonl"))
	tee := io.TeeReader(stream, streamLog)
	summary, relayErr := relay.Relay(ctx, tee, func(ctx context.Context, line []byte) (int, []byte, error) {
		resp, err := d.client.Event(ctx, a.Attempt, line)
		return resp.Status, resp.Body, err
	}, func(msg string) { logf("%s", msg) })
	_ = stream.Close()
	if streamLog != nil {
		_ = streamLog.Close()
	}
	code := <-done
	logf("act exited %d; relayed %d line(s) (%d accepted, %d refused, %d dropped); jobResult %q",
		code, summary.Lines, summary.Accepted, summary.Refused, summary.Dropped, summary.JobResult)
	if relayErr != nil {
		d.fail(ctx, a, "relay: "+relayErr.Error(), logf)
		return
	}
	if summary.JobResult == "" {
		d.fail(ctx, a, fmt.Sprintf("act exited %d without a jobResult", code), logf)
		return
	}
	resp, err := d.client.Result(ctx, a.Attempt, summary.JobResult)
	if err != nil {
		logf("result POST failed: %v", err)
		return
	}
	logf("result %s POST -> %s", summary.JobResult, resp.Error())
}
