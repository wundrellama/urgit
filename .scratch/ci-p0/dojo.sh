#!/bin/bash
# usage: dojo.sh '<dojo line>' [timeout-seconds] [lines-to-read]
# Clears the dojo input line, sends one line, waits until the prompt is idle
# again (last pane line is a bare prompt), then prints the pane tail.
source "$(dirname "$0")/env.sh"
line="$1"; timeout="${2:-120}"; lines="${3:-15}"
herdr pane send-keys "$PANE" 'ctrl+a' 'ctrl+k' >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex '~ryx:dojo>\s*$' --timeout $((timeout * 1000)) >/dev/null
herdr pane run "$PANE" "$line" >/dev/null
sleep 1
herdr pane wait-output "$PANE" --lines 1 --regex '~ryx:dojo>\s*$' --timeout $((timeout * 1000)) >/dev/null \
  || echo "dojo.sh: timed out waiting for the prompt after: $line" >&2
herdr pane read "$PANE" --lines "$lines"
