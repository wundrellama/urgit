#!/bin/bash
# Composed P0 -> P1 -> P2 regression. Optional phase resumes a completed
# earlier phase without erasing its logs. No phase can silently skip a failure.
source "$(dirname "$0")/env.sh"
set -euo pipefail
phase=${1:-all}
run_step() {
  local name=$1; shift
  printf '\n################ %s (%s)\n' "$name" "$(date -Is)"
  "$@" 2>&1 | tee "$TMP/s6-$name.log"
}
if [[ "$phase" == all || "$phase" == p0 ]]; then
  run_step reset "$P1/p2-regression-reset.sh"
  run_step p0-battery "$P0/battery.sh"
fi
if [[ "$phase" == all || "$phase" == p1 ]]; then
  run_step store-configure "$P1/store.sh" configure
  export ERPIT_REPO=${ERPIT_REPO:-erpit-p15-s6}
  steps=(p-setup p1 p2 p3-5 p6-9 "p10-14 p10 p11 p12 p13 p14" p15-prep p15 p16-20 "negatives red" "negatives green" "p15-prep 1" p2-socket-regression foreground)
  for step in "${steps[@]}"; do
    read -ra words <<< "$step"
    cmd=${words[0]}; args=("${words[@]:1}")
    run_step "p1-${step// /-}" "$P1/$cmd.sh" "${args[@]}"
  done
fi
if [[ "$phase" == all || "$phase" == p2 ]]; then
  run_step p2-battery "$P1/p2-battery.sh"
  run_step shutdown "$P1/p2-shutdown.sh"
fi
case "$phase" in all|p0|p1|p2) ;; *) echo 'usage: battery.sh [all|p0|p1|p2]' >&2; exit 2 ;; esac
echo "battery.sh: $phase passed ($(date -Is))"
