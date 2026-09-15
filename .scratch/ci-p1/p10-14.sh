#!/bin/bash
# Rows P10-P14:
#   P10 SIGKILL act inside the sandbox -> the daemon POSTs abandon -> the
#       attempt is %infrastructure-error with the reason within 5 s (no
#       deadline wait); candidate %unknown; push refused.
#   P11 SIGKILL the daemon mid-job -> no abandon; the attempt closes at
#       its deadline (~s20 via %assign); the restarted daemon reconciles
#       the orphaned sandbox by asking the ship and destroys it.
#   P12 Destroy made to fail (docker wrapped on PATH) -> slot quarantined,
#       capacity decremented, logged; the next assignment runs on the
#       remaining slot.
#   P13 two daemons, one at capacity -> the assignment goes to the other;
#       a daemon unseen for ~m5 is never selected.
#   P14 |nuke %urgit-ci then |revive mid-candidate -> the daemon's next
#       poll answers 401; it logs the enrollment loss and exits non-zero.
source "$(dirname "$0")/lib.sh"
source "$TMP/p2.env"
rows="${*:-p10 p11 p12 p13 p14}"
has() { case " $rows " in *" $1 "*) return 0;; *) return 1;; esac; }
sandbox_of() { echo "ci-$1"; }
# wait until act runs inside the attempt's sandbox (the runner container
# has an `act` process)
wait_act_running() {  # <attempt> <seconds>
  for _ in $(seq 1 "$2"); do
    $DK exec "$(sandbox_of "$1")" pgrep -x act >/dev/null 2>&1 && return 0; sleep 2
  done; return 1
}

if has p10; then
row "P10: SIGKILL act mid-job -> abandon -> %infrastructure-error within 5 s; candidate %unknown; push refused"
sync_clone; set_workflows fixture-slow.yml
push_commit "ten: a slow job to kill"
S_ATT=$(wait_for_attempt "$CID" slow 120) || true
echo "-- attempt $S_ATT"
wait_act_running "$S_ATT" 60 && echo "-- act is running inside $(sandbox_of "$S_ATT")"
sleep 15
$DK exec "$(sandbox_of "$S_ATT")" pkill -KILL -x act && echo "-- SIGKILL sent to act inside the sandbox at $(date -Is)"
t0=$(date +%s)
st=$(wait_att "$S_ATT" '%infrastructure-error' 20)
echo "-- attempt closed $st after $(( $(date +%s) - t0 )) s"
check "attempt %infrastructure-error" '%infrastructure-error' "$st"
check "closed within 5 s of the kill" "yes" "$([ $(( $(date +%s) - t0 )) -le 5 ] && echo yes || echo no)"
check_contains "reason carries the daemon's abandon" "abandoned: act exited 137 without a jobResult" "$(att_reason "$S_ATT")"
check_contains "daemon logged the abandon" "abandon POST -> 200" "$(runner_log a)"
check "candidate %unknown" '%unknown' "$(wait_cand "$CID" '%unknown' 20)"
p=$(git -C "$CLONE" push origin master 2>&1 | grep -o 'remote rejected.*' | head -1)
check_contains "push refused" "staged as ci candidate" "$p"
sleep 3
check "sandbox destroyed" "" "$($DK network ls --format '{{.Name}}' | grep -F "$(sandbox_of "$S_ATT")" || true)"
end_row P10
fi

