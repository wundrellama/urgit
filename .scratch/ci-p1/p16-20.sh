#!/bin/bash
# Rows P16-P20 on the ci-p1 repository with daemon a polling:
#   P16 refs/ci/candidate/* absent from the repository API's ref list
#       while a candidate is open (advertised to git for the clone) and
#       gone after it closes.
#   P17 stale destination: X staged on master; the operator moves master
#       to Y (%set-ref) while X runs; X passes; landing refused; X stays
#       %passed unlanded with the reason; master = Y.
#   P18 a push with no credentials and one with a wrong write token are
#       refused by write-authorized before the gate; no candidate staged.
#   P19 desk-linked repositories: CI protection refused; a repo bound after
#       protection is refused at landing with the same reason.
#   P20 projection tripwire: b's stream has no jobID:a line; a ran once;
#       the ship's event count for b equals b's own lines.
source "$(dirname "$0")/lib.sh"
rows="${*:-p16 p17 p18 p19 p20}"
has() { case " $rows " in *" $1 "*) return 0;; *) return 1;; esac; }
cand_count() { dojo_value '~(wyt by .^((map @uv candidate:ci) %gx /=urgit-ci=/candidates/noun))' | one '^[0-9.]+$'; }

if has p16; then
row "P16: refs/ci/candidate/* hidden from the repository API while open; gone after the candidate closes"
sync_clone; set_workflows fixture-wait.yml
push_commit "sixteen: a candidate to watch"
sleep 3
check "scratch ref advertised to git while the candidate is open" "refs/ci/candidate/$CID" "$(ls_remote | grep -o "refs/ci/candidate/$CID" || true)"
check_not_contains "[%x %repository @ ~] ref list hides refs/ci/ (D9)" "refs/ci/" "$(peek_ref_names)"
echo "-- observation: the authenticated /api/repository/<name> list (repository-json, outside D9's filter): $(repo_ref_names)"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 300)"
sleep 3
check "scratch ref gone after the close" "" "$(ls_remote | grep -o "refs/ci/candidate/$CID" || true)"
check_not_contains "[%x %repository @ ~] ref list still clean" "refs/ci/" "$(peek_ref_names)"
end_row P16
fi

if has p17; then
row "P17: stale destination -> %land-candidate refused; X stays %passed unlanded; master = Y"
sync_clone; set_workflows fixture-wait.yml
BASE=$(repo_master)
echo "x $(date +%s)" > X.md
push_commit "seventeen: candidate X"
X_CID=$CID; X_OID=$OID
# Y: an unrelated commit on the same base, pushed to an unprotected ref so
# its object is in the store, then made master's tip by the operator
git reset -q --hard "$BASE"; echo "y $(date +%s)" > Y.md; git add -A; git commit -qm "seventeen: unrelated Y"
Y_OID=$(git rev-parse HEAD)
git push -q origin "HEAD:refs/heads/p17-y" 2>&1 | tail -1
Y_UX="0x$(printf '%s' "$Y_OID" | sed 's/^0*//' | rev | sed 's/\(....\)/\1./g' | rev | sed 's/^\.//')"
"$dojo" ":urgit &git-action [%set-ref '$REPO' 'refs/heads/master' $Y_UX]" 60 3 | tail -1
check "operator moved master to Y while X runs" "$Y_OID" "$(repo_master)"
check "X passed" '%passed' "$(wait_cand "$X_CID" '%passed' 300)"
sleep 4
check "X stays unlanded: master = Y" "$Y_OID" "$(repo_master)"
check "verdict-reason" "'destination moved; rebase and push again'" "$(cand_reason "$X_CID")"
check "X is still %passed" '%passed' "$(cand_status "$X_CID")"
git reset -q --hard "$Y_OID"
end_row P17
fi

if has p18; then
row "P18: a push with no credentials and one with a wrong write token are refused before the gate; nothing staged"
before=$(cand_count)
sync_clone; echo "eighteen" > EIGHTEEN.md; git add -A; git commit -qm "eighteen: unauthenticated"
RP="$URL/git/$REPO/git-receive-pack"
code=$(curl -s -o "$TMP/p18-anon.txt" -w '%{http_code}' -X POST -H 'content-type: application/x-git-receive-pack-request' --data-binary '0000' "$RP")
echo "-- POST git-receive-pack with no credentials: $code $(cat "$TMP/p18-anon.txt")"
check "no credentials -> 401" "401" "$code"
out=$(GIT_TERMINAL_PROMPT=0 git -c credential.helper= -c http.cookieFile=/dev/null push origin master 2>&1 | tail -1)
echo "-- git push with no credentials: $out"
check "git reports the refusal (a 401 it cannot answer)" "yes" "$(case "$out" in *"Authentication failed"*|*"could not read Username"*) echo yes;; *) echo "no: $out";; esac)"
"$api" POST "/repository/$REPO/token" '{"token":"right-token-p18"}' | cut -c1-60
code=$(curl -s -o "$TMP/p18-wrong.txt" -w '%{http_code}' -X POST -u "git:wrong-token" -H 'content-type: application/x-git-receive-pack-request' --data-binary '0000' "$RP")
echo "-- POST git-receive-pack with a wrong write token: $code $(cat "$TMP/p18-wrong.txt")"
check "wrong write token -> 401" "401" "$code"
out=$(GIT_TERMINAL_PROMPT=0 git -c credential.helper= -c http.cookieFile=/dev/null -c http.extraHeader="Authorization: Basic $(printf 'git:wrong-token' | base64)" push origin master 2>&1 | tail -1)
echo "-- git push with a wrong write token: $out"
check "git reports the refusal (a 401 it cannot answer)" "yes" "$(case "$out" in *"Authentication failed"*|*"could not read Username"*) echo yes;; *) echo "no: $out";; esac)"
check "no candidate staged" "$before" "$(cand_count)"
check "master unchanged" "$(git -C "$CLONE" rev-parse HEAD~1)" "$(repo_master)"
"$api" DELETE "/repository/$REPO/token" | cut -c1-40
git reset -q --hard HEAD~1
end_row P18
fi

