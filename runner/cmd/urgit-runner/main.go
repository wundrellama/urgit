// urgit-runner: the runner daemon for %urgit-ci (BRIEF-CI-P1 D7).
//
//	urgit-runner -config /etc/urgit-runner.toml
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"urgit/runner/internal/config"
	"urgit/runner/internal/daemon"
)

func main() {
	configPath := flag.String("config", "urgit-runner.toml", "path to the TOML configuration")
	flag.Parse()
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Printf("urgit-runner: %v", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	d, err := daemon.New(ctx, cfg, logger)
	if err != nil {
		logger.Printf("urgit-runner: %v", err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, d.Banner())
	if err := d.Reconcile(ctx); err != nil {
		logger.Printf("urgit-runner: reconcile: %v", err)
		logger.Printf("enrollment lost; re-enroll with a fresh token")
		logger.Printf("urgit-runner: exit status %d", daemon.ExitEnrollmentLost)
		os.Exit(daemon.ExitEnrollmentLost)
	}
	status := d.Run(ctx)
	if status != 0 {
		logger.Printf("urgit-runner: exit status %d", status)
	}
	os.Exit(status)
}
