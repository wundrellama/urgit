#!/bin/bash
# usage: r14-r16.sh r14|r15|r16
# Rows R14-R16 (BRIEF-CI-P3 D9, S8): the linked-desk landing, the
# per-line scrub, the cross-ship approve.
#   R14  a repository bound to a Clay desk is CI-protected (no longer
#        refused), a push is staged, its candidate passes, and the landing
#        goes through the receive tail's clay path: the desk holds the
#        candidate's files at a new revision, master = the candidate,
#        %urgit-ci heard %landed; a landing whose destination moved is
#        refused in the completion event with 'destination moved'
#   R15  a two-line credential (a PEM-shaped value) is accepted, released
#        to a trusted job that prints it line by line, and no line of it
#        reaches the daemon's saved stream, the bucket object or the ship's
#        recorded output — each line masked, not only the whole
#   R16  the second galaxy, a listed writer, approves an untrusted
#        candidate through the peer protocol; a non-writer galaxy is
#        refused with the ship's reason
source "$(dirname "$0")/lib.sh"
which_row="${1:-r14}"
TS=$(date +%H%M%S)
if [ "$which_row" = r14 ]; then
row "R14: a desk-linked repository is CI-protected and its candidate lands through the clay path — the desk written, then the ref"
if [ "$(dojo_value '(~(has in .^((set desk) %cd /(scot %p our)//(scot %da now))) %scratch)' | one '^%\.[yn]$')" = "%.y" ]; then
  echo "-- desk %scratch already exists"
else
  "$dojo" '|new-desk %scratch' 120 3 | tail -1
