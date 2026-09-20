#!/bin/bash
# The P3 cold battery, one command, scripts only (BRIEF-CI-P3 §5): boot
# both fresh ships from the footer as tmux sessions (~sud, then ~tug),
# start the rootless Docker daemon and the store fixture (asserting it
# answers), run the P0 battery, the P1 battery, the P2 battery and the P3
# battery, then the shutdown by /proc-verified pid. Nothing typed. Every
# step's output is tee'd to $TMP/cold.log. Run detached with stdin closed:
#   nohup setsid .scratch/ci-p3/cold.sh < /dev/null > /dev/null 2>&1 &
# The first step that exits non-zero stops the run BEFORE the shutdown,
# so a failed run leaves its ships up to be read. COLD_FROM=p3 resumes a
# stopped run at the P3 battery (START_AT=<step> is battery.sh's own
# resumption point) on the same ships; COLD_FROM=p2 at the P2 battery.
# The P0-P2 batteries run with the store advertised on 127.0.0.1 (their
# rows are unchanged); the P3 battery re-points %storage at the LAN
# address in p3-setup.
source "$(dirname "$0")/../ci-p2/lib.sh"
P3="$ROOT/.scratch/ci-p3"
set +e
exec > >(tee -a "$TMP/cold.log") 2>&1
boot2() { SHIP_ROLE=2 "$P0/boot.sh"; }
store_up() { "$store" start && "$store" ready; }
steps=("$P0/boot.sh" boot2 "$P1/docker-rootless.sh start" store_up "$P0/battery.sh" "$P1/battery.sh" "$P2/battery.sh" "$P3/battery.sh" "$P3/shutdown.sh")
case "${COLD_FROM:-}" in
  p3) steps=("$P3/battery.sh" "$P3/shutdown.sh")
      echo "################ cold battery RESUMED at the P3 battery (START_AT=${START_AT:-<first step>}) on the same ships ~$SHIP and ~$SHIP2  ($(date -Is))" ;;
  p2) steps=("$P2/battery.sh" "$P3/battery.sh" "$P3/shutdown.sh")
      echo "################ cold battery RESUMED at the P2 battery (START_AT=${START_AT:-<first step>}) on the same ships ~$SHIP and ~$SHIP2  ($(date -Is))" ;;
  "") echo "################ cold battery: ships ~$SHIP :$PORT ($PIER, tmux $TTY) and ~$SHIP2 :$PORT2 ($PIER2), rootless $DOCKER_STATE, store $STORE_URL advertised as $STORE_ENDPOINT for P0-P2 and http://${P3_STORE_ADVERTISE:-192.168.1.229}:$STORE_PORT for P3, DAEMON_CAPACITY=$DAEMON_CAPACITY  ($(date -Is))" ;;
  *) echo "cold.sh: COLD_FROM must be p2, p3 or unset" >&2; exit 2 ;;
esac
for step in "${steps[@]}"; do
  printf '\n\n################ %s  (%s)\n' "$step" "$(date -Is)"
  $step
  rc=$?
  [ "$rc" = 0 ] || { echo "cold.sh: $step exited $rc; stopping ($(date -Is))" >&2; exit "$rc"; }
  # START_AT names a step of the FIRST battery only: left exported, the
  # next battery would skip every step looking for it and report "all
  # steps ran" (the P3 battery did, 22:54, and the shutdown followed)
  unset START_AT
done
echo; echo "cold.sh: every step ran ($(date -Is))"
