#!/bin/bash
# The P3 shutdown: P2's (every runner daemon the rows started, the store
# fixture by its process pid, both ships through P0's shutdown.sh by the
# king's /proc-verified pid, then the rootless Docker daemon), with the
# P3 rows' test daemons (b, c, r1, r2, r3) stopped first.
source "$(dirname "$0")/lib.sh"
for d in c r1 r2 r3; do "$P1/runner.sh" stop "$d" 2>/dev/null | head -1; done
exec "$P2/shutdown.sh"
