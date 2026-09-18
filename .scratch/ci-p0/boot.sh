#!/bin/bash
# Harness step 1: boot the ship from the launch footer and install the desk,
# with nothing typed by hand. Run once, first; the other scripts read the
# +code it records under $TMP.
#   1. the pier must not exist yet (footer rule), nor the ship's tmux session
#   2. start the boot line as a detached tmux session named from the footer
#      (`tmux new-session -d -s $TTY "<urbit> -F … -B … --http-port … -c …"`,
#      200 columns so the dojo seldom pretty-prints a value over lines);
#      the session IS the ship's terminal: no shell, no prompt to wait for
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
if tty_alive; then
  echo "boot.sh: tmux session $TTY already exists; shut that ship down first (shutdown.sh)" >&2
  exit 1
fi
rm -f "$TMP/code$ROLE_SUFFIX.txt" "$JAR"
[ -z "$ROLE_SUFFIX" ] && rm -f "$TMP/oids.env"
boot="$URBIT -F $SHIP -B $PILL --http-port $PORT -c $PIER"
echo "== tmux new-session -d -s $TTY -x 200 -y 50 \"$boot\""
echo "$boot" > "$TMP/boot-line$ROLE_SUFFIX.txt"
date -Is > "$TMP/launched-at$ROLE_SUFFIX"
tmux new-session -d -s "$TTY" -x 200 -y 50 -c "$ROOT" "$boot"
echo "== waiting for the dojo prompt"
tty_wait "$DOJO_PROMPT_RE" 900 || { echo "boot.sh: no dojo prompt in $TTY within 900 s" >&2; tty_read 20 >&2; exit 1; }
tty_read 4
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
echo "== +code -> $TMP/code$ROLE_SUFFIX.txt"
"$dojo" '+code' 60 10 | grep -oE '^[a-z]{6}(-[a-z]{6}){3}$' | tail -1 > "$TMP/code$ROLE_SUFFIX.txt"
[ -s "$TMP/code$ROLE_SUFFIX.txt" ] || { echo "boot.sh: could not read +code from the dojo" >&2; exit 1; }
echo "code recorded ($(wc -c < "$TMP/code$ROLE_SUFFIX.txt") bytes)"
