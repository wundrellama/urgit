#!/bin/bash
# P2 setup, after the P1 battery (or on a ship whose %urgit-ci state the
# P1 rows left behind): a fresh %urgit-ci state (nuke/revive), the
# rootless Docker daemon with the act image, the store fixture up and the
# ship's %storage pointed at it (the P0 recipe against a REAL store), the
# static act, the runner binary, a fresh public repository `ci-p2` seeded
# with one commit (fixture-pass.yml) and CI-protected, a local clone, and
# daemon `a` enrolled afresh at the footer's capacity ($DAEMON_CAPACITY,
# default 3) — once; no row restarts it at another capacity (T1).
source "$(dirname "$0")/lib.sh"
"$P1/nuke-revive.sh" p2-setup | tail -3
"$P1/docker-rootless.sh" start | tail -2
"$P1/docker-rootless.sh" info | tail -1
"$store" start | tail -3
# the store must ANSWER before %storage is pointed at it and Q2 uploads
# (T3: the operator's first attempt 401'd into a store that was not up —
# `store.sh start | tail` had hidden its exit); a bounded retry, then a
# non-zero exit naming the port
"$store" ready || { echo "p2-setup: the store fixture on 127.0.0.1:$STORE_PORT does not answer; the P2 rows need it before Q2" >&2; exit 1; }
"$store" configure | tail -1
"$P1/act-static.sh"
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) && echo "runner built: $RUNNER_BIN"
echo "== create repository $REPO (public)"
created=$("$api" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}")
echo "$created" | cut -c1-60
case "$created" in 201*) ;; *) echo "p2-setup: could not create $REPO ($created); the P2 table needs a fresh repository" >&2; exit 1 ;; esac
rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .
git config user.name p2; git config user.email p2@example
git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
set_workflows fixture-pass.yml
echo "seed" > README.md
git add -A && git commit -qm "seed: fixture-pass"
SEED=$(git rev-parse HEAD)
git push -q origin master 2>&1 | tail -1
check "seed landed" "$SEED" "$(repo_master)"
# the repository's default branch is master (creation defaults to main):
# a peer fork checks the head ref exists and refuses the graph otherwise
"$api" POST "/repository/$REPO/branches/default" '{"name":"master"}' | cut -c1-30
check "the second galaxy ~$SHIP2 is up (its API answers)" "200" "$(SHIP_ROLE=2 "$P0/api.sh" GET /repositories | cut -d' ' -f1)"
"$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 3 | tail -1
check "CI-protected" '%.y' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
"$P1/runner.sh" stop a >/dev/null 2>&1
rm -rf "$RUNNER_HOME/a"
TOKEN=$("$P1/mint.sh") || exit 1
"$P1/runner.sh" start a "$DAEMON_CAPACITY" "$TOKEN" | head -1
DAEMON_A=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/a/state.json")
echo "export DAEMON_A=$DAEMON_A" > "$TMP/p2.env"
echo "daemon a enrolled as $DAEMON_A with capacity $DAEMON_CAPACITY (DAEMON_CAPACITY)"
echo "== prelude: ci bound in the dojo"
"$P0/prelude.sh" | tail -1
[ "$ROW_FAIL" = 0 ]
