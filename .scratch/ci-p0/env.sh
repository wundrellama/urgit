#!/bin/bash
# Harness environment for the P2 close-out ships (~mep:8358 and, for
# Q5-Q8's pull-request author, ~lut:8359; launch footer of
# BRIEF-CI-P2-CLOSEOUT.md; the P2 build ran on ~dep:8353/~put:8355, the
# P1 close-out on ~peg:8350, the P1 build on ~ryp:8346). Source this from
# every script. Nothing secret lives here: boot.sh writes the ship's +code
# to $TMP/code.txt and this file reads it back ($TMP is git-ignored); it
# is empty until boot.sh has run. ROOT is this worktree, found from here.
# Each ship lives in a tmux session named from the footer ($TTY); the
# drivers in tty.sh are the only way the harness reads or types.
set -euo pipefail
export ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export SHIP=mep
export PORT=8358
export PIER=/var/home/michael/piers/urgit-ci-p2-mep
# the second galaxy (BRIEF-CI-P2 §5, re-freeze 2): the non-writer author
# of Q5-Q8's pull request. SHIP_ROLE=2 makes every P0 driver (boot, dojo,
# api, shutdown) act on it: its own tmux session, +code and cookie jar
export SHIP2=lut
export PORT2=8359
export PIER2=/var/home/michael/piers/urgit-ci-p2-lut
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
# the ship's tmux session (footer: ci-p2-closeout-mep / ci-p2-closeout-lut)
export TTY="ci-p2-closeout-$SHIP"
# the last line of an idle dojo
export DOJO_PROMPT_RE="~$SHIP:dojo>\\s*\$"
mkdir -p "$TMP"
export CODE="$(cat "$TMP/code$ROLE_SUFFIX.txt" 2>/dev/null || true)"
source "$HERE/tty.sh"
