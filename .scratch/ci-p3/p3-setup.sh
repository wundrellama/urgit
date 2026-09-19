#!/bin/bash
# P3 setup (after the P0/P1/P2 batteries, or on a fresh pair): a fresh
# %urgit-ci state (nuke/revive), the rootless Docker daemon with the act
# image, the store fixture up and ANSWERING, the ship's %storage pointed
# at it on the host's LAN address (STORE_ADVERTISE, D5: the endpoint every
# viewer's browser must reach), the static act, the runner binary, the fe
# built into the desk, a fresh public repository `ci-p3` seeded with one
# commit (fixture-pass.yml), its default branch master, CI-protected
# through the web action route (the operator surface, not the dojo), a
# local clone, and daemon a enrolled at the footer's DAEMON_CAPACITY
# through the mint route — once.
source "$(dirname "$0")/lib.sh"
"$P1/nuke-revive.sh" p3-setup | tail -3
"$P1/docker-rootless.sh" start | tail -2
"$P1/docker-rootless.sh" info | tail -1
"$store" start | tail -3
"$store" ready || { echo "p3-setup: the store fixture on 127.0.0.1:$STORE_PORT does not answer" >&2; exit 1; }
echo "-- %storage endpoint: $STORE_ENDPOINT"
"$store" configure | tail -1
"$P1/act-static.sh"
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) && echo "runner built: $RUNNER_BIN"
echo "== create repository $REPO (public)"
created=$("$api" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}")
echo "$created" | cut -c1-60
case "$created" in 201*) ;; *) echo "p3-setup: could not create $REPO ($created)" >&2; exit 1 ;; esac
rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .
git config user.name p3; git config user.email p3@example
git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
set_workflows fixture-pass.yml
echo "seed" > README.md
git add -A && git commit -qm "seed: fixture-pass"
SEED=$(git rev-parse HEAD)
git push -q origin master 2>&1 | tail -1
check "seed landed" "$SEED" "$(repo_master)"
"$api" POST "/repository/$REPO/branches/default" '{"name":"master"}' | cut -c1-30
check "the second galaxy ~$SHIP2 is up (its API answers)" "200" "$(SHIP_ROLE=2 "$P0/api.sh" GET /repositories | cut -d' ' -f1)"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$REPO\",\"ref\":\"refs/heads/master\",\"protected\":true}")
check "CI required on master through POST ci/action" "200" "$(status_of "$r")"
check "CI-protected (scry)" '%.y' "$(ci_protected refs/heads/master)"
: > "$TMP/p3.env"
start_daemon a "$DAEMON_CAPACITY"
echo "== prelude: ci bound in the dojo"
"$P0/prelude.sh" | tail -1
[ "$ROW_FAIL" = 0 ]