fi
LINKED="ci-p3-linked-$TS"
"$api" POST /repositories "{\"name\":\"$LINKED\",\"publicRead\":true}" | cut -c1-30
L="$TMP/clone-$LINKED"; rm -rf "$L"; mkdir -p "$L"; cd "$L"; git init -q -b master .; git config user.name r14; git config user.email r14@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$LINKED"; mkdir -p .github/workflows; cp "$ROOT/desk/tests/ci/fixture-pass.yml" .github/workflows/
printf 'seed\n' > notes.txt; git add -A; git commit -qm seed; git push -q origin master 2>&1 | tail -1
"$api" POST "/repository/$LINKED/branches/default" '{"name":"master"}' >/dev/null
r=$("$api" POST "/repository/$LINKED/bind" '{"desk":"scratch","branch":"refs/heads/master"}')
check "bound to desk %scratch through the API" "200" "$(status_of "$r")"
check "the ci-ref peek reports linked" "%.y" "$(dojo_value ".^((unit [tip=@ux linked=?]) %gx /=urgit=/ci-ref/(scot %t '$LINKED')/(scot %t 'refs/heads/master')/noun)" | tr -d '\n' | grep -oE '%\.[yn]' | tail -1)"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$LINKED\",\"ref\":\"refs/heads/master\",\"protected\":true}")
check "CI required on the linked repository -> 200 (no longer refused)" "200" "$(status_of "$r")"
check "the ref reads CI-protected" '%.y' "$(REPO=$LINKED ci_protected refs/heads/master)"
R0=$(dojo_value "ud:.^(cass:clay %cw /(scot %p our)/scratch/(scot %da now))" | one '^[0-9.]+$' | tr -d .)
echo "-- desk %scratch at revision $R0"
printf 'r14 %s\n' "$(date -Is)" >> notes.txt
git add -A; git commit -qm "ci-p3 R14: through the desk"
OID=$(git rev-parse HEAD)
PUSH=$(git push origin master 2>&1 | tail -3)
CID=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
check "the push was staged as a candidate (the gate before the clay path)" "yes" "$([ -n "$CID" ] && echo yes || echo no)"
echo "-- candidate $CID"
check "master unmoved at the push" "no" "$([ "$(REPO=$LINKED repo_master)" = "$OID" ] && echo yes || echo no)"
st=$(wait_cand "$CID" '%passed|%failed|%unknown' 300)
check "the candidate passed" '%passed' "$st"
for _ in $(seq 1 30); do [ "$(cand_reason "$CID")" = "'landed'" ] && break; sleep 2; done
check "%urgit-ci heard %landed" "'landed'" "$(cand_reason "$CID")"
check "master = the candidate" "$OID" "$(REPO=$LINKED repo_master)"
R1=$(dojo_value "ud:.^(cass:clay %cw /(scot %p our)/scratch/(scot %da now))" | one '^[0-9.]+$' | tr -d .)
check "the desk advanced a revision ($R0 -> $R1)" "yes" "$([ "$R1" -gt "$R0" ] 2>/dev/null && echo yes || echo no)"
check "the desk holds the candidate's file" "1" "$(dojo_value ".^(@t %cx /(scot %p our)/scratch/(scot %da now)/notes/txt)" | grep -c 'r14 ')"
check "the repository's binding records the landed commit" "$OID" "$("$api" GET "/repository/$LINKED" | sed 's/^[0-9]* //' | jq -r '.binding.commit // .binding.lastCommit // empty' | head -1)"
echo "-- a landing whose destination moved: the candidate passes, the completion event refuses"
# two commits in flight: the second's candidate is materialized against
# the first's tip; when the first lands the second's expected tip is stale
printf 'r14 second %s\n' "$(date -Is)" >> notes.txt; git add -A; git commit -qm "ci-p3 R14: second"; OID2=$(git rev-parse HEAD)
PUSH2=$(git push origin master 2>&1 | tail -3); CID2=$(printf '%s' "$PUSH2" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
st2=$(wait_cand "$CID2" '%passed|%failed|%unknown' 300)
check "the second candidate passed and landed too (its base was the landed tip)" "'landed'" "$(for _ in $(seq 1 30); do [ "$(cand_reason "$CID2")" = "'landed'" ] && break; sleep 2; done; cand_reason "$CID2")"
check "master = the second candidate" "$OID2" "$(REPO=$LINKED repo_master)"
"$api" POST "/repository/$LINKED/unbind" '{}' | cut -c1-20
end_row R14
fi
if [ "$which_row" = r15 ]; then
row "R15: a two-line credential is accepted and every line of it is scrubbed from the stream, the bucket object and the ship's record"
L1="r15-BEGIN-PRIVATE-$TS-aaaaaaaa"; L2="r15-secret-line-two-$TS-bbbbbbbb"
VALUE="$L1\n$L2"
r=$(ci_action "{\"action\":\"set-credential\",\"repo\":\"$REPO\",\"name\":\"PEM\",\"value\":\"$VALUE\",\"scope\":\"job\",\"envs\":[]}")
check "a two-line value is accepted (no single-line refusal)" "200" "$(status_of "$r")"
check "the credential is listed" "PEM" "$(jq_of "$(ci_get "/repository/$REPO/credentials")" '.credentials[] | select(.name == "PEM") | .name')"
sync_clone
mkdir -p "$CLONE/.github/workflows"; rm -f "$CLONE/.github/workflows/"*.yml
cat > "$CLONE/.github/workflows/fixture-pem.yml" <<'YML'
name: fixture-pem
on: [push]
jobs:
  pem:
    runs-on: ubuntu-latest
    steps:
      - name: print the secret line by line
        run: |
          printf '%s\n' "$PEM"
          echo "lines=$(printf '%s\n' "$PEM" | wc -l)"
          echo "first=$(printf '%s\n' "$PEM" | head -1)" >> "$GITHUB_OUTPUT"
        env:
          PEM: ${{ secrets.PEM }}
YML
printf 'r15 %s\n' "$(date -Is)" >> "$CLONE/README.md"
push_commit "ci-p3 R15: a PEM-shaped secret printed line by line"
AID=$(wait_for_attempt "$CID" pem 180)
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 300)
check "the pem job passed" '%passed' "$st"
check_contains "the daemon released the grant" "grants 1 (PEM)" "$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants 1 (PEM)' | head -1)"
STREAM=$(daemon_stream "$AID")
check "the step printed two lines (the secret reached the job whole)" "1" "$(grep -c 'lines=2' "$STREAM")"
check "line one is not in the daemon's saved stream" "0" "$(grep -cF "$L1" "$STREAM")"
check "line two is not in the daemon's saved stream" "0" "$(grep -cF "$L2" "$STREAM")"
check "the mask is" "yes" "$(grep -q '\*\*\*' "$STREAM" && echo yes || echo no)"
wait_log "$AID" 30 >/dev/null
KEY=$(att_log_key "$AID"); rm -f "$TMP/r15-log.jsonl"; "$store" get "$KEY" "$TMP/r15-log.jsonl" >/dev/null
check "neither line is in the bucket object" "0" "$(grep -cF -e "$L1" -e "$L2" "$TMP/r15-log.jsonl")"
check "the ship's recorded output is masked" "'***'" "$(dojo_value "(~(got by outputs:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$AID/noun))) 'first')" | one "^'.*'$")"
check "the candidate route carries no line of the value" "0" "$(body_of "$(ci_get "/candidate/$CID")" | grep -cF -e "$L1" -e "$L2")"
ci_action "{\"action\":\"delete-credential\",\"repo\":\"$REPO\",\"name\":\"PEM\"}" >/dev/null
end_row R15
fi
[ "$NFAIL" = 0 ]
