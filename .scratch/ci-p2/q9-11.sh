#!/bin/bash
# usage: q9-11.sh q9|q10|q11
# Rows Q9-Q11 (D4): credentials released as grants to trusted jobs only,
# masked in the log by act and scrubbed by the daemon and the ship, and
# never present in any read. Needs p2-setup (daemon a polling, master
# CI-protected, the store fixture configured).
#   Q9   %set-credential TOKEN (scope %job); a trusted push of
#        fixture-secret (a step echoes $TOKEN and writes it to an output):
#        the job passes; the daemon's assignment line names the grant; the
#        log in the bucket and the daemon's saved stream carry `token is
#        ***` and the value's length, never the value; the daemon log
#        never carries the value (the --secret argument is redacted)
#   Q10  an untrusted candidate under %restricted runs the same workflow
#        with grants=~: the assignment line says 0 grants and the step
#        sees an empty $TOKEN (length 0)
#   Q11  the value is in no read: the attempt's outputs on the ship hold
#        the scrubbed leak (***), the candidate/attempt/assignment scries,
#        the credential-names scry, the attempt JSON route and the log
#        route's object are all grep -c 0; %delete-credential removes it
#        and a rerun's assignment line says 0 grants of 0 offered
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env" 2>/dev/null
which_row="${1:-q9}"
TS=$(date +%H%M%S)
VALUE="q9-hunter2-${TS}-a1b2c3d4"   # 8+ characters, unique per run
cred_names() { dojo_value ".^((list [name=@t scope=?(%job %env) envs=(set @t) created=@da]) %gx /=urgit-ci=/credential-names/(scot %t '$REPO')/noun)" 120 | tr -d '\n'; }
count_in() { local n; n=$(grep -c -F -- "$VALUE" "$1" 2>/dev/null); echo "${n:-0}"; }
if [ "$which_row" = q9 ]; then
row "Q9: a %job credential reaches a trusted job as a grant and is masked everywhere it could print"
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %approval]" 60 3 | tail -1 >/dev/null
"$dojo" ":urgit-ci &ci-action [%set-credential '$REPO' 'TOKEN' '$VALUE' %job ~]" 60 3 | tail -1 >/dev/null
check_contains "credential-names lists TOKEN with scope %job" "name='TOKEN'" "$(cred_names)"
short=$("$dojo" ":urgit-ci &ci-action [%set-credential '$REPO' 'SHORT' 'abc' %job ~]" 60 10 | awk 1 | grep -o 'at least 8 characters' | head -1)
check "a 3-character value is refused" "at least 8 characters" "$short"
nl=$("$dojo" ":urgit-ci &ci-action [%set-credential '$REPO' 'MULTI' 'line-one-here\\0aline-two-here' %job ~]" 60 10 | awk 1 | grep -o 'must be a single line' | head -1)
check "a value with a newline is refused (act 0.2.89 does not mask a multi-line secret)" "must be a single line" "$nl"
check "neither refused credential was stored" "no" "$(cred_names | grep -qE "name='(SHORT|MULTI)'" && echo yes || echo no)"
sync_clone
set_workflows fixture-secret.yml
printf 'q9 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q9: a job that uses the credential"
AID=$(wait_for_attempt "$CID" secret 180)
check "a job attempt for secret exists" "yes" "${AID:+yes}"
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "attempt %passed" '%passed' "$st"
line=$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants [0-9]* ([^)]*) of [0-9]* offered' | head -1)
echo "-- daemon: $line"
check "the assignment carried the grant" "grants 1 (TOKEN) of 1 offered" "$line"
check "the daemon log never carries the value" "0" "$(count_in "$RUNNER_HOME/a/daemon.log")"
check_contains "the --secret argument is redacted in the daemon log" "--secret TOKEN=***" "$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o -- '--secret TOKEN=[^ ]*' | head -1)"
LOG=$(wait_log "$AID" 30); KEY=$(att_log_key "$AID")
rm -f "$TMP/q9-log.jsonl"; "$store" get "$KEY" "$TMP/q9-log.jsonl" >/dev/null
check "the bucket log carries the masked line" "1" "$(grep -c 'token is \*\*\*' "$TMP/q9-log.jsonl")"
check "the bucket log carries the value's length (the secret was present)" "1" "$(grep -c "token length ${#VALUE}" "$TMP/q9-log.jsonl")"
check "the bucket log never carries the value" "0" "$(count_in "$TMP/q9-log.jsonl")"
check "the daemon's saved stream never carries the value" "0" "$(count_in "$(daemon_stream "$AID")")"
check "the saved stream equals the bucket object" "$(sha_of "$(daemon_stream "$AID")")" "$(sha_of "$TMP/q9-log.jsonl")"
check "the step's set-output of the secret is recorded as *** on the ship" "'***'" "$(dojo_value "(~(got by outputs:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$AID/noun))) 'leak')" | one "'[^']*'")"
check "the daemon's jsonl set-output line has no raw copy (arg scrubbed)" "0" "$(grep '"command":"set-output"' "$(daemon_stream "$AID")" | grep -c -F -- "$VALUE")"
echo "export Q9_CID=$CID; export Q9_AID=$AID; export Q9_VALUE=$VALUE; export Q9_OID=$OID" > "$TMP/q9.env"
end_row Q9
fi
source "$TMP/q9.env" 2>/dev/null; VALUE="${Q9_VALUE:-$VALUE}"
if [ "$which_row" = q10 ]; then
row "Q10: an untrusted attempt gets grants=~ even with the credential set"
# the row stores its own credential (a fresh repository in the mutant
# phases has none), so the withheld grant is the class's doing
"$dojo" ":urgit-ci &ci-action [%set-credential '$REPO' 'TOKEN' '$VALUE' %job ~]" 60 3 | tail -1 >/dev/null
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %restricted]" 60 3 | tail -1 >/dev/null
sync_clone
BASE=$(repo_master)
git checkout -q -B "q10-contrib-$TS" master; set_workflows fixture-secret.yml; printf 'q10 %s\n' "$(date -Is)" >> README.md
git add -A && git commit -qm "ci-p2 Q10: a contributor's revision using the secret"; HEAD=$(git rev-parse HEAD)
git push -q origin "q10-contrib-$TS" 2>&1 | tail -1 >/dev/null; git checkout -q master
"$dojo" ":urgit-ci &ci-action [%stage-candidate '$REPO' 'refs/heads/master' (rash '$HEAD' hex) (rash '$BASE' hex) ~sampel-palnet %session %untrusted ~]" 60 3 | tail -1 >/dev/null
CID=$(dojo_value "(scot %uv (sham ['$REPO' 'refs/heads/master' \`@ux\`(rash '$HEAD' hex) \`@ux\`(rash '$BASE' hex) %untrusted]))" | one '0v[0-9a-v.]+')
check "staged %untrusted" '%untrusted' "$(cand_trust "$CID")"
AID=$(wait_for_attempt "$CID" secret 180)
check "the restricted job ran" "yes" "${AID:+yes}"
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "attempt %passed" '%passed' "$st"
check "attempt is %untrusted" '%untrusted' "$(att_trust "$AID")"
line=$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants [0-9]* ([^)]*) of [0-9]* offered' | head -1)
echo "-- daemon: $line"
check "grants=~ on the untrusted assignment" "grants 0 () of 0 offered" "$line"
check "the step saw an empty TOKEN" "1" "$(grep -c 'token length 0' "$(daemon_stream "$AID")")"
check "the untrusted stream never carries the value" "0" "$(count_in "$(daemon_stream "$AID")")"
check "the credential is still stored (the class, not the store, withheld it)" "yes" "$(cred_names | grep -q "name='TOKEN'" && echo yes || echo no)"
"$dojo" ":urgit-ci &ci-action [%set-untrusted-policy '$REPO' %approval]" 60 3 | tail -1 >/dev/null
end_row Q10
fi
if [ "$which_row" = q11 ]; then
row "Q11: the value is in no read; deletion empties the next grant list"
AID=$Q9_AID; CID=$Q9_CID
check "the attempt's recorded output is the scrubbed leak" "'***'" "$(dojo_value "(~(got by outputs:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$AID/noun))) 'leak')" | one "'[^']*'")"
n=0
for expr in \
  ".^((unit attempt:ci) %gx /=urgit-ci=/attempt/$AID/noun)" \
  ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID/noun)" \
  ".^((map @uv attempt:ci) %gx /=urgit-ci=/attempts/noun)" \
  ".^((map @uv candidate:ci) %gx /=urgit-ci=/candidates/noun)" \
  ".^((map @uv assignment:ci) %gx /=urgit-ci=/assignments/noun)" \
  ".^((map @uv daemon:ci) %gx /=urgit-ci=/daemons/noun)" \
  ".^((list [name=@t scope=?(%job %env) envs=(set @t) created=@da]) %gx /=urgit-ci=/credential-names/(scot %t '$REPO')/noun)"; do
  hits=$(dojo_value "$expr" 120 | grep -c -F -- "$VALUE"); n=$((n+hits))
