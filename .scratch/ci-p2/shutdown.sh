#!/bin/bash
# The P2 shutdown: every runner daemon the rows started (by recorded pid,
# verified through /proc/<pid>/cmdline), the store fixture container
# (store.sh stop is part of shutdown; its data dir stays), then the ship
# through P0's shutdown.sh (ctrl+d to the dojo, pids verified gone, the
# pane closed, the pier retained). The rootless Docker daemon is left
# running, as P1's close-out left it.
source "$(dirname "$0")/lib.sh"
for d in a b; do "$P1/runner.sh" stop "$d" 2>/dev/null | head -1; done
"$store" status 2>/dev/null | head -1
"$store" stop
"$P0/shutdown.sh"