if has p11; then
row "P11: SIGKILL the daemon mid-job -> no abandon; closes at the ~s20 deadline; restart reconciles the orphan"
sync_clone; set_workflows fixture-slow.yml
push_commit "eleven: a slow job whose daemon dies"
# the automatic attempt carries ~h1 and fills the daemon's one slot: it is
# abandoned the P10 way, then the operator's %assign override (~s20) is
# the attempt the daemon is running when it dies
S1=$(wait_for_attempt "$CID" slow 120) || true
wait_act_running "$S1" 60 && $DK exec "$(sandbox_of "$S1")" pkill -KILL -x act && echo "-- automatic attempt $S1 (~h1) abandoned by killing act: $(wait_att "$S1" '%infrastructure-error' 20)"
sleep 3
"$dojo" ":urgit-ci &ci-action [%assign $CID $DAEMON_A %job \`'fixture-slow.yml' \`'slow' \`~s20]" 60 3 | tail -1
S2=""; for _ in $(seq 1 30); do for a in $(cand_attempt_ids "$CID"); do [ "$a" != "$S1" ] && [ "$(att_kind "$a")" = "%job" ] && S2="$a"; done; [ -n "$S2" ] && break; sleep 2; done
echo "-- override attempt $S2 (~s20)"
wait_act_running "$S2" 60 && echo "-- act is running inside ci-$S2"
"$P1/runner.sh" kill a | tail -1
t0=$(date +%s)
sleep 5
check "no abandon from a dead daemon (attempt still running 5 s later)" '%running' "$(att_status "$S2")"
st=$(wait_att "$S2" '%infrastructure-error' 40)
echo "-- $st after $(( $(date +%s) - t0 )) s"
check "closed at the deadline" '%infrastructure-error' "$st"
check "reason is the deadline's" "no result arrived before the deadline" "$(att_reason "$S2")"
orphans="$($DK network ls --format '{{.Name}}' | grep -E "^ci-" | tr '\n' ' ')"
echo "-- sandboxes left on the rootless daemon: $orphans"
check "the dead daemon's sandbox is an orphan" "ci-$S2" "$(echo "$orphans" | grep -o "ci-$S2" || true)"
"$P1/runner.sh" start a >/dev/null; sleep 8
log=$(runner_log a | tail -n 12)
check_contains "restart reconciled the orphan by asking the ship (terminal -> destroyed)" "reconcile ci-$S2 (attempt infrastructure-error): destroyed" "$log"
check "orphan gone" "" "$($DK network ls --format '{{.Name}}' | grep -F "ci-$S2" || true)"
check "candidate %unknown" '%unknown' "$(cand_status "$CID")"
end_row P11
fi

if has p12; then
row "P12: Destroy fails (docker wrapped to refuse `network rm`) -> slot quarantined, capacity decremented, logged; next assignment runs on the remaining slot"
"$P1/runner.sh" stop a | tail -1
mkdir -p "$TMP/p12-bin"
cat > "$TMP/p12-bin/docker" <<'SH'
#!/bin/bash
# P12 wrapper: refuse exactly one `network rm` (the first teardown after
# the flag file appears), then behave
if [ -e "$P12_FLAG" ] && printf '%s ' "$@" | grep -q ' network rm '; then rm -f "$P12_FLAG"; echo "p12: network rm refused by the harness" >&2; exit 1; fi
exec /usr/bin/docker "$@"
SH
chmod +x "$TMP/p12-bin/docker"
export P12_FLAG="$TMP/p12.flag"; touch "$P12_FLAG"
"$P1/runner.sh" config a 2 >/dev/null   # capacity 2 in config; the ship still records the enrolled 1 (D6), the daemon's own slots are what P12 measures
RUNNER_PATH="$TMP/p12-bin" "$P1/runner.sh" start a >/dev/null; sleep 3
sync_clone; set_workflows fixture-pass.yml
push_commit "twelve: a job whose teardown fails"
# the first teardown after the push is the plan attempt's: that slot is
# the one quarantined; the job then runs on the remaining slot
P_ATT=$(wait_for_attempt "$CID" pass 120) || true
check "job passed (on the remaining slot)" '%passed' "$(wait_att "$P_ATT" '%passed' 240)"
sleep 4
log=$(runner_log a)
Q=$(printf '%s' "$log" | grep -o 'QUARANTINED slot ci-[0-9a-v.]*' | head -1 | sed 's/QUARANTINED slot //')
echo "-- quarantined sandbox: $Q"
echo "-- the daemon's quarantine line: $(printf '%s' "$log" | grep -o 'QUARANTINED slot.*' | head -1 | cut -c1-300)"
check_contains "slot quarantined and logged" "QUARANTINED slot $Q: teardown failed" "$log"
check_contains "capacity decremented" "advertised capacity now 1" "$log"
check "only one slot quarantined" "1" "$(printf '%s' "$log" | grep -c 'QUARANTINED slot')"
rm -f "$P12_FLAG"
check "the quarantined sandbox's network was left behind" "$Q" "$($DK network ls --format '{{.Name}}' | grep -F "$Q" || true)"
sync_clone; echo "twelve-b" > TWELVE.md
push_commit "twelve-b: the next assignment on the remaining slot"
P2_ATT=$(wait_for_attempt "$CID" pass 120) || true
check "next job ran on the remaining slot and passed" '%passed' "$(wait_att "$P2_ATT" '%passed' 240)"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 60)"
echo "-- harness cleanup of the quarantined sandbox $Q (the daemon never reuses it)"
$DK rm -f "$Q" >/dev/null 2>&1; $DK volume rm -f "$Q-work" >/dev/null 2>&1; $DK network rm "$Q" >/dev/null 2>&1 || true
"$P1/runner.sh" stop a | tail -1
"$P1/runner.sh" config a 1 >/dev/null
"$P1/runner.sh" start a >/dev/null; sleep 3
end_row P12
fi

