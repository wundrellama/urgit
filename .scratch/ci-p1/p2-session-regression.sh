#!/bin/bash
source "$(dirname "$0")/env.sh"
set -euo pipefail
cid=${1:?candidate with an uploaded log}
trap '"$P1/q-mutants.sh" revert' EXIT
"$P1/q-mutants.sh" apply Q16
"$P1/p2-reload.sh"
"$P1/p2-web.sh" q16 red "$cid" | tee "$TMP/q16-red.log"
grep -qF "$("$P1/q-mutants.sh" tripwire Q16)" "$TMP/q16-red.log"
"$P1/q-mutants.sh" revert
"$P1/p2-reload.sh"
"$P1/p2-web.sh" q16 green "$cid" | tee "$TMP/q16-green.log"
