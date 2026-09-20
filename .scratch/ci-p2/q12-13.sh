#!/bin/bash
# usage: q12-13.sh q12|q13
# Rows Q12-Q13 (D5): the CI signing key, the assignment signature the
# daemon verifies, the daemon that refuses with a wrong pinned key, and
# the grant whose expiry has passed. Needs p2-setup (daemon a polling,
# master CI-protected, the store fixture configured).
#   Q12  GET ci/key (session) answers {pub, cert, ship, life} and never a
#        secret; daemon a's state file pins that pub; a normal job's
#        assignment line says its signature verified; daemon b, started
#        with a WRONG ci_public_key in its config while a is stopped,
#        refuses every assignment with a logged reason, prepares no
#        sandbox, and abandons the attempt with that reason
#   Q13  a grant past its expiry is refused by the daemon and not passed
#        to act. a grant expires at the attempt's deadline, measured from
#        the ship's assignment time, while the daemon's own deadline runs
#        from receipt: with daemon a stopped (and its dead poll forgotten),
#        an operator %assign with a 25 s deadline waits 15 s for a's poll,
#        and a harness docker
#        wrapper delays the sandbox copy by 12 s, so act starts after the
#        grant expired and before the daemon's own deadline (25 s from
#        receipt); the daemon
#        logs the refusal and the step sees an empty TOKEN
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env" 2>/dev/null
which_row="${1:-q12}"
TS=$(date +%H%M%S)
pinned_key() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("ci_public_key",""))' "$RUNNER_HOME/$1/state.json"; }
if [ "$which_row" = q12 ]; then
row "Q12: the CI key is certified and every assignment is signed; a wrong pinned key refuses all work"
r=$(ci_get /key)
echo "-- GET ci/key -> $(printf '%s' "$r" | cut -c1-200)"
check "GET ci/key (session) -> 200" "200" "$(status_of "$r")"
PUB=$(jq_of "$r" .pub); CERT=$(jq_of "$r" .cert)
check "pub is 32 hex bytes" "64" "$(printf '%s' "$PUB" | grep -cE '^[0-9a-f]{64}$' | sed 's/^1$/64/')"
check "cert is 64 hex bytes (the ship's signature over pub)" "128" "$(printf '%s' "$CERT" | grep -cE '^[0-9a-f]{128}$' | sed 's/^1$/128/')"
check "ship named" "~$SHIP" "$(jq_of "$r" .ship)"
check "no secret in the answer (sek, sec, ring, private)" "0" "$(body_of "$r" | grep -ciE '"(sek|sec|ring|private[a-z-]*)"')"
check "GET ci/key without a session -> 401" "401" "$(status_of "$(ci_get /key -)")"
check "daemon a pinned the same pub at enrollment" "$PUB" "$(pinned_key a)"
# the certificate chain's first link, checked by Go's ed25519: the ship's
# signing key (Jael's ring, derived as ames derives it) over the CI pub
certv=$(cd "$ROOT/runner" && URGIT_CI_KEY_JSON="$(body_of "$r")" go test -count=1 -run TestLiveCertificate -v ./internal/sig/ 2>&1 | grep -oE 'certificate verified|does not verify|FAIL' | head -1)
check "Go's crypto/ed25519 verifies the certificate against the ship's signing key" "certificate verified" "$certv"
sync_clone
set_workflows fixture-pass.yml
printf 'q12 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q12: a signed assignment"
AID=$(wait_for_attempt "$CID" pass 180)
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "attempt %passed under daemon a" '%passed' "$st"
check_contains "daemon a verified the assignment signature" "assignment signature verified" "$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o 'assignment signature verified' | head -1)"
echo "-- daemon b with a wrong pinned key in its config; the operator assigns the job to it by name"
rm -rf "$RUNNER_HOME/b"
TOKEN=$("$P1/mint.sh") || exit 1
"$P1/runner.sh" config b 1 "$TOKEN" >/dev/null
WRONG=$(printf '%s' "$PUB" | sed 's/^f/0/; t; s/^./f/')   # the first hex digit flipped: not the ship's key
printf 'ci_public_key = "%s"\n' "$WRONG" >> "$RUNNER_HOME/b/config.toml"
"$P1/runner.sh" start b | head -1
sleep 3
check "daemon b enrolled and polls" "running" "$("$P1/runner.sh" status b | head -1 | cut -d' ' -f1)"
check_contains "daemon b logged the config's pin" "CI public key pinned by the config: $WRONG" "$(grep -o 'CI public key pinned by the config: [0-9a-f]*' "$RUNNER_HOME/b/daemon.log" | head -1)"
DAEMON_B=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/b/state.json")
# the operator's %assign names the daemon, so the scheduler's choice
# (daemon a, the oldest) does not decide who receives it
"$dojo" ":urgit-ci &ci-action [%assign $CID $DAEMON_B %job \`'fixture-pass.yml' \`'pass' \`~s30]" 60 3 | tail -1 >/dev/null
sleep 12
refused=$(grep -c 'assignment refused: ' "$RUNNER_HOME/b/daemon.log")
echo "-- daemon b: $(grep 'assignment refused: ' "$RUNNER_HOME/b/daemon.log" | head -1 | cut -c1-200)"
check "daemon b refused the assignment with a logged reason" "yes" "$([ "$refused" -ge 1 ] && echo yes || echo no)"
check_contains "the reason names the signature" "signature does not verify" "$(grep 'assignment refused: ' "$RUNNER_HOME/b/daemon.log" | head -1)"
check "daemon b prepared no sandbox (no work)" "0" "$(grep -c 'sandbox .* prepared' "$RUNNER_HOME/b/daemon.log")"
PA=$(cand_attempt_ids "$CID" | head -1)
check "the override attempt went to daemon b" "$DAEMON_B" "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$PA/noun))" | one '0v[0-9a-v.]+')"
# P3 (CI-DELIVERY-1.1 b, D6): a refused assignment is re-offered once on
# another live daemon when the ship has one — daemon a here — and the
# attempt closes %reoffered; only with no other live daemon does it close
# as P2 asserted, %infrastructure-error. The reason names the signature
# either way, and the refusing daemon is de-listed (its record refused).
others=$(dojo_value "(lent (skim ~(val by .^((map @uv daemon:ci) %gx /=urgit-ci=/daemons/noun)) |=(d=daemon:ci ?&(!=(id.d $DAEMON_B) ?=(^ enrolled.d) ?=(^ bearer-hash.d) ?=(~ revoked.d) ?=(~ refused.d) ?=(^ last-seen.d) (lte (sub now (min now (need last-seen.d))) ~m5)))))" | grep -oE '^[0-9]+$' | tail -1)
echo "-- other live daemon records: ${others:-?}"
if [ "${others:-0}" -gt 0 ]; then
  check "the ship closed b's attempt as re-offered (P3 D6: another live daemon exists)" '%reoffered' "$(wait_att "$PA" '%reoffered|%infrastructure-error|%passed|%failed' 60)"
