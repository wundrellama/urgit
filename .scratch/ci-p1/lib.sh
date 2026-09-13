#!/bin/bash
# Shared readers and drivers for the P1 rows (source after env.sh). Every
# check prints PASS/FAIL against the brief's expectation and the row's
# verdict is the conjunction; nothing is typed by hand.
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
source "$P0/lib.sh"
set +e   # rows check every outcome explicitly; a refused push or an empty grep is data
# P0's dojo_value reads 8 pane lines; a candidate with nine attempts prints
# its id list over more, so the P1 reader takes the last 60
dojo_value() {
  "$dojo" "$1" "${2:-60}" 60 | awk -v p="~$SHIP:dojo>" '
    index($0, "> ") == 1 { f = 1; buf = ""; next }
    index($0, p) == 1    { f = 0; next }
    f { buf = buf $0 "\n" }
    END { printf "%s", buf }'
}
api="$P0/api.sh"; dojo="$P0/dojo.sh"; poke="$P0/poke.sh"
REPO="${REPO:-ci-p1}"
CLONE="$TMP/clone-$REPO"
DK="docker --host unix://$DOCKER_SOCK"
# ---- verdicts ----------------------------------------------------------------
ROW_FAIL=0; NPASS=0; NFAIL=0; PASSED=""; FAILED=""
check() {  # <name> <expected> <observed>
  if [ "$2" = "$3" ]; then echo "  $1: PASS (observed: $3)"; else echo "  $1: FAIL (observed: $3, expected: $2)"; ROW_FAIL=1; fi
}
check_contains() {  # <name> <needle> <haystack>
  case "$3" in *"$2"*) echo "  $1: PASS (observed contains: $2)";; *) echo "  $1: FAIL (observed: ${3:0:200}, expected to contain: $2)"; ROW_FAIL=1;; esac
}
check_not_contains() {  # <name> <needle> <haystack>
  case "$3" in *"$2"*) echo "  $1: FAIL (observed contains: $2)"; ROW_FAIL=1;; *) echo "  $1: PASS (observed does not contain: $2)";; esac
}
row() { ROW_FAIL=0; printf '\n##### %s  (%s)\n' "$*" "$(date -Is)"; }
end_row() { if [ "$ROW_FAIL" = 0 ]; then echo "$1: PASS"; NPASS=$((NPASS+1)); PASSED="$PASSED $1"; else echo "$1: FAIL"; NFAIL=$((NFAIL+1)); FAILED="$FAILED $1"; fi; }
# ---- ship readers --------------------------------------------------------------
one() { grep -oE "$1" | tail -1; }
cand_status() { dojo_value "status:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | one '^%[a-z-]+$'; }
cand_reason() { dojo_value "verdict-reason:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | tr -d '\n' | sed 's/^\[~ //; s/\]$//'; }
cand_object() { dojo_value "candidate:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | one '^(~|\[~ 0x[0-9a-f.]+\])$'; }
cand_plan_jobs() {  # the plan's [workflow id] pairs, one per line, sorted
  dojo_value "\`(list @t)\`(turn (need plan:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))) |=(j=job:ci (rap 3 ~[workflow.j '/' id.j])))" 120 \
    | tr -d '\n' | sed 's/^<|//; s/|>$//' | tr ' ' '\n' | grep -E '\.ya?ml/' | sort
}
cand_attempt_ids() { dojo_value "attempts:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" 120 | tr -d '\n' | grep -oE '0v[0-9a-v.]+'; }
att_status() { dojo_value "status:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '^%[a-z-]+$'; }
att_kind()   { dojo_value "kind:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '^%[a-z-]+$'; }
att_job()    { dojo_value "job:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | tr -d '\n' | sed "s/^\[~ '//; s/'\]$//"; }
# the dojo prints a quote inside a cord as \' ; the reader unescapes it
att_reason() { dojo_value "reason:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | tr -d '\n' | sed "s/^\[~ '//; s/'\]$//; s/\\\\'/'/g"; }
att_events() { dojo_value "events:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '^[0-9.]+$' | tr -d .; }
att_started() { dojo_value "started:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '^~[0-9.a-z]+$'; }
att_finished() { dojo_value "finished:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | tr -d '\n' | sed 's/^\[~ //; s/\]$//'; }
# attempts of a candidate as "kind job status" lines (skipped attempts included)
cand_attempts() { for a in $(cand_attempt_ids "$1"); do echo "$a $(att_kind "$a") $(att_job "$a") $(att_status "$a")"; done; }
# the attempt id of <cid> for job <job> with status not %skipped (newest first)
att_of_job() { for a in $(cand_attempt_ids "$1"); do [ "$(att_job "$a")" = "$2" ] && [ "$(att_kind "$a")" = "%job" ] && { echo "$a"; return; }; done; }
repo_master() { "$api" GET "/repository/$REPO" | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print([r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'; }
# the authenticated API's ref list (repository-json, unfiltered) and the
# [%x %repository @ ~] peek's (public-repository-json, where D9 filters)
repo_ref_names() { "$api" GET "/repository/$REPO" | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(" ".join(sorted(r["name"] for r in d["refs"])))'; }
peek_ref_names() {
  dojo_value "=/  j=json  .^(json %gx /=urgit=/repository/$REPO/json)  ?>  ?=([%o *] j)  =/  r=json  (need (~(get by p.j) 'refs'))  ?>  ?=([%a *] r)  \`(list @t)\`(turn p.r |=(x=json ?>(?=([%o *] x) =/(n=json (need (~(get by p.x) 'name')) ?>(?=([%s *] n) p.n)))))" \
    | tr -d '\n' | sed 's/^<|//; s/|>$//'
}
ls_remote() { git -C "$CLONE" ls-remote origin 2>/dev/null | awk '{print $2}' | sort | tr '\n' ' '; }
# wait_cand <cid> <status-regex> <seconds>: poll the candidate's status
# (a regex, so alternatives like '%passed|%failed' work; a `case` pattern
# from a variable would match its `|` literally)
wait_cand() {
  local s; for _ in $(seq 1 "$3"); do s=$(cand_status "$1"); if [[ "$s" =~ ^($2)$ ]]; then echo "$s"; return 0; fi; sleep 1; done
  echo "$s"; return 1
}
# wait_att <aid> <status-regex> <seconds>
wait_att() {
  local s; for _ in $(seq 1 "$3"); do s=$(att_status "$1"); if [[ "$s" =~ ^($2)$ ]]; then echo "$s"; return 0; fi; sleep 1; done
  echo "$s"; return 1
}
# wait_for_attempt <cid> <job> <seconds>: until the candidate has a job attempt for <job>
wait_for_attempt() {
  local a; for _ in $(seq 1 "$3"); do a=$(att_of_job "$1" "$2"); [ -n "$a" ] && { echo "$a"; return 0; }; sleep 2; done; return 1
}
# ---- git drivers ----------------------------------------------------------------
# sync_clone: the local clone at the ship's master
# every git driver refuses to run outside the clone: a missing clone must
# never turn the worktree itself into the pushed repository
in_clone() { [ -d "$CLONE/.git" ] && cd "$CLONE" && [ "$(git rev-parse --show-toplevel)" = "$CLONE" ]; }
sync_clone() { in_clone || { echo "lib.sh: no clone at $CLONE" >&2; return 1; }; git fetch -q origin master && git reset -q --hard FETCH_HEAD; }
# set_workflows <file...>: .github/workflows holds exactly these fixtures
set_workflows() {
  in_clone || { echo "lib.sh: no clone at $CLONE" >&2; return 1; }
  rm -rf "$CLONE/.github/workflows"; mkdir -p "$CLONE/.github/workflows"
  for f in "$@"; do cp "$ROOT/desk/tests/ci/$f" "$CLONE/.github/workflows/$f"; done
}
# push_commit <message>: commit the clone and push master; sets OID, CID, PUSH
push_commit() {
  in_clone || { echo "lib.sh: no clone at $CLONE" >&2; return 1; }
  git add -A && git commit -qm "$1" --allow-empty
  OID=$(git rev-parse HEAD)
  PUSH=$(git push origin master 2>&1 | tail -3 || true)
  CID=$(printf '%s' "$PUSH" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //' || true)
  echo "-- pushed $OID: ${CID:+staged as candidate $CID}${CID:-$(printf '%s' "$PUSH" | tail -1)}"
}
# ---- daemon readers -----------------------------------------------------------------
# the daemon's log is appended across restarts: a row reads the current
# run only, from the last startup banner on
runner_log() { awk '/^urgit-runner: daemon /{buf=""} {buf=buf $0 "\n"} END{printf "%s", buf}' "$RUNNER_HOME/$1/daemon.log" 2>/dev/null; }
