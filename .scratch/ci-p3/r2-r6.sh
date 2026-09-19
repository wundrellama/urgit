#!/bin/bash
# usage: r2-r6.sh r2|r6
# Rows R2 and R6 (BRIEF-CI-P3 D4, S3): the live channel. The row is the
# browser's subscriber: it opens Eyre's channel with the ship session,
# subscribes to %urgit-ci's fact path, and reads what arrives — never a
# GET after the subscribe, so every state it sees came unasked.
#   R2  subscribe /ci/runners; mint a token -> a runner fact `minted`
#       arrives; start a daemon with the token -> a fact `healthy` arrives
#       within one poll window; retire it -> `revoked`, then `runner-gone`
#   R6  subscribe /ci/repository/<repo>; push a commit -> facts carry the
#       candidate pending (staged), then planned, each attempt running then
#       passed, the verdict passed, 'landed' — with no read; delete the
#       channel -> the fallback read (Refresh) still answers the list
source "$(dirname "$0")/lib.sh"
which_row="${1:-r2}"
if [ "$which_row" = r2 ]; then
row "R2: a daemon enrolling with a minted token flips the panel row minted -> healthy through the channel, without Refresh"
STREAM="$TMP/r2-stream.txt"
PID=$(channel_open /ci/runners "$STREAM" 240) || exit 1
check "the initial fact is the runner list" "yes" "$(wait_fact "$STREAM" 'select(.kind == "runners") | .runners | type == "array"' 15 && echo yes || echo no)"
TOKEN=$(mint_token); ID=$(mint_id)
check "a runner fact for the minted record arrives (state minted)" "minted" "$(wait_fact "$STREAM" "select(.kind == \"runner\" and .id == \"$ID\") | .patch.state == \"minted\"" 15 && channel_facts "$STREAM" | jq -r "select(.kind == \"runner\" and .id == \"$ID\") | .patch.state" | head -1)"
rm -rf "$RUNNER_HOME/r2"
T0=$(date +%s)
RUNNER_LABELS='"r2-only"' "$P1/runner.sh" start r2 1 "$TOKEN" | head -1
check "a runner fact reads healthy after the daemon enrolls, with no GET" "healthy" "$(wait_fact "$STREAM" "select(.kind == \"runner\" and .id == \"$ID\") | .patch.state == \"healthy\"" 40 && channel_facts "$STREAM" | jq -r "select(.kind == \"runner\" and .id == \"$ID\") | .patch.state" | tail -1)"
echo "-- healthy fact after $(( $(date +%s) - T0 )) s"
check "the fact carries the daemon's labels" '["r2-only"]' "$(channel_facts "$STREAM" | jq -c "select(.kind == \"runner\" and .id == \"$ID\" and .patch.state == \"healthy\") | .patch.labels" | tail -1)"
check "the fact carries enrolled and lastSeen" "2" "$(channel_facts "$STREAM" | jq -c "select(.kind == \"runner\" and .id == \"$ID\" and .patch.state == \"healthy\") | [.patch.enrolled, .patch.lastSeen] | map(select(. != null)) | length" | tail -1)"
retire_daemon r2
check "a fact reads revoked after the revoke" "revoked" "$(wait_fact "$STREAM" "select(.kind == \"runner\" and .id == \"$ID\") | .patch.state == \"revoked\"" 15 && echo revoked)"
check "a runner-gone fact follows the removal" "gone" "$(wait_fact "$STREAM" "select(.kind == \"runner-gone\" and .id == \"$ID\")" 15 && echo gone)"
check "no fact ever carried the token" "0" "$(grep -cF "$TOKEN" "$STREAM")"
kill "$PID" 2>/dev/null
end_row R2
fi
if [ "$which_row" = r6 ]; then
row "R6: a push is watched live through the channel — staged, planned, each attempt, the verdict, landed — with no read; the fallback read still works after the channel is gone"
STREAM="$TMP/r6-stream.txt"
PID=$(channel_open "/ci/repository/$REPO" "$STREAM" 600) || exit 1
check "the initial fact is the repository's candidate list with the runner list" "yes" "$(wait_fact "$STREAM" "select(.kind == \"candidates\" and .repo == \"$REPO\") | (.candidates | type == \"array\") and (.runners | type == \"array\")" 15 && echo yes || echo no)"
N0=$(channel_facts "$STREAM" | wc -l)
sync_clone; set_workflows fixture-chain.yml
printf 'r6 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R6: watched live"
T0=$(date +%s)
f() { channel_facts "$STREAM" | jq -c "select(.kind == \"candidate\" and .id == \"$CID\") | .patch"; }
check "a fact for the staged candidate arrives (status pending)" "pending" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\")" 30 && f | head -1 | jq -r .status)"
check "a fact shows it planned" "true" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.planned == true" 120 && echo true)"
check "a fact shows a job attempt running" "running" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.attempts[] | select(.kind == \"job\" and .status == \"running\")" 120 && echo running)"
check "a fact shows job a passed with a log handle" "yes" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.attempts[] | select(.job == \"a\" and .status == \"passed\" and .log != null)" 300 && echo yes)"
check "a fact shows the verdict passed" "passed" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.status == \"passed\"" 300 && echo passed)"
check "a fact shows it landed" "landed" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.verdictReason == \"landed\"" 60 && echo landed)"
echo "-- $(( $(channel_facts "$STREAM" | wc -l) - N0 )) facts in $(( $(date +%s) - T0 )) s; the candidate's rows: $(f | jq -r '[.status, .planned, (.attempts | length)] | @csv' | uniq | tr '\n' ' ')"
check "every candidate fact is a full row (id, status, attempts), never a diff" "0" "$(f | jq -r 'select(.id == null or .status == null or (.attempts | type) != "array") | .id' | wc -l)"
check "the ship never sent a fact for another repository on this path" "0" "$(channel_facts "$STREAM" | jq -r "select(.kind == \"candidate\" and .patch.repo != \"$REPO\") | .id" | wc -l)"
check "landed: master = the candidate" "$OID" "$(repo_master)"
# the channel goes away (the browser's pip reads polling, fe/src/ciLive.js
# feedPip); the fallback read the Refresh button makes still answers
kill "$PID" 2>/dev/null; sleep 1
r=$(ci_get "/repository/$REPO/candidates")
check "Refresh's read still answers 200 with the landed candidate" "landed" "$(jq_of "$r" ".candidates[] | select(.id == \"$CID\") | .verdictReason")"
end_row R6
fi
[ "$NFAIL" = 0 ]
