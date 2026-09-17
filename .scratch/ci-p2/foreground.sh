#!/bin/bash
# The P2 foreground suites: P1's foreground.sh (every +urgit!*-vector, the
# fe tests, go test/-race/vet/gofmt, the live Docker boundary test, the
# static binary), which already covers everything P2 added: the storage
# and event vectors grew, fe/src/ci.test.js is under src/*.test.js, and
# runner/internal/sig is under ./...
source "$(dirname "$0")/lib.sh"
exec "$P1/foreground.sh"