else
  check "the ship recorded the refusal as the attempt's reason (no other live daemon)" '%infrastructure-error' "$(wait_att "$PA" '%infrastructure-error|%passed|%failed' 60)"
fi
check_contains "attempt reason names the signature" "signature does not verify" "$(att_reason "$PA")"
check "the ship de-listed b: its record reads refused (P3 D6 b)" "refused" "$("$api" GET /ci/runners | sed 's/^[0-9]* //' | jq -r ".runners[] | select(.id == \"$DAEMON_B\") | .state")"
"$P1/runner.sh" stop b >/dev/null 2>&1
end_row Q12
fi
if [ "$which_row" = q13 ]; then
row "Q13: a grant past its expiry is refused by the daemon and never reaches act"
"$dojo" ":urgit-ci &ci-action [%set-credential '$REPO' 'TOKEN' 'q13-hunter2-$TS-a1b2c3d4' %job ~]" 60 3 | tail -1 >/dev/null
sync_clone
set_workflows fixture-secret.yml
printf 'q13 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q13: the secret job, first the normal way"
AID=$(wait_for_attempt "$CID" secret 180)
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "the normal attempt %passed with the grant" '%passed' "$st"
check_contains "the normal attempt got the grant" "grants 1 (TOKEN)" "$(grep "$AID" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants 1 (TOKEN)' | head -1)"
echo "-- daemon a stopped; %assign to it with a 25 s deadline; a starts 15 s later behind a docker wrapper that delays the first cp by 12 s"
mkdir -p "$TMP/q13-bin"
cat > "$TMP/q13-bin/docker" <<'SH'
#!/bin/bash
# the harness wrapper: the first `docker cp` of a run sleeps 12 s so act
# starts after the grant's expiry; everything else passes straight through
if [ "$1" = --host ] && [ "$3" = cp ]; then
  if [ ! -e "$Q13_FLAG" ]; then touch "$Q13_FLAG"; sleep 12; fi
