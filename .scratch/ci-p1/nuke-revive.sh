#!/bin/bash
# usage: nuke-revive.sh <label>
# The D12 round-trip for a state-0 that grew in place: |nuke %urgit-ci
# (answering kiln's y/N prompt), rebuild and |commit the desk with the
# agent stopped (a commit that reloads an agent whose old state no longer
# nests fails as a whole and Clay rolls it back), then |revive %urgit and
# wait until %urgit-ci answers %gu. State is wiped by construction.
source "$(dirname "$0")/env.sh"
dojo="$P0/dojo.sh"
label="${1:-nuke}"
echo "== |nuke %urgit-ci (answering y)"
tty_keys C-a C-k
tty_wait "$DOJO_PROMPT_RE" 60
tty_send '|nuke %urgit-ci'
tty_wait 'nuke\? \(y/N\)' 60
tty_keys y Enter
tty_wait "$DOJO_PROMPT_RE" 120
for _ in $(seq 1 30); do
  live=$("$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | grep -oE '^%\.[yn]$' | tail -1 || true)
  [ "$live" = "%.n" ] && break; sleep 2
done
echo "%gu after nuke: $live"
echo "== zig build -Ddesk=$PIER/urgit ($label)"
( cd "$ROOT" && zig build -Ddesk="$PIER/urgit" 2>&1 | tail -1 )
marker="nuke-$label-$(date +%s)"
"$dojo" "'$marker'" 60 2 >/dev/null
echo "== |commit %urgit (marker $marker)"
"$dojo" '|commit %urgit' 300 3 | tail -2
after_marker() { tty_read 400 | awk -v m="'$marker'" 'index($0, m) { found = 1; next } found'; }
# a commit of an unchanged desk prints nothing (no reload, no file lines)
# and does not re-boot the nuked agent; the revive below covers both cases.
# the build runs after the prompt returns: wait for its verdict (a failed
# commit prints `crud: %into event failed` and Clay rolls back; a good one
# re-boots the nuked agent) for up to two minutes before reading it
for _ in $(seq 1 60); do
  if after_marker | grep -q 'crud: %into event failed'; then
    echo "nuke-revive.sh: the commit event failed (the desk was rolled back)" >&2
    after_marker | grep -vE '^/sys|^/app/urgit/hoon::|^/lib/|^/sur/' | grep -A30 'nest-fail\|mint-\|find\|bail' | head -40 >&2
    exit 1
  fi
  after_marker | grep -qE "^gall: (reloading|booted|unnuking) %urgit-ci" && break
  sleep 2
done
after_marker | grep -E "^gall: (reloading|booted|unnuking) %urgit|^[:+] /~$SHIP/urgit/" | head -5 || true
echo "== |revive %urgit"
"$dojo" '|revive %urgit' 120 4 | tail -3
for _ in $(seq 1 60); do
  live=$("$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | grep -oE '^%\.[yn]$' | tail -1 || true)
  [ "$live" = "%.y" ] && break; sleep 2
done
echo "%gu after revive: $live"
[ "$live" = "%.y" ] || exit 1
printf 'state version: '; "$dojo" '.^(@ud %gx /=urgit-ci=/state/version/noun)' 60 3 | grep -oE '^[0-9]+$' | tail -1
# the dojo's `ci` binding is a build of sur/ci.hoon: re-bind it, since a
# grown mold makes every cast through the old binding crash the scry
"$P0/prelude.sh" | tail -1
