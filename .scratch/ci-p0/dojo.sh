#!/bin/bash
# usage: dojo.sh '<dojo line>' [timeout-seconds] [lines-to-read]
# Clears the dojo input line, sends one line, waits until the prompt is idle
# again (last terminal line is a bare prompt), then prints the terminal
# tail — through the ship's tmux session (tty.sh), nothing else.
source "$(dirname "$0")/env.sh"
line="$1"; timeout="${2:-120}"; lines="${3:-15}"
tty_alive || { echo "dojo.sh: no tmux session $TTY; run boot.sh first" >&2; exit 1; }
tty_keys C-a C-k
tty_wait "$DOJO_PROMPT_RE" "$timeout"
tty_send "$line"
sleep 1
tty_wait "$DOJO_PROMPT_RE" "$timeout" \
  || echo "dojo.sh: timed out waiting for the prompt after: $line" >&2
# logical lines (tty_read joins the terminal's hard wraps); what remains
# is the dojo's own pretty-printing, which splits a wide noun at its
# structure and never inside a cord; every line is newline-terminated, so
# a row's next echo never glues onto the prompt (`~peg:dojo>P19: PASS`)
tty_read "$lines"
