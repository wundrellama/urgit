#!/bin/bash
# Row H14: with %urgit-ci suspended, a push to the CI-protected ref is
# refused with the D2 outage reason, and so is a push to an unprotected
# ref; after |revive the unprotected push lands and the protected one is
# staged. First a fresh passed candidate (commit four) is made the same way
# as H3-H8. P1 (D14): the ship lands the passed OID itself, so `master`
# already equals FOUR before the suspension, and the protected-ref probe
# is a fresh commit (five) rather than the passed OID.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
poll() { "$api" GET "/ci/daemon/$1/assignment" "" "$2"; }
attempt_of() { grep -o '"attempt":"[^"]*"' | head -1 | cut -d'"' -f4; }
cd "$TMP/clone-a"
git checkout -q master
echo "five" >> README.md
git commit -qam "five: a second candidate to pass"
FOUR=$(git rev-parse HEAD)
echo "FOUR=$FOUR"
git push origin master 2>&1 | tee "$TMP/h14-push0.log" | grep -E 'rejected|staged' || true
CAND4=$(grep -o 'staged as ci candidate 0v[0-9a-v.]*' "$TMP/h14-push0.log" | head -1 | sed 's/.*candidate //' || true)
echo "CAND4=$CAND4"
"$dojo" ":urgit-ci &ci-action [%materialize $CAND4]" 60 3 | tail -1
sleep 2
"$dojo" ":urgit-ci &ci-action [%assign $CAND4 $DAEMON %job \`'fixture-pass.yml' \`'pass' ~]" 60 3 | tail -1
A5=$(poll "$DAEMON" "$BEARER" | tee "$TMP/h14-assignment.txt" | attempt_of || true)
echo "A5=$A5"
act push -W .github/workflows/fixture-pass.yml -j pass -P ubuntu-latest=catthehacker/ubuntu:act-latest \
  --network bridge --json --pull=false > "$TMP/h14-act.log" 2> "$TMP/h14-act.err" || true
n=0; : > "$TMP/h14-relay.log"
while IFS= read -r line; do
  printf '%s' "$line" > "$TMP/h14-line.json"
  "$api" POST "/ci/attempt/$A5/event" "@$TMP/h14-line.json" "$BEARER" >> "$TMP/h14-relay.log"
  n=$((n+1))
done < "$TMP/h14-act.log"
echo "relayed $n lines"
"$api" POST "/ci/attempt/$A5/result" '{"job-result":"success"}' "$BEARER"
sleep 3
echo "== P1: the ship landed FOUR itself (D14); master now:"
"$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print("master oid:", [r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'
echo "six" >> README.md
git commit -qam "six: a commit to probe the gate while %urgit-ci is stopped"
SIX=$(git rev-parse HEAD)
echo "SIX=$SIX"
echo "== stop the agent: |rein %urgit [%.n %urgit-ci] (|suspend takes a desk)"
"$dojo" '|rein %urgit [%.n %urgit-ci]' 60 4 | tail -3
"$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | tail -2
echo "-- push commit six to master while suspended (expect the outage refusal):"
git push origin master 2>&1 | tee "$TMP/h14-push1.log" | grep -E 'rejected|refused|master' || true
echo "-- push FOUR to an unprotected ref (refs/heads/side) while suspended (expect the outage refusal):"
git push origin "$FOUR:refs/heads/side" 2>&1 | tee "$TMP/h14-push2.log" | grep -E 'rejected|refused|side' || true
echo "== restart the agent: |rein %urgit [%.y %urgit-ci]"
"$dojo" '|rein %urgit [%.y %urgit-ci]' 90 4 | tail -3
"$dojo" '.^(? %gu /=urgit-ci=/$)' 60 3 | tail -2
echo "-- push commit six to master again (expect: staged as a candidate; the gate is back):"
git push origin master 2>&1 | tee "$TMP/h14-push3.log" | grep -E 'rejected|staged|master' || true
echo "-- push FOUR to the unprotected ref again (expect: lands):"
git push origin "$FOUR:refs/heads/side" 2>&1 | tee "$TMP/h14-push4.log" | tail -2
git reset -q --hard "$FOUR"
"$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print({r["name"]: r["oid"] for r in d["refs"] if not r["name"].startswith("refs/ci/")})'
echo "expected master=side=$FOUR (six staged, not landed)"
cat >> "$TMP/oids.env" <<EOF
export FOUR=$FOUR
export CAND4=$CAND4
export A5=$A5
EOF
