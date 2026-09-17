#!/bin/bash
# Reload without resetting fixtures. Require a reload of the named agent
# (default urgit-ci), after this invocation's marker, before claiming success.
source "$(dirname "$0")/env.sh"
agent="${1:-urgit-ci}"
zig build -Ddesk="$PIER/urgit" > "$TMP/p2-build.log" 2>&1
marker="p2-reload-$(date +%s%N)"
"$P0/dojo.sh" "'$marker'" 60 2 >/dev/null
"$P0/dojo.sh" '|commit %urgit' 300 3 >/dev/null
for i in $(seq 1 150); do
  if [ "$i" = 6 ]; then
    # Clay may already hold this exact build after an assertion retry.
    # Re-enable the agent from its saved state; Gall confirms a bump.
    "$P0/dojo.sh" "|rein %urgit [%.n %$agent]" 120 3 >/dev/null
    "$P0/dojo.sh" "|rein %urgit [%.y %$agent]" 120 3 >/dev/null
  fi
  herdr pane read "$PANE" --source recent-unwrapped --lines 800 > "$TMP/p2-reload.log"
  status=$(python3 - "$TMP/p2-reload.log" "$marker" "$agent" <<'PY'
import pathlib, re, sys
text=pathlib.Path(sys.argv[1]).read_text().split(sys.argv[2])[-1]
if any(x in text for x in ('nest-fail', 'syntax error', 'mull-grow', 'event failed', '-find.', 'mint-nice')):
    print('failed')
elif re.search(r'gall: (?:reloading|booted|unnuking|bumped) %'+re.escape(sys.argv[3])+r'\b', text):
    print('loaded')
else:
    print('waiting')
PY
  )
  case "$status" in
    loaded) echo "desk reloaded: %$agent"; exit 0 ;;
    failed) tail -60 "$TMP/p2-reload.log"; echo 'desk reload failed' >&2; exit 1 ;;
  esac
  sleep 2
done
echo "p2-reload.sh: no %$agent reload after $marker" >&2
exit 1
