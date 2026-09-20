#!/bin/bash
# usage: r17.sh
# R17 (rider 5, opus §5): one Clay operation at a time. A plain push into a
# bound repository parks its clay-push (a 1 s start timer, the desk write,
# a 1 s report timer: the ref advances in the completion event); a peer
# push from ~tug into the same repository that completes inside that
# window is refused with the receive path's existing message, 'another
# Clay operation is in progress', and the parked push completes: master
# = its head, the desk holds its file, the peer push's commit is nowhere.
# The six pre-P3 guards compared against a bare `^` and never fired, so
# the second operation overwrote the first's parked push (QUESTIONS §5);
# the fix is one token at each site. The window is at least two seconds
# by the timers; the peer push is the longer operation, so it goes first
# and the plain push starts into it (the loop below adapts the delay and
# tries again with a fresh fork, up to five times).
source "$(dirname "$0")/lib.sh"
TS=$(date +%H%M%S)
row "R17: a concurrent linked peer push is refused 'another Clay operation is in progress' while the parked push completes"
DESK="r17-$TS"
"$dojo" "|new-desk %$DESK" 120 3 | tail -1
check "a fresh desk %$DESK" '%.y' "$(dojo_value "(~(has in .^((set desk) %cd /(scot %p our)//(scot %da now))) %$DESK)" | one '^%\.[yn]$')"
LINKED="ci-p3-linked-r17-$TS"
"$api" POST /repositories "{\"name\":\"$LINKED\",\"publicRead\":true}" | cut -c1-30
# desk-shaped, as R14 seeds (the marks a new desk has, plus yml and mime)
L="$TMP/clone-$LINKED"; rm -rf "$L"; mkdir -p "$L"; cd "$L"; git init -q -b master .; git config user.name r17; git config user.email r17@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$LINKED"; mkdir -p mar
for m in txt yml mime hoon kelvin noun; do cp "$ROOT/desk/mar/$m.hoon" mar/; done; cp "$ROOT/desk/sys.kelvin" sys.kelvin
printf 'seed\n' > notes.txt; git add -A; git commit -qm seed; git push -q origin master 2>&1 | tail -1
SEED=$(git rev-parse HEAD)
"$api" POST "/repository/$LINKED/branches/default" '{"name":"master"}' >/dev/null
r=$("$api" POST "/repository/$LINKED/bind" "{\"desk\":\"$DESK\",\"branch\":\"refs/heads/master\"}")
check "bound to desk %$DESK through the API" "200" "$(status_of "$r")"
check "~$SHIP2 listed as a writer of $LINKED -> 200" "200" "$(status_of "$("$api" POST "/repository/$LINKED/writers" "{\"ship\":\"~$SHIP2\",\"allowed\":true}")")"
# the second ship's side: a native fork of the repository over the peer
# protocol, a commit on the fork through ~tug's own git route, and the
# peer push of that fork back into the origin
wait_transfer() {  # <role> <transfer> <seconds>: prints "ok message" once inactive
  local t="" ; for _ in $(seq 1 "$3"); do
    t=$(SHIP_ROLE=$1 "$P0/api.sh" GET /peer/transfers | sed 's/^[0-9]* //' | jq -c ".transfers[] | select(.transfer == \"$2\")")
    [ -n "$t" ] && [ "$(printf '%s' "$t" | jq -r .active)" = false ] && break; sleep 1
  done
  printf '%s %s' "$(printf '%s' "$t" | jq -r .ok)" "$(printf '%s' "$t" | jq -r .message)"
}
# the peer push is the long one (measured alone on this pair: ~4 s from the
# POST to its finish event on ~sud, then its own 2 s clay-push); the plain
# push's window is ~2 s (its two timers and the write). So the peer push
# goes first and the plain push starts DELAY seconds into it, parking for
# the peer push's finish. A miss is diagnosed by the peer push's result:
# landed = the plain push started too late (the window had not opened),
# not a fast-forward = too early (the window had closed); the delay is
# adjusted and the row tries again with a fresh fork, up to five times.
outcome=""; tries=0; delay=3.0
while [ $tries -lt 5 ]; do
  tries=$((tries+1)); FORK="r17-fork-$TS-$tries"
  r=$(SHIP_ROLE=2 "$P0/api.sh" POST /peer/fork "{\"ship\":\"~$SHIP\",\"repository\":\"$LINKED\",\"name\":\"$FORK\",\"publicRead\":true}")
  FT=$(jq_of "$r" .transfer)
  echo "-- try $tries (delay $delay s): ~$SHIP2 forks $LINKED as $FORK: $(status_of "$r") transfer $FT"
  fr=$(wait_transfer 2 "$FT" 60); echo "-- the fork: $fr"
  [ "${fr%% *}" = true ] || { echo "r17: the fork did not complete: $fr"; continue; }
  F="$TMP/clone-$FORK"; rm -rf "$F"; mkdir -p "$F"; cd "$F"; git init -q -b master .; git config user.name r17tug; git config user.email r17tug@example
  git config http.cookieFile "$TMP/cookies-$SHIP2.txt"
  SHIP_ROLE=2 "$P0/api.sh" GET /repositories >/dev/null   # logs ~tug's session in (the jar)
  git remote add origin "http://127.0.0.1:$PORT2/git/$FORK"; git pull -q origin master 2>&1 | tail -1
  printf 'r17 peer %s try %s\n' "$(date -Is)" "$tries" >> notes.txt; git add -A; git commit -qm "r17: the peer push's commit"; PEER=$(git rev-parse HEAD)
  git push -q origin master 2>&1 | tail -1
  check "the fork on ~$SHIP2 carries the peer commit" "$PEER" "$(REPO=$FORK SHIP_ROLE=2 repo_master)"
  # the plain push's commit, on the current tip of the bound branch
  cd "$L"; git fetch -q origin master 2>/dev/null; git reset -q --hard FETCH_HEAD
  printf 'r17 plain %s try %s\n' "$(date -Is)" "$tries" >> notes.txt; git add -A; git commit -qm "r17: the parked plain push"; PLAIN=$(git rev-parse HEAD)
  r=$(SHIP_ROLE=2 "$P0/api.sh" POST /peer/push "{\"name\":\"$FORK\"}")
  PT=$(jq_of "$r" .transfer)
  echo "-- the peer push from ~$SHIP2: $(status_of "$r") transfer $PT (sent $(date +%T.%N | cut -c1-12))"
  sleep "$delay"
  echo "-- the plain push starts $(date +%T.%N | cut -c1-12)"
  # bounded: under a guard that never fires the peer push's finish overwrites
  # the parked push, whose held response is never sent — git would wait for
  # ever (the R17 mutant's RED)
  plain=$(timeout 40 git push origin master 2>&1 | tail -1); echo "-- the plain push ($(date +%T.%N | cut -c1-12)): ${plain:-(no answer within 40 s)}"
  pr=$(wait_transfer 2 "$PT" 60); echo "-- the peer push's result: $pr"
  case "$pr" in
    *"another Clay operation is in progress"*) outcome="$pr"; break ;;
    *"not a fast-forward"*) echo "-- the peer push arrived after the window closed: the plain push earlier next time"; delay=$(awk -v d="$delay" 'BEGIN{printf "%.1f", d-0.7}') ;;
    true*) echo "-- the peer push landed before the window opened: the plain push later next time"; delay=$(awk -v d="$delay" 'BEGIN{printf "%.1f", d+0.7}') ;;
    *) outcome="$pr"; break ;;
  esac
done
check "the peer push was refused with the receive path's message" "false another Clay operation is in progress" "$outcome"
check "the parked plain push completed: master = its head" "$PLAIN" "$(for _ in $(seq 1 20); do [ "$(REPO=$LINKED repo_master)" = "$PLAIN" ] && break; sleep 1; done; REPO=$LINKED repo_master)"
check "the desk holds the plain push's file" "1" "$(dojo_value "(of-wain:format .^(wain %cx /(scot %p our)/$DESK/(scot %da now)/notes/txt))" | grep -c "r17 plain .* try $tries")"
check "the peer push's commit is not master" "no" "$([ "$(REPO=$LINKED repo_master)" = "$PEER" ] && echo yes || echo no)"
check "the desk does not hold the peer push's file" "0" "$(dojo_value "(of-wain:format .^(wain %cx /(scot %p our)/$DESK/(scot %da now)/notes/txt))" | grep -c "r17 peer .* try $tries")"
echo "-- tries: $tries"
"$api" POST "/repository/$LINKED/unbind" '{}' | cut -c1-20
"$api" POST "/repository/$LINKED/writers" "{\"ship\":\"~$SHIP2\",\"allowed\":false}" | cut -c1-20
end_row R17
[ "$NFAIL" = 0 ]