if has p13; then
row "P13: two daemons, one at capacity -> the assignment goes to the other; a daemon unseen for ~m5 is never selected"
TOKEN_B=$("$P1/mint.sh"); rm -rf "$RUNNER_HOME/b"
"$P1/runner.sh" start b 1 "$TOKEN_B" >/dev/null; sleep 3
DAEMON_B=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/b/state.json")
echo "-- daemon b = $DAEMON_B"
sync_clone; set_workflows fixture-slow.yml
push_commit "thirteen-a: a slow job to fill one daemon"
S_ATT=$(wait_for_attempt "$CID" slow 120) || true
busy=$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$S_ATT/noun))" | one '0v[0-9a-v.]+')
idle=$DAEMON_A; [ "$busy" = "$DAEMON_A" ] && idle=$DAEMON_B
echo "-- slow job on $busy; the other daemon is $idle"
sync_clone; echo "thirteen-b" > THIRTEEN.md; set_workflows fixture-pass.yml
push_commit "thirteen-b: must go to the daemon with capacity"
PL=""; for _ in $(seq 1 30); do for a in $(cand_attempt_ids "$CID"); do [ "$(att_kind "$a")" = "%plan" ] && PL="$a"; done; [ -n "$PL" ] && break; sleep 2; done
check "plan assignment went to the daemon with capacity" "$idle" "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$PL/noun))" | one '0v[0-9a-v.]+')"
check "candidate passed on the other daemon" '%passed' "$(wait_cand "$CID" '%passed' 240)"
# release the slow job and stop b: it goes stale after ~m5
bearer_busy=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["bearer"])' "$RUNNER_HOME/$([ "$busy" = "$DAEMON_A" ] && echo a || echo b)/state.json")
"$api" POST "/ci/attempt/$S_ATT/abandon" '{"reason":"harness: released after P13"}' "$bearer_busy" | cut -c1-60
"$P1/runner.sh" stop b | tail -1
sleep 5; $DK ps -a --format '{{.Names}}' | grep -E '^ci-' | xargs -r -n1 $DK rm -f >/dev/null 2>&1; $DK network ls --format '{{.Name}}' | grep -E '^ci-' | xargs -r -n1 $DK network rm >/dev/null 2>&1; $DK volume ls --format '{{.Name}}' | grep -E '^ci-' | xargs -r -n1 $DK volume rm -f >/dev/null 2>&1
echo "-- waiting 5 min 10 s for daemon b to go stale (D6: last-seen older than ~m5)"
sleep 310
"$P1/runner.sh" stop a | tail -1   # a is stopped too, but seen within ~m5: the ship may still pick it (the assignment then waits for its next poll); b is stale and may not be picked
sync_clone; echo "thirteen-c" > THIRTEEN.md
push_commit "thirteen-c: b is stale; a was seen within ~m5"
sleep 8
PL=""; for a in $(cand_attempt_ids "$CID"); do [ "$(att_kind "$a")" = "%plan" ] && PL="$a"; done
check "the stale daemon b was never selected (the plan went to a, seen within ~m5, or waited)" "yes" "$([ -z "$PL" ] && echo yes || { d=$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$PL/noun))" | one '0v[0-9a-v.]+'); [ "$d" != "$DAEMON_B" ] && echo yes || echo "no: $d"; })"
"$P1/runner.sh" start a >/dev/null; sleep 6
PL=""; for _ in $(seq 1 30); do for a in $(cand_attempt_ids "$CID"); do [ "$(att_kind "$a")" = "%plan" ] && PL="$a"; done; [ -n "$PL" ] && break; sleep 2; done
check "the candidate is planned on a once it polls, never on stale b" "$DAEMON_A" "$(dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$PL/noun))" | one '0v[0-9a-v.]+')"
check "b's last-seen is older than ~m5 while a's is fresh" "yes" "$(python3 - "$(dojo_value "last-seen:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$DAEMON_B/noun))" | tr -d '\n')" "$(dojo_value "last-seen:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$DAEMON_A/noun))" | tr -d '\n')" <<'PY2'
import sys
b, a = sys.argv[1], sys.argv[2]
def key(s): return s.replace('[~ ~','').replace(']','').replace('..','.').split('.')
print("yes" if key(b) < key(a) else "no")
PY2
)"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 240)"
echo "export DAEMON_B=$DAEMON_B" > "$TMP/p13.env"
end_row P13
fi

