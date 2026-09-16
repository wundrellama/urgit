#!/bin/bash
# usage: q2-4.sh q2|q3|q4
# Rows Q2-Q4 (D1/D2): the log upload and the presigned read, the trust
# class in the key, and the upload name fence. Needs p2-setup (daemon a
# polling, master CI-protected, the store fixture configured).
#   Q2  a pushed commit's `pass` job finishes -> attempt.log is
#       [key size sha256] under the attempt's trusted key; GET
#       ci/attempt/<id>/log with the session 302s to a query-presigned URL;
#       a bare `curl -L` (no headers, no cookie) returns the jsonl from the
#       PRIVATE bucket; its sha256 equals the ship's handle and the
#       daemon's saved stream; an unsigned read of the key is 403; a
#       presign that expires in 2 s answers 403 after 4 s
#   Q3  the key carries /trusted/; the sign-get scry for %untrusted on that
#       attempt answers ~ (cross-class refusal at signing time)
#   Q4  a RUNNING attempt (fixture-wait's 45 s job): POST
#       ci/attempt/<id>/upload with name=../x (daemon bearer) -> 400, and
#       artifact/../x, artifact/a/b likewise; a good name on the closed Q2
#       attempt -> 409 (the fence runs before the state check); no
#       credentials -> 401. the fence is probed on a running attempt so a
#       build without it answers 200 with a signed URL (the tripwire)
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env" 2>/dev/null
BEARER_A=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["bearer"])' "$RUNNER_HOME/a/state.json")
which_row="${1:-q2}"
# Q2's push happens once; Q3/Q4 reuse its attempt from $TMP/q2.env
if [ "$which_row" = q2 ]; then
row "Q2: log upload — the finished stream in the private bucket, read through a presigned 302"
sync_clone
set_workflows fixture-pass.yml
printf 'q2 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q2: one commit through the log upload"
check "push staged a candidate" "yes" "${CID:+yes}"
AID=$(wait_for_attempt "$CID" pass 180)
check "a job attempt for pass exists" "yes" "${AID:+yes}"
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "attempt %passed" '%passed' "$st"
LOG=$(wait_log "$AID" 30)
echo "-- attempt.log = $LOG"
KEY=$(att_log_key "$AID"); SHA=$(att_log_sha "$AID"); SIZE=$(att_log_size "$AID")
check "attempt.log key is the attempt's own trusted log.jsonl" "ci/$REPO/$CID/$AID/trusted/log.jsonl" "$KEY"
check "sha256 is 64 hex" "64" "$(printf '%s' "$SHA" | grep -cE '^[0-9a-f]{64}$' | sed 's/1/64/')"
loc=$(ci_location "/attempt/$AID/log")
echo "-- GET ci/attempt/$AID/log -> $loc"
check "log route answers 302" "302" "$(printf '%s' "$loc" | cut -d' ' -f1)"
LINK=$(printf '%s' "$loc" | cut -d' ' -f2-)
check_contains "Location is query-presigned (X-Amz-Signature)" "X-Amz-Signature=" "$LINK"
check_contains "Location signs host only" "X-Amz-SignedHeaders=host" "$LINK"
check_contains "Location is the store, under the key" "$STORE_URL/$STORE_BUCKET/$KEY?" "$LINK"
rm -f "$TMP/q2-log.jsonl"
code=$(curl -s -L -o "$TMP/q2-log.jsonl" -w '%{http_code}' "$LINK")
check "bare curl -L of the presigned URL (no headers, no cookie)" "200" "$code"
check "body sha256 = the ship's handle" "$SHA" "$(sha_of "$TMP/q2-log.jsonl")"
check "body size = the ship's handle" "$SIZE" "$(stat -c %s "$TMP/q2-log.jsonl")"
check "body sha256 = the daemon's saved stream" "$(sha_of "$(daemon_stream "$AID")")" "$(sha_of "$TMP/q2-log.jsonl")"
check "the body is act's jsonl (a jobResult line)" "1" "$(grep -c '"jobResult":"success"' "$TMP/q2-log.jsonl")"
check "unsigned GET of the key (private bucket)" "403" "$("$store" anon "$KEY")"
check "signed HEAD of the key (the fixture's own key)" "200" "$("$store" head "$KEY" | cut -d' ' -f1)"
short=$(presign_scry "$AID" trusted log.jsonl 2)
check "a 2 s presign reads now" "200" "$(curl -s -o /dev/null -w '%{http_code}' "$short")"
sleep 4
check "the same URL after its expiry" "403" "$(curl -s -o /dev/null -w '%{http_code}' "$short")"
check "the expiry answer names it" "Request has expired" "$(curl -s "$short" | grep -o 'Request has expired')"
echo "export CID=$CID; export AID=$AID; export KEY=$KEY" > "$TMP/q2.env"
end_row Q2
fi
source "$TMP/q2.env" 2>/dev/null
if [ "$which_row" = q3 ]; then
row "Q3: trust class in the key — /trusted/ in a trusted attempt's key; a %untrusted sign-get answers ~"
check "attempt is %trusted" '%trusted' "$(att_trust "$AID")"
check_contains "key carries /trusted/" "/trusted/" "$(att_log_key "$AID")"
check "sign-get %untrusted for the trusted attempt" "~" "$(sign_get_scry "$AID" untrusted log.jsonl)"
check_contains "sign-get %trusted signs the key" "/trusted/log.jsonl" "$(sign_get_scry "$AID" trusted log.jsonl)"
check "presign-get %untrusted for the trusted attempt" "~" "$(dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/presign-get/$AID/untrusted/(scot %t 'log.jsonl')/60/noun)")"
end_row Q3
fi
if [ "$which_row" = q4 ]; then
row "Q4: upload name fence — ../x is refused on a running attempt, before anything is signed"
sync_clone
set_workflows fixture-wait.yml
printf 'q4 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q4: a 45 s job to probe the upload fence while it runs"
RUNNING=$(wait_for_attempt "$CID" wait 180)
check "a running job attempt exists" '%running' "$(att_status "$RUNNING")"
sha=$(printf 'x' | sha256sum | cut -d' ' -f1)
r=$(ci_post "/attempt/$RUNNING/upload" "{\"name\":\"../x\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" "$BEARER_A")
echo "-- upload name=../x -> $(printf '%s' "$r" | cut -c1-160)"
check "upload name=../x -> 400" "400" "$(status_of "$r")"
check_contains "the refusal names the fence" "name must be log.jsonl, summary.md or artifact/<file>" "$(body_of "$r")"
r=$(ci_post "/attempt/$RUNNING/upload" "{\"name\":\"artifact/../x\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" "$BEARER_A")
check "upload name=artifact/../x -> 400" "400" "$(status_of "$r")"
r=$(ci_post "/attempt/$RUNNING/upload" "{\"name\":\"artifact/a/b\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" "$BEARER_A")
check "upload name=artifact/a/b -> 400" "400" "$(status_of "$r")"
r=$(ci_post "/attempt/$RUNNING/upload" "{\"name\":\"artifact/ok.txt\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" "$BEARER_A")
check "a good artifact name on the running attempt -> 200" "200" "$(status_of "$r")"
check_contains "signed under the attempt's trusted artifact key" "/$RUNNING/trusted/artifact/ok.txt" "$(jq_of "$r" .key)"
r=$(ci_post "/attempt/$AID/upload" "{\"name\":\"log.jsonl\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" "$BEARER_A")
check "a good name on the closed Q2 attempt -> 409 (state checked after the fence)" "409" "$(status_of "$r")"
r=$(ci_post "/attempt/$RUNNING/upload" "{\"name\":\"log.jsonl\",\"contentType\":\"text/plain\",\"sha256\":\"$sha\",\"size\":1}" -)
check "no credentials -> 401" "401" "$(status_of "$r")"
st=$(wait_att "$RUNNING" '%passed|%failed|%infrastructure-error' 240)
check "the wait job finishes %passed" '%passed' "$st"
end_row Q4
fi
[ "$NFAIL" = 0 ]
