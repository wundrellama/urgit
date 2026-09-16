#!/bin/bash
# Reload without resetting the stage's fixtures; use nuke-revive for mold changes.
source "$(dirname "$0")/env.sh"
zig build -Ddesk="$PIER/urgit" > "$TMP/p2-build.log" 2>&1
marker="p2-reload-$(date +%s%N)"
"$P0/dojo.sh" "'$marker'" 60 2 >/dev/null
"$P0/dojo.sh" '|commit %urgit' 300 200 > "$TMP/p2-reload.log"
python3 - "$TMP/p2-reload.log" "$marker" <<'PY'
import pathlib, sys
text=pathlib.Path(sys.argv[1]).read_text().split(sys.argv[2])[-1]
if any(x in text for x in ('nest-fail', 'syntax error', 'mull-grow', 'event failed', 'find-fail')):
    print(text); raise SystemExit('desk reload failed')
print('desk reloaded')
PY
