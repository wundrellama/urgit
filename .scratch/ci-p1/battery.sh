#!/bin/bash
# The whole P1 table after the P0 battery: p-setup, P1..P20 in order, the
# mutant RED and GREEN phases, then the foreground suites. Every step's
# output also lands in $TMP/<step>.log. Stops at the first step that exits
# non-zero. P14 wipes the ship's state, so P15 re-enrolls (capacity 3)
# and P16-P20 run on that daemon; the negatives enroll per phase.
source "$(dirname "$0")/env.sh"
set +e
steps=(p-setup p1 p2 p3-5 p6-9 "p10-14 p10 p11 p12 p13 p14" p15-prep p15 p16-20 "negatives red" "negatives green" foreground)
for step in "${steps[@]}"; do
  cmd=${step%% *}; args=""; [ "$step" != "$cmd" ] && args=${step#* }
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  "$P1/$cmd.sh" $args 2>&1 | tee "$TMP/${step// /-}.log"
  rc=${PIPESTATUS[0]}
  [ "$rc" = 0 ] || { echo "battery.sh: $step exited $rc; stopping" >&2; exit "$rc"; }
done
echo; echo "battery.sh: all steps ran ($(date -Is))"
