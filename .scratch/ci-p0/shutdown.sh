#!/bin/bash
# Shut down only the ship this harness booted, by /proc-verified pid and
# nothing else (BRIEF-CI-P2-CLOSEOUT T2): find its processes by
# /proc/<pid>/cmdline (the urbit binary, naming the pier path; never a
# pattern kill), SIGTERM the king (Vere's clean exit: the serf is told to
# stop and the snapshot is written), wait until every pier pid is gone,
# SIGKILL whatever survives 120 s and wait again. No terminal is needed:
# the ship's tmux session ends with its process and is only removed here
# if it somehow outlived it. The pier directory stays on disk.
source "$(dirname "$0")/env.sh"
pier_pids() {   # the king and the serf: both name the pier
  for p in /proc/[0-9]*; do
    local cmd; cmd=$({ tr '\0' ' ' < "$p/cmdline"; } 2>/dev/null)
    case "$cmd" in "$URBIT "*"$PIER"*) basename "$p" ;; esac
  done
}
king_pids() {   # the king: `urbit -F … -c <pier>`, not `urbit work --snap-dir <pier>`
  for p in $(pier_pids); do
    case "$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null)" in "$URBIT work "*) ;; *) echo "$p" ;; esac
  done
}
wait_gone() { local i; for ((i = 0; i < $1; i++)); do [ -z "$(pier_pids)" ] && return 0; sleep 1; done; return 1; }
echo "== processes whose /proc/<pid>/cmdline is $URBIT … $PIER:"
for pid in $(pier_pids); do printf '%s  %s\n' "$pid" "$(tr '\0' ' ' < /proc/$pid/cmdline | cut -c1-140)"; done
if [ -z "$(pier_pids)" ]; then
  echo "nothing to stop"
  tty_alive && { echo "== tmux session $TTY has no ship behind it; removing it"; tmux kill-session -t "=$TTY"; }
  exit 0
fi
for k in $(king_pids); do echo "== SIGTERM to the king, pid $k"; kill -TERM "$k"; done
if wait_gone 120; then echo "== all pier pids gone after SIGTERM ($(date -Is))"
else
  echo "== pids still present after 120 s: $(pier_pids | tr '\n' ' '); SIGKILL" >&2
  for p in $(pier_pids); do kill -KILL "$p" 2>/dev/null; done
  wait_gone 30 || { echo "shutdown.sh: pids still present after SIGKILL: $(pier_pids | tr '\n' ' ')" >&2; exit 1; }
  echo "== all pier pids gone after SIGKILL"
fi
if tty_alive; then echo "== tmux session $TTY outlived the ship; removing it"; tmux kill-session -t "=$TTY"; else echo "== tmux session $TTY ended with the ship"; fi
echo "pier retained: $PIER ($(du -sh "$PIER" 2>/dev/null | cut -f1))"