done
check "every scry dump: grep -c value = 0 (7 scries)" "0" "$n"
r=$(ci_get "/attempt/$AID")
check "GET ci/attempt/<id> (session): grep -c value = 0" "0" "$(printf '%s' "$r" | grep -c -F -- "$VALUE")"
check_contains "that attempt JSON shows the scrubbed output count" '"events":' "$(body_of "$r")"
rm -f "$TMP/q11-log.jsonl"; curl -s -L -o "$TMP/q11-log.jsonl" "$(ci_location "/attempt/$AID/log" | cut -d' ' -f2-)"
check "the log route's object: grep -c value = 0" "0" "$(count_in "$TMP/q11-log.jsonl")"
check "the store holds no key named by the value" "0" "$("$store" ls "ci/$REPO/" | grep -c -F -- "$VALUE")"
"$dojo" ":urgit-ci &ci-action [%delete-credential '$REPO' 'TOKEN']" 60 3 | tail -1 >/dev/null
check "credential-names no longer lists TOKEN" "no" "$(cred_names | grep -q "name='TOKEN'" && echo yes || echo no)"
"$dojo" ":urgit-ci &ci-action [%rerun-candidate $CID]" 60 3 | tail -1 >/dev/null
sleep 2
RERUN=$(dojo_value "\`(list @uv)\`(turn (skim ~(val by .^((map @uv candidate:ci) %gx /=urgit-ci=/candidates/noun)) |=(c=candidate:ci &(=(repo.c '$REPO') =(head.c head:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID/noun))) !=(id.c $CID)))) |=(c=candidate:ci id.c))" 120 | tr -d '\n' | grep -oE '0v[0-9a-v.]+' | tail -1)
check "a rerun candidate exists" "yes" "${RERUN:+yes}"
RAID=$(wait_for_attempt "$RERUN" secret 180)
st=$(wait_att "$RAID" '%passed|%failed|%infrastructure-error' 240)
line=$(grep "$RAID" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants [0-9]* ([^)]*) of [0-9]* offered' | head -1)
echo "-- rerun daemon: $line"
check "the rerun's grant list is empty" "grants 0 () of 0 offered" "$line"
check "the rerun's step saw an empty TOKEN" "1" "$(grep -c 'token length 0' "$(daemon_stream "$RAID")")"
end_row Q11
fi
[ "$NFAIL" = 0 ]
