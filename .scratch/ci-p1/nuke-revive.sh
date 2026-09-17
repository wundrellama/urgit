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
herdr pane send-keys "$PANE" 'ctrl+a' 'ctrl+k' >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout 60000 >/dev/null
herdr pane run "$PANE" '|nuke %urgit-ci' >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex 'nuke\? \(y/N\)' --timeout 60000 >/dev/null
herdr pane send-keys "$PANE" 'y' 'enter' >/dev/null
herdr pane wait-output "$PANE" --lines 1 --regex "$DOJO_PROMPT_RE" --timeout 120000 >/dev/null
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
after_marker() { herdr pane read "$PANE" --source recent-unwrapped --lines 800 | awk -v m="'$marker'" 'index($0, m) { found = 1; next } found'; }
# a commit of an unchanged desk prints nothing (no reload, no file lines)
# and does not re-boot the nuked agent; the revive below covers both cases
sleep 10
if after_marker | grep -q 'crud: %into event failed'; then echo "nuke-revive.sh: the commit event failed" >&2; after_marker | grep -v '^/sys' | tail -40 >&2; exit 1; fi
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