if has p19; then
row "P19: desk-linked repositories cannot be CI-protected; a repo bound after protection is refused at landing"
# the scratch desk survives across runs; kiln asks before overwriting one
if [ "$(dojo_value '(~(has in .^((set desk) %cd /(scot %p our)//(scot %da now))) %scratch)' | one '^%\.[yn]$')" = "%.y" ]; then
  echo "-- desk %scratch already exists"
else
  "$dojo" '|new-desk %scratch' 120 3 | tail -1
fi
LINKED=ci-p1-linked
"$api" POST /repositories "{\"name\":\"$LINKED\",\"publicRead\":true}" | cut -c1-30
L="$TMP/clone-linked"; rm -rf "$L"; mkdir -p "$L"; cd "$L"; git init -q -b master .; git config user.name p19; git config user.email p19@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$LINKED"; echo seed > README.md; git add -A; git commit -qm seed; git push -q origin master 2>&1 | tail -1
"$dojo" ":urgit &git-action [%bind-desk '$LINKED' %scratch 'refs/heads/master']" 60 3 | tail -1
check "linked repo reports linked=%.y" "%.y" "$(dojo_value ".^((unit [tip=@ux linked=?]) %gx /=urgit=/ci-ref/(scot %t '$LINKED')/(scot %t 'refs/heads/master')/noun)" | tr -d '\n' | grep -oE '%\.[yn]' | tail -1)"
out=$("$dojo" ":urgit-ci &ci-action [%set-ci-protected '$LINKED' 'refs/heads/master' %.y]" 60 8)
check_contains "CI protection refused for the linked repo" "CI protection is not available for desk-linked repositories in this release" "$out"
check "linked repo not protected" '%.n' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$LINKED')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
echo "-- bind the CI-protected plain repo AFTER protection, then push"
"$dojo" ":urgit &git-action [%bind-desk '$REPO' %scratch 'refs/heads/master']" 60 3 | tail -1
sync_clone; set_workflows fixture-pass.yml
push_commit "nineteen: a candidate on a repo bound after protection"
check "candidate passes" '%passed' "$(wait_cand "$CID" '%passed' 300)"
sleep 4
check "landing refused with the same reason" "'CI protection is not available for desk-linked repositories in this release'" "$(cand_reason "$CID")"
check "master unchanged" "$(git -C "$CLONE" rev-parse HEAD~1)" "$(repo_master)"
"$dojo" ":urgit &git-action [%unbind-desk '$REPO']" 60 3 | tail -1
end_row P19
fi

if has p20; then
row "P20: projection tripwire — b's stream carries no jobID:a line; a ran once; the ship's event count for b equals b's own lines"
sync_clone; set_workflows fixture-chain.yml
push_commit "twenty: the chain under projection"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 300)"
A_ATT=$(att_of_job "$CID" a); B_ATT=$(att_of_job "$CID" b)
B_STREAM="$RUNNER_HOME/a/work/$B_ATT.act.jsonl"
check "b's attempt stream exists" "yes" "$([ -s "$B_STREAM" ] && echo yes || echo no)"
check "no jobID a line in b's stream" "0" "$(grep -c '"jobID":"a"' "$B_STREAM" || true)"
check "a ran once, in its own attempt" "1" "$(cand_attempts "$CID" | grep -c ' %job a ')"
b_lines=$(grep -c '"jobID":"b"' "$B_STREAM" || true)
check "ship's event count for b equals b's own lines" "$b_lines" "$(att_events "$B_ATT")"
# the ship's 409 on a's lines arriving on b's attempt is the projection
# tripwire (CI-PROJECT-1): the daemon logs the ship's refusal body
echo "-- first refusal the daemon logged for b: $(grep -F "[job $B_ATT] ship refused event" "$RUNNER_HOME/a/daemon.log" | head -1 | cut -c1-200)"
check "no refusal in the daemon log for b" "0" "$(grep -c "\[job $B_ATT\] ship refused event" "$RUNNER_HOME/a/daemon.log" || true)"
check "the projected file had one job and no needs/if" "yes" "$(grep -q "running: act push -W /work/projected/fixture-chain.yml -j b" "$RUNNER_HOME/a/daemon.log" && echo yes || echo no)"
end_row P20
fi
echo; echo "== rows: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# })"
[ "$NFAIL" = 0 ]
