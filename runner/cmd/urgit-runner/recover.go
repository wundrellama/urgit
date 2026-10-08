package main

// The runner's side of recovery ruling A (runner/launcher/INTEGRATION.md
// §8.4): its retentions — the slots it withholds for sandboxes whose
// release it could not prove — listed for selection, inspected, their
// cleanup retried where the runner owns what they name (a docker-rootless
// retention's objects; a microvm one's retry is the launcher operator's),
// and released separately once proven released: a microvm retention by
// the launcher's durable evidence of its exact incarnation's release — its
// absence from the launcher's list is no such proof (INTEGRATION.md §11.8)
// — a Docker one by the absence of its objects at release time. An
// unsettled admission — a reserve request whose outcome was never learnt —
// is released only on the launcher's settlement of exactly that request
// (INTEGRATION.md §11.10; settled-admission ruling 01), never on an empty
// inventory. A legacy retention — one a runner before settled admission
// recorded without its request — is never released here: its release comes
// from Urgit, carried out by the running daemon (legacy-recovery UI ruling
// 01; INTEGRATION.md §11.12). Listing and inspection read the
// state file; a retry and a release hold its lock, so the daemon is
// stopped. A released retention is kept in the file as evidence.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/sandbox"
	"urgit/runner/internal/state"
)

// dockerObjects is what the tool needs of the compatibility backend.
type dockerObjects interface {
	Leftovers(ctx context.Context, h sandbox.Handle) ([]string, error)
	Destroy(ctx context.Context, h sandbox.Handle) error
}

// recovery is the tool's view of the outside, injected: the launcher's
// owner-scoped records (launcherRecords), its evidence of a release
// (launcherReleased, when released is nil) and the Docker objects.
type recovery struct {
	cfg      *config.Config
	records  func(cfg *config.Config, daemonID string) ([]launcher.Record, error)
	released func(cfg *config.Config, daemonID string, ref launcher.Ref) (launcher.Release, error)
	// settle is the launcher's settlement of a reserve request
	// (launcherSettle when nil; INTEGRATION.md §11.10)
	settle func(cfg *config.Config, daemonID, attempt, request string) (launcher.Settlement, error)
	docker dockerObjects // nil: no docker-rootless backend configured
	now    func() time.Time
}

// newRecovery is the production wiring.
func newRecovery(cfg *config.Config) recovery {
	rc := recovery{cfg: cfg, records: launcherRecords, now: time.Now}
	if cfg.Sandbox == "docker-rootless" {
		if box, err := sandbox.NewDocker(cfg.DockerHost); err == nil {
			if d, ok := box.(dockerObjects); ok {
				rc.docker = d
			}
		}
	}
	return rc
}

// view is a retention as the operator sees it.
type view struct {
	Q        state.Quarantine
	Held     []string
	Launcher *launcher.Record // the launcher's record of it (microvm), when it holds one
	Proven   bool             // released at the backend: a release is allowed
	Why      string
}

// fromUrgit is where a legacy retention's release comes from
// (legacy-recovery UI ruling 01; INTEGRATION.md §11.12): the approved origin
// is the Urgit UI, and the daemon that owns the state file carries it out.
const fromUrgit = "a legacy retention is released from Urgit — Settings → Runners → this runner → Retentions — which the running daemon carries out on its evidence (the launcher's protocol and its authoritative inventory, taken again then); this tool never releases one (QUESTIONS-SOURCE-01 §11)"

// handleOf is the sandbox handle q withholds. A Docker entry written before
// retentions named their objects names them by the scheme every Docker
// sandbox of its handle has (INTEGRATION.md §1): the network and container
// are the handle, the volume the handle's -work.
func handleOf(q state.Quarantine) sandbox.Handle {
	h := sandbox.Handle{ID: q.Handle, Attempt: q.Attempt, VM: q.VM, Incarnation: q.Incarnation, CID: q.CID, Created: q.Created,
		Network: q.Network, Volume: q.Volume, Container: q.Container, Label: q.Label}
	if q.Backend != "microvm" && h.Network == "" && h.Volume == "" && h.Container == "" {
		h.Network, h.Volume, h.Container = q.Handle, q.Handle+"-work", q.Handle
	}
	return h
}

