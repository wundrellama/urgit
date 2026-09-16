#!/bin/bash
# usage: q5-8.sh q5a|q5|q6|q7|q8
# Rows Q5a-Q8 (D3/D3a): the web merge gate, untrusted staging, the
# restricted policy, approval, and the non-writer refusal. Needs p2-setup
# (daemon a polling, master CI-protected). Q5 stages the untrusted
# candidate that Q6 runs and Q7 approves; Q8 stages its own.
#   Q5a  a writer's PR to the CI-protected master, merged through the web:
#        202 + candidate id, master unmoved, candidate %trusted with the
#        pull number; it runs and lands; the pull reads merged only then.
#        a PR to an unprotected branch still merges directly (200).
#   Q5   a revision from a non-writer (the stage poke with a foreign actor
#        and the %untrusted class %urgit's can-write decides; see the
#        record's Deviations for why the harness pokes rather than opens
#        a fork PR): %untrusted %pending, plan=~, zero attempts, and the
#        daemon is offered nothing for it
#   Q6   %set-untrusted-policy %restricted: the same candidate is planned
#        and run with trust=%untrusted on every attempt, its log lands
#        under /untrusted/, it reaches %passed and cannot land: the
#        verdict reason names trust and master is unmoved
#   Q7   %approve-candidate by the writer: a new %trusted candidate of the
#        same head and base, the old one %skipped 'superseded by
#        approval'; the new one runs and lands
#   Q8   %approve-candidate by a non-writer: refused; the candidate stays
#        %untrusted and no trusted twin appears
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env" 2>/dev/null
which_row="${1:-q5a}"
TS=$(date +%H%M%S)   # branch names are unique per run: a reused name would refuse the push
# ---- pull-request drivers ------------------------------------------------------------
open_pr() {  # <source-ref> <target-ref> <title> -> number
  "$api" POST "/repository/$REPO/pulls" "{\"title\":\"$3\",\"sourceBranch\":\"$1\",\"targetBranch\":\"$2\"}" | sed 's/^[0-9]* //' | jq -r .number
}
merge_pr() { "$api" POST "/repository/$REPO/pulls/$1/merge" '{}'; }   # -> "<status> <body>"
pull_state() { "$api" GET "/repository/$REPO" | sed 's/^[0-9]* //' | jq -r ".pullRequests[] | select(.number == $1) | .state"; }
ref_oid() { "$api" GET "/repository/$REPO" | sed 's/^[0-9]* //' | jq -r ".refs[] | select(.name == \"$1\") | .oid"; }
# push_branch <name> <message> [workflow]: a new branch from the clone's
# master with one commit (and, when named, exactly that workflow file);
# sets BOID. lands directly (only master is CI-protected)
push_branch() {
  in_clone || return 1
  git checkout -q -B "$1" master
  [ -n "${3:-}" ] && set_workflows "$3"
  printf '%s %s\n' "$1" "$(date -Is)" >> README.md
  git add -A && git commit -qm "$2"
  BOID=$(git rev-parse HEAD)
  git push -q origin "$1" 2>&1 | tail -1
  git checkout -q master
}
# stage_untrusted <head> <base>: the stage poke as %urgit sends it for a
# fork pull request whose author cannot write the repository
# a 40-hex oid is not a dojo literal (@ux wants dots every four digits);
# it is parsed from its text with hex
stage_untrusted() {
  "$dojo" ":urgit-ci &ci-action [%stage-candidate '$REPO' 'refs/heads/master' (rash '$1' hex) (rash '$2' hex) ~sampel-palnet %session %untrusted ~]" 60 3 | tail -1 >/dev/null
  dojo_value "(scot %uv (sham ['$REPO' 'refs/heads/master' \`@ux\`(rash '$1' hex) \`@ux\`(rash '$2' hex) %untrusted]))" | one '0v[0-9a-v.]+'
}
trusted_id() {  # <head> <base>
  dojo_value "(scot %uv (sham ['$REPO' 'refs/heads/master' \`@ux\`(rash '$1' hex) \`@ux\`(rash '$2' hex)]))" | one '0v[0-9a-v.]+'
}
# ---- Q5a ---------------------------------------------------------------------------------
if [ "$which_row" = q5a ]; then
row "Q5a: the web merge of a PR to the CI-protected master stages a trusted candidate and lands it"
sync_clone
BEFORE=$(repo_master)
# the feature branch carries the workflow the candidate runs (a push to
# master here would itself be staged and would move master under the PR)
push_branch "q5a-feature-$TS" "ci-p2 Q5a: a feature for the pull request" fixture-pass.yml
FEATURE=$BOID
check "the feature branch landed directly (unprotected)" "$FEATURE" "$(ref_oid "refs/heads/q5a-feature-$TS")"
N=$(open_pr "refs/heads/q5a-feature-$TS" refs/heads/master "Q5a feature")
check "pull request opened" "yes" "${N:+yes}"
r=$(merge_pr "$N")
echo "-- Merge -> $(printf '%s' "$r" | cut -c1-200)"
check "merge of a PR to the CI-protected branch -> 202" "202" "$(status_of "$r")"
CID=$(jq_of "$r" .candidate)
check "the answer names a candidate" "yes" "${CID:+yes}"
check "the answer names the trust class" "trusted" "$(jq_of "$r" .trust)"
check "master unmoved after Merge" "$BEFORE" "$(repo_master)"
check "the pull is still open" "open" "$(pull_state "$N")"
check "candidate is %trusted" '%trusted' "$(cand_trust "$CID")"
check "candidate carries the pull number" "$N" "$(cand_pull "$CID")"
check "candidate actor is the pull's source ship" "~$SHIP" "$(dojo_value "actor:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID/noun))" | one '^~[a-z-]+$')"
if [ -n "$CID" ]; then   # a merge that wrote instead of staging (the mutant) has nothing to wait for
st=$(wait_cand "$CID" '%passed|%failed|%unknown' 300)
check "candidate %passed" '%passed' "$st"
sleep 3
check "landed: master = the candidate object" "$(cand_object_hex "$CID")" "$(repo_master)"
check "the pull reads merged after landing" "merged" "$(pull_state "$N")"
check "verdict-reason" "'landed'" "$(cand_reason "$CID")"
fi
echo "-- an unprotected target keeps the direct write"
sync_clone
push_branch "q5a-side-$TS" "ci-p2 Q5a: an unprotected side branch"
git checkout -q -B "q5a-side2-$TS" "q5a-side-$TS"; printf 'side2\n' >> README.md; git add -A; git commit -qm "side2"; git push -q origin "q5a-side2-$TS" 2>&1 | tail -1 >/dev/null; git checkout -q master
M=$(open_pr "refs/heads/q5a-side2-$TS" "refs/heads/q5a-side-$TS" "Q5a side")
r=$(merge_pr "$M")
check "merge of a PR to an unprotected branch -> 200" "200" "$(status_of "$r")"
check "the unprotected branch moved to the merge commit" "$(jq_of "$r" .commit)" "$(ref_oid "refs/heads/q5a-side-$TS")"
check "that pull reads merged at once" "merged" "$(pull_state "$M")"
end_row Q5a
fi
# ---- Q5 ------------------------------------------------------------------------------------
if [ "$which_row" = q5 ]; then
row "Q5: a non-writer's revision stages %untrusted and waits: no plan, no attempts, no work offered"
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %approval]" 60 3 | tail -1 >/dev/null
check "policy is %approval" '%approval' "$(dojo_value ".^(untrusted-policy:ci %gx /=urgit-ci=/policy/(scot %t '$REPO')/noun)" | one '^%[a-z]+$')"
sync_clone
BASE=$(repo_master)
push_branch "q5-contrib-$TS" "ci-p2 Q5: a contributor's revision" fixture-pass.yml
HEAD=$BOID
CID=$(stage_untrusted "$HEAD" "$BASE")
echo "-- staged $CID (head $HEAD onto $BASE, actor ~sampel-palnet)"
sleep 20
check "candidate is %untrusted" '%untrusted' "$(cand_trust "$CID")"
check "candidate is %pending" '%pending' "$(cand_status "$CID")"
check "candidate was materialized (the object exists)" "yes" "$(case "$(cand_object "$CID")" in "[~ "*) echo yes;; *) echo no;; esac)"
check "plan=~" "~" "$(dojo_value "plan:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID/noun))" | one '^~$')"
check "zero attempts" "0" "$(cand_attempt_ids "$CID" | wc -l)"
check "the daemon was offered nothing for it" "0" "$(grep -c "candidate $CID" "$RUNNER_HOME/a/daemon.log")"
check "master unmoved" "$BASE" "$(repo_master)"
echo "export Q5_CID=$CID; export Q5_HEAD=$HEAD; export Q5_BASE=$BASE" > "$TMP/q5.env"
end_row Q5
fi
source "$TMP/q5.env" 2>/dev/null
# ---- Q6 ------------------------------------------------------------------------------------
if [ "$which_row" = q6 ]; then
row "Q6: %restricted policy — the untrusted candidate runs untrusted, passes, and cannot land"
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %restricted]" 60 3 | tail -1 >/dev/null
st=$(wait_cand "$Q5_CID" '%passed|%failed|%unknown' 300)
check "candidate %passed" '%passed' "$st"
n_att=0; n_untrusted=0
for a in $(cand_attempt_ids "$Q5_CID"); do n_att=$((n_att+1)); [ "$(att_trust "$a")" = '%untrusted' ] && n_untrusted=$((n_untrusted+1)); done
echo "-- $n_att attempts, $n_untrusted %untrusted"
check "every attempt is %untrusted" "$n_att" "$n_untrusted"
check "at least the plan and one job ran" "yes" "$([ "$n_att" -ge 2 ] && echo yes || echo no)"
JOB=$(att_of_job "$Q5_CID" pass)
check_contains "the job's log key is under /untrusted/" "/untrusted/log.jsonl" "$(att_log_key "$JOB")"
check_contains "the daemon saw trust untrusted on the assignment" "trust untrusted" "$(grep "$JOB" "$RUNNER_HOME/a/daemon.log" | grep -o 'trust [a-z]*' | head -1)"
check_contains "verdict-reason names trust" "untrusted candidate cannot land" "$(cand_reason "$Q5_CID")"
sleep 3
check "master unmoved (restricted check cannot land)" "$Q5_BASE" "$(repo_master)"
check "eligibility scry answers %.n for the untrusted object" '%.n' "$(dojo_value ".^(? %gx /=urgit-ci=/eligible/(scot %t '$REPO')/(scot %t 'refs/heads/master')/(scot %t '$(cand_object_hex "$Q5_CID")')/noun)" | one '^%\.[yn]$')"
end_row Q6
fi
# ---- Q7 ------------------------------------------------------------------------------------
if [ "$which_row" = q7 ]; then
row "Q7: approval by the writer — a trusted twin runs and lands; the untrusted one is superseded"
TWIN=$(trusted_id "$Q5_HEAD" "$Q5_BASE")
"$dojo" ":urgit-ci &ci-action [%approve-candidate $Q5_CID ~$SHIP]" 60 3 | tail -1 >/dev/null
sleep 2
check "old candidate is %skipped" '%skipped' "$(cand_status "$Q5_CID")"
check "old verdict-reason" "'superseded by approval'" "$(cand_reason "$Q5_CID")"
check "a trusted twin exists" '%trusted' "$(cand_trust "$TWIN")"
check "twin has the same head" "$Q5_HEAD" "$(cand_head_hex "$TWIN")"
st=$(wait_cand "$TWIN" '%passed|%failed|%unknown' 300)
check "twin %passed" '%passed' "$st"
sleep 3
check "landed: master = the twin's object" "$(cand_object_hex "$TWIN")" "$(repo_master)"
check "twin verdict-reason" "'landed'" "$(cand_reason "$TWIN")"
check "old candidate still %skipped after the twin's attempts closed" '%skipped' "$(cand_status "$Q5_CID")"
end_row Q7
fi
# ---- Q8 ------------------------------------------------------------------------------------
if [ "$which_row" = q8 ]; then
row "Q8: approval by a non-writer is refused"
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %approval]" 60 3 | tail -1 >/dev/null
sync_clone
BASE=$(repo_master)
push_branch "q8-contrib-$TS" "ci-p2 Q8: another contributor's revision" fixture-pass.yml
CID=$(stage_untrusted "$BOID" "$BASE")
sleep 3
check "staged %untrusted" '%untrusted' "$(cand_trust "$CID")"
out=$("$dojo" ":urgit-ci &ci-action [%approve-candidate $CID ~sampel-palnet]" 60 12 | awk 1)
if printf '%s' "$out" | grep -q 'only a writer can approve a candidate'; then answer="refused: only a writer can approve a candidate"; else answer="accepted (>=)"; fi
check "approval by ~sampel-palnet refused" "refused: only a writer can approve a candidate" "$answer"
check "candidate still %untrusted %pending" '%untrusted %pending' "$(cand_trust "$CID") $(cand_status "$CID")"
check "no trusted twin was staged" "~" "$(dojo_value ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$(trusted_id "$BOID" "$BASE")/noun)" | tr -d '\n' | cut -c1-1)"
end_row Q8
fi
[ "$NFAIL" = 0 ]
