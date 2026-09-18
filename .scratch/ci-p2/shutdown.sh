#!/bin/bash
# The P2 shutdown, every process by /proc-verified pid, no terminal
# needed (BRIEF-CI-P2-CLOSEOUT T2): the runner daemons the rows started
# (runner.sh stop: recorded pid, cmdline checked), the store fixture (the
# container's process pid from `docker inspect`, its cmdline read from
# /proc, `docker rm -f` through the rootless socket, the pid waited gone;
# its data dir stays), both ships through P0's shutdown.sh (the king
# SIGTERMed by pid, KILLed if it survives; the piers retained) — the
# second galaxy first, then the first — and last the rootless Docker
# daemon itself (docker-rootless.sh stop: the pidfile's dockerd verified
# against our data root, TERM, KILL if it survives, its rootlesskit pair).
source "$(dirname "$0")/lib.sh"
for d in a b; do "$P1/runner.sh" stop "$d" 2>/dev/null | head -1; done
"$store" status 2>/dev/null | head -1
spid=$($DK inspect -f '{{.State.Pid}}' "$STORE_NAME" 2>/dev/null)
if [ -n "$spid" ] && [ "$spid" != 0 ]; then
  echo "store container $STORE_NAME: pid $spid, /proc/$spid/cmdline = $(tr '\0' ' ' < "/proc/$spid/cmdline" 2>/dev/null | cut -c1-80)"
  "$store" stop
  for _ in $(seq 1 30); do [ -d "/proc/$spid" ] || break; sleep 1; done
  [ -d "/proc/$spid" ] && echo "shutdown.sh: store pid $spid still present after 30 s" >&2 || echo "store pid $spid gone"
else
  "$store" stop
fi
SHIP_ROLE=2 "$P0/shutdown.sh"
"$P0/shutdown.sh"
"$P1/docker-rootless.sh" stop
