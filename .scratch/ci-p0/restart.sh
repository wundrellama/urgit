#!/bin/bash
# Restart a ship this harness booted, on its existing pier, in its tmux
# session, with the footer's loom (--loom 34) and HTTP port; wait for the
# dojo prompt and %urgit-ci's liveness. For a ship whose process died
# (the serf SEGV of 2026-09-19); never for a first boot (boot.sh).
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
[ -d "$PIER" ] || { echo "restart.sh: no pier at $PIER" >&2; exit 1; }
tty_alive && { echo "restart.sh: tmux session $TTY exists; is the ship up?" >&2; exit 1; }
line="$URBIT --loom 34 --http-port $PORT $PIER"
echo "== tmux new-session -d -s $TTY -x 200 -y 50 \"$line\"  ($(date -Is))"
tmux new-session -d -s "$TTY" -x 200 -y 50 -c "$ROOT" "$line"
tty_wait "$DOJO_PROMPT_RE" 900 || { echo "restart.sh: no dojo prompt within 900 s" >&2; tty_read 20 >&2; exit 1; }
for _ in $(seq 1 60); do
  live=$("$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | grep -oE '^%\.[yn]$' | tail -1 || true)
  [ "$live" = "%.y" ] && break; sleep 2
done
echo "%gu after restart: $live"
[ "$live" = "%.y" ] || exit 1
"$HERE/prelude.sh" | tail -1