if has p13b; then
row "P13-overlap (CI-PROJECT-1.1): one daemon at capacity 2, two candidates on the same single-job workflow pushed within seconds -> both attempts %passed, both job containers alive at once"
# capacity 2: the daemon restarts with the new config and reports it on
# its next poll (same identity: no ghost record with a fresh last-seen)
"$P1/runner.sh" stop a >/dev/null 2>&1
"$P1/runner.sh" config a 2 >/dev/null; "$P1/runner.sh" start a | head -1
sleep 4
check "the ship recorded capacity 2 from the poll" "2" "$(dojo_value "capacity:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$DAEMON_A/noun))" | one '^[0-9]+$')"
sync_clone; set_workflows fixture-wait.yml
echo "overlap-1 $(date +%s)" > OVERLAP.md; push_commit "thirteen-overlap-1: first of two on the same job"
C1=$CID
echo "overlap-2 $(date +%s)" > OVERLAP.md; push_commit "thirteen-overlap-2: second of two on the same job"
C2=$CID
W1=$(wait_for_attempt "$C1" wait 120) || true; W2=$(wait_for_attempt "$C2" wait 120) || true
echo "-- attempts: $W1 (candidate $C1), $W2 (candidate $C2)"
wait_act_running "$W1" 60 && wait_act_running "$W2" 60
sleep 6
both=$($DK ps --format '{{.Names}}' | grep -E '^act-' | sort | tr '\n' ' ')
echo "-- act job containers alive now: $both"
check "two job containers alive at once" "2" "$($DK ps --format '{{.Names}}' | grep -c -E '^act-')"
check "attempt 1 passed" '%passed' "$(wait_att "$W1" '%passed|%failed|%infrastructure-error' 240)"
check "attempt 2 passed" '%passed' "$(wait_att "$W2" '%passed|%failed|%infrastructure-error' 240)"
# the collision an unprefixed projection name produces (CI-PROJECT-1.1):
# act's loser either meets the job container name in use at create time
# or is force-removed and dies 137; its act stream and the daemon log
# carry the message. The real build shows none.
evidence=$({ cat "$RUNNER_HOME/a/work/$W1.act.jsonl" "$RUNNER_HOME/a/work/$W2.act.jsonl" 2>/dev/null; grep -hF -e "$W1" -e "$W2" "$RUNNER_HOME/a/daemon.log"; } | grep -F -e "exitcode '137'" -e "is already in use by container" | head -1 | cut -c1-240)
echo "-- act collision evidence in the two streams and the daemon log: ${evidence:-none}"
check "projection names differ and carry the attempt ids" "yes" "$(p1=$(dojo_value "projection-name:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$W1/noun))" | tr -d '\n'); p2=$(dojo_value "projection-name:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$W2/noun))" | tr -d '\n'); echo "$p1 / $p2" >&2; case "$p1" in *"$W1/fixture-wait"*) case "$p2" in *"$W2/fixture-wait"*) echo yes;; *) echo "no: $p2";; esac;; *) echo "no: $p1";; esac)"
check "the plan carries the real workflow name" "fixture-wait" "$(dojo_value "name:(head (need plan:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$C1/noun))))" | tr -d "\n'")"
check "both candidates passed" "%passed %passed" "$(wait_cand "$C1" '%passed' 60) $(wait_cand "$C2" '%passed' 60)"
end_row P13-overlap
fi

