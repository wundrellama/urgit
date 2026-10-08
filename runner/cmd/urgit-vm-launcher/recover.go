package main

// recover is the operator's incident tooling (recovery ruling A;
// runner/launcher/INTEGRATION.md §8.4). It lists the incidents — the
// quarantined records — for selection by number; shows the one selected
// (its job, its exact incarnation, the obligation it was quarantined under,
// every cleanup attempt, what it still holds, the charge it withholds, and
// why a release is allowed or refused); retries its cleanup, which never
// releases; and releases it, separately and confirmed, on the exact
// incarnation at the revision shown. Listing and inspection only read the
// state directory, so they work while serve runs; a retry or a release owns
// it, so serve is stopped first.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"urgit/runner/internal/launcher"
)

// incidents is every incident in the state directory, read only: the
// quarantined records, oldest first, as the core inspects them.
func incidents(cfg *Config) ([]launcher.Inspection, []launcher.Problem, error) {
	snap, err := launcher.ReadState(cfg.StateDir, cfg.IDPrefix)
	if err != nil {
		return nil, nil, err
	}
	var out []launcher.Inspection
	for _, r := range snap.Records {
		if r.State == launcher.StateQuarantined {
			out = append(out, launcher.InspectRecord(r))
		}
	}
	return out, snap.Problems, nil
}

// lookup is the incident of sel's exact incarnation as it stands now (any
// revision), read only: the incarnation by its token (INTEGRATION.md
// §11.1). Not finding it among the records it can read is no proof of its
// release while an entry is unaccounted for (§11.7): that is an error, not
// a not-found.
func lookup(cfg *Config, sel launcher.Selection) (launcher.Inspection, bool, error) {
	list, problems, err := incidents(cfg)
	if err != nil {
		return launcher.Inspection{}, false, err
	}
	for _, in := range list {
		if sel.Ref().Names(in.Record) {
			return in, true, nil
		}
	}
	if len(problems) > 0 {
		return launcher.Inspection{}, false, fmt.Errorf("no incident of %s among the records this launcher can read, but %d state entries could not be accounted for (first: %s): any of them may be it, so its absence proves nothing", sel.Ref(), len(problems), problems[0])
	}
	return launcher.Inspection{}, false, nil
}

// absentWhy is what the launcher's evidence says of sel's incarnation when
// it is no incident here (INTEGRATION.md §11.8): released, with its
// disposition — or no evidence of a release, which no absence stands in for.
func absentWhy(cfg *Config, sel launcher.Selection) string {
	ev, found, err := launcher.ReadEvidence(cfg.StateDir, sel.Ref())
	switch {
	case err != nil:
		return "its evidence could not be read: " + err.Error()
	case !found:
		return "no evidence of its release is kept: another incarnation may hold its id now, or its record is gone without one"
	}
	rel := launcher.Release{By: launcher.ByOperator}
	if ev.Release != nil {
		rel = *ev.Release
	}
	out := "released (" + rel.By
	if rel.Late() {
		out += "; LATE BOOKKEEPING: its accounting was not confirmed before its obligation's deadline"
	}
	return out + "; its evidence is kept under released/)"
}

// act is a retry or a release of exactly sel: it owns the state directory
// for its run — refused while serve holds it — and answers the incident as
// the core left it (nothing, after a release).
func act(host *realHost, sel launcher.Selection, action string) (launcher.Inspection, error) {
	if action != "retry" && action != "release" {
		return launcher.Inspection{}, fmt.Errorf("unknown action %q (inspect, retry, release)", action)
	}
	svc, err := host.service()
	if err != nil {
		if errors.Is(err, launcher.ErrStateBusy) {
			return launcher.Inspection{}, fmt.Errorf("%w: stop serve first — a retry and a release own the state directory", err)
		}
		return launcher.Inspection{}, err
	}
	var in launcher.Inspection
	if action == "retry" {
		in, err = svc.RetryCleanup(sel)
	} else {
		err = svc.Release(sel)
	}
	return in, errors.Join(err, svc.Close())
}

// incarnationOf is a token as the listing shows it.
func incarnationOf(token string) string {
	if token == "" {
		return "(none: written before tokens)"
	}
	return token
}

