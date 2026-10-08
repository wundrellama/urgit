// urgit-runner: the runner daemon for %urgit-ci (BRIEF-CI-P1 D7).
//
//	urgit-runner -config /etc/urgit-runner.toml
//	urgit-runner -config /etc/urgit-runner.toml -recover [-select <selection> -action inspect|retry|release]
//
// The second form is the operator's recovery of the slots this runner
// withholds (rider 04; recovery ruling A — recover.go): it lists them for
// selection, inspects one, retries a Docker retention's cleanup (never a
// release), and releases one separately once it is proven released — a
// microvm retention after the launcher's own release of its exact
// incarnation, an unsettled admission once the launcher settles its
// reserve request (INTEGRATION.md §11.10), a Docker one once none of its
// objects remain. The slot returns at the next start; nothing is released
// by a restart on its own.
// A retry and a release need the daemon stopped (it holds the state file's
// lock); inspection reads the file (runner/launcher/INTEGRATION.md §8.4).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"urgit/runner/internal/config"
	"urgit/runner/internal/daemon"
	"urgit/runner/internal/launcher"
	"urgit/runner/internal/ship"
	"urgit/runner/internal/state"
)

func main() {
	configPath := flag.String("config", "urgit-runner.toml", "path to the TOML configuration")
	clearQuarantine := flag.String("clear-quarantine", "", "gone: use -recover")
	recoverRetentions := flag.Bool("recover", false, "the operator's recovery of this runner's retentions: list and inspect them, retry a Docker retention's cleanup, release one once proven released (a retry and a release need the daemon stopped); interactive unless -select")
	selection := flag.String("select", "", "with -recover: a retention's selection, as its inspection shows it")
	action := flag.String("action", "inspect", "with -recover -select: inspect, retry or release")
	flag.Parse()
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Printf("urgit-runner: %v", err)
		os.Exit(2)
	}
	if *clearQuarantine != "" {
		logger.Printf("urgit-runner: -clear-quarantine is gone (recovery ruling A): -recover lists the retentions, retries a Docker retention's cleanup and releases one separately, once it is proven released")
		os.Exit(2)
	}
	if *recoverRetentions {
		rc := newRecovery(cfg)
		if *selection == "" {
			os.Exit(rc.interactive(os.Stdin, os.Stdout))
		}
		os.Exit(rc.scripted(*selection, *action, os.Stdout))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	d, err := daemon.New(ctx, cfg, logger)
	if err != nil {
		logger.Printf("urgit-runner: %v", err)
		os.Exit(2)
	}
	defer d.Close()
	fmt.Fprintln(os.Stdout, d.Banner())
	if status := reconcileAtStart(ctx, d, logger); status != 0 {
		logger.Printf("urgit-runner: exit status %d", status)
		d.Close()
		os.Exit(status)
	}
	status := d.Run(ctx)
	if status != 0 {
		logger.Printf("urgit-runner: exit status %d", status)
	}
	d.Close()
	os.Exit(status)
}

// reconcileAtStart is the daemon's first reconcile (D7 c): 0 to run, or
// the status to exit with. Only the ship's 401 is an enrollment lost. A
// sandbox backend that cannot account for what an earlier life left — its
// list refused, as a launcher whose inventory is not authoritative refuses
// it (runner/launcher/INTEGRATION.md §§11.6, 11.7), or not answered —
// starts nothing, and is no reason to re-enroll: a new daemon identity
// would leave the launcher's records of this one behind.
func reconcileAtStart(ctx context.Context, d *daemon.Daemon, logger *log.Logger) int {
	err := d.Reconcile(ctx)
	if err == nil {
		return 0
	}
	logger.Printf("urgit-runner: reconcile: %v", err)
	if errors.Is(err, ship.ErrUnauthorized) {
		logger.Printf("enrollment lost; re-enroll with a fresh token")
		return daemon.ExitEnrollmentLost
	}
	logger.Printf("urgit-runner: the sandbox backend's records could not be reconciled: nothing starts until they can (no enrollment was lost: do not re-enroll)")
	return 2
}

// microvm says whether q withholds a launcher reservation: recorded so,
// or recorded before retentions named their backend on a microvm runner.
func microvm(q state.Quarantine, cfg *config.Config) bool {
	return q.Backend == "microvm" || q.Backend == "" && cfg.Sandbox == "microvm"
}

// launcherReleased is the launcher's durable evidence of the release of
// exactly the incarnation ref names, asked as the daemon (INTEGRATION.md
// §11.8): the only proof a microvm retention's release takes.
func launcherReleased(cfg *config.Config, daemonID string, ref launcher.Ref) (launcher.Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := launcher.Dial(ctx, cfg.LauncherSocket, daemonID)
	if err != nil {
		return launcher.Release{}, err
	}
	defer cl.Close()
	defer cl.Bind(ctx)()
	return cl.Released(ref)
}

// launcherSettle is the launcher's settlement of this daemon's reserve
// request for attempt, asked as the daemon (INTEGRATION.md §11.10): the only
// answer that releases an unsettled admission.
func launcherSettle(cfg *config.Config, daemonID, attempt, request string) (launcher.Settlement, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := launcher.Dial(ctx, cfg.LauncherSocket, daemonID)
	if err != nil {
		return launcher.Settlement{}, err
	}
	defer cl.Close()
	defer cl.Bind(ctx)()
	return cl.Settle(attempt, request)
}

// launcherRecords is the launcher's list of this daemon's records, asked
// as the daemon (the same owner-scoped view; no other authority).
func launcherRecords(cfg *config.Config, daemonID string) ([]launcher.Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := launcher.Dial(ctx, cfg.LauncherSocket, daemonID)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	defer cl.Bind(ctx)()
	return cl.List()
}
