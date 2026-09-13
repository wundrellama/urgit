#!/bin/bash
# Negative rows H9, H13, H12, H10, H11 on the merge candidate (CAND2/MERGE).
source "$(dirname "$0")/env.sh"
source "$HERE/lib.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
poll() { "$api" GET "/ci/daemon/$1/assignment" "" "$2"; }
attempt_of() { grep -o '"attempt":"[^"]*"' | head -1 | cut -d'"' -f4; }
cand_status() { cand_state "$1"; }

echo "########## H9: claim success with no relayed jobResult -> 409; candidate stays %pending"
"$dojo" ":urgit-ci &ci-action [%assign $CAND2 $DAEMON %job \`'fixture-pass.yml' \`'pass' ~]" 60 3 | tail -2
A2=$(poll "$DAEMON" "$BEARER" | tee "$TMP/h9-assignment.txt" | attempt_of || true)
echo "A2=$A2"
"$api" POST "/ci/attempt/$A2/result" '{"job-result":"success"}' "$BEARER"
cand_status "$CAND2"

echo "########## H13: a 65 KiB event line -> 413; attempt unaffected"
python3 -c 'import json,sys; sys.stdout.write(json.dumps({"job":"j","jobID":"j","time":"2026-09-12T17:03:50-05:00","msg":"a"*66560}))' > "$TMP/h13-big.json"
echo "line bytes: $(wc -c < "$TMP/h13-big.json")"
"$api" POST "/ci/attempt/$A2/event" "@$TMP/h13-big.json" "$BEARER"
att_state "$A2"

echo "########## H12: an event for daemon A's attempt with daemon B's bearer -> 401"
"$dojo" ':urgit-ci|mint-enroll-token' 60 6 > "$TMP/h12-mint.txt"
TOKEN_B=$(grep -o 'ci-enroll-token 0v[0-9a-v.]*' "$TMP/h12-mint.txt" | tail -1 | sed 's/.* //' || true)
ENROLL_B=$("$api" POST /ci/daemon/enroll "{\"token\":\"$TOKEN_B\"}" -)
echo "$ENROLL_B"
BEARER_B=$(echo "$ENROLL_B" | grep -o '"bearer":"[^"]*"' | cut -d'"' -f4 || true)
DAEMON_B=$(echo "$ENROLL_B" | grep -o '"daemon-id":"[^"]*"' | cut -d'"' -f4 || true)
head -n 1 "$TMP/h7-act.log" > "$TMP/h12-line.json"
echo "-- with B's bearer:"
"$api" POST "/ci/attempt/$A2/event" "@$TMP/h12-line.json" "$BEARER_B"
echo "-- same line with A's bearer (the line itself is fine):"
"$api" POST "/ci/attempt/$A2/event" "@$TMP/h12-line.json" "$BEARER"

echo "########## H10: deadline passes with no result -> attempt %infrastructure-error, candidate %unknown; push -> ng"
"$dojo" ":urgit-ci &ci-action [%assign $CAND2 $DAEMON %job \`'fixture-pass.yml' \`'pass' \`~s20]" 60 3 | tail -2
A3=$(poll "$DAEMON" "$BEARER" | tee "$TMP/h10-assignment.txt" | attempt_of || true)
echo "A3=$A3 (deadline-seconds: $(grep -o '"deadline-seconds":[0-9]*' "$TMP/h10-assignment.txt"))"
echo "-- before the deadline:"
att_state "$A3"; cand_status "$CAND2"
sleep 28
echo "-- after the deadline:"
att_state "$A3"; cand_status "$CAND2"
echo "-- push the merge OID to master:"
cd "$TMP/clone-b"
git push --force origin "$MERGE:refs/heads/master" 2>&1 | tee "$TMP/h10-push.log" | grep -E 'rejected|staged|master' || true

echo "########## H11: fixture-fail -> jobResult failure -> candidate %failed; push -> ng"
"$dojo" ":urgit-ci &ci-action [%assign $CAND2 $DAEMON %job \`'fixture-fail.yml' \`'fail' ~]" 60 3 | tail -2
A4=$(poll "$DAEMON" "$BEARER" | tee "$TMP/h11-assignment.txt" | attempt_of || true)
echo "A4=$A4"
git checkout -q "$MERGE"
act push -W .github/workflows/fixture-fail.yml -j fail -P ubuntu-latest=catthehacker/ubuntu:act-latest \
  --network bridge --json --pull=false > "$TMP/h11-act.log" 2> "$TMP/h11-act.err" || true
echo "act lines=$(wc -l < "$TMP/h11-act.log"); jobResult line:"
grep -o '"jobResult":"[a-z]*"' "$TMP/h11-act.log"
n=0; : > "$TMP/h11-relay.log"
while IFS= read -r line; do
  printf '%s' "$line" > "$TMP/h11-line.json"
  "$api" POST "/ci/attempt/$A4/event" "@$TMP/h11-line.json" "$BEARER" >> "$TMP/h11-relay.log"
  n=$((n+1))
done < "$TMP/h11-act.log"
echo "relayed $n lines; last answer:"; tail -n 2 "$TMP/h11-relay.log"
echo "-- claiming success against a relayed failure is refused:"
"$api" POST "/ci/attempt/$A4/result" '{"job-result":"success"}' "$BEARER"
echo "-- the honest result:"
"$api" POST "/ci/attempt/$A4/result" '{"job-result":"failure"}' "$BEARER"
cand_status "$CAND2"
echo "-- push the merge OID to master:"
git checkout -q master
git push --force origin "$MERGE:refs/heads/master" 2>&1 | tee "$TMP/h11-push.log" | grep -E 'rejected|staged|master' || true
cat >> "$TMP/oids.env" <<EOF
export A2=$A2
export A3=$A3
export A4=$A4
export TOKEN_B=$TOKEN_B
export BEARER_B=$BEARER_B
export DAEMON_B=$DAEMON_B
EOF
