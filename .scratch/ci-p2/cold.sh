#!/bin/bash
# The cold battery, one command: boot a fresh ship from the footer, start
# the rootless Docker daemon and the store fixture, run the P0 battery,
# the P1 battery (Q17) and the P2 battery, nothing typed. Every step's
# output is tee'd to $TMP/cold.log. Run detached with stdin closed:
#   nohup setsid .scratch/ci-p2/cold.sh < /dev/null > /dev/null 2>&1 &
# shutdown.sh and store.sh stop are not part of it: the record is
# written first.
source "$(dirname "$0")/lib.sh"
set +e
exec > >(tee -a "$TMP/cold.log") 2>&1
steps=("$P0/boot.sh" "$P1/docker-rootless.sh start" "$P1/store.sh start" "$P0/battery.sh" "$P1/battery.sh" "$P2/battery.sh")
echo "################ cold battery: ship ~$SHIP :$PORT, pier $PIER, rootless $DOCKER_STATE, store $STORE_URL  ($(date -Is))"
for step in "${steps[@]}"; do
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  $step
  rc=$?
  [ "$rc" = 0 ] || { echo "cold.sh: $step exited $rc; stopping ($(date -Is))" >&2; exit "$rc"; }
done
echo; echo "cold.sh: every step ran ($(date -Is))"
