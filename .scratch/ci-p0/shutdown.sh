#!/bin/bash
# Shut down only the ship this harness booted: find its processes by
# /proc/<pid>/cmdline containing the pier path (never a pattern kill), send
# ctrl+d to the dojo, wait until those pids are gone, then close the pane by
# id. The pier directory stays on disk.
source "$(dirname "$0")/env.sh"
pier_pids() {
  for p in /proc/[0-9]*; do
    if tr '\0' ' ' < "$p/cmdline" 2>/dev/null | grep -qF -- "$PIER"; then basename "$p"; fi
  done
}
echo "== processes whose /proc/<pid>/cmdline names $PIER:"
for pid in $(pier_pids); do printf '%s  %s\n' "$pid" "$(tr '\0' ' ' < /proc/$pid/cmdline | cut -c1-140)"; done
[ -n "$(pier_pids)" ] || { echo "nothing to stop"; exit 0; }
echo "== ctrl+d to the dojo in pane $PANE"
herdr pane send-keys "$PANE" 'ctrl+d' >/dev/null
for _ in $(seq 1 120); do [ -z "$(pier_pids)" ] && break; sleep 1; done
if [ -n "$(pier_pids)" ]; then echo "shutdown.sh: pids still present after 120 s: $(pier_pids | tr '\n' ' ')" >&2; exit 1; fi
echo "== all pier pids gone; waiting for the shell prompt, then closing the pane"
herdr pane wait-output "$PANE" --lines 1 --regex "$SHELL_PROMPT_RE" --timeout 60000 >/dev/null || true
herdr pane read "$PANE" --lines 3
herdr pane close "$PANE" | head -c 200; echo
echo "pier retained: $PIER ($(du -sh "$PIER" 2>/dev/null | cut -f1))"
