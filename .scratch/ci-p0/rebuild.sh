#!/bin/bash
# usage: rebuild.sh <label> [agent ...]
# Rebuilds the desk from the working tree into the mounted desk, commits it,
# and waits until gall has reported `gall: reloading %<agent>` for every
# agent named (default: %urgit %urgit-ci) AFTER a marker printed just before
# the commit, so a reload line from an earlier build cannot satisfy the wait.
# Ends with the %gu liveness of every named agent.
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
label="${1:-rebuild}"; shift || true
if [ $# -gt 0 ]; then agents=("$@"); else agents=(urgit urgit-ci); fi
echo "== zig build -Ddesk=$PIER/urgit ($label)"
( cd "$ROOT" && zig build -Ddesk="$PIER/urgit" 2>&1 | tail -1 )
marker="rebuild-$label-$(date +%s)"
"$dojo" "'$marker'" 60 2 >/dev/null
echo "== |commit %urgit (marker $marker)"
"$dojo" '|commit %urgit' 300 3 | tail -2
after_marker() { tty_read 400 | awk -v m="'$marker'" 'index($0, m) { found = 1; next } found'; }
for agent in "${agents[@]}"; do
  ok=0
  for _ in $(seq 1 150); do
    if after_marker | grep -qE "^gall: reloading %$agent\\s*\$"; then ok=1; break; fi
    sleep 2
  done
  [ "$ok" = 1 ] && echo "gall: reloading %$agent — seen" \
    || { echo "rebuild.sh: no 'gall: reloading %$agent' after the marker within 300 s" >&2; after_marker | tail -30 >&2; exit 1; }
done
for agent in "${agents[@]}"; do
  printf '%%gu /=%s=/$ -> ' "$agent"
  "$dojo" ".^(? %gu /=$agent=/\$)" 60 3 | grep -oE '^%\.[yn]$' | tail -1
done
