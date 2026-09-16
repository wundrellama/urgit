#!/bin/bash
# The cold battery, one command: boot a fresh ship from the footer in
# env.sh, start the rootless Docker daemon, run the P0 battery and then
# the P1 battery, nothing typed. Every step's output is tee'd to
# $TMP/cold.log and the step logs the batteries write; the first step
# that exits non-zero stops the run. Run it detached with stdin closed
# and no controlling tty (a driving shell that loses its terminal kills
# the run with `tcsetattr: Inappropriate ioctl`):
#   nohup setsid .scratch/ci-p1/cold.sh < /dev/null > /dev/null 2>&1 &
# shutdown.sh is not part of it: the record is written first.
source "$(dirname "$0")/env.sh"
set +e
exec > >(tee -a "$TMP/cold.log") 2>&1
steps=("$P0/boot.sh" "$P1/docker-rootless.sh start" "$P0/battery.sh" "$P1/battery.sh")
echo "################ cold battery: ship ~$SHIP :$PORT, pier $PIER, rootless $DOCKER_STATE  ($(date -Is))"
for step in "${steps[@]}"; do
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  $step
  rc=$?
  [ "$rc" = 0 ] || { echo "cold.sh: $step exited $rc; stopping ($(date -Is))" >&2; exit "$rc"; }
done
echo; echo "cold.sh: every step ran ($(date -Is))"
