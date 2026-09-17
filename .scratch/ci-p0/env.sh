#!/bin/bash
# Harness environment for the P2 astra ship (~lup, launch footer of
# BRIEF-CI-P2.md). Source this
# from every script. Nothing secret lives here:
# boot.sh writes the ship's pane id to $TMP/ship-pane.id and its +code to
# $TMP/code.txt, and this file reads them back ($TMP is git-ignored). Both
# are empty until boot.sh has run. ROOT is this worktree, found from here.
set -euo pipefail
export ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export SHIP=lup
export PORT=8352
export PIER=/var/home/michael/piers/urgit-ci-p2-lup
export SHIP2=dys
export PORT2=8354
export PIER2=/var/home/michael/piers/urgit-ci-p2-dys
if [ "${CI_PEER:-0}" = 1 ]; then
  export SHIP="$SHIP2" PORT="$PORT2" PIER="$PIER2"
fi
export URBIT=/var/home/michael/workspace/urbit/bin/urbit
export PILL=/var/home/michael/workspace/urbit/pills/brass-408k-1.pill
export CLICK=/var/home/michael/workspace/urbit/bin/click
export URL="http://127.0.0.1:$PORT"
export TMP="$ROOT/.scratch/tmp"
if [ "${CI_PEER:-0}" = 1 ]; then export TMP="$TMP/peer"; fi
export JAR="$TMP/cookies-$SHIP.txt"
export HERE="$ROOT/.scratch/ci-p0"
# the last line of an idle shell prompt in a fresh herdr pane on this host
# (boot.sh waits for it before typing the boot line) and of an idle dojo
export SHELL_PROMPT_RE='^\s*➜\s*$'
export DOJO_PROMPT_RE="~$SHIP:dojo>\\s*\$"
mkdir -p "$TMP"
export PANE="$(cat "$TMP/ship-pane.id" 2>/dev/null || true)"
export CODE="$(cat "$TMP/code.txt" 2>/dev/null || true)"
