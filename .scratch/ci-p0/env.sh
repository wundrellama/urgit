#!/bin/bash
# Harness environment for the P0 close-out ship (~sev). Source this from every
# script. Nothing secret lives here: boot.sh writes the ship's pane id to
# $TMP/ship-pane.id and its +code to $TMP/code.txt, and this file reads them
# back ($TMP is git-ignored). Both are empty until boot.sh has run.
set -euo pipefail
export ROOT=/var/home/michael/workspace/urbit/urgit-ci-p0
export SHIP=sev
export PORT=8345
export PIER=/var/home/michael/piers/urgit-ci-p0-sev
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
export PANE="$(cat "$TMP/ship-pane.id" 2>/dev/null || true)"
export CODE="$(cat "$TMP/code.txt" 2>/dev/null || true)"
