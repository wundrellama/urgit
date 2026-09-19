#!/bin/bash
# The P3 foreground suites: P2's foreground.sh (every +urgit!*-vector —
# ci-plan-vector now carries the runs-on/timeout cases — the fe tests
# under src/*.test.js, which now include runners, ciLive and storageProbe,
# go test/-race/vet/gofmt with the daemon's heartbeat and labels, the live
# Docker boundary test, the static binary). Nothing P3 added lives
# outside those suites.
exec "$(dirname "$0")/../ci-p2/foreground.sh" "$@"
