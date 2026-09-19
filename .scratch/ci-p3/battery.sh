#!/bin/bash
# The P3 table (BRIEF-CI-P3 §4), after the P0, P1 and P2 batteries or on
# a fresh pair: p3-setup, the rows in stage order, the mutant RED and
# GREEN phases per negative group, then the foreground suites. Every
# step's output also lands in $TMP/p3-<step>.log. Stops at the first step
# that exits non-zero. START_AT=<step> resumes a run at that step.
# The negative groups: a row is never turned red by another row's mutant —
# R4b (the revoke's re-offer) and R5b (the refusal de-list) run apart from
# R4 and R5, whose mutants would mask them.
source "$(dirname "$0")/lib.sh"
set +e
GROUP_A="R4 R5 R3"
GROUP_B="R4b R5b"
# R11b's mutant is the pre-rider-3 daemon, which exits 3 at any start
# beside another runner's sandbox — it would kill R10's daemon b, so it
# is a group of its own; both REDs run before the one GREEN of C and D
GROUP_C="R9 R10"
GROUP_D="R11b"
steps=(p3-setup "r1-r5 r1" "r1-r5 r3" "r1-r5 r4" "r1-r5 r5" "r2-r6 r2" "r2-r6 r6" "r9-r11 r11a" "r9-r11 r11b" "r7-r8 r7" "r7-r8 r8" "r9-r11 r9" "r9-r11 r10" "r9-r11 r11" "r14-r16 r14" "r14-r16 r15" "r14-r16 r16" r12 "r-negatives red $GROUP_A" "r-negatives green $GROUP_A" "r-negatives red $GROUP_B" "r-negatives green $GROUP_B" "r-negatives red $GROUP_C" "r-negatives red $GROUP_D" "r-negatives green $GROUP_C $GROUP_D" foreground)
skipping="${START_AT:-}"
for step in "${steps[@]}"; do
  if [ -n "$skipping" ]; then [ "$step" = "$skipping" ] && skipping="" || continue; fi
  cmd=${step%% *}; args=""; [ "$step" != "$cmd" ] && args=${step#* }
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  "$P3/$cmd.sh" $args 2>&1 | tee "$TMP/p3-${step// /-}.log"
  rc=${PIPESTATUS[0]}
  [ "$rc" = 0 ] || { echo "battery.sh: $step exited $rc; stopping" >&2; exit "$rc"; }
done
echo; echo "battery.sh: all steps ran ($(date -Is))"
