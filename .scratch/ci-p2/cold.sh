#!/bin/bash
# The cold battery, one command, scripts only: boot both fresh ships from
# the footer as tmux sessions (~mep, then ~lut), start the rootless Docker
# daemon and the store fixture (asserting it answers), run the P0 battery,
# the P1 battery (Q17) and the P2 battery (Q1-Q19, the three mutant
# groups, the foreground), then T2's shutdown by /proc-verified pid
# (BRIEF-CI-P2-CLOSEOUT T7). Nothing typed. Every step's output is tee'd
# to $TMP/cold.log. Run detached with stdin closed:
#   nohup setsid .scratch/ci-p2/cold.sh < /dev/null > /dev/null 2>&1 &
# The first step that exits non-zero stops the run BEFORE the shutdown,
# so a failed run leaves its ships up to be read.
source "$(dirname "$0")/lib.sh"
set +e
exec > >(tee -a "$TMP/cold.log") 2>&1
# the second galaxy (~lut) boots after the first: Q5-Q8's pull-request author
boot2() { SHIP_ROLE=2 "$P0/boot.sh"; }
store_up() { "$store" start && "$store" ready; }
steps=("$P0/boot.sh" boot2 "$P1/docker-rootless.sh start" store_up "$P0/battery.sh" "$P1/battery.sh" "$P2/battery.sh" "$P2/shutdown.sh")
# COLD_FROM=p2 resumes a stopped run on the SAME ships, scripts only: the
# P2 battery (START_AT=<step> is battery.sh's own resumption point) and
# the shutdown; the earlier steps' output stands in cold.log
case "${COLD_FROM:-}" in
  p2) steps=("$P2/battery.sh" "$P2/shutdown.sh")
      echo "################ cold battery RESUMED at the P2 battery (START_AT=${START_AT:-<first step>}) on the same ships ~$SHIP and ~$SHIP2  ($(date -Is))" ;;
  "") echo "################ cold battery: ships ~$SHIP :$PORT ($PIER, tmux $TTY) and ~$SHIP2 :$PORT2 ($PIER2), rootless $DOCKER_STATE, store $STORE_URL, DAEMON_CAPACITY=$DAEMON_CAPACITY  ($(date -Is))" ;;
  *) echo "cold.sh: COLD_FROM must be p2 or unset" >&2; exit 2 ;;
esac
for step in "${steps[@]}"; do
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  $step
  rc=$?
  [ "$rc" = 0 ] || { echo "cold.sh: $step exited $rc; stopping ($(date -Is))" >&2; exit "$rc"; }
done
echo; echo "cold.sh: every step ran ($(date -Is))"