func stamp(unix int64) string {
	if unix == 0 {
		return "(not recorded)"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func labelOf(in launcher.Inspection) string {
	if in.Record.Label != "" {
		return in.Record.Label
	}
	return "(no job label)"
}

func holdsOf(in launcher.Inspection) string {
	if len(in.Remaining) == 0 {
		return "nothing"
	}
	return strings.Join(in.Remaining, ", ")
}

func obligationOf(in launcher.Inspection) string {
	inc := in.Record.Incident
	if inc == nil {
		return "not recorded (quarantined before incidents were recorded)"
	}
	met := "met: quarantined in time"
	switch {
	case inc.Missed:
		met = "MISSED: its cleanup began after it"
	case inc.Due == 0:
		met = "whether it was met is not recorded"
	}
	return fmt.Sprintf("%s; due %s; %s", inc.Trigger, stamp(inc.Due), met)
}

// printSummary is one numbered line of the list.
func printSummary(out io.Writer, n int, in launcher.Inspection) {
	attempts, missed := 0, "not recorded"
	if inc := in.Record.Incident; inc != nil {
		attempts = inc.Count
		switch {
		case inc.Missed:
			missed = "MISSED"
		case inc.Due != 0:
			missed = "met"
		}
	}
	fmt.Fprintf(out, "  [%d] %s — attempt %s, daemon %s\n", n, labelOf(in), in.Record.Attempt, in.Record.Owner.Daemon)
	fmt.Fprintf(out, "      %s · incarnation %s · cid %d · created %s · revision %d\n", in.Selection.ID, incarnationOf(in.Selection.Incarnation), in.Selection.CID, stamp(in.Selection.Created), in.Selection.Rev)
	fmt.Fprintf(out, "      obligation %s; holds %s; charge %d cpu, %d MiB, 1 guest; %d cleanup attempt(s)\n", missed, holdsOf(in), in.CPUs, in.MemoryMiB, attempts)
}

// printInspection is the incident in full.
func printInspection(out io.Writer, in launcher.Inspection) {
	r := in.Record
	fmt.Fprintf(out, "incident: %s\n", labelOf(in))
	fmt.Fprintf(out, "  attempt     %s (daemon %s)\n", r.Attempt, r.Owner.Daemon)
	fmt.Fprintf(out, "  record      %s, incarnation %s, cid %d, created %s, revision %d\n", in.Selection.ID, incarnationOf(in.Selection.Incarnation), in.Selection.CID, stamp(in.Selection.Created), in.Selection.Rev)
	fmt.Fprintf(out, "  select      %s\n", in.Selection)
	fmt.Fprintf(out, "  obligation  %s\n", obligationOf(in))
	fmt.Fprintf(out, "  holds       %s\n", holdsOf(in))
	fmt.Fprintf(out, "  charge      %d cpu, %d MiB (guest and overhead), 1 guest — withheld until its release\n", in.CPUs, in.MemoryMiB)
	fmt.Fprintf(out, "  reason      %s\n", r.Reason)
	if inc := r.Incident; inc != nil {
		fmt.Fprintf(out, "  attempts    %d in all%s:\n", inc.Count, map[bool]string{true: fmt.Sprintf(" (the latest %d shown)", len(inc.Attempts)), false: ""}[inc.Count > len(inc.Attempts)])
		for _, a := range inc.Attempts {
			left := ""
			if len(a.Left) > 0 {
				left = " (left " + strings.Join(a.Left, ", ") + ")"
			}
			detail := ""
			if a.Detail != "" {
				detail = ": " + a.Detail
			}
			fmt.Fprintf(out, "    %s %s: %s%s%s\n", stamp(a.At), a.By, a.Result, left, detail)
		}
	}
	verdict := "refused"
	if in.Releasable {
		verdict = "allowed"
	}
	fmt.Fprintf(out, "  release     %s: %s\n", verdict, in.Why)
}

// recoverScripted is -select/-action: the same checks as the interactive
// session, and no shortcut past them.
func recoverScripted(host *realHost, selection, action string, out io.Writer) int {
	sel, err := launcher.ParseSelection(selection)
	if err != nil {
		fmt.Fprintf(out, "recover: %v\n", err)
		return 2
	}
	switch action {
	case "inspect":
		in, ok, err := lookup(host.cfg, sel)
		switch {
		case err != nil:
			fmt.Fprintf(out, "recover: %v\n", err)
			return 1
		case !ok:
			fmt.Fprintf(out, "recover: no incident of %s: %s\n", sel.Ref(), absentWhy(host.cfg, sel))
			return 1
		}
		printInspection(out, in)
		if in.Selection.Rev != sel.Rev {
			fmt.Fprintf(out, "note: it changed since that selection (revision %d then, %d now): select it again to release it\n", sel.Rev, in.Selection.Rev)
		}
		return 0
	case "retry", "release":
		in, err := act(host, sel, action)
		if err != nil {
			fmt.Fprintf(out, "recover: %s of %s refused or failed: %v\n", action, sel, err)
			if in.Selection.ID != "" {
				printInspection(out, in)
			}
			return 1
		}
		if action == "retry" {
			fmt.Fprintf(out, "cleanup retried; it stays quarantined until its release\n")
			printInspection(out, in)
			return 0
		}
		fmt.Fprintf(out, "released %s: its evidence is kept, its charge returned\n", sel)
		return 0
	}
	fmt.Fprintf(out, "recover: -action is inspect, retry or release\n")
	return 2
}

// recoverInteractive is the operator's session on in/out: select by
// number, inspect, retry, release (confirmed by typing "release").
func recoverInteractive(host *realHost, in io.Reader, out io.Writer) int {
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
		list, problems, err := incidents(host.cfg)
		if err != nil {
			fmt.Fprintf(out, "recover: cannot read the state directory: %v\n", err)
			return 1
		}
		for _, p := range problems {
			fmt.Fprintf(out, "UNSAFE STATE %s\n", p)
		}
		if len(list) == 0 {
			if len(problems) > 0 {
				// the absence of an incident is no proof while an entry is
				// unaccounted for (INTEGRATION.md §11.7)
				fmt.Fprintf(out, "no incident among the records this launcher can read, but %d state entries could not be accounted for (above): any of them may be one\n", len(problems))
				return 1
			}
			fmt.Fprintln(out, "no incidents: no reservation is quarantined")
			return 0
		}
		fmt.Fprintln(out, "incidents (quarantined reservations), oldest first:")
		for i, inc := range list {
			printSummary(out, i+1, inc)
		}
		answer, ok := read(fmt.Sprintf("select an incident [1-%d], or q to quit: ", len(list)))
		if !ok || answer == "q" {
			return 0
		}
		n, err := strconv.Atoi(answer)
		if err != nil || n < 1 || n > len(list) {
			fmt.Fprintf(out, "there is no incident %q\n", answer)
			continue
		}
		cur := list[n-1]
	session:
		for {
			printInspection(out, cur)
			a, ok := read("action: [r] retry its cleanup, [R] release it, [b] back to the list, [q] quit: ")
			if !ok || a == "q" {
				return 0
			}
			switch a {
			case "b":
				break session
			case "r":
				after, err := act(host, cur.Selection, "retry")
				if err != nil {
					fmt.Fprintf(out, "the cleanup retry left it unresolved or was refused: %v\n", err)
				} else {
					fmt.Fprintln(out, "cleanup retried: it stays quarantined until its release")
				}
				if after.Selection.ID != "" {
					cur = after
				}
			case "R":
				confirm, ok := read(fmt.Sprintf("type release to release %s (%s): ", labelOf(cur), cur.Selection))
				if !ok || confirm != "release" {
					fmt.Fprintln(out, "not released")
					continue
				}
				if _, err := act(host, cur.Selection, "release"); err != nil {
					fmt.Fprintf(out, "NOT RELEASED: %v\n", err)
				} else {
					fmt.Fprintf(out, "released %s: its evidence is kept, its charge returned\n", cur.Selection)
					break session
				}
			default:
				fmt.Fprintf(out, "no action %q\n", a)
				continue
			}
			// whatever happened, show the incident as it is now
			now, ok, err := lookup(host.cfg, cur.Selection)
			switch {
			case err != nil:
				fmt.Fprintf(out, "recover: %v\n", err)
				return 1
			case !ok:
				fmt.Fprintln(out, "it is no longer an incident")
				break session
			}
			cur = now
		}
	}
}
