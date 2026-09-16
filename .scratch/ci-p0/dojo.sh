#!/bin/bash
# usage: dojo.sh '<dojo line>' [timeout-seconds] [lines-to-read]
# Clears the dojo input line, sends one line, waits until the prompt is idle
# again (last pane line is a bare prompt), then prints the pane tail.
source "$(dirname "$0")/env.sh"
# Concurrent row readers share one dojo; keep each send/read pair together.
exec {dojo_lock}> "$TMP/dojo.lock"
flock "$dojo_lock"
line="$1"; timeout="${2:-120}"; lines="${3:-15}"
[ -n "$PANE" ] || { echo "dojo.sh: no ship pane recorded; run boot.sh first" >&2; exit 1; }
herdr pane send-keys "$PANE" 'ctrl+a' 'ctrl+k' >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout $((timeout * 1000)) >/dev/null
herdr pane run "$PANE" "$line" >/dev/null
sleep 1
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout $((timeout * 1000)) >/dev/null \
  || echo "dojo.sh: timed out waiting for the prompt after: $line" >&2
# the unwrapped snapshot undoes the terminal's hard wraps (a 74-column
# pane wraps most of the row scries' echoed commands and every wide
# value); what remains is the dojo's own pretty-printing, which splits
# a wide noun at its structure and never inside a cord. The unwrapped
# snapshot ends without a newline; awk terminates every line, so a row's
# next echo never glues onto the prompt (`~peg:dojo>P19: PASS`).
herdr pane read "$PANE" --source recent-unwrapped --lines "$lines" | awk 1
