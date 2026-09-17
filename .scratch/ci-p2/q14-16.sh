#!/bin/bash
# usage: q14-16.sh q14|q15|q16
# Rows Q14-Q16 (D6): the CI tab's routes and the settings actions, and the
# session fence. The frontend is exercised through the routes it calls
# (fe/src/api.js `ci`) plus `node --test` on its helpers; Q14 reads the
# same JSON the tab renders and renders the log the way the tab does
# (fe/src/ci.js renderLog) through node.
#   Q14  GET ci/repository/<repo>/candidates lists the repository's
#        candidates newest first with attempts; the ERPit candidate (from
#        Q18's run, or a fixture run when Q18 has not run yet) shows every
#        job's status and a log handle; GET ci/candidate/<id> has the rows;
#        the log route's jsonl renders through renderLog into [job] step:
#        msg lines with groups
#   Q15  POST ci/action round-trips: set-ci-protected on/off reads back
#        through the policy route; the untrusted radio reads back; the
#        credential form adds and deletes, the credentials route lists the
#        name without the value, and the built page source (desk/web) and
#        every settings read never contain the value
#   Q16  every ci/* read and POST ci/action without a session -> 401;
#        with a session -> 200
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env" 2>/dev/null
which_row="${1:-q14}"
TS=$(date +%H%M%S)
if [ "$which_row" = q14 ]; then
row "Q14: the CI tab's reads — candidates, the candidate page, the rendered log"
# the candidate the tab shows: ERPit's when Q18 ran, else a fixture-chain run here
if [ -s "$TMP/q18.env" ]; then source "$TMP/q18.env"; TAB_REPO=$Q18_REPO; TAB_CID=$Q18_CID; WANT_JOBS=8
else
  sync_clone; set_workflows fixture-chain.yml; printf 'q14 %s\n' "$(date -Is)" >> README.md
  push_commit "ci-p2 Q14: a chain for the CI tab"
  wait_cand "$CID" '%passed|%failed|%unknown' 300 >/dev/null; TAB_REPO=$REPO; TAB_CID=$CID; WANT_JOBS=2
