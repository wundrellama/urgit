#!/bin/bash
# usage: r9-r11.sh r9|r10|r11|r11a|r11b
# Rows R9-R11 (BRIEF-CI-P3 D6, S5) and R11a-R11b (D2b, S1b): the ghost,
# the silent runner, N+1 at t+0, labels, the repository binding. Every
# row leaves the pool as it found it: daemon a alone at DAEMON_CAPACITY,
# every test daemon retired (revoked and removed).
#   R9   daemon b enrolls with a WRONG pinned CI key and polls; a live
#        ERPit push (eight jobs) lands 8/8 on a with no ~h1 wait: b's one
#        refusal marks it refused, its attempt is re-offered, b is offered
#        nothing more although it keeps polling; re-enrolling b restores it
#   R10  a job with timeout-minutes 1 runs on b; b is SIGSTOPped mid-job;
#        at timeout + 2 min the attempt is re-offered and completes on a;
#        SIGCONT: b's late result is refused `attempt is closed`
#   R11  four jobs at t+0 on a alone at capacity 3: all four complete
#   R11a labels: `runs-on: [self-hosted, big-mem]` lands only on the
#        big-mem daemon, a plain job on either, `gpu` waits with the reason
#        on the row and in the feed, a gpu daemon enrolling takes it
#   R11b daemon b bound to ci-p3: a push to another repository with a
#        full and b idle waits for a; b's restart keeps the binding; unbound
#        in the panel, b takes the next job
source "$(dirname "$0")/lib.sh"
which_row="${1:-r9}"
ERPIT=/var/home/michael/workspace/urbit/erpit
ERPIT_REV=a6d15ed
att_daemon() { dojo_value "daemon:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '0v[0-9a-v.]+'; }
# job_attempts <cid>: "id job status daemon" for every job attempt
job_attempts() { for a in $(cand_attempt_ids "$1"); do [ "$(att_kind "$a")" = "%job" ] || continue; echo "$a $(att_job "$a") $(att_status "$a") $(att_daemon "$a")"; done; }
# seed a fresh CI-protected repository from ERPit at the P2 pin; sets CLONE
erpit_seed() {  # <repo>
  local repo="$1"
  "$api" POST /repositories "{\"name\":\"$repo\",\"publicRead\":true}" | cut -c1-30
  CLONE="$TMP/clone-$repo"; rm -rf "$CLONE"; git clone -q "$ERPIT" "$CLONE"
  ( cd "$CLONE" && git checkout -q -B master "$ERPIT_REV" && git config user.name p3 && git config user.email p3@example && git config http.cookieFile "$JAR" && git remote remove origin && git remote add origin "$URL/git/$repo" && git push -q origin master 2>&1 | tail -1 )
  "$api" POST "/repository/$repo/branches/default" '{"name":"master"}' >/dev/null
  ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$repo\",\"ref\":\"refs/heads/master\",\"protected\":true}" | cut -c1-20
  echo "-- $repo seeded from ERPit $ERPIT_REV ($(git -C "$CLONE" rev-list --count HEAD) commits), CI-protected"
}
if [ "$which_row" = r9 ]; then
row "R9: the ghost — a wrong-key daemon enrolled and polling, a live eight-job push: 8/8 lands with no ~h1 wait, the daemon reads refused and is offered nothing more until it re-enrolls"
PUB=$(jq_of "$(ci_get /key)" .pub)
WRONG=$(printf '%s' "$PUB" | sed 's/^f/0/; t; s/^./f/')
start_daemon b 1 "" "ci_public_key = \"$WRONG\""
wait_live_only "$DAEMON_A" "$DAEMON_B" || { echo "R9: not run — another daemon record is still selectable"; exit 1; }
check "daemon b pinned the wrong key" "$WRONG" "$(grep -o 'CI public key pinned by the config: [0-9a-f]*' "$RUNNER_HOME/b/daemon.log" | head -1 | awk '{print $NF}')"
R9_REPO="erpit-r9-$(date +%H%M%S)"
erpit_seed "$R9_REPO"
REPO="$R9_REPO"
printf '\nci-p3 R9 %s\n' "$(date -Is)" >> "$CLONE/README.md"
T0=$(date +%s)
( cd "$CLONE" && git add -A && git commit -qm "ci-p3 R9: the ghost" )
OID=$(git -C "$CLONE" rev-parse HEAD)
PUSH=$(git -C "$CLONE" push origin master 2>&1 | tail -3)
CID=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
echo "-- pushed $OID: candidate $CID"
for _ in $(seq 1 30); do grep -q 'no result: assignment refused: signature does not verify' "$RUNNER_HOME/b/daemon.log" && break; sleep 5; done
echo "-- b's first refusal after $(( $(date +%s) - T0 )) s"
check "the ship marked b refused" "refused" "$(wait_runner_state "$DAEMON_B" refused 30)"
st=$(wait_cand "$CID" '%passed|%failed|%unknown' 3000)
T1=$(date +%s)
echo "-- candidate $st after $(( T1 - T0 )) s"
check "candidate %passed" '%passed' "$st"
job_attempts "$CID" | sed 's/^/   /'
check "eight job attempts passed" "8" "$(job_attempts "$CID" | grep -c ' %passed ')"
check "no attempt closed at a deadline (no ~h1 wait)" "0" "$(for a in $(cand_attempt_ids "$CID"); do att_reason "$a"; done | grep -c 'no result arrived before the deadline')"
check "push to verdict under forty minutes" "yes" "$([ $(( T1 - T0 )) -lt 2400 ] && echo yes || echo "no ($(( T1 - T0 )) s)")"
check "landed: master = the candidate" "$OID" "$(repo_master)"
n_refused=$(grep -c 'no result: assignment refused: signature does not verify' "$RUNNER_HOME/b/daemon.log")
echo "-- daemon b: $n_refused refusal(s) logged"
check "b refused exactly once: offered nothing after its refusal (N ordinary polls did not restore it)" "1" "$n_refused"
check "b's attempts on this candidate: exactly one, and it is re-offered" "1 %reoffered" "$(for a in $(cand_attempt_ids "$CID"); do [ "$(att_daemon "$a")" = "$DAEMON_B" ] && att_status "$a"; done | sort | uniq -c | awk '{print $1, $2}' | tr '\n' ' ' | sed 's/ $//')"
check "b kept polling (last-seen within stale-after) yet reads refused" "refused" "$(runner_state "$DAEMON_B")"
check "b's last-seen is fresh (it polls; the bearer is fine)" "yes" "$([ $(( $(date +%s) - $(runner_field "$DAEMON_B" .lastSeen) )) -lt 120 ] && echo yes || echo no)"
echo "-- README recovery: the refused record is revoked and removed, b re-enrolls with a fresh token and no pinned override"
retire_daemon b
start_daemon b 1
check "the re-enrolled b reads healthy" "healthy" "$(wait_runner_state "$DAEMON_B" healthy 30)"
check "bind a away so b takes the next job -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":[\"somewhere-else\"]}")")"
sync_clone
mkdir -p "$CLONE/.github/workflows"; rm -f "$CLONE/.github/workflows/"*.yml; cp "$ROOT/desk/tests/ci/fixture-pass.yml" "$CLONE/.github/workflows/"
printf 'r9 after %s\n' "$(date -Is)" >> "$CLONE/README.md"
push_commit "ci-p3 R9: after re-enrollment"
AID=$(wait_for_attempt "$CID" pass 180)
check "the next job ran on the re-enrolled b" "$DAEMON_B" "$(att_daemon "$AID")"
check "and passed" '%passed' "$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)"
ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":null}" >/dev/null
retire_daemon b
echo "export R9_REPO=$R9_REPO; export R9_CID=$CID; export R9_T=$(( T1 - T0 ))" > "$TMP/r9.env"
end_row R9
fi
if [ "$which_row" = r10 ]; then
row "R10: the silent runner — SIGSTOP the daemon mid-job: re-offered at timeout-minutes + 2 min, completes on a; SIGCONT: the late result is refused 'attempt is closed'"
start_daemon b 1
wait_live_only "$DAEMON_A" "$DAEMON_B" || { echo "R10: not run"; exit 1; }
check "bind a away -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":[\"somewhere-else\"]}")")"
sync_clone; set_workflows fixture-silent.yml
printf 'r10 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R10: a job whose runner goes silent"
S1=$(wait_for_attempt "$CID" silent 120)
check "the job runs on b" "$DAEMON_B" "$(att_daemon "$S1")"
for _ in $(seq 1 60); do [ "$(att_events "$S1")" -gt 0 ] 2>/dev/null && break; sleep 2; done
check "the attempt has events (act started)" "yes" "$([ "$(att_events "$S1")" -gt 0 ] && echo yes || echo no)"
check "the plan carried timeout-minutes 1 (the deadline is 3 min, not ~h1)" "1" "$(jq_of "$(ci_get "/candidate/$CID")" '.candidate.plan[] | select(.id == "silent") | .timeoutMinutes')"
BPID=$(cat "$RUNNER_HOME/b/pid")
kill -STOP "$BPID" && echo "-- SIGSTOP to daemon b (pid $BPID) at $(date -Is)"
T0=$(date +%s)
check "unbind a -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":null}")")"
sleep 60
check "one minute on, the attempt is still running (the deadline has not come)" '%running' "$(att_status "$S1")"
st=$(wait_att "$S1" '%reoffered|%infrastructure-error|%passed' 240)
echo "-- $st after $(( $(date +%s) - T0 )) s"
check "the attempt is re-offered after the timeout + 2 min, not closed" '%reoffered' "$st"
check_contains "with the silent reason" "runner went silent; re-offered" "$(att_reason "$S1")"
S2=""; for _ in $(seq 1 60); do for a in $(cand_attempt_ids "$CID"); do [ "$a" != "$S1" ] && [ "$(att_kind "$a")" = "%job" ] && S2="$a"; done; [ -n "$S2" ] && break; sleep 2; done
check "the fresh attempt went to a" "$DAEMON_A" "$(att_daemon "$S2")"
check "it passed on a" '%passed' "$(wait_att "$S2" '%passed|%failed|%infrastructure-error' 400)"
check "the candidate passed" '%passed' "$(wait_cand "$CID" '%passed|%failed|%unknown' 60)"
check "landed: master = the candidate" "$OID" "$(repo_master)"
kill -CONT "$BPID" && echo "-- SIGCONT to daemon b at $(date -Is); its act finishes the sleep and posts"
for _ in $(seq 1 120); do grep -q "$S1.*result .* POST\|$S1.*abandon POST\|$S1.*no result" "$RUNNER_HOME/b/daemon.log" && break; sleep 2; done
echo "-- b: $(grep "$S1" "$RUNNER_HOME/b/daemon.log" | grep -E 'result .* POST|abandon POST|no result' | tail -1 | cut -c1-160)"
check "b's late result was refused 409 attempt is closed" "1" "$(grep "$S1" "$RUNNER_HOME/b/daemon.log" | grep -cE '(result|abandon).* (POST -> )?409.*attempt is closed|409 attempt is closed')"
check "the late result did not change the attempt" '%reoffered' "$(att_status "$S1")"
check "b's events after the re-offer were refused, not counted (events unchanged)" "yes" "$([ "$(att_events "$S1")" -lt 20 ] && echo yes || echo no)"
retire_daemon b
end_row R10
fi
if [ "$which_row" = r11 ]; then
row "R11: N+1 jobs on capacity N at t+0 — four jobs on daemon a alone (capacity 3): all four complete"
retire_daemon b 2>/dev/null
wait_live_only "$DAEMON_A" || { echo "R11: not run"; exit 1; }
check "daemon a's capacity is 3" "3" "$(runner_field "$DAEMON_A" .capacity)"
sync_clone; set_workflows fixture-four.yml
printf 'r11 %s\n' "$(date -Is)" >> README.md
T0=$(date +%s)
push_commit "ci-p3 R11: four jobs at t+0"
st=$(wait_cand "$CID" '%passed|%failed|%unknown' 900)
echo "-- candidate $st after $(( $(date +%s) - T0 )) s"
check "candidate %passed" '%passed' "$st"
job_attempts "$CID" | sed 's/^/   /'
check "four job attempts passed" "4" "$(job_attempts "$CID" | grep -c ' %passed ')"
check "every job attempt ran on a" "4" "$(job_attempts "$CID" | grep -c " $DAEMON_A$")"
check "no attempt closed at a deadline" "0" "$(for a in $(cand_attempt_ids "$CID"); do att_reason "$a"; done | grep -c 'no result arrived before the deadline')"
check "a's running set is empty after" "{}" "$(daemon_field "$DAEMON_A" running | tr -d '\n')"
check "landed: master = the candidate" "$OID" "$(repo_master)"
end_row R11
fi
if [ "$which_row" = r11a ]; then
row "R11a: labels — big-mem lands only on the big-mem daemon, a plain job on either; gpu waits with the reason on the row and in the feed until a gpu daemon enrolls"
start_daemon b 1 '"big-mem"'
wait_live_only "$DAEMON_A" "$DAEMON_B" || { echo "R11a: not run"; exit 1; }
check "the panel lists b's label" '["big-mem"]' "$(runner_field "$DAEMON_B" '.labels | tojson')"
STREAM="$TMP/r11a-stream.txt"
PID=$(channel_open "/ci/repository/$REPO" "$STREAM" 600) || exit 1
sync_clone; set_workflows fixture-labels.yml
printf 'r11a %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R11a: labels"
BIG=$(wait_for_attempt "$CID" big 120); PLAIN=$(wait_for_attempt "$CID" plain 120)
check "the big-mem job ran on b" "$DAEMON_B" "$(att_daemon "$BIG")"
check "and passed" '%passed' "$(wait_att "$BIG" '%passed|%failed|%infrastructure-error' 240)"
check "the plain job ran on a or b" "yes" "$(d=$(att_daemon "$PLAIN"); { [ "$d" = "$DAEMON_A" ] || [ "$d" = "$DAEMON_B" ]; } && echo yes || echo "no ($d)")"
check "and passed" '%passed' "$(wait_att "$PLAIN" '%passed|%failed|%infrastructure-error' 240)"
sleep 5
check "the gpu job has no attempt (no daemon has gpu)" "" "$(att_of_job "$CID" gpu)"
check "the candidate is pending with the reason" "no runner has labels [gpu]" "$(cand_reason "$CID" | tr -d "'")"
check "the candidates route carries it as verdictReason" "no runner has labels [gpu]" "$(jq_of "$(ci_get "/repository/$REPO/candidates")" ".candidates[] | select(.id == \"$CID\") | .verdictReason")"
check "the feed carried it (a fact with the reason)" "yes" "$(wait_fact "$STREAM" "select(.kind == \"candidate\" and .id == \"$CID\") | .patch.verdictReason == \"no runner has labels [gpu]\"" 30 && echo yes || echo no)"
check "the job rows carry runs-on from the plan" '["gpu","self-hosted"]' "$(body_of "$(ci_get "/candidate/$CID")" | jq -c '.candidate.plan[] | select(.id == "gpu") | .runsOn')"
echo "-- a gpu daemon enrolls"
start_daemon c 1 '"gpu"'
T0=$(date +%s)
GPU=$(wait_for_attempt "$CID" gpu 60)
echo "-- the gpu job was assigned $(( $(date +%s) - T0 )) s after c enrolled"
check "the gpu job landed within one poll window of c enrolling" "yes" "$([ -n "$GPU" ] && [ $(( $(date +%s) - T0 )) -le 30 ] && echo yes || echo no)"
check "on c" "$DAEMON_C" "$(att_daemon "$GPU")"
check "it passed" '%passed' "$(wait_att "$GPU" '%passed|%failed|%infrastructure-error' 240)"
check "the candidate passed" '%passed' "$(wait_cand "$CID" '%passed|%failed|%unknown' 60)"
check "the reason cleared" "'landed'" "$(cand_reason "$CID")"
check "landed: master = the candidate" "$OID" "$(repo_master)"
kill "$PID" 2>/dev/null
retire_daemon b; retire_daemon c
end_row R11a
fi
if [ "$which_row" = r11b ]; then
row "R11b: repository binding — b bound to ci-p3 is never selected for another repository, survives b's restart, and takes work once unbound in the panel"
start_daemon b 1
wait_live_only "$DAEMON_A" "$DAEMON_B" || { echo "R11b: not run"; exit 1; }
check "bind b to $REPO -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_B\",\"repos\":[\"$REPO\"]}")")"
check "the panel shows the binding" "[\"$REPO\"]" "$(runner_field "$DAEMON_B" '.repos | tojson')"
OTHER="ci-p3-other-$(date +%H%M%S)"
"$api" POST /repositories "{\"name\":\"$OTHER\",\"publicRead\":true}" | cut -c1-30
OCLONE="$TMP/clone-$OTHER"; rm -rf "$OCLONE"; mkdir -p "$OCLONE"
( cd "$OCLONE" && git init -q -b master . && git config user.name r11b && git config user.email r11b@example && git config http.cookieFile "$JAR" && git remote add origin "$URL/git/$OTHER" && mkdir -p .github/workflows && cp "$ROOT/desk/tests/ci/fixture-pass.yml" .github/workflows/ && echo seed > README.md && git add -A && git commit -qm seed && git push -q origin master 2>&1 | tail -1 )
"$api" POST "/repository/$OTHER/branches/default" '{"name":"master"}' >/dev/null
ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$OTHER\",\"ref\":\"refs/heads/master\",\"protected\":true}" | cut -c1-20
# a is made busy: capacity 1 (the config; capacity rides every poll) and
# a slow job of ci-p3 fills it
"$P1/runner.sh" stop a >/dev/null; "$P1/runner.sh" config a 1 >/dev/null; "$P1/runner.sh" start a | head -1
sleep 3
check "a reports capacity 1" "1" "$(runner_field "$DAEMON_A" .capacity)"
sync_clone; set_workflows fixture-slow.yml
printf 'r11b %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R11b: a is busy"
SLOW=$(wait_for_attempt "$CID" slow 120)
echo "-- the slow job runs on $(att_daemon "$SLOW")"
SLOW_D=$(att_daemon "$SLOW")
[ "$SLOW_D" = "$DAEMON_B" ] && { echo "-- b took the slow job (both eligible; b ran fewer): b is busy, a is free — bind the roles the other way"; ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_B\",\"repos\":null}" >/dev/null; ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":[\"$REPO\"]}" >/dev/null; BOUND="$DAEMON_A"; BOUND_NAME=a; FREE="$DAEMON_B"; FREE_NAME=b; } || { BOUND="$DAEMON_B"; BOUND_NAME=b; FREE="$DAEMON_A"; FREE_NAME=a; }
echo "-- bound daemon: $BOUND_NAME ($BOUND); the busy one: $FREE_NAME ($FREE)"
( cd "$OCLONE" && printf 'r11b %s\n' "$(date -Is)" >> README.md && git add -A && git commit -qm "ci-p3 R11b: the other repository" )
OOID=$(git -C "$OCLONE" rev-parse HEAD)
OPUSH=$(git -C "$OCLONE" push origin master 2>&1 | tail -3)
OCID=$(printf '%s' "$OPUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
echo "-- pushed to $OTHER: candidate $OCID"
sleep 40
check "forty seconds on, the other repository's candidate has no attempt (the bound daemon is idle and never selected; the pool daemon is full)" "0" "$(cand_attempt_ids "$OCID" | wc -l)"
check "the bound daemon is idle (running 0)" "0" "$(runner_field "$BOUND" .running)"
"$P1/runner.sh" stop "$BOUND_NAME" >/dev/null; "$P1/runner.sh" start "$BOUND_NAME" | head -1; sleep 3
check "after the restart the binding is intact (ship state)" "[\"$REPO\"]" "$(runner_field "$BOUND" '.repos | tojson')"
check "still no attempt for the other repository" "0" "$(cand_attempt_ids "$OCID" | wc -l)"
check "unbind in the panel -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$BOUND\",\"repos\":null}")")"
T0=$(date +%s)
OA=""; for _ in $(seq 1 30); do OA=$(cand_attempt_ids "$OCID" | tail -1); [ -n "$OA" ] && break; sleep 2; done
echo "-- the other repository's first attempt $OA on $(att_daemon "$OA") after $(( $(date +%s) - T0 )) s"
check "the unbound daemon took the other repository's plan" "$BOUND" "$(att_daemon "$OA")"
check "the other repository's candidate passed" '%passed' "$(REPO=$OTHER wait_cand "$OCID" '%passed|%failed|%unknown' 300)"
check "and landed" "$OOID" "$("$api" GET "/repository/$OTHER" | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print([r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])')"
check "the slow job on $FREE_NAME finished too" '%passed' "$(wait_att "$SLOW" '%passed|%failed|%infrastructure-error' 400)"
ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":null}" >/dev/null
"$P1/runner.sh" stop a >/dev/null; "$P1/runner.sh" config a "$DAEMON_CAPACITY" >/dev/null; "$P1/runner.sh" start a | head -1
retire_daemon b
end_row R11b
fi
[ "$NFAIL" = 0 ]
