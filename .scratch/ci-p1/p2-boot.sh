#!/bin/bash
# Boot a footer ship once, or resume its retained pier without re-creating it.
# CI_PEER=1 selects the second ship through the shared P0 environment.
source "$(dirname "$0")/env.sh"
if [ ! -e "$PIER" ]; then exec "$P0/boot.sh"; fi
python3 - "$URBIT" "$PIER" <<'PY'
import pathlib, sys
binary, pier = (x.encode() for x in sys.argv[1:])
for p in pathlib.Path('/proc').iterdir():
    if not p.name.isdigit(): continue
    try: args=(p/'cmdline').read_bytes().split(b'\0')
    except OSError: continue
    if args[0]==binary and pier in args:
        raise SystemExit(f'pier already running: PID {p.name}')
PY
value() { python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["pane"]["pane_id"])'; }
from="${HERDR_PANE_ID:-$(herdr pane current | value)}"
PANE=$(herdr pane split --pane "$from" --direction down --no-focus --cwd "$ROOT" | value)
echo "$PANE" > "$TMP/ship-pane.id"
echo "== resuming ~$SHIP in pane $PANE; waiting for shell prompt"
herdr pane wait-output "$PANE" --lines 1 --regex "$SHELL_PROMPT_RE" --timeout 60000 >/dev/null
boot="$URBIT --http-port $PORT $PIER"
echo "== $boot"
herdr pane run "$PANE" "$boot" >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout 900000 >/dev/null
"$P0/dojo.sh" '+code' 60 10 | rg '^[a-z]{6}(-[a-z]{6}){3}$' | tail -1 > "$TMP/code.txt"
[ -s "$TMP/code.txt" ]
rm -f "$JAR"
echo "resumed ~$SHIP; session code recorded"
