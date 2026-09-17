#!/bin/bash
# usage: peer.sh fork <repo> <fork-name>
#        peer.sh push <fork-name> <branch> <message> [workflow-fixture]
#        peer.sh pull-request <fork-name> <title> <source-branch> <target-branch>
# The second galaxy's side of a fork pull request (BRIEF-CI-P2 §5, re-freeze
# 2): every request goes to the SECOND ship's API (SHIP_ROLE=2) with its own
# session, and every peer request is matched by the id the ship answered
# with — read back from GET /peer/discoveries or /peer/transfers — never by
# "the last entry".
#   fork          POST /peer/discover {ship} (the catalog of the first ship)
#                 then POST /peer/fork {ship, repository, name, publicRead},
#                 waiting for each to finish (active=false, ok=true)
#   push          a commit on <branch> of a local clone of the fork, pushed
#                 over Smart HTTP to the second ship (the fork is the second
#                 ship's own repository); sets and prints the branch's oid
#   pull-request  POST /peer/pull-request {name, title, sourceBranch,
#                 targetBranch}: the second ship offers the branch to the
#                 first, which records the pull with source-ship = the
#                 second galaxy; prints the pull request number on the
#                 first ship once the transfer finished
SHIP_ROLE=2
export SHIP_ROLE
source "$(dirname "$0")/env.sh"
api2="$P0/api.sh"
cmd="${1:?usage}"
FIRST=$(SHIP_ROLE=1 bash -c "source '$P0/env.sh'; printf '%s' \"\$SHIP\"")
FIRST_URL=$(SHIP_ROLE=1 bash -c "source '$P0/env.sh'; printf '%s' \"\$URL\"")
FIRST_JAR=$(SHIP_ROLE=1 bash -c "source '$P0/env.sh'; printf '%s' \"\$JAR\"")
answer() { printf '%s' "$1" | sed 's/^[0-9]* //'; }
status() { printf '%s' "$1" | cut -d' ' -f1; }
# wait_transfer <id> <seconds>: until GET /peer/transfers shows it inactive; prints ok/message
wait_transfer() {
  local t
  for _ in $(seq 1 "$2"); do
    t=$("$api2" GET /peer/transfers | sed 's/^[0-9]* //' | jq -c --arg id "$1" '.transfers[] | select(.transfer == $id)')
    if [ -n "$t" ] && [ "$(printf '%s' "$t" | jq -r .active)" = false ]; then
      printf '%s\n' "$(printf '%s' "$t" | jq -r '"\(.ok) \(.message // "")"')"; return 0
    fi
    sleep 1
  done
  echo "timeout ${t:0:200}"; return 1
}
wait_discovery() {
  local d
  for _ in $(seq 1 "$2"); do
    d=$("$api2" GET /peer/discoveries | sed 's/^[0-9]* //' | jq -c --arg id "$1" '.discoveries[] | select(.request == $id)')
    if [ -n "$d" ] && [ "$(printf '%s' "$d" | jq -r .active)" = false ]; then
      printf '%s\n' "$(printf '%s' "$d" | jq -r '"\(.ok) \(.message // "") repositories=\((.repositories // []) | length)"')"; return 0
    fi
    sleep 1
  done
  echo "timeout ${d:0:200}"; return 1
}
case "$cmd" in
  fork)
    repo="${2:?repo}"; fork="${3:?fork-name}"
    r=$("$api2" POST /peer/discover "{\"ship\":\"~$FIRST\"}")
    req=$(answer "$r" | jq -r '.request // empty')
    echo "-- discover ~$FIRST: $(status "$r") request $req"
    [ -n "$req" ] || { echo "peer.sh: discover refused: $r" >&2; exit 1; }
    echo "-- discovery: $(wait_discovery "$req" 60)"
    r=$("$api2" POST /peer/fork "{\"ship\":\"~$FIRST\",\"repository\":\"$repo\",\"name\":\"$fork\",\"publicRead\":true}")
    tr=$(answer "$r" | jq -r '.transfer // empty')
    echo "-- fork $repo as $fork: $(status "$r") transfer $tr"
    [ -n "$tr" ] || { echo "peer.sh: fork refused: $r" >&2; exit 1; }
    echo "-- transfer: $(wait_transfer "$tr" 300)"
    "$api2" GET "/repository/$fork" | sed 's/^[0-9]* //' | jq -r '"-- fork on ~'"$SHIP"': \(.name) head \(.head) refs \([.refs[].name] | join(" ")) origin \(.peerOrigin.ship)/\(.peerOrigin.repository)"'
    ;;
  push)
    fork="${2:?fork-name}"; branch="${3:?branch}"; message="${4:?message}"; fixture="${5:-}"
    clone="$TMP/clone-$SHIP-$fork"
    [ -s "$JAR" ] || "$api2" GET /repositories >/dev/null
    if [ ! -d "$clone/.git" ]; then
      rm -rf "$clone"; git clone -q -c http.cookieFile="$JAR" "$URL/git/$fork" "$clone"
      git -C "$clone" config user.name "$SHIP"; git -C "$clone" config user.email "$SHIP@example"; git -C "$clone" config http.cookieFile "$JAR"
    fi
    cd "$clone" && git fetch -q origin && git checkout -q -B "$branch" origin/master
    if [ -n "$fixture" ]; then rm -rf .github/workflows; mkdir -p .github/workflows; cp "$ROOT/desk/tests/ci/$fixture" .github/workflows/; fi
    # a file of the branch's own, so two contributions never conflict
    printf '%s %s %s\n' "$SHIP" "$branch" "$(date -Is)" > "contrib-$branch.txt"
    git add -A && git commit -qm "$message"
    oid=$(git rev-parse HEAD)
    git push -q origin "$branch" 2>&1 | tail -1
    echo "$oid"
    ;;
  pull-request)
    fork="${2:?fork-name}"; title="${3:?title}"; src="${4:?source-branch}"; dst="${5:?target-branch}"
    before=$(curl -s -b "$FIRST_JAR" "$FIRST_URL/apps/urgit/api/repository/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["peerOrigin"]["repository"])' <("$api2" GET "/repository/$fork" | sed 's/^[0-9]* //'))" | jq -r '[.pullRequests[].number] | max // 0')
    r=$("$api2" POST /peer/pull-request "{\"name\":\"$fork\",\"title\":\"$title\",\"sourceBranch\":\"$src\",\"targetBranch\":\"$dst\"}")
    tr=$(answer "$r" | jq -r '.transfer // empty')
    echo "-- pull-request from $fork ($src -> $dst): $(status "$r") transfer $tr"
    [ -n "$tr" ] || { echo "peer.sh: pull-request refused: $r" >&2; exit 1; }
    echo "-- transfer: $(wait_transfer "$tr" 300)"
    origin=$("$api2" GET "/repository/$fork" | sed 's/^[0-9]* //' | jq -r .peerOrigin.repository)
    n=$(curl -s -b "$FIRST_JAR" "$FIRST_URL/apps/urgit/api/repository/$origin" | jq -r --argjson b "$before" '[.pullRequests[] | select(.number > $b) | select(.sourceShip == "~'"$SHIP"'")] | .[0].number // empty')
    echo "-- pull request on ~$FIRST/$origin: #$n (source ship $(curl -s -b "$FIRST_JAR" "$FIRST_URL/apps/urgit/api/repository/$origin" | jq -r --argjson n "${n:-0}" '.pullRequests[] | select(.number == $n) | .sourceShip'))"
    echo "$n"
    ;;
  *) echo "usage: peer.sh fork|push|pull-request ..." >&2; exit 2 ;;
esac