fi
exec /usr/bin/docker "$@"
SH
chmod +x "$TMP/q13-bin/docker"
export Q13_FLAG="$TMP/q13-delayed"; rm -f "$Q13_FLAG"
"$P1/runner.sh" stop a >/dev/null 2>&1
# eyre reports the stopped daemon's closed long-poll about twelve seconds
# later; an assignment made before that would be answered into the dead
# poll and re-offered only two minutes on (CI-DELIVERY-1), past a short
# deadline. so the assignment waits for the poll to be forgotten first.
sleep 20
"$dojo" ":urgit-ci &ci-action [%assign $CID $DAEMON_A %job \`'fixture-secret.yml' \`'secret' \`~s25]" 60 3 | tail -1 >/dev/null
T0=$(date +%s)
LATE=$(cand_attempt_ids "$CID" | head -1)
echo "-- assigned at $T0: attempt $LATE (grant expires at the 25 s deadline)"
sleep 15
RUNNER_PATH="$TMP/q13-bin" "$P1/runner.sh" start a | head -1
for _ in $(seq 1 40); do grep -q "$LATE.*act exited" "$RUNNER_HOME/a/daemon.log" && break; sleep 2; done
EXITED_AT=$(( $(date +%s) - T0 ))
echo "-- act exited at +$EXITED_AT s: $(grep "$LATE" "$RUNNER_HOME/a/daemon.log" | grep -o 'act exited [0-9]*' | head -1)"
check "the override attempt is not the normal one" "no" "$([ "$LATE" = "$AID" ] && echo yes || echo no)"
line=$(grep "$LATE" "$RUNNER_HOME/a/daemon.log" | grep -o 'grant TOKEN refused: expired at [^;]*' | head -1)
echo "-- daemon: $line"
check "expired grant refused by the daemon" "1" "$(grep "$LATE" "$RUNNER_HOME/a/daemon.log" | grep -c 'grant TOKEN refused: expired')"
check_contains "the assignment line counts it out" "grants 0 () of 1 offered" "$(grep "$LATE" "$RUNNER_HOME/a/daemon.log" | grep -o 'grants [0-9]* ([^)]*) of [0-9]* offered' | head -1)"
for _ in $(seq 1 60); do [ -s "$(daemon_stream "$LATE")" ] && grep -q 'token length' "$(daemon_stream "$LATE")" && break; sleep 2; done
check "act ran without the secret (token length 0)" "1" "$(grep -c 'token length 0' "$(daemon_stream "$LATE")")"
check "the secret was not passed (no non-zero token length line)" "0" "$(grep -c 'token length [1-9]' "$(daemon_stream "$LATE")")"
# the mutant's tripwire (T4): act ran past the grant's 25 s expiry and the step saw the secret
[ "$(grep -c 'token length [1-9]' "$(daemon_stream "$LATE")")" -ge 1 ] && [ "$EXITED_AT" -ge 25 ] && echo "Q13 RED: expired signed grant reached act"
"$P1/runner.sh" stop a >/dev/null 2>&1
"$P1/runner.sh" start a | head -1
sleep 3
"$dojo" ":urgit-ci &ci-action [%delete-credential '$REPO' 'TOKEN']" 60 3 | tail -1 >/dev/null
end_row Q13
fi
[ "$NFAIL" = 0 ]
