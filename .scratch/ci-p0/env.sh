#!/bin/bash
# Harness environment for the P2 ships (~dep:8353 and, for Q5-Q8's
# pull-request author, ~put:8355; launch footer of BRIEF-CI-P2.md; the P1
# close-out ran on ~peg:8350, the P1 build on ~ryp:8346). Source this
# from every script. Nothing secret lives here:
# boot.sh writes the ship's pane id to $TMP/ship-pane.id and its +code to
# $TMP/code.txt, and this file reads them back ($TMP is git-ignored). Both
# are empty until boot.sh has run. ROOT is this worktree, found from here.
set -euo pipefail
export ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export SHIP=dep
export PORT=8353
export PIER=/var/home/michael/piers/urgit-ci-p2-dep
# the second galaxy (BRIEF-CI-P2 §5, re-freeze 2): the non-writer author
# of Q5-Q8's pull request. SHIP_ROLE=2 makes every P0 driver (boot, dojo,
# api, shutdown) act on it: its own pane id, +code and cookie jar
export SHIP2=put
export PORT2=8355
export PIER2=/var/home/michael/piers/urgit-ci-p2-put
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
# the last line of an idle shell prompt in a fresh herdr pane on this host
# (boot.sh waits for it before typing the boot line) and of an idle dojo
export SHELL_PROMPT_RE='^\s*➜\s*$'
export DOJO_PROMPT_RE="~$SHIP:dojo>\\s*\$"
mkdir -p "$TMP"
export PANE="$(cat "$TMP/ship$ROLE_SUFFIX-pane.id" 2>/dev/null || true)"
export CODE="$(cat "$TMP/code$ROLE_SUFFIX.txt" 2>/dev/null || true)"