if has p14; then
row "P14: |nuke %urgit-ci then |revive mid-candidate -> the daemon's next poll answers 401; it logs the loss and exits non-zero"
sync_clone; set_workflows fixture-wait.yml
push_commit "fourteen: a candidate to lose"
S_ATT=$(wait_for_attempt "$CID" wait 120) || true
echo "-- the operator sees on the ship:"
"$P1/nuke-revive.sh" p14 2>&1 | grep -E 'nuke|revive|version|gu' | head -8
echo "-- state after the revive: candidates $(dojo_value '~(wyt by .^((map @uv candidate:ci) %gx /=urgit-ci=/candidates/noun))' | one '^[0-9.]+$'), daemons $(dojo_value '~(wyt by .^((map @uv daemon:ci) %gx /=urgit-ci=/daemons/noun))' | one '^[0-9.]+$')"
check "state-0 wiped: no candidates" "0" "$(dojo_value '~(wyt by .^((map @uv candidate:ci) %gx /=urgit-ci=/candidates/noun))' | one '^[0-9.]+$')"
check "state-0 wiped: no daemons" "0" "$(dojo_value '~(wyt by .^((map @uv daemon:ci) %gx /=urgit-ci=/daemons/noun))' | one '^[0-9.]+$')"
echo "-- the operator sees on the daemon side (the 45 s job ends, its result and the next poll meet the wiped state):"
"$P1/runner.sh" wait-exit a 300
log=$(runner_log a)
check_contains "daemon logged the enrollment loss" "enrollment lost; re-enroll with a fresh token" "$log"
check "daemon exited non-zero" "not running" "$("$P1/runner.sh" status a | head -1)"
check "exit status 3 (enrollment lost)" "3" "$(grep -oE 'exit status [0-9]+' "$RUNNER_HOME/a/daemon.log" | tail -1 | awk '{print $3}')"
# the abandoned job's sandbox: the killed attempt is unknown to the new state
$DK ps -a --format '{{.Names}}' | grep -E '^ci-' | xargs -r -n1 $DK rm -f >/dev/null 2>&1; $DK network ls --format '{{.Name}}' | grep -E '^ci-' | xargs -r -n1 $DK network rm >/dev/null 2>&1; $DK volume ls --format '{{.Name}}' | grep -E '^ci-' | xargs -r -n1 $DK volume rm -f >/dev/null 2>&1
echo "-- CI protection and %storage: ci-protected set is part of state-0 and is gone too:"
check "ci-protected wiped with state-0" '%.n' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
end_row P14
fi
echo; echo "== rows: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# })"
[ "$NFAIL" = 0 ]
