package main

// serve's line for each teardown the launcher started on its own
// (INTEGRATION.md §§7.4, 11.3): a release whose withdrawal was published
// after its obligation's deadline is said so, never logged as a plain
// release nor dropped.

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"urgit/runner/internal/launcher"
)

func TestServeLogsAReleasePublishedLate(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	logOutcome(logger, launcher.Outcome{ID: "t-x", Owner: launcher.Owner{Daemon: "0vd"}, By: "reaper", Kind: "released", LateAccounting: true,
		Reason: "deadline reached; its withdrawal was published after its obligation's deadline"})
	// policy-dependent (late-accounting ruling 01; INTEGRATION.md §11.8):
	// "RELEASED, PUBLISHED LATE" before it
	if line := buf.String(); !strings.HasPrefix(line, "RELEASED, ACCOUNTING CONFIRMED LATE t-x (owner 0vd, reaper): ") || !strings.Contains(line, "published after") {
		t.Fatalf("A RELEASE PUBLISHED LATE WAS NOT LOGGED AS ONE: %q", line)
	}
}
