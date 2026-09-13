#!/bin/bash
# Harness step 1: boot the ship from the launch footer and install the desk,
# with nothing typed by hand. Run once, first; the other scripts read the
# pane id and +code it records under $TMP.
#   1. the pier must not exist yet (footer rule)
#   2. split a herdr pane from this one and WAIT FOR THE SHELL PROMPT before
#      typing the boot line: a fresh pane paints its MOTD first, and a line
#      sent during that paint is eaten
#   3. wait for the dojo prompt; |new-desk %urgit; |mount %urgit; wait for
#      the mount to appear on disk
#   4. zig build -Ddesk=$PIER/urgit; |commit %urgit (wait until clay has
#      desk.bill); |install our %urgit (wait until %urgit-ci answers %gu)
#   5. .^(@ud %gx /=urgit-ci=/state/version/noun) must print 0
#   6. read +code into $TMP/code.txt; env.sh sources it for api.sh
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
if [ -e "$PIER" ]; then
  echo "boot.sh: $PIER exists; the footer says it must not exist before boot" >&2
  exit 1
fi
rm -f "$TMP/ship-pane.id" "$TMP/code.txt" "$JAR" "$TMP/oids.env"
value() { python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["pane"]["pane_id"])'; }
from="${HERDR_PANE_ID:-$(herdr pane current | value)}"
PANE=$(herdr pane split --pane "$from" --direction down --no-focus --cwd "$ROOT" | value)
export PANE
echo "$PANE" > "$TMP/ship-pane.id"
echo "== ship pane $PANE (split from $from); waiting for its shell prompt"
herdr pane wait-output "$PANE" --lines 1 --regex "$SHELL_PROMPT_RE" --timeout 60000 >/dev/null
boot="$URBIT -F $SHIP -B $PILL --http-port $PORT -c $PIER"
echo "== $boot"
echo "$boot" > "$TMP/boot-line.txt"
date -Is > "$TMP/launched-at"
herdr pane run "$PANE" "$boot" >/dev/null
echo "== waiting for the dojo prompt"
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout 900000 >/dev/null
herdr pane read "$PANE" --lines 4
echo "== |new-desk %urgit ; |mount %urgit"
"$dojo" '|new-desk %urgit' 120 3 | tail -2
"$dojo" '|mount %urgit' 120 3 | tail -2
for _ in $(seq 1 60); do [ -d "$PIER/urgit" ] && break; sleep 1; done
[ -d "$PIER/urgit" ] || { echo "boot.sh: $PIER/urgit never appeared" >&2; exit 1; }
echo "== zig build -Ddesk=$PIER/urgit"
( cd "$ROOT" && zig build -Ddesk="$PIER/urgit" 2>&1 | tail -2 )
echo "== |commit %urgit"
"$dojo" '|commit %urgit' 300 3 | tail -2
for _ in $(seq 1 150); do
  have=$("$dojo" '.^(? %cu /=urgit=/desk/bill)' 60 3 | grep -oE '^%\.[yn]$' | tail -1 || true)
  [ "$have" = "%.y" ] && break
  sleep 2
done
[ "$have" = "%.y" ] || { echo "boot.sh: clay never showed /=urgit=/desk/bill" >&2; exit 1; }
echo "== |install our %urgit"
"$dojo" '|install our %urgit' 300 3 | tail -2
for _ in $(seq 1 150); do
  live=$("$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | grep -oE '^%\.[yn]$' | tail -1 || true)
  [ "$live" = "%.y" ] && break
  sleep 2
done
[ "$live" = "%.y" ] || { echo "boot.sh: %urgit-ci never answered %gu liveness" >&2; exit 1; }
echo "== %urgit-ci is live; state version (expect 0):"
"$dojo" '.^(@ud %gx /=urgit-ci=/state/version/noun)' 60 3 | tail -2
echo "== +code -> $TMP/code.txt"
"$dojo" '+code' 60 4 | grep -oE '^[a-z]{6}(-[a-z]{6}){3}$' | tail -1 > "$TMP/code.txt"
[ -s "$TMP/code.txt" ] || { echo "boot.sh: could not read +code from the dojo" >&2; exit 1; }
echo "code recorded ($(wc -c < "$TMP/code.txt") bytes)"