// inspect asks the backend what q still holds. It settles nothing: an
// unsettled admission is shown with what the launcher's list holds of its
// request — a presence, never proof of an absence (INTEGRATION.md §11.10).
func (rc recovery) inspect(q state.Quarantine, daemonID string) view {
	v := view{Q: q}
	if microvm(q, rc.cfg) && q.Admission() {
		recs, err := rc.records(rc.cfg, daemonID)
		if err != nil {
			v.Why = "the launcher could not be asked (its serve must run for this): " + err.Error()
			return v
		}
		for _, r := range recs {
			if r.Request == q.Request {
				v.Launcher = &r
				v.Held = []string{fmt.Sprintf("the launcher's record %s (%s), admitted by this request", r.ID, r.State)}
				v.Why = fmt.Sprintf("its reserve request was admitted: the launcher holds %s (%s). That reservation follows its own disposition — the daemon's reconcile at its next start, and an incident's release by the launcher's operator — so this release is refused", r.Ref(), r.State)
				return v
			}
		}
		v.Why = "the launcher's list holds no reservation of its request, but a list is no settlement: its release asks the launcher to settle the request, and is allowed only if the launcher never admitted it (and closes it) or proves the release of what it admitted"
		return v
	}
	if microvm(q, rc.cfg) {
		recs, err := rc.records(rc.cfg, daemonID)
		if err != nil {
			v.Why = "the launcher could not be asked whether it still holds it (its serve must run for this): " + err.Error()
			return v
		}
		attempt := q.Attempt
		if attempt == "" {
			attempt = strings.TrimPrefix(q.Handle, "ci-")
		}
		for _, r := range recs {
			// the exact incarnation, by its token (INTEGRATION.md §11.1)
			exact := q.VM != "" && launcher.Ref{ID: q.VM, Incarnation: q.Incarnation, CID: q.CID, Created: q.Created}.Names(r)
			unknown := q.VM == "" && r.Attempt == attempt
			if exact || unknown {
				v.Launcher = &r
				v.Held = []string{fmt.Sprintf("the launcher's record %s (%s)", r.ID, r.State)}
				v.Why = fmt.Sprintf("the launcher still holds it (%s: %s); its operator retries its cleanup and releases it there first — urgit-vm-launcher recover -select %s — then this release", r.State, r.Reason,
					r.Selection())
				if unknown {
					// an entry recorded without its reserve request: no
					// launcher release is followed by this one (below)
					v.Why = fmt.Sprintf("the launcher holds a reservation of this attempt (%s: %s), which follows its own disposition there — urgit-vm-launcher recover -select %s. This entry names no reserve request, so nothing can settle it here: its slot stays withheld whatever the launcher does (QUESTIONS-SOURCE-01 §11)", r.State, r.Reason,
						r.Selection())
					if q.IsLegacy() {
						v.Why += ". " + fromUrgit
					}
				}
				return v
			}
		}
		if q.VM == "" {
			// recorded without its reserve request: nothing can settle it,
			// and an empty inventory is no settlement (settled-admission
			// ruling 01; INTEGRATION.md §11.10). Policy-dependent: before the
			// ruling this absence of every reservation of the attempt allowed
			// the release. How such an entry, which only an earlier source
			// wrote, is ever released is QUESTIONS-SOURCE-01 §11, open
			v.Why = "the launcher holds no reservation of this attempt now, but this entry names no reserve request, so nothing can settle it: an empty list is no settlement, and its slot stays withheld (QUESTIONS-SOURCE-01 §11)"
			if q.IsLegacy() {
				// legacy-recovery UI ruling 01: its release comes from Urgit,
				// by the running daemon, never from here
				v.Why = "the launcher holds no reservation of this attempt now, and this entry names no reserve request, so nothing this tool asks can settle it: " + fromUrgit
			}
			return v
		}
		// not held: released only on the launcher's durable evidence of this
		// exact incarnation's release, never on its absence (§11.8)
		released := rc.released
		if released == nil {
			released = launcherReleased
		}
		rel, err := released(rc.cfg, daemonID, launcher.Ref{ID: q.VM, Incarnation: q.Incarnation, CID: q.CID, Created: q.Created})
		if err != nil {
			v.Why = "the launcher holds no record of this incarnation, but its release is not proven: " + err.Error()
			return v
		}
		v.Proven, v.Why = true, "released by the launcher, on its evidence: "+describeRelease(rel)
		return v
	}
	if rc.docker == nil {
		v.Why = "this runner is not configured for docker-rootless: its objects cannot be looked at"
		return v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	left, err := rc.docker.Leftovers(ctx, handleOf(q))
	switch {
	case err != nil:
		v.Held, v.Why = left, "its objects could not all be looked at: "+err.Error()
	case len(left) > 0:
		v.Held, v.Why = left, "its objects remain ("+strings.Join(left, ", ")+"): retry its cleanup"
	default:
		v.Proven, v.Why = true, "none of its objects remain"
	}
	return v
}

// settleAdmission is the launcher's settlement of q's reserve request, taken
// now (INTEGRATION.md §11.10): the release of an unsettled admission is
// allowed only when the launcher never admitted it and has closed it, or
// proves the release of what it admitted. An admitted request's
// reservation refuses it, and so does any answer that settles nothing.
func (rc recovery) settleAdmission(q state.Quarantine, daemonID string) view {
	v := view{Q: q}
	settle := rc.settle
	if settle == nil {
		settle = launcherSettle
	}
	s, err := settle(rc.cfg, daemonID, q.Attempt, q.Request)
	switch {
	case err != nil:
		v.Why = "its reserve request is not settled: " + err.Error()
	case s.Outcome == launcher.SettledAdmitted && s.Record != nil:
		r := *s.Record
		v.Launcher = &r
		v.Held = []string{fmt.Sprintf("the launcher's record %s (%s), admitted by this request", r.ID, r.State)}
		v.Why = fmt.Sprintf("its reserve request was admitted as %s (%s): that reservation follows its own disposition — the daemon's reconcile at its next start, and an incident's release by the launcher's operator", r.Ref(), r.State)
	case s.Outcome == launcher.SettledReleased && s.Release != nil:
		v.Proven, v.Why = true, "settled: the reservation its request admitted has been released, on the launcher's evidence: "+describeRelease(*s.Release)
	case s.Outcome == launcher.SettledClosed:
		v.Proven, v.Why = true, "settled: the launcher never admitted its request, and closed it for good ("+s.Why+")"
	default:
		v.Why = fmt.Sprintf("the launcher's answer settles nothing (%q)", s.Outcome)
	}
	return v
}

// describeRelease is a launcher release's disposition as the operator reads
// it, its late bookkeeping said.
func describeRelease(rel launcher.Release) string {
	at := func(n int64) string {
		if n == 0 {
			return "(not recorded)"
		}
		return time.Unix(0, n).UTC().Format(time.RFC3339Nano)
	}
	out := rel.By
	if rel.DueUnixNano != 0 {
		out += fmt.Sprintf(" (its cleanup finished at %s, before its obligation's deadline %s)", at(rel.CleanedUnixNano), at(rel.DueUnixNano))
	}
	switch {
	case rel.Late():
		out += fmt.Sprintf("; LATE BOOKKEEPING: its accounting was confirmed at %s, not before that deadline", at(rel.ConfirmedUnixNano))
	case rel.ConfirmedUnixNano != 0:
		out += "; its accounting confirmed at " + at(rel.ConfirmedUnixNano)
	}
	return out
}

func when(unix int64) string {
	if unix == 0 {
		return "(not recorded)"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func labelOf(q state.Quarantine) string {
	if q.Label != "" {
		return q.Label
	}
	return "(no job label)"
}

func identityOf(q state.Quarantine) string {
	switch {
	case q.VM != "" && q.Incarnation != "":
		return fmt.Sprintf("launcher record %s, incarnation %s (cid %d, created %s)", q.VM, q.Incarnation, q.CID, when(q.Created))
	case q.VM != "":
		return fmt.Sprintf("launcher record %s, cid %d, created %s (no incarnation token: written before tokens)", q.VM, q.CID, when(q.Created))
	case q.Container != "" || q.Network != "":
		return fmt.Sprintf("container %s, volume %s, network %s", q.Container, q.Volume, q.Network)
	case q.Admission():
		return fmt.Sprintf("reserve request %s, its outcome not settled (the daemon settles it at each reconcile; its release settles it too)", q.Request)
	case q.IsLegacy():
		return "a launcher reservation whose identity was never learnt, recorded without its reserve request by a runner before settled admission (legacy)"
	case q.Backend == "microvm":
		return "a launcher reservation whose identity was never learnt, recorded without its reserve request"
	}
	return "recorded before retentions named their backend (matched by its attempt)"
}

func printRetention(out io.Writer, n int, q state.Quarantine) {
	backend := q.Backend
	if backend == "" {
		backend = "(backend not recorded)"
	}
	if q.IsLegacy() {
		backend += ", legacy: released from Urgit"
	}
	fmt.Fprintf(out, "  [%d] %s — attempt %s, %s\n", n, labelOf(q), q.Attempt, backend)
	fmt.Fprintf(out, "      %s; retained %s, revision %d\n", identityOf(q), when(q.At), q.Rev)
	fmt.Fprintf(out, "      why: %s\n", q.Reason)
}

func printView(out io.Writer, v view) {
	q := v.Q
	fmt.Fprintf(out, "retention: %s\n", labelOf(q))
	fmt.Fprintf(out, "  handle      %s (attempt %s, %s)\n", q.Handle, q.Attempt, q.Backend)
	fmt.Fprintf(out, "  identity    %s\n", identityOf(q))
	fmt.Fprintf(out, "  select      %s\n", q.Selection())
	fmt.Fprintf(out, "  retained    %s: %s\n", when(q.At), q.Reason)
	if q.Legacy != nil {
		fmt.Fprintf(out, "  provenance  legacy: in a state file an earlier runner wrote last (format %d), first loaded by this version at %s\n", q.Legacy.Format, when(q.Legacy.Found))
	}
	fmt.Fprintf(out, "  charge      one slot of this runner, withheld until its release\n")
	if r := v.Launcher; r != nil && r.Incident != nil {
		inc := r.Incident
		missed := "met"
		if inc.Missed {
			missed = "MISSED"
		}
		fmt.Fprintf(out, "  launcher    %s: %s, due %s, obligation %s; %d cleanup attempt(s)\n", r.ID, inc.Trigger, when(inc.Due), missed, inc.Count)
		for _, a := range inc.Attempts {
			fmt.Fprintf(out, "    %s %s: %s %s\n", when(a.At), a.By, a.Result, a.Detail)
		}
	}
	for _, a := range q.Attempts {
		fmt.Fprintf(out, "  retry       %s %s: %s %s %s\n", when(a.At), a.By, a.Result, strings.Join(a.Left, ", "), a.Detail)
	}
	if len(v.Held) > 0 {
		fmt.Fprintf(out, "  holds       %s\n", strings.Join(v.Held, ", "))
	} else if v.Proven {
		fmt.Fprintf(out, "  holds       nothing\n")
	}
	verdict := "refused"
	if v.Proven {
		verdict = "allowed"
	}
	fmt.Fprintf(out, "  release     %s: %s\n", verdict, v.Why)
}

// load is the state file as the tool reads it, deduplicated as the daemon
// would load it.
func load(cfg *config.Config) (*state.State, error) {
	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errors.New("no state file (the daemon never enrolled)")
	}
	st.Dedupe()
	return st, nil
}

// act is a retry or a release of exactly sel, holding the state file's
// lock for its run (the daemon stopped). It returns the exit status: 0
// done, 1 refused or unresolved, 2 the daemon runs or the state file could
// not be read or saved.
func (rc recovery) act(sel, action string, out io.Writer) int {
	lock, err := state.Acquire(rc.cfg.StateFile)
	if err != nil {
		fmt.Fprintf(out, "%s refused: %v; stop the daemon first\n", action, err)
		return 2
	}
	defer lock.Release()
	st, err := load(rc.cfg)
	if err != nil {
		fmt.Fprintf(out, "%s: %v\n", action, err)
		return 2
	}
	i, err := st.Find(sel, action == "release")
	if err != nil {
		fmt.Fprintf(out, "%s refused: %v\n", action, err)
		return 1
	}
	q := st.Quarantined[i]
	switch action {
	case "retry":
		if q.Admission() {
			fmt.Fprintln(out, "an unsettled admission has nothing to clean up: its release settles its reserve request")
			return 1
		}
		if microvm(q, rc.cfg) {
			v := rc.inspect(q, st.DaemonID)
			fmt.Fprintf(out, "a microvm retention's cleanup is the launcher operator's (root, serve stopped), never this runner's: %s\n", v.Why)
			return 1
		}
		if rc.docker == nil {
			fmt.Fprintln(out, "this runner is not configured for docker-rootless: its objects cannot be removed from here")
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		derr := rc.docker.Destroy(ctx, handleOf(q))
		cancel()
		v := rc.inspect(q, st.DaemonID)
		a := state.Attempt{At: rc.now().Unix(), By: "operator retry", Result: "resolved", Left: v.Held}
		if !v.Proven {
			a.Result = "unresolved"
		}
		if derr != nil {
			a.Detail = derr.Error()
		}
		st.Record(i, a)
		if err := state.Save(rc.cfg.StateFile, st); err != nil {
			fmt.Fprintf(out, "the retry ran (%s) but its record is not saved durably: %v; run it again\n", a.Result, err)
			return 2
		}
		v.Q = st.Quarantined[i]
		fmt.Fprintf(out, "cleanup retried: %s; the retention stays until its release\n", a.Result)
		printView(out, v)
		if !v.Proven {
			return 1
		}
		return 0
	case "release":
		// the proof is taken now, under the lock: never an earlier look — an
		// unsettled admission's is the launcher's settlement of its request
		v := rc.inspect(q, st.DaemonID)
		if microvm(q, rc.cfg) && q.Admission() {
			v = rc.settleAdmission(q, st.DaemonID)
		}
		if !v.Proven {
			fmt.Fprintf(out, "release refused: %s\n", v.Why)
			return 1
		}
		released := st.Release(i, rc.now().Unix())
		if err := state.Save(rc.cfg.StateFile, st); err != nil {
			fmt.Fprintf(out, "the release of %s is not recorded durably: %v; nothing is released — run it again\n", released.Handle, err)
			return 2
		}
		fmt.Fprintf(out, "released %s (%s): the slot returns at the daemon's next start; the entry is kept as released evidence\n", released.Handle, v.Why)
		return 0
	}
	fmt.Fprintln(out, "-action is inspect, retry or release")
	return 2
}

// scripted is -recover -select <sel> -action inspect|retry|release.
func (rc recovery) scripted(sel, action string, out io.Writer) int {
	if action != "inspect" {
		return rc.act(sel, action, out)
	}
	st, err := load(rc.cfg)
	if err != nil {
		fmt.Fprintf(out, "inspect: %v\n", err)
		return 2
	}
	i, err := st.Find(sel, false)
	if err != nil {
		fmt.Fprintf(out, "inspect: %v\n", err)
		return 1
	}
	printView(out, rc.inspect(st.Quarantined[i], st.DaemonID))
	if cur := st.Quarantined[i].Selection(); cur != strings.TrimSpace(sel) {
		fmt.Fprintf(out, "note: it changed since that selection (now %s): select it again to release it\n", cur)
	}
	return 0
}

// interactive is the operator's session on in/out.
func (rc recovery) interactive(in io.Reader, out io.Writer) int {
	lines := bufio.NewScanner(in)
	read := func(prompt string) (string, bool) {
		fmt.Fprint(out, prompt)
		if !lines.Scan() {
			fmt.Fprintln(out)
			return "", false
		}
		return strings.TrimSpace(lines.Text()), true
	}
	for {
		st, err := load(rc.cfg)
		if err != nil {
			fmt.Fprintf(out, "recover: %v\n", err)
			return 2
		}
		if len(st.Quarantined) == 0 {
			fmt.Fprintln(out, "no retentions: every slot of this runner is offered")
			return 0
		}
		fmt.Fprintln(out, "retentions (slots withheld until the operator's release):")
		for i, q := range st.Quarantined {
			printRetention(out, i+1, q)
		}
		answer, ok := read(fmt.Sprintf("select a retention [1-%d], or q to quit: ", len(st.Quarantined)))
		if !ok || answer == "q" {
			return 0
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(st.Quarantined) {
			fmt.Fprintf(out, "there is no retention %q\n", answer)
			continue
		}
		sel := st.Quarantined[n-1].Selection()
		daemonID := st.DaemonID
	session:
		for {
			cur, err := load(rc.cfg)
			if err != nil {
				fmt.Fprintf(out, "recover: %v\n", err)
				return 2
			}
			i, err := cur.Find(sel, false)
			if err != nil {
				fmt.Fprintf(out, "%v\n", err)
				break
			}
			sel = cur.Quarantined[i].Selection()
			printView(out, rc.inspect(cur.Quarantined[i], daemonID))
			a, ok := read("action: [r] retry its cleanup, [R] release it, [b] back to the list, [q] quit: ")
			if !ok || a == "q" {
				return 0
			}
			switch a {
			case "b":
				break session
			case "r":
				rc.act(sel, "retry", out)
			case "R":
				confirm, ok := read(fmt.Sprintf("type release to release %s (%s): ", labelOf(cur.Quarantined[i]), sel))
				if !ok || confirm != "release" {
					fmt.Fprintln(out, "not released")
					continue
				}
				if rc.act(sel, "release", out) == 0 {
					break session
				}
			default:
				fmt.Fprintf(out, "no action %q\n", a)
			}
		}
	}
}
