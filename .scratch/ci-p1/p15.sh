#!/bin/bash
# Row P15: ERPit for real. ERPit's current master is pushed into a fresh
# repository on the ship as the seed, master is CI-protected, one commit
# is pushed: the plan is suite.yml (plan, structural, suite) + fixtures.yml
# (pins, plan, replay, erasure, duo); suite and replay/erasure/duo are
# gated on their plan outputs by the ship; every job runs once under the
# daemon on its projection; all eight green; the candidate passes and the
# ship lands it. Needs an enrolled, polling daemon (capacity 3 after P14's
# re-enrollment). Budget: ~25 min of act time.
source "$(dirname "$0")/lib.sh"
REPO=erpit-p15; CLONE="$TMP/clone-erpit"
ERPIT=/var/home/michael/workspace/urbit/erpit
if [ "${1:-}" != verify ]; then
row "P15: ERPit for real — eight jobs under the daemon, candidate %passed, landed"
echo "-- ERPit source at $(git -C "$ERPIT" rev-parse HEAD) ($(git -C "$ERPIT" status --short | wc -l) dirty files)"
"$api" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}" | cut -c1-40
rm -rf "$CLONE"; git clone -q "$ERPIT" "$CLONE"; cd "$CLONE"
git config user.name p15; git config user.email p15@example; git config http.cookieFile "$JAR"
git remote remove origin; git remote add origin "$URL/git/$REPO"
SEED=$(git rev-parse HEAD)
echo "-- seed push of ERPit master $SEED ($(git rev-list --count HEAD) commits)"
git push -q origin master 2>&1 | tail -1
check "seed landed" "$SEED" "$(repo_master)"
"$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 3 | tail -1
check "CI-protected" '%.y' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
printf '\nci-p1 P15 %s\n' "$(date -Is)" >> README.md
T0=$(date +%s)
push_commit "ci-p1 P15: one commit through native CI"
echo "export CID=$CID; export OID=$OID; export T0=$T0" > "$TMP/p15-run.env"
fi   # end of the push half; `p15.sh verify` resumes from p15-run.env
if [ "${1:-}" = verify ]; then source "$TMP/p15-run.env"; row "P15 (verify): resumed on candidate $CID"; fi
for _ in $(seq 1 120); do jobs=$(cand_plan_jobs "$CID" | tr '\n' ' '); [ -n "$jobs" ] && break; sleep 2; done
echo "-- plan after $(( $(date +%s) - T0 )) s: $jobs"
check "plan = eight jobs of suite.yml and fixtures.yml" "fixtures.yml/duo fixtures.yml/erasure fixtures.yml/pins fixtures.yml/plan fixtures.yml/replay suite.yml/plan suite.yml/structural suite.yml/suite " "$jobs"
st=$(wait_cand "$CID" '%passed|%failed|%unknown' 2700)
T1=$(date +%s)
echo "-- candidate $st after $(( T1 - T0 )) s"
check "candidate %passed" '%passed' "$st"
echo "-- attempts (id kind job status started finished):"
n_pass=0; n_job=0
for a in $(cand_attempt_ids "$CID"); do
  k=$(att_kind "$a"); j=$(att_job "$a"); s=$(att_status "$a")
  echo "   $a $k $j $s $(att_started "$a") $(att_finished "$a")"
  [ "$k" = "%job" ] && n_job=$((n_job+1)) && [ "$s" = "%passed" ] && n_pass=$((n_pass+1))
done
check "eight job attempts, no reruns" "8" "$n_job"
check "all eight green" "8" "$n_pass"
gated=$(for j in suite replay erasure duo; do a=$(att_of_job "$CID" "$j"); [ -n "$a" ] && echo "$j"; done | tr '\n' ' ')
check "the gated jobs ran once the ship evaluated their plan outputs" "suite replay erasure duo " "$gated"
sleep 3
check "landed by the ship: master = candidate OID" "$OID" "$(repo_master)"
check "verdict-reason" "'landed'" "$(cand_reason "$CID")"
echo "-- daemon log, projections and results:"; grep -E 'running: act push|result .* POST|act exited' "$RUNNER_HOME/a/daemon.log" | grep -F "$CID" -A0 | cut -c1-200 | tail -24
grep -E 'result success POST|act exited' "$RUNNER_HOME/a/daemon.log" | tail -16 | cut -c1-160
echo "-- P15 wall time: $(( T1 - T0 )) s from push to verdict"
echo "export ERPIT_CID=$CID; export ERPIT_OID=$OID; export ERPIT_T=$(( T1 - T0 ))" > "$TMP/p15.env"
end_row P15
# a failed row fails the script, so battery.sh stops at it
[ "$NFAIL" = 0 ]
