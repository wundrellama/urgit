#!/bin/bash
# The whole P2 table after the P0 and P1 batteries (Q17 is the P1 battery
# itself, 20/20 on this tree): p2-setup, Q2-Q4, Q5a-Q8, Q9-Q11, Q12-Q13,
# Q18 (ERPit, so Q14 can read its eight attempts), Q14-Q16, the mutant
# RED and GREEN phases for the P2 negatives, then the foreground suites.
# Every step's output also lands in $TMP/<step>.log. Stops at the first
# step that exits non-zero.
source "$(dirname "$0")/lib.sh"
set +e
# the negatives in three groups: Q5 and Q8 run on the merge gate that
# Q5a's mutant removes, and Q8 approves the untrusted candidate that Q5's
# mutant classes trusted, so each of the two is sabotaged on its own
GROUP_A="Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19"
GROUP_B="Q5"
GROUP_C="Q8"
steps=(p2-setup "q2-4 q2" "q2-4 q3" "q2-4 q4" "q5-8 q5a" "q5-8 q5" "q5-8 q6" "q5-8 q7" "q5-8 q8" "q9-11 q9" "q9-11 q10" "q9-11 q11" "q12-13 q12" "q12-13 q13" q18 "q14-16 q14" "q14-16 q15" "q14-16 q16" q19 "q-negatives red $GROUP_A" "q-negatives green $GROUP_A" "q-negatives red $GROUP_B" "q-negatives green $GROUP_B" "q-negatives red $GROUP_C" "q-negatives green $GROUP_C" foreground)
# START_AT=<step> resumes a run at that step (the earlier steps' logs stand)
skipping="${START_AT:-}"
for step in "${steps[@]}"; do
  if [ -n "$skipping" ]; then [ "$step" = "$skipping" ] && skipping="" || continue; fi
  cmd=${step%% *}; args=""; [ "$step" != "$cmd" ] && args=${step#* }
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  "$P2/$cmd.sh" $args 2>&1 | tee "$TMP/p2-${step// /-}.log"
  rc=${PIPESTATUS[0]}
  [ "$rc" = 0 ] || { echo "battery.sh: $step exited $rc; stopping" >&2; exit "$rc"; }
done
echo; echo "battery.sh: all steps ran ($(date -Is))"
