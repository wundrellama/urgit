#!/bin/bash
# Rows P3, P4, P5: push to the CI-protected master -> staged; within 10 s,
# with no poke, materialized, refs/ci/candidate/<id> set, plan assignment
# delivered; the daemon posts the plan (fixture-pass + fixture-chain);
# job a runs and passes, b is assigned only after a closes, b passes, the
# candidate passes and the ship lands it: master = candidate OID with no
# second push.
source "$(dirname "$0")/lib.sh"
source "$TMP/p1.env"; source "$TMP/p2.env"
row "P3: push -> staged; within 10 s materialized, scratch ref set, plan assignment delivered (no poke)"
sync_clone
BEFORE=$(repo_master)
echo "three $(date +%s)" > THREE.md
push_commit "three: first CI-protected push"
check_contains "push answered with the staging notice" "staged as ci candidate" "$PUSH"
t0=$(date +%s)
for _ in $(seq 1 10); do
  obj=$(cand_object "$CID"); case "$obj" in '[~ 0x'*) break;; esac; sleep 1
done
echo "-- materialized after $(( $(date +%s) - t0 )) s: candidate=$obj"
check "candidate materialized (fast-forward: the head itself)" "[~ 0x$(printf '%s' "$OID" | sed 's/^0*//' | rev | sed 's/\(....\)/\1./g' | rev | sed 's/^\.//')]" "$obj"
check_contains "refs/ci/candidate/<id> advertised to git" "refs/ci/candidate/$CID" "$(ls_remote)"
check "master unchanged" "$BEFORE" "$(repo_master)"
for _ in $(seq 1 10); do grep -q "assignment .*candidate $CID .*kind\|\[plan .*candidate $CID" "$RUNNER_HOME/a/daemon.log" 2>/dev/null && break; grep -q "\[plan " "$RUNNER_HOME/a/daemon.log" && break; sleep 1; done
echo "-- $(( $(date +%s) - t0 )) s after the push:"
check_contains "plan assignment delivered to the daemon" "[plan " "$(runner_log a)"
end_row P3

row "P4: the daemon runs act -l and POSTs the plan; the ship stores fixture-pass/pass + fixture-chain/a + fixture-chain/b"
for _ in $(seq 1 90); do jobs=$(cand_plan_jobs "$CID" 2>/dev/null | tr '\n' ' '); [ -n "$jobs" ] && break; sleep 2; done
check "plan jobs" "fixture-chain.yml/a fixture-chain.yml/b fixture-pass.yml/pass " "$jobs"
check_contains "plan POST answered 200" "plan POST -> 200" "$(runner_log a)"
check "plan-oid is the candidate oid" "$obj" "$(dojo_value "plan-oid:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID/noun))" | one '^(~|\[~ 0x[0-9a-f.]+\])$')"
end_row P4

row "P5: a runs and passes; b assigned only after a closes; b passes; candidate %passed; the ship lands master = candidate OID"
A_ATT=$(wait_for_attempt "$CID" a 60) || true
echo "-- attempt for a: $A_ATT"
check "a passes" '%passed' "$(wait_att "$A_ATT" '%passed' 240)"
B_ATT=$(wait_for_attempt "$CID" b 60) || true
echo "-- attempt for b: $B_ATT (started $(att_started "$B_ATT"); a finished $(att_finished "$A_ATT"))"
check "b was created after a finished" "yes" "$(python3 - "$(att_finished "$A_ATT")" "$(att_started "$B_ATT")" <<'PY'
import sys
def key(s): return s.replace('~','').replace('..','.').split('.')
print("yes" if key(sys.argv[1]) <= key(sys.argv[2]) else "no")
PY
)"
check "b passes" '%passed' "$(wait_att "$B_ATT" '%passed' 240)"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 30)"
sleep 3
check "landed by the ship: master = candidate OID" "$OID" "$(repo_master)"
check "verdict-reason records the landing" "'landed'" "$(cand_reason "$CID")"
check_not_contains "no second push happened (clone is at the same commit as master)" "ahead" "$(cd "$CLONE" && git fetch -q origin && git status -sb | head -1)"
echo "-- attempts of $CID:"; cand_attempts "$CID"
echo "export CID5=$CID; export OID5=$OID; export A_ATT=$A_ATT; export B_ATT=$B_ATT" > "$TMP/p5.env"
end_row P5
