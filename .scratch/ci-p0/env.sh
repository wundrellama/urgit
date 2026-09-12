#!/bin/bash
# Harness environment for the P0 ship (~ryx). Source this from every script.
set -euo pipefail
export ROOT=/var/home/michael/workspace/urbit/urgit-ci-p0
export PIER=/var/home/michael/piers/urgit-ci-p0-ryx
export SHIP=ryx
export PORT=8340
export URL="http://127.0.0.1:$PORT"
export PANE="$(cat "$ROOT/.scratch/tmp/ship-pane.id")"
export TMP="$ROOT/.scratch/tmp"
export JAR="$TMP/cookies-$SHIP.txt"
export CLICK=/var/home/michael/workspace/urbit/bin/click
export CODE=haddut-wanpel-bonmeb-davfus
mkdir -p "$TMP"
