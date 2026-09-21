#!/bin/bash
# usage: r18.sh
# R18 (close-out T8, the dogfood row), in two parts.
#   The deletion case first (the finding R18's third try made, T8b): a
#   candidate passes and lands on a fixture repository; the repository
#   is DELETEd, re-created under the same name, seeded and CI-protected;
#   a push of the SAME oid must be STAGED as a fresh candidate, never
#   landed — %urgit-ci's records under the name (its candidates and
#   attempts, the protection, the policy, the credentials, the name in a
#   daemon's binding) went with the repository (%urgit's delete pokes
#   %repository-deleted). Before the fix a passed candidate of the
#   deleted repository answered the eligibility peek %.y and the same oid
#   landed unstaged. RED: the mutant that skips the cleanup
#   (r-mutants.sh R18) — the push lands unstaged.
#   Then the dogfood row as written: this tree pushed to the ship's own
# `urgit` repository with CI required. The candidate plans the three jobs
# of .github/workflows/urgit.yml (go-test, fe-test, hoon-vectors) and all
# three land %passed; the hoon-vectors log carries every vector's line
# and `vectors: passed=23 of=23`, no failures line. RED first: a push
# whose one difference from the seed is a broken vector fixture (a wrong
# expected reason string in desk/gen/ci-plan-vector.hoon) -> hoon-vectors
# fails, the candidate is %failed with the vector's name in its log,
# master unmoved; then this tree's HEAD lands, master = HEAD exactly.
# The seed is HEAD's parent, so both pushes are real candidates (the RED
# commit sits on HEAD, on a side branch of the clone) and the landed
# commit is this tree's own HEAD. Needs p3-setup (daemon a at capacity
# 3: the three jobs run together, the whole row is two CI rounds).
source "$(dirname "$0")/lib.sh"
TS=$(date +%H%M%S)
row "R18: a deleted repository's CI records go with it (the same oid is staged afresh in one re-created under the name); this tree's push to the ship's own urgit repository — three jobs planned, RED on a broken vector fixture, then all three land"
echo "-- the deletion case: a passed candidate must not outlive its repository"
D="ci-p3-r18-del-$TS"; DC="$TMP/clone-$D"
eligible() {  # <repo> <oid> -> %.y/%.n: the peek the push gate reads before staging
  dojo_value ".^(? %gx /=urgit-ci=/eligible/(scot %t '$1')/(scot %t 'refs/heads/master')/(scot %t '$2')/noun)" | one '^%\.[yn]$'
}
seed_del_repo() {  # create $D, seed it from $DC (a fresh clone the first time), default branch master, CI required
  check "$1: repository $D created -> 201" "201" "$(status_of "$("$api" POST /repositories "{\"name\":\"$D\",\"publicRead\":true}")")"
  ( cd "$DC" && git push -q origin "$SEED_D:refs/heads/master" 2>&1 | tail -1 )
  "$api" POST "/repository/$D/branches/default" '{"name":"master"}' >/dev/null
  check "$1: the seed landed (master = $SEED_D)" "$SEED_D" "$(REPO=$D repo_master)"
  check "$1: CI required -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$D\",\"ref\":\"refs/heads/master\",\"protected\":true}")")"
}
rm -rf "$DC"; mkdir -p "$DC"; cd "$DC"; git init -q -b master .; git config user.name r18; git config user.email r18@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$D"; mkdir -p .github/workflows; cp "$ROOT/desk/tests/ci/fixture-pass.yml" .github/workflows/
echo seed > README.md; git add -A; git commit -qm seed; SEED_D=$(git rev-parse HEAD)
printf 'r18 %s\n' "$(date -Is)" >> README.md; git add -A; git commit -qm "ci-p3 R18: the oid that passes, then outlives its repository"; X=$(git rev-parse HEAD)
seed_del_repo "before"
check "a credential set on it -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-credential\",\"repo\":\"$D\",\"name\":\"DELME\",\"value\":\"r18-credential-$TS\",\"scope\":\"job\",\"envs\":[]}")")"
check "daemon a bound to it -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":[\"$D\"]}")")"
check "a's binding names it" "1" "$(runner_field "$DAEMON_A" '.repos' | grep -cF "$D")"
PUSH=$(git push origin "$X:refs/heads/master" 2>&1 | tail -3); CID1=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
check "the push of $X was staged" "yes" "$([ -n "$CID1" ] && echo yes || echo no)"
check "its candidate passed" '%passed' "$(wait_cand "$CID1" '%passed|%failed|%unknown' 300)"
check "and landed: master = $X" "$X" "$(for _ in $(seq 1 30); do [ "$(REPO=$D repo_master)" = "$X" ] && break; sleep 2; done; REPO=$D repo_master)"
check "the eligibility peek answers %.y for the landed oid (a passed candidate exists)" '%.y' "$(eligible "$D" "$X")"
T_DEL=$(dojo_value "now" | one '^~[0-9.a-z]+$')
check "DELETE /repository/$D -> 200" "200" "$(status_of "$("$api" DELETE "/repository/$D")")"
check "the repository is gone from %urgit" "404" "$(status_of "$("$api" GET "/repository/$D")")"
sleep 2
check "the eligibility peek answers %.n now (the candidate went with the repository)" '%.n' "$(eligible "$D" "$X")"
check "its candidate record is gone (scry)" "~" "$(dojo_value ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID1/noun)" | tr -d '\n')"
check "no attempt of it remains (scry)" "0" "$(dojo_value "(lent (skim ~(tap by .^((map @uv attempt:ci) %gx /=urgit-ci=/attempts/noun)) |=([* a=attempt:ci] =(candidate.a $CID1))))" | one '^[0-9.]+$' | tr -d .)"
check "no assignment of it remains (scry)" "0" "$(dojo_value "(lent (skim ~(tap by .^((map @uv assignment:ci) %gx /=urgit-ci=/assignments/noun)) |=([* a=assignment:ci] =(candidate.a $CID1))))" | one '^[0-9.]+$' | tr -d .)"
check "the ref is no longer CI-protected (scry)" '%.n' "$(REPO=$D ci_protected refs/heads/master)"
check "its credential is gone (scry)" "0" "$(dojo_value "(lent .^((list [name=@t scope=?(%job %env) envs=(set @t) created=@da]) %gx /=urgit-ci=/credential-names/(scot %t '$D')/noun))" | one '^[0-9.]+$' | tr -d .)"
check "a's binding no longer names it (bound to nothing, not the pool)" "[]" "$(runner_field "$DAEMON_A" '.repos | tostring')"
check "unbind a -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-daemon-repos\",\"id\":\"$DAEMON_A\",\"repos\":null}")")"
seed_del_repo "re-created"
PUSH=$(git push origin "$X:refs/heads/master" 2>&1 | tail -3); CID2=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
echo "-- the same oid $X pushed into the re-created repository: $(printf '%s' "$PUSH" | tail -1 | cut -c1-120)"
check "the push of the SAME oid was STAGED as a fresh candidate, not landed" "yes" "$([ -n "$CID2" ] && echo yes || echo no)"
[ -z "$CID2" ] && echo "R18 RED: a push after the repository's deletion landed unstaged — the deleted repository's passed candidate answered the eligibility peek"
check "the candidate is a fresh record, created after the deletion (its id is the same sham of repo, ref, head and base)" '%.y' "$(dojo_value "(gth created:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CID2/noun)) $T_DEL)" | one '^%\.[yn]$')"
check "master unmoved at the push (= the seed)" "$SEED_D" "$(REPO=$D repo_master)"
check "the fresh candidate passes" '%passed' "$(wait_cand "$CID2" '%passed|%failed|%unknown' 300)"
check "and lands: master = $X" "$X" "$(for _ in $(seq 1 30); do [ "$(REPO=$D repo_master)" = "$X" ] && break; sleep 2; done; REPO=$D repo_master)"
echo "-- the dogfood row: this tree's push to the ship's own urgit repository"
UREPO="urgit"
U="$TMP/clone-urgit"
HEAD_SHA=$(git -C "$ROOT" rev-parse HEAD); SEED_SHA=$(git -C "$ROOT" rev-parse HEAD~1)
echo "-- this tree: HEAD $HEAD_SHA (seed = its parent $SEED_SHA); $(git -C "$ROOT" rev-list --count HEAD) commits"
check "the tree carries the workflow and both composite actions" "3" "$(cd "$ROOT" && git ls-tree -r --name-only HEAD | grep -cE '^\.github/(workflows/urgit\.yml|actions/(urbit-toolchain|boot-fake-ship)/action\.yml)$')"
# a re-run: the repository an earlier run left is removed first (the
# product drops %urgit-ci's records with it, the deletion case above), so
# the row always starts from the ship's own empty `urgit`
if [ "$(status_of "$("$api" GET "/repository/$UREPO")")" = 200 ]; then
  echo "-- a re-run: an earlier run's $UREPO removed first (DELETE -> $(status_of "$("$api" DELETE "/repository/$UREPO")"))"
