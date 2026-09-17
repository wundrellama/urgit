#!/bin/bash
# Stop only this chair's recorded runners, store and /proc-verified ship.
source "$(dirname "$0")/env.sh"
for name in a b; do
  [ ! -d "$RUNNER_HOME/$name" ] || "$P1/runner.sh" stop "$name"
done
"$P1/store.sh" stop
CI_PEER=1 "$P0/shutdown.sh"
"$P0/shutdown.sh"
