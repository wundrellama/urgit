#!/bin/bash
# Harness environment for the P3 close-out ships (~mex:8420 and, for the
# non-writer author of R14-R16 and Q5-Q8 and R6a's foreign watcher,
# ~ryt:8421; launch footer of BRIEF-CI-P3-CLOSEOUT.md; the P3 build ran
# on ~sud:8390/~tug:8391, the P2 close-out on ~mep:8358/~lut:8359, the P2
# build on ~dep:8353/~put:8355, the P1 close-out on ~peg:8350, the P1
# build on ~ryp:8346). Source this from
# every script. Nothing secret lives here: boot.sh writes the ship's +code
# to $TMP/code.txt and this file reads it back ($TMP is git-ignored); it
# is empty until boot.sh has run. ROOT is this worktree, found from here.
# Each ship lives in a tmux session named from the footer ($TTY); the
# drivers in tty.sh are the only way the harness reads or types.
set -euo pipefail
export ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export SHIP=mex
export PORT=8420
export PIER=/var/home/michael/piers/urgit-ci-p3c-mex
# the second galaxy (BRIEF-CI-P2 §5, re-freeze 2; BRIEF-CI-P3 R14-R16):
# the non-writer author of Q5-Q8's pull request and R16's approver.
# SHIP_ROLE=2 makes every P0 driver (boot, dojo, api, shutdown) act on
# it: its own tmux session, +code and cookie jar
export SHIP2=ryt
export PORT2=8421
export PIER2=/var/home/michael/piers/urgit-ci-p3c-ryt
export ROLE_SUFFIX=
if [ "${SHIP_ROLE:-1}" = 2 ]; then
  export SHIP="$SHIP2" PORT="$PORT2" PIER="$PIER2" ROLE_SUFFIX=2
fi
export URBIT=/var/home/michael/workspace/urbit/bin/urbit
export PILL=/var/home/michael/workspace/urbit/pills/brass-408k-1.pill
export CLICK=/var/home/michael/workspace/urbit/bin/click
export URL="http://127.0.0.1:$PORT"
export TMP="$ROOT/.scratch/tmp"
export JAR="$TMP/cookies-$SHIP.txt"
export HERE="$ROOT/.scratch/ci-p0"
# the ship's tmux session (footer: ci-p3-closeout-mex / ci-p3-closeout-ryt)
export TTY="ci-p3-closeout-$SHIP"
# the last line of an idle dojo
export DOJO_PROMPT_RE="~$SHIP:dojo>\\s*\$"
mkdir -p "$TMP"
export CODE="$(cat "$TMP/code$ROLE_SUFFIX.txt" 2>/dev/null || true)"
source "$HERE/tty.sh"
