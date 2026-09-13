#!/bin/bash
# Row H8: POST the job result (success) after the relayed jobResult event;
# the candidate becomes %passed, and pushing the candidate OID to master
# lands (`ok refs/heads/master`).
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
echo "== result: success"
"$api" POST "/ci/attempt/$ATTEMPT/result" '{"job-result":"success"}' "$BEARER"
echo "== candidate status"
"$dojo" ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CAND/noun)" 60 16 | grep -E 'status|candidate='
echo "== eligibility scry for the candidate OID (raw 40-hex is a path syntax error when it starts with a digit: wrap it)"
"$dojo" ".^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/(scot %t '$THREE')/noun)" 60 3 | tail -2
echo "== push the candidate OID to master (must land)"
cd "$TMP/clone-a"
git checkout -q master
git push origin master 2>&1 | tee "$TMP/h8-push.log" | tail -3
"$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print("master oid:", [r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'
echo "expected THREE=$THREE"
