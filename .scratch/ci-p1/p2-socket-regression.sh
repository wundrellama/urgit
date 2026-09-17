#!/bin/bash
# Q19 is part of the P1 regression: dropping the act flag must go RED.
source "$(dirname "$0")/env.sh"
set -euo pipefail
restore() {
  "$P1/runner.sh" stop a >/dev/null
  "$P1/q-mutants.sh" revert
  (cd "$ROOT/runner" && CGO_ENABLED=0 go build -trimpath -o urgit-runner ./cmd/urgit-runner)
}
trap restore EXIT
"$P1/runner.sh" stop a
"$P1/q-mutants.sh" apply Q19
(cd "$ROOT/runner" && CGO_ENABLED=0 go build -trimpath -o urgit-runner ./cmd/urgit-runner)
"$P1/p2-socket.sh" red | tee "$TMP/q19-mutant-red.log"
grep -qF "$("$P1/q-mutants.sh" tripwire Q19)" "$TMP/q19-mutant-red.log"
restore
trap - EXIT
"$P1/p2-socket.sh" green | tee "$TMP/q19-restored-green.log"
