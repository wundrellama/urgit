#!/bin/bash
# usage: r1-r5.sh r1|r3|r4|r5
# Rows R1, R3, R4, R5 (BRIEF-CI-P3 D1-D2, S1): the mint route answers the
# token once and the dojo generator is gone; expire deletes a minted
# record and refuses an enrolled one; revoke 401s the daemon's next poll
# with the reason, the daemon exits non-zero and its running attempt is
# re-offered and completes on another daemon; rotate makes an enrolled
# daemon refuse its next assignment (and de-lists it, CI-DELIVERY-1.1 b)
# until it re-enrolls. Needs p3-setup (daemon a polling, master
# CI-protected, the store configured).
source "$(dirname "$0")/lib.sh"
which_row="${1:-r1}"
TS=$(date +%H%M%S)
if [ "$which_row" = r1 ]; then
row "R1: the ship mints the token and shows it once; the record is minted, not enrolled; the dojo generator is gone"
r=$("$api" POST /ci/runners/mint '{}')
check "POST ci/runners/mint (session) -> 200" "200" "$(status_of "$r")"
TOKEN=$(jq_of "$r" .token); ID=$(jq_of "$r" .id)
echo "-- minted $ID: $(body_of "$r" | jq -c '{id, shipUrl, runner: .runner.state}')"
check "the answer carries a @uv token" "1" "$(printf '%s' "$TOKEN" | grep -cE '^0v[0-9a-v.]+$')"
check "the config snippet carries ship_url, enroll_token and sandbox" "3" "$(jq_of "$r" .configSnippet | grep -cE '^(ship_url|enroll_token|sandbox) = ')"
check "the snippet's token is the answer's" "1" "$(jq_of "$r" .configSnippet | grep -cF "enroll_token = \"$TOKEN\"")"
check "the snippet's ship_url is this ship's origin" "ship_url = \"$URL\"" "$(jq_of "$r" .configSnippet | grep -E '^ship_url' )"
check "the answer's record reads minted" "minted" "$(jq_of "$r" .runner.state)"
check "GET ci/runners lists it as minted" "minted" "$(runner_state "$ID")"
check "GET ci/runners never carries the token (grep -c the token)" "0" "$(runners_json | grep -cF "$TOKEN")"
check "the record's enrolled is ~ (scry)" "~" "$(daemon_field "$ID" enrolled | tr -d '\n')"
check "the record's bearer-hash is ~ (scry)" "~" "$(daemon_field "$ID" bearer-hash | tr -d '\n')"
check "the record's minted is set (scry)" "1" "$(daemon_field "$ID" minted | grep -cE '^~[0-9]{4}\.')"
check "the token hash is stored, never the token (a scry of the record finds the token 0 times)" "0" "$(dojo_value "(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$ID/noun))" 60 | grep -cF "$TOKEN")"
check "mint without a session -> 401" "401" "$(status_of "$("$api" POST /ci/runners/mint '{}' -)")"
check "GET ci/runners without a session -> 401" "401" "$(status_of "$(ci_get /runners -)")"
check "the dojo generator is gone from the desk (desk/gen/urgit-ci)" "0" "$(/bin/ls "$ROOT/desk/gen/urgit-ci" 2>/dev/null | grep -c mint)"
check "the mounted desk has no gen/urgit-ci/mint-enroll-token.hoon" "0" "$(/bin/ls "$PIER/urgit/gen/urgit-ci" 2>/dev/null | grep -c mint)"
gen=$("$dojo" ':urgit-ci|mint-enroll-token' 60 6 | tr '\n' ' ')
check_contains ":urgit-ci|mint-enroll-token no longer resolves" "not found" "$(printf '%s' "$gen" | grep -oE 'not found|no such|unknown generator|%dojo-poke-fail' | head -1)$(printf '%s' "$gen" | grep -qE '0v[0-9a-v.]{20,}' || echo ' not found: no token printed')"
check "the action union has no %mint-enroll-token (a poke of it fails to compile)" "1" "$("$dojo" ':urgit-ci &ci-action [%mint-enroll-token ~]' 60 8 | grep -cE 'mint-vain|nest-fail|-find|mull-'  )"
check "the daemon enrolls with the minted token (the route's token is real)" "healthy" "$(rm -rf "$RUNNER_HOME/r1"; "$P1/runner.sh" start r1 1 "$TOKEN" >/dev/null; sleep 3; wait_runner_state "$ID" healthy 20)"
check "revoke the test daemon's record (its process is stopped; a live record would be a ghost) -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"revoke-daemon\",\"id\":\"$ID\"}")")"
check "and remove it -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"expire-token\",\"id\":\"$ID\"}")")"
"$P1/runner.sh" stop r1 >/dev/null 2>&1
end_row R1
fi
if [ "$which_row" = r3 ]; then
row "R3: expire deletes a minted, never enrolled record; expiring an enrolled daemon is refused 409"
TOKEN=$(mint_token); ID=$(mint_id)
check "a fresh minted record" "minted" "$(runner_state "$ID")"
r=$(ci_action "{\"action\":\"expire-token\",\"id\":\"$ID\"}")
check "POST ci/action expire-token (minted) -> 200" "200" "$(status_of "$r")"
check "the record is gone from GET ci/runners" "" "$(runner_state "$ID")"
check "the record is gone from the ship (scry)" '%.n' "$(daemon_exists "$ID")"
enr=$(rm -rf "$RUNNER_HOME/r3"; "$P1/runner.sh" start r3 1 "$TOKEN" 2>&1 | tail -1)
check "the expired token enrolls nothing" "1" "$(sleep 2; grep -c 'enroll token is not recognized' "$RUNNER_HOME/r3/daemon.log")"
# an enrolled test daemon (not a: under the row's mutant the record would
# be deleted and the pool's daemon would exit, which is another row's
# evidence lost)
rm -rf "$RUNNER_HOME/r3"
T3=$(mint_token); ID3=$(mint_id)
"$P1/runner.sh" start r3 1 "$T3" >/dev/null; sleep 3
check "a test daemon enrolled with a second token" "healthy" "$(wait_runner_state "$ID3" healthy 20)"
r=$(ci_action "{\"action\":\"expire-token\",\"id\":\"$ID3\"}")
check "expire enrolled -> 409" "409" "$(status_of "$r")"
check_contains "the refusal names the alternative" "daemon is enrolled; revoke it instead" "$(jq_of "$r" .error)"
check "the enrolled daemon is still there" "healthy" "$(runner_state "$ID3")"
check "expire an unknown id -> 409 no such daemon" "no such daemon" "$(jq_of "$(ci_action '{"action":"expire-token","id":"0v0"}')" .error)"
retire_daemon r3
end_row R3
fi
if [ "$which_row" = r4 ]; then
row "R4: revoke an enrolled daemon -> its next poll 401s 'revoked by the ship', it exits non-zero, its running attempt is re-offered and completes on daemon a"
start_daemon b 1
wait_live_only "$DAEMON_A" "$DAEMON_B" || { echo "R4: not run — another daemon record is still selectable"; exit 1; }
# the scheduler prefers a (older, idle): the operator binds a to another
# repository for the push (D2b, the panel's lever), so the plan and the
# slow job land on b; a is unbound before the revoke so it is the other
# daemon the re-offer finds
check "bind a to another repository -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":[\"somewhere-else\"]}")")"
sync_clone; set_workflows fixture-slow.yml
printf 'r4 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R4: a slow job to revoke under"
S1=$(wait_for_attempt "$CID" slow 120)
echo "-- automatic attempt $S1 on $(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$S1/noun))" | one '0v[0-9a-v.]+')"
check "the slow attempt runs on daemon b" "$DAEMON_B" "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$S1/noun))" | one '0v[0-9a-v.]+')"
for _ in $(seq 1 60); do [ "$(att_events "$S1")" -gt 0 ] 2>/dev/null && break; sleep 2; done
echo "-- attempt $S1 has $(att_events "$S1") events (act is running on b)"
check "b's running set holds it (scry)" "1" "$(daemon_field "$DAEMON_B" running | grep -c "$S1")"
check "unbind a -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":null}")")"
r=$(ci_action "{\"action\":\"revoke-daemon\",\"id\":\"$DAEMON_B\"}")
T0=$(date +%s)
check "POST ci/action revoke-daemon -> 200" "200" "$(status_of "$r")"
check "the panel reads revoked" "revoked" "$(runner_state "$DAEMON_B")"
check "bearer-hash cleared (scry)" "~" "$(daemon_field "$DAEMON_B" bearer-hash | tr -d '\n')"
check "the attempt on b is re-offered, not left running" '%reoffered' "$(wait_att "$S1" '%reoffered' 20)"
check_contains "its reason names the revocation" "daemon revoked; re-offered" "$(att_reason "$S1")"
# b is at capacity with the slow job, and polls anyway (D6 f, the
# heartbeat): its next poll — within the 25 s window — answers 401, it
# cancels the job and exits
T_REVOKE=$(date +%s)
"$P1/runner.sh" wait-exit b 90 | head -1
echo "-- b exited $(( $(date +%s) - T_REVOKE )) s after the revoke"
check "b exited within two poll windows of the revoke (it polled while busy)" "yes" "$([ $(( $(date +%s) - T_REVOKE )) -le 60 ] && echo yes || echo no)"
check_contains "daemon b logged the revocation on its next poll" "revoked by the ship" "$(grep -o 'revoked by the ship' "$RUNNER_HOME/b/daemon.log" | head -1)"
check_contains "daemon b exited non-zero (status 5)" "exit status 5" "$(grep -o 'exit status [0-9]*' "$RUNNER_HOME/b/daemon.log" | tail -1)"
check "a poll with b's old bearer answers 401 revoked by the ship" "401 revoked by the ship" "$(bearer=$(jq -r .bearer "$RUNNER_HOME/b/state.json"); r=$("$api" GET "/ci/daemon/$DAEMON_B/assignment" "" "$bearer"); echo "$(status_of "$r") $(jq_of "$r" .error)")"
S2=""; for _ in $(seq 1 60); do for a in $(cand_attempt_ids "$CID"); do [ "$a" != "$S1" ] && [ "$(att_kind "$a")" = "%job" ] && S2="$a"; done; [ -n "$S2" ] && break; sleep 2; done
echo "-- the re-offered attempt $S2 on $(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$S2/noun))" | one '0v[0-9a-v.]+')"
check "the fresh attempt went to daemon a" "$DAEMON_A" "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$S2/noun))" | one '0v[0-9a-v.]+')"
check "it passed on a" '%passed' "$(wait_att "$S2" '%passed|%failed|%infrastructure-error' 300)"
check "the candidate passed" '%passed' "$(wait_cand "$CID" '%passed|%failed|%unknown' 60)"
check "landed: master = the candidate" "$OID" "$(repo_master)"
check "b's running set is empty (scry)" "{}" "$(daemon_field "$DAEMON_B" running | tr -d '\n')"
check "remove b's revoked record -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"expire-token\",\"id\":\"$DAEMON_B\"}")")"
end_row R4
fi
if [ "$which_row" = r5 ]; then
row "R5: rotate the CI key -> an enrolled daemon's next assignment fails verification and it is de-listed as refused; re-enroll -> works"
before=$(jq_of "$(ci_get /key)" .pub)
r=$(ci_action '{"action":"rotate-ci-key"}')
check "POST ci/action rotate-ci-key -> 200" "200" "$(status_of "$r")"
k=$(ci_get /key)
after=$(jq_of "$k" .pub)
check "GET ci/key answers a new public key" "yes" "$([ -n "$after" ] && [ "$after" != "$before" ] && echo yes || echo "no ($before -> $after)")"
check "the new key is certified by the ship (the cert was re-signed)" "true" "$(jq_of "$k" .certified)"
check "Go's crypto/ed25519 verifies the new certificate" "certificate verified" "$(cd "$ROOT/runner" && URGIT_CI_KEY_JSON="$(body_of "$k")" go test -count=1 -run TestLiveCertificate -v ./internal/sig/ 2>&1 | grep -oE 'certificate verified|does not verify|FAIL' | head -1)"
check "daemon a still pins the old key" "$before" "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("ci_public_key",""))' "$RUNNER_HOME/a/state.json")"
sync_clone; set_workflows fixture-pass.yml
printf 'r5 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R5: after the rotation"
PA=""; for _ in $(seq 1 45); do PA=$(cand_attempt_ids "$CID" | tail -1); [ -n "$PA" ] && break; sleep 2; done
echo "-- first attempt $PA ($(att_kind "$PA"))"
# the refusal line is the daemon's `no result:`; since S5 the daemon also
# logs the ship's answer to its abandon, which echoes the reason
check "daemon a refused the assignment after the rotation" "1" "$(for _ in $(seq 1 30); do grep -q "$PA.*assignment refused: " "$RUNNER_HOME/a/daemon.log" && break; sleep 2; done; grep "$PA" "$RUNNER_HOME/a/daemon.log" | grep -c 'no result: assignment refused: signature does not verify')"
check "the ship de-listed a: the panel reads refused" "refused" "$(wait_runner_state "$DAEMON_A" refused 30)"
check_contains "with the reason" "assignment refused: signature does not verify" "$(runner_field "$DAEMON_A" .refused)"
check "a keeps polling (last-seen fresh) yet stays refused after N polls" "refused" "$(sleep 30; runner_state "$DAEMON_A")"
check "the attempt closed with the refusal (no other daemon to take it)" '%infrastructure-error' "$(wait_att "$PA" '%infrastructure-error|%reoffered|%passed' 30)"
check "no attempt was created for a after the refusal" "0" "$(for a in $(cand_attempt_ids "$CID"); do [ "$a" != "$PA" ] && [ "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$a/noun))" | one '0v[0-9a-v.]+')" = "$DAEMON_A" ] && echo x; done | wc -l)"
echo "-- re-enroll a (README recovery: delete the state file, a new token)"
OLD_A="$DAEMON_A"
start_daemon a "$DAEMON_CAPACITY"
check "a's new record pins the new key" "$after" "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("ci_public_key",""))' "$RUNNER_HOME/a/state.json")"
check "the old record stays refused (it is not the same daemon)" "refused" "$(runner_state "$OLD_A")"
r=$(ci_action "{\"action\":\"expire-token\",\"id\":\"$OLD_A\"}")
check "the refused record can be revoked then removed (revoke -> 200)" "200" "$(status_of "$(ci_action "{\"action\":\"revoke-daemon\",\"id\":\"$OLD_A\"}")")"
check "remove the revoked record (expire -> 200)" "200" "$(status_of "$(ci_action "{\"action\":\"expire-token\",\"id\":\"$OLD_A\"}")")"
r=$(ci_action "{\"action\":\"rerun-candidate\",\"id\":\"$CID\"}")
check "re-run after re-enrollment -> 200" "200" "$(status_of "$r")"
NEW=""; for _ in $(seq 1 30); do NEW=$(python3 - "$(body_of "$(ci_get "/repository/$REPO/candidates")")" <<'PY'
import json,sys; d=json.loads(sys.argv[1]); c=[x for x in d["candidates"] if x["status"] in ("pending","passed")]; print(c[0]["id"] if c else "")
PY
); [ -n "$NEW" ] && break; sleep 2; done
echo "-- the re-run candidate $NEW"
check "the re-run passes on the re-enrolled daemon" '%passed' "$(wait_cand "$NEW" '%passed|%failed|%unknown' 300)"
check_contains "its assignment verified against the new key" "assignment signature verified" "$(runner_log a | grep -o 'assignment signature verified' | head -1)"
check "landed: master = the candidate" "$OID" "$(repo_master)"
end_row R5
fi
[ "$NFAIL" = 0 ]