fi
r=$("$api" POST /repositories "{\"name\":\"$UREPO\",\"publicRead\":true}")
check "repository $UREPO created -> 201" "201" "$(status_of "$r")"
rm -rf "$U"; git clone -q "$ROOT" "$U" 2>&1 | tail -1
cd "$U"; git config user.name r18; git config user.email r18@example; git config http.cookieFile "$JAR"
git remote add ship "$URL/git/$UREPO"
check "the clone's HEAD is this tree's HEAD" "$HEAD_SHA" "$(git rev-parse HEAD)"
git push -q ship "$SEED_SHA:refs/heads/master" 2>&1 | tail -1
"$api" POST "/repository/$UREPO/branches/default" '{"name":"master"}' | cut -c1-30
check "seed landed: master = HEAD's parent" "$SEED_SHA" "$(REPO=$UREPO repo_master)"
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$UREPO\",\"ref\":\"refs/heads/master\",\"protected\":true}")
check "CI required on master -> 200" "200" "$(status_of "$r")"
check "CI-protected (scry)" '%.y' "$(REPO=$UREPO ci_protected refs/heads/master)"
push_to_master() {  # <commit>: push it to master, set CID
  PUSH=$(git push ship "$1:refs/heads/master" 2>&1 | tail -3)
  CID=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
  echo "-- pushed $1: ${CID:+staged as candidate $CID}${CID:-$(printf '%s' "$PUSH" | tail -1)}"
}
att_daemon_log() {  # <aid>: the daemon's saved act stream for the attempt (any daemon dir)
  /bin/ls "$RUNNER_HOME"/*/work/"$1".act.jsonl 2>/dev/null | head -1
}
echo "-- RED: this tree plus one broken vector fixture (a wrong expected reason in ci-plan-vector)"
git checkout -q -B r18-red "$HEAD_SHA"
sed -i "s/'job pass in pass.yml failed'/'job pass in pass.yml exploded'/" desk/gen/ci-plan-vector.hoon
check "the fixture is broken in exactly one place" "1" "$(grep -c "pass.yml exploded" desk/gen/ci-plan-vector.hoon)"
git add -A; git commit -qm "ci-p3 R18 RED: a wrong expected reason in ci-plan-vector"; RED_SHA=$(git rev-parse HEAD)
push_to_master "$RED_SHA"
check "the RED push was staged" "yes" "$([ -n "$CID" ] && echo yes || echo no)"
REDC="$CID"
for _ in $(seq 1 60); do [ "$(REPO=$UREPO cand_plan_jobs "$REDC" | wc -l)" = 3 ] && break; sleep 2; done
check "the plan is the workflow's three jobs" "urgit.yml/fe-test urgit.yml/go-test urgit.yml/hoon-vectors" "$(REPO=$UREPO cand_plan_jobs "$REDC" | tr '\n' ' ' | sed 's/ $//')"
T0=$(date +%s)
st=$(wait_cand "$REDC" '%passed|%failed|%unknown' 1200)
echo "-- the RED candidate $st after $(( $(date +%s) - T0 )) s"
check "the RED candidate failed" '%failed' "$st"
[ "$st" = '%passed' ] && echo "R18 RED: the broken vector fixture landed — hoon-vectors did not fail on a wrong expected reason"
HA=$(att_of_job "$REDC" hoon-vectors)
check "the hoon-vectors attempt failed" '%failed' "$(att_status "$HA")"
check_contains "the verdict names the job" "job hoon-vectors in urgit.yml failed" "$(cand_reason "$REDC")"
L=$(att_daemon_log "$HA")
check_contains "the job's log names the vector" "ci-plan-vector: FAIL" "$(grep -o 'ci-plan-vector: FAIL[^"]*' "$L" | head -1)"
check_contains "and lists it as the failure" "failures: ci-plan-vector" "$(grep -o 'failures: ci-plan-vector' "$L" | head -1)"
check_contains "with the generator's own count" "passed=30 of=31" "$(grep -o 'ci-plan-vector passed=30 of=31' "$L" | head -1)"
check "master unmoved (= the seed)" "$SEED_SHA" "$(REPO=$UREPO repo_master)"
echo "-- GREEN: this tree's HEAD"
git checkout -q "$HEAD_SHA"
push_to_master "$HEAD_SHA"
check "the push was staged" "yes" "$([ -n "$CID" ] && echo yes || echo no)"
GC="$CID"
for _ in $(seq 1 60); do [ "$(REPO=$UREPO cand_plan_jobs "$GC" | wc -l)" = 3 ] && break; sleep 2; done
check "the plan is the workflow's three jobs" "urgit.yml/fe-test urgit.yml/go-test urgit.yml/hoon-vectors" "$(REPO=$UREPO cand_plan_jobs "$GC" | tr '\n' ' ' | sed 's/ $//')"
T0=$(date +%s)
st=$(wait_cand "$GC" '%passed|%failed|%unknown' 1200)
echo "-- the candidate $st after $(( $(date +%s) - T0 )) s"
check "the candidate passed" '%passed' "$st"
for j in go-test fe-test hoon-vectors; do
  A=$(att_of_job "$GC" "$j")
  check "job $j passed" '%passed' "$(att_status "$A")"
done
HA=$(att_of_job "$GC" hoon-vectors); L=$(att_daemon_log "$HA")
check "the hoon-vectors log carries every vector's line (23)" "23" "$(grep -oE '[a-z-]+-vector: (%\.y|built and ran)' "$L" | sort -u | wc -l)"
check_contains "and the run's own count" "vectors: passed=23 of=23" "$(grep -o 'vectors: passed=23 of=23' "$L" | head -1)"
check_contains "ci-plan-vector's printed count beside its verdict" "ci-plan-vector: %.y (passed=31 of=31)" "$(grep -o 'ci-plan-vector: %\.y (passed=31 of=31)' "$L" | head -1)"
check "no failures line" "0" "$(grep -c 'failures: ' "$L")"
# act JSON-encodes a step's lines: a tab is the two characters \t there
GA=$(att_of_job "$GC" go-test); check_contains "the go-test log shows the daemon package ok" "urgit/runner/internal/daemon" "$(grep -oE 'ok  [^"]{0,4}urgit/runner/internal/daemon' "$(att_daemon_log "$GA")" | head -1)"
FA=$(att_of_job "$GC" fe-test); check_contains "the fe-test log shows fail 0" "fail 0" "$(grep -oE '(ℹ|#|u2139) fail 0' "$(att_daemon_log "$FA")" | head -1)"
wait_log "$HA" 60 >/dev/null
check "the hoon-vectors log is in the bucket with the ship's sha256" "yes" "$(KEY=$(att_log_key "$HA"); [ -n "$KEY" ] && "$store" get "$KEY" "$TMP/r18-hoon-log.jsonl" >/dev/null && [ "$(sha_of "$TMP/r18-hoon-log.jsonl")" = "$(att_log_sha "$HA")" ] && echo yes || echo no)"
check_contains "the bucket object carries the count too" "vectors: passed=23 of=23" "$(grep -o 'vectors: passed=23 of=23' "$TMP/r18-hoon-log.jsonl" | head -1)"
check "landed: master = this tree's HEAD" "$HEAD_SHA" "$(for _ in $(seq 1 30); do [ "$(REPO=$UREPO repo_master)" = "$HEAD_SHA" ] && break; sleep 2; done; REPO=$UREPO repo_master)"
check "%urgit-ci heard %landed" "'landed'" "$(cand_reason "$GC")"
end_row R18
[ "$NFAIL" = 0 ]