fi
r=$(ci_get "/repository/$TAB_REPO/candidates")
check "GET candidates -> 200" "200" "$(status_of "$r")"
check "the list carries the candidate" "yes" "$(body_of "$r" | jq -r --arg id "$TAB_CID" '.candidates[] | select(.id == $id) | "yes"' | head -1)"
check "newest first" "yes" "$(body_of "$r" | jq -r '[.candidates[].created] | . == (. | sort | reverse) | if . then "yes" else "no" end')"
check "each candidate carries ref, head, actor, status, trust, created, attempts" "yes" "$(body_of "$r" | jq -r '.candidates | all(has("ref") and has("head") and has("actor") and has("status") and has("trust") and has("created") and has("attempts")) | if . then "yes" else "no" end')"
JOBS=$(body_of "$r" | jq -r --arg id "$TAB_CID" '.candidates[] | select(.id == $id) | [.attempts[] | select(.kind == "job")] | length')
check "the candidate lists its job attempts" "$WANT_JOBS" "$JOBS"
r=$(ci_get "/candidate/$TAB_CID")
check "GET candidate/<id> -> 200" "200" "$(status_of "$r")"
check "every job row has a status" "$WANT_JOBS" "$(body_of "$r" | jq -r '[.attempts[] | select(.kind == "job") | .status] | length')"
check "every job row passed" "$WANT_JOBS" "$(body_of "$r" | jq -r '[.attempts[] | select(.kind == "job") | select(.status == "passed")] | length')"
check "every job row has a log handle (a log link)" "$WANT_JOBS" "$(body_of "$r" | jq -r '[.attempts[] | select(.kind == "job") | select(.log != null)] | length')"
check "the candidate page names the landing" "landed" "$(body_of "$r" | jq -r '.candidate.verdictReason')"
ONE=$(body_of "$r" | jq -r '[.attempts[] | select(.kind == "job")][0].attempt')
rm -f "$TMP/q14-log.jsonl"; curl -s -L -o "$TMP/q14-log.jsonl" "$(ci_location "/attempt/$ONE/log" | cut -d' ' -f2-)"
rendered=$(cd "$ROOT/fe" && node --input-type=module -e "
import { renderLog, lineText } from './src/ci.js'
import { readFileSync } from 'node:fs'
const groups = renderLog(readFileSync(process.argv[1], 'utf8'))
const lines = groups.flatMap((g) => g.lines)
const event = lines.find((l) => l.job) || {}
console.log(JSON.stringify({ groups: groups.length, lines: lines.length, banner: lineText(lines[0] || {}), first: lineText(event), result: lines.filter((l) => l.result).length }))
" "$TMP/q14-log.jsonl")
echo "-- rendered: $rendered"
check "the log renders into lines" "yes" "$([ "$(printf '%s' "$rendered" | jq -r .lines)" -gt 5 ] && echo yes || echo no)"
check_contains "event lines read [job] step: msg" "] " "$(printf '%s' "$rendered" | jq -r .first)"
check_contains "act's non-event banner line is kept as text" "Using docker host" "$(printf '%s' "$rendered" | jq -r .banner)"
check "a result line is marked" "yes" "$([ "$(printf '%s' "$rendered" | jq -r .result)" -ge 1 ] && echo yes || echo no)"
end_row Q14
fi
if [ "$which_row" = q15 ]; then
row "Q15: the settings actions round-trip through POST ci/action"
VALUE="q15-hunter2-$TS-a1b2c3d4"
policy() { ci_get "/repository/$REPO/policy" | sed 's/^[0-9]* //'; }
sync_clone
git checkout -q -B "q15-branch-$TS" master; printf 'q15\n' >> README.md; git add -A; git commit -qm "q15 branch"; git push -q origin "q15-branch-$TS" 2>&1 | tail -1 >/dev/null; git checkout -q master
REF="refs/heads/q15-branch-$TS"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$REPO\",\"ref\":\"$REF\",\"protected\":true}")
check "CI required on -> 200" "200" "$(status_of "$r")"
check "policy lists the ref as CI-protected" "yes" "$(policy | jq -r --arg ref "$REF" '.ciProtected | index($ref) != null | if . then "yes" else "no" end')"
check "the ci-protected scry agrees" '%.y' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t '$REF')/noun)" | one '^%\.[yn]$')"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$REPO\",\"ref\":\"$REF\",\"protected\":false}")
check "CI required off -> 200" "200" "$(status_of "$r")"
check "policy no longer lists it" "no" "$(policy | jq -r --arg ref "$REF" '.ciProtected | index($ref) != null | if . then "yes" else "no" end')"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$REPO\",\"ref\":\"refs/heads/no-such-branch\",\"protected\":true}")
check "CI required on a ref with no tip -> 409 with the ruling's reason" "409" "$(status_of "$r")"
check_contains "the refusal reason" "ref has no tip" "$(body_of "$r")"
r=$(ci_action "{\"action\":\"set-untrusted-policy\",\"repo\":\"$REPO\",\"policy\":\"restricted\"}")
check "untrusted radio -> restricted -> 200" "200" "$(status_of "$r")"
check "policy reads restricted" "restricted" "$(policy | jq -r .untrusted)"
r=$(ci_action "{\"action\":\"set-untrusted-policy\",\"repo\":\"$REPO\",\"policy\":\"approval\"}")
check "policy reads approval again" "approval" "$(policy | jq -r .untrusted)"
r=$(ci_action "{\"action\":\"set-credential\",\"repo\":\"$REPO\",\"name\":\"Q15_TOKEN\",\"value\":\"$VALUE\",\"scope\":\"env\",\"envs\":[\"staging\"]}")
check "credential form add -> 200" "200" "$(status_of "$r")"
c=$(ci_get "/repository/$REPO/credentials")
check "credentials route lists the name with its scope and envs" "Q15_TOKEN env staging" "$(body_of "$c" | jq -r '.credentials[] | select(.name == "Q15_TOKEN") | "\(.name) \(.scope) \(.envs | join(","))"')"
check "credentials route never carries the value" "0" "$(printf '%s' "$c" | grep -c -F -- "$VALUE")"
check "policy route never carries the value" "0" "$(policy | grep -c -F -- "$VALUE")"
check "the repository JSON never carries the value" "0" "$("$api" GET "/repository/$REPO" | grep -c -F -- "$VALUE")"
check "the built page source never carries the value" "0" "$(grep -r -c -F -- "$VALUE" "$ROOT/desk/web" 2>/dev/null | awk -F: '{s+=$2} END{print s+0}')"
check "the served app never carries the value" "0" "$(curl -s "$URL/apps/urgit/" | grep -c -F -- "$VALUE")"
r=$(ci_action "{\"action\":\"set-credential\",\"repo\":\"$REPO\",\"name\":\"Q15_SHORT\",\"value\":\"abc\",\"scope\":\"job\",\"envs\":[]}")
check "a short value -> 409" "409" "$(status_of "$r")"
r=$(ci_action "{\"action\":\"delete-credential\",\"repo\":\"$REPO\",\"name\":\"Q15_TOKEN\"}")
check "credential delete -> 200" "200" "$(status_of "$r")"
check "credentials route no longer lists it" "no" "$(ci_get "/repository/$REPO/credentials" | sed 's/^[0-9]* //' | jq -r '[.credentials[] | select(.name == "Q15_TOKEN")] | length | if . == 0 then "no" else "yes" end')"
r=$(ci_action '{"action":"approve-candidate","id":"0v0"}')
check "an action the ship refuses answers 409 with the reason" "409" "$(status_of "$r")"
check_contains "the reason" "no such candidate" "$(body_of "$r")"
r=$(ci_action '{"action":"nope"}')
check "an unknown action -> 400" "400" "$(status_of "$r")"
end_row Q15
fi
if [ "$which_row" = q16 ]; then
row "Q16: the session fence on every ci/* read and on POST ci/action"
ANY=$(cand_attempt_ids "$(ci_get "/repository/$REPO/candidates" | sed 's/^[0-9]* //' | jq -r '.candidates[0].id')" 2>/dev/null | head -1)
CANY=$(ci_get "/repository/$REPO/candidates" | sed 's/^[0-9]* //' | jq -r '.candidates[0].id')
for path in "/repository/$REPO/candidates" "/candidate/$CANY" "/repository/$REPO/policy" "/repository/$REPO/credentials" "/key" "/attempt/$ANY/log"; do
  check "GET ci$path without a session -> 401" "401" "$(status_of "$(ci_get "$path" -)")"
done
for path in "/repository/$REPO/candidates" "/candidate/$CANY" "/repository/$REPO/policy" "/repository/$REPO/credentials" "/key"; do
  check "GET ci$path with a session -> 200" "200" "$(status_of "$(ci_get "$path")")"
done
check "GET ci/attempt/<id>/log with a session -> 302 or 404 (a log or none), never 401" "yes" "$(case "$(status_of "$(ci_get "/attempt/$ANY/log")")" in 302|404) echo yes;; *) echo no;; esac)"
check "POST ci/action without a session -> 401" "401" "$(status_of "$("$api" POST /ci/action "{\"action\":\"set-untrusted-policy\",\"repo\":\"$REPO\",\"policy\":\"approval\"}" -)")"
check "POST ci/action with a session -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-untrusted-policy\",\"repo\":\"$REPO\",\"policy\":\"approval\"}")")"
BEARER_A=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["bearer"])' "$RUNNER_HOME/a/state.json")
check "a daemon bearer is not a session: GET candidates with the bearer -> 401" "401" "$(status_of "$(ci_get "/repository/$REPO/candidates" "$BEARER_A")")"
end_row Q16
fi
[ "$NFAIL" = 0 ]
