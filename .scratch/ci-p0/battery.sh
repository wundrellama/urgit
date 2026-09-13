#!/bin/bash
# The whole table after boot.sh: setup.sh through h15.sh, the mutant RED
# phase and the real GREEN phase of negatives.sh, then the foreground
# suites. Every step's output is also written to $TMP/<step>.log. Stops at
# the first step that exits non-zero.
source "$(dirname "$0")/env.sh"
set +e
steps=(setup h1 h2 h3 h4 h5 h6 h7 h8 h9-13 h14 h15 "negatives red" "negatives green" foreground)
for step in "${steps[@]}"; do
  cmd=${step%% *}; args=""; [ "$step" != "$cmd" ] && args=${step#* }
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  "$HERE/$cmd.sh" $args 2>&1 | tee "$TMP/${step// /-}.log"
  rc=${PIPESTATUS[0]}
  [ "$rc" = 0 ] || { echo "battery.sh: $step exited $rc; stopping" >&2; exit "$rc"; }
done
echo; echo "battery.sh: all steps ran ($(date -Is))"
