#!/bin/bash
# usage: negatives.sh red|green|all [rows...]     (rows default: h9 … h15)
# The mutant RED phase for the negative rows H9–H15, ported from the opus
# branch's harness. One script rather than per-row files: the main table's
# per-row scripts share state built up by H3–H8, while each row here stages
# its own fresh commit so the two phases and the seven rows are independent.
#
#   red    mutants.sh apply -> rebuild.sh -> every row must FAIL (RED)
#   green  mutants.sh revert (git checkout --) -> rebuild.sh -> every row
#          must PASS (GREEN)
#   all    red, then green
#
# Every check prints `PASS (observed: …)` or `FAIL (observed: …, expected: …)`
# against the SPEC expectation; a row is PASS only when all its checks are.
# Needs oids.env from the main run (DAEMON/BEARER from h6.sh, BEARER_B from
# h9-13.sh, ATTEMPT from h6.sh) and the dojo prelude (`ci` bound by setup.sh).
source "$(dirname "$0")/env.sh"
source "$HERE/lib.sh"   # dojo_unit_cord; the helpers below override lib.sh's where names meet
set +e
source "$TMP/oids.env"
api="$HERE/api.sh"; dojo="$HERE/dojo.sh"
phase="${1:?usage: negatives.sh red|green|all [rows...]}"; shift || true
rows="${*:-h9 h10 h11 h12 h13 h14 h15}"
if [ "$phase" = all ]; then
  "$0" red $rows; r=$?
  "$0" green $rows; g=$?
  echo; echo "== negatives: red exit $r, green exit $g"
  exit $(( r || g ))
fi
case "$phase" in
  red)   want=FAIL; "$HERE/mutants.sh" apply  || exit 1; "$HERE/rebuild.sh" red   || exit 1 ;;
  green) want=PASS; "$HERE/mutants.sh" revert || exit 1; "$HERE/rebuild.sh" green || exit 1 ;;
  *) echo "usage: negatives.sh red|green|all [rows...]" >&2; exit 2 ;;
esac
echo "== build under test: $("$HERE/mutants.sh" status | tail -1); rows must $want"
for v in DAEMON BEARER BEARER_B ATTEMPT; do
  [ -n "${!v:-}" ] || { echo "negatives.sh: $v missing from $TMP/oids.env; run h6.sh and h9-13.sh first" >&2; exit 1; }
done

# ---- verdicts -------------------------------------------------------------
ROW_FAIL=0; NPASS=0; NFAIL=0; PASSED=""; FAILED=""
verdict() { if [ "$1" = "$2" ]; then echo "PASS (observed: $2)"; else echo "FAIL (observed: $2, expected: $1)"; fi; }
check() {  # <name> <expected> <observed>
  local v; v=$(verdict "$2" "$3"); echo "  $1: $v"; case "$v" in FAIL*) ROW_FAIL=1;; esac
}
check_prefix() {  # <name> <expected-prefix> <observed>
  if [ "${3:0:${#2}}" = "$2" ]; then echo "  $1: PASS (observed: $3)"; else echo "  $1: FAIL (observed: $3, expected: $2…)"; ROW_FAIL=1; fi
}
row() { ROW_FAIL=0; printf '\n##### %s\n' "$*"; }
end_row() {  # <row-name>
  if [ "$ROW_FAIL" = 0 ]; then echo "$1 [$phase]: PASS"; NPASS=$((NPASS+1)); PASSED="$PASSED $1"
  else echo "$1 [$phase]: FAIL"; NFAIL=$((NFAIL+1)); FAILED="$FAILED $1"; fi
}
has() { case " $rows " in *" $1 "*) return 0;; *) return 1;; esac; }

# ---- ship helpers -----------------------------------------------------------
one() { grep -oE "$1" | tail -1; }
cand_status() { "$dojo" "status:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" 60 8 | one '^%[a-z-]+$'; }
cand_object() { "$dojo" "candidate:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" 60 8 | one '^(~|\[~ 0x[0-9a-f.]+\])$'; }
att_status()  { "$dojo" "status:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" 60 8 | one '^%[a-z-]+$'; }
att_events()  { "$dojo" "events:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" 60 8 | one '^[0-9.]+$'; }
att_trust()   { "$dojo" "trust:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" 60 8 | one '^%[a-z-]+$'; }
liveness()    { "$dojo" '.^(? %gu /=urgit-ci=/$)' 60 8 | one '^%\.[yn]$'; }
# the unit cord read joined (lib.sh): a pane narrower than the URL makes
# the dojo pretty-print it over four lines and wrap the cord itself
sign_get()    { dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/sign-get/$1/$2/(scot %t 'cache.tar')/noun)" | one "^(~|\[~ '[^']*'\])$"; }
wait_live() {  # <%.y|%.n>
  for _ in $(seq 1 30); do [ "$(liveness)" = "$1" ] && return 0; sleep 2; done
  echo "negatives.sh: %urgit-ci liveness never became $1" >&2; return 1
}

# stage_commit <label>: a fresh commit on top of the ship's master, pushed
# from clone-a -> staged as a candidate. Sets OID and CID.
stage_commit() {
  cd "$TMP/clone-a" || return 1
  git fetch -q origin master && git checkout -q -B master FETCH_HEAD
  echo "$1 $(date +%s%N)" >> HISTORY.md
  git add HISTORY.md && git commit -qm "$1"
  OID=$(git rev-parse HEAD)
  local report; report=$(git push origin master 2>&1 | tail -3)
  CID=$(printf '%s' "$report" | grep -o 'staged as ci candidate 0v[0-9a-v.]*' | sed 's/.*candidate //')
  echo "-- staged $OID as candidate ${CID:-<none>}"
  [ -n "$CID" ] || echo "$report"
}
# materialize <cid>: %materialize by poke, wait for %urgit's answer
materialize() {
  "$dojo" ":urgit-ci &ci-action [%materialize $1]" 60 3 >/dev/null
  for _ in $(seq 1 20); do
    local c; c=$(cand_object "$1")
    case "$c" in '[~ 0x'*) echo "-- materialized: candidate=$c"; return 0;; esac
    sleep 1
  done
  echo "-- candidate $1 never materialized" >&2; return 1
}
# assign <cid> <daemon> [deadline] [bearer] [workflow] [job]: %assign by
# poke (P1 shape: kind %job, workflow file, job id, deadline), then take the
# assignment from the channel as the daemon (draining any older one,
# including P1's automatic plan assignments). Sets AID.
assign() {
  local cid="$1" daemon="$2" deadline="${3:-~}" bearer="${4:-$BEARER}" wf="${5:-fixture-pass.yml}" job="${6:-pass}"
  "$dojo" ":urgit-ci &ci-action [%assign $cid $daemon %job \`'$wf' \`'$job' $deadline]" 60 3 >/dev/null
  AID=""
  for _ in 1 2 3 4 5 6; do
    local out got_cid got_aid
    out=$("$api" GET "/ci/daemon/$daemon/assignment" "" "$bearer")
    got_cid=$(printf '%s' "$out" | grep -o '"candidate":"[^"]*"' | head -1 | cut -d'"' -f4)
    got_aid=$(printf '%s' "$out" | grep -o '"attempt":"[^"]*"' | head -1 | cut -d'"' -f4)
    [ -n "$got_cid" ] || { echo "-- channel answered: ${out:0:60}"; break; }
    if [ "$got_cid" = "$cid" ]; then AID="$got_aid"; break; fi
    echo "-- drained an older assignment for $got_cid (attempt $got_aid)"
  done
  echo "-- assigned $cid -> attempt ${AID:-<none>}"
}
# relay <aid> <workflow> <job> <oid> <tag>: real act on a checkout of <oid>,
# every --json line POSTed to the attempt's event route with the bearer
relay() {
  local aid="$1" wf="$2" job="$3" oid="$4" tag="$5" rc=0 n=0
  cd "$TMP/clone-a" && git checkout -q "$oid"
  act push -W ".github/workflows/$wf.yml" -j "$job" -P ubuntu-latest=catthehacker/ubuntu:act-latest \
    --network bridge --json --pull=false > "$TMP/$tag-act.log" 2> "$TMP/$tag-act.err" || rc=$?
  : > "$TMP/$tag-relay.log"
  while IFS= read -r line; do
    printf '%s' "$line" > "$TMP/$tag-line.json"
    "$api" POST "/ci/attempt/$aid/event" "@$TMP/$tag-line.json" "$BEARER" >> "$TMP/$tag-relay.log"
    n=$((n+1))
  done < "$TMP/$tag-act.log"
  git checkout -q master
  echo "-- act exit=$rc, relayed $n lines, $(grep -o '"jobResult":"[a-z]*"' "$TMP/$tag-act.log" | tail -1); last answer: $(grep . "$TMP/$tag-relay.log" | tail -n1 | cut -c1-70)"
}
# push_to <oid> <ref>: prints `ok`, `rejected: <reason>` or `error: <line>`
push_to() {
  cd "$TMP/clone-a" || return 1
  local out; out=$(git push origin "$1:$2" 2>&1)
  if printf '%s' "$out" | grep -q 'remote rejected'; then
    printf 'rejected: %s\n' "$(printf '%s' "$out" | grep 'remote rejected' | head -1 | sed 's/^[^(]*(\(.*\))[^)]*$/\1/')"
  elif printf '%s' "$out" | grep -qE '^ +[0-9a-f]+\.\.[0-9a-f]+ |\* \[new branch\]|Everything up-to-date'; then
    echo ok
  else
    printf 'error: %s\n' "$(printf '%s' "$out" | tail -1)"
  fi
}
OUTAGE='rejected: ci: %urgit-ci is not running; protected-ref writes are refused until it is'
master_oid() { "$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print([r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'; }

# ---- rows ---------------------------------------------------------------------
if has h9; then
row "H9 [$phase]: /result success with no jobResult event -> 409; candidate stays %pending"
stage_commit "$phase-h9"; materialize "$CID"; assign "$CID" "$DAEMON"
out=$("$api" POST "/ci/attempt/$AID/result" '{"job-result":"success"}' "$BEARER")
echo "POST /result success -> ${out:0:120}"
check "status" 409 "${out%% *}"
check "candidate" '%pending' "$(cand_status "$CID")"
end_row H9
fi

if has h10; then
row "H10 [$phase]: deadline passes with no result -> attempt %infrastructure-error, candidate %unknown, push -> ng"
stage_commit "$phase-h10"; materialize "$CID"; assign "$CID" "$DAEMON" '`~s20'
echo "-- before the deadline: attempt $(att_status "$AID"), candidate $(cand_status "$CID"); waiting 28 s"
sleep 28
check "attempt" '%infrastructure-error' "$(att_status "$AID")"
check "candidate" '%unknown' "$(cand_status "$CID")"
p=$(push_to "$OID" refs/heads/master); echo "push $OID -> master: $p"
check "push" rejected "${p%%:*}"
end_row H10
fi

if has h11; then
row "H11 [$phase]: fixture-fail under real act -> jobResult failure -> candidate %failed, push -> ng"
stage_commit "$phase-h11"; materialize "$CID"; assign "$CID" "$DAEMON" '~' "$BEARER" fixture-fail.yml fail
relay "$AID" fixture-fail fail "$OID" "neg-$phase-h11"
out=$("$api" POST "/ci/attempt/$AID/result" '{"job-result":"failure"}' "$BEARER")
echo "POST /result failure -> ${out:0:120}"
check "result accepted" 200 "${out%% *}"
check "candidate" '%failed' "$(cand_status "$CID")"
p=$(push_to "$OID" refs/heads/master); echo "push $OID -> master: $p"
check "push" rejected "${p%%:*}"
end_row H11
fi

if has h12; then
row "H12 [$phase]: an event for daemon A's attempt with daemon B's bearer -> 401; attempt unaffected"
stage_commit "$phase-h12"; materialize "$CID"; assign "$CID" "$DAEMON"
line='{"job":"x/y","jobID":"y","time":"2026-09-12T18:00:00-05:00","msg":"from the wrong daemon"}'
out=$("$api" POST "/ci/attempt/$AID/event" "$line" "$BEARER_B")
echo "POST event with B's bearer -> ${out:0:120}"
check "status" 401 "${out%% *}"
check "events on A" 0 "$(att_events "$AID")"
out=$("$api" POST "/ci/attempt/$AID/event" "$line" "$BEARER")
echo "control: the same line with A's bearer -> ${out%% *}"
end_row H12
fi

if has h13; then
row "H13 [$phase]: a 65 KiB event line -> 413; attempt unaffected"
stage_commit "$phase-h13"; materialize "$CID"; assign "$CID" "$DAEMON"
before=$(att_events "$AID")
big="$TMP/neg-$phase-h13-big.json"
python3 -c 'import json,sys; sys.stdout.write(json.dumps({"job":"j","jobID":"j","time":"2026-09-12T17:03:50-05:00","msg":"a"*66560}))' > "$big"
out=$("$api" POST "/ci/attempt/$AID/event" "@$big" "$BEARER")
echo "POST a $(wc -c < "$big")-byte line -> ${out:0:120}"
check "status" 413 "${out%% *}"
check "events unchanged" "$before" "$(att_events "$AID")"
end_row H13
fi

if has h14; then
row "H14 [$phase]: %urgit-ci stopped (|rein) -> a push to the CI-protected ref is refused with the outage reason, an unprotected ref too; restarted -> the unprotected push lands and the protected one is staged (P1: the passed OID was landed by the ship itself, D14)"
stage_commit "$phase-h14"; materialize "$CID"; assign "$CID" "$DAEMON"
relay "$AID" fixture-pass pass "$OID" "neg-$phase-h14"
out=$("$api" POST "/ci/attempt/$AID/result" '{"job-result":"success"}' "$BEARER")
echo "POST /result success -> ${out%% *}; candidate $(cand_status "$CID")"
sleep 3
check "landed by the ship (D14)" "$OID" "$(master_oid)"
cd "$TMP/clone-a" && echo "probe $(date +%s%N)" >> HISTORY.md && git commit -qam "$phase-h14 probe" && PROBE=$(git rev-parse HEAD)
"$dojo" '|rein %urgit [%.n %urgit-ci]' 60 3 >/dev/null
wait_live '%.n'; echo "-- liveness while stopped: $(liveness)"
p=$(push_to "$PROBE" refs/heads/master); echo "push probe -> master while stopped: $p"
check "protected push while stopped" "$OUTAGE" "$p"
p=$(push_to "$OID" "refs/heads/side-$phase"); echo "push -> side-$phase while stopped: $p"
check "unprotected push while stopped" "$OUTAGE" "$p"
"$dojo" '|rein %urgit [%.y %urgit-ci]' 90 3 >/dev/null
wait_live '%.y'; echo "-- liveness after restart: $(liveness)"
p=$(push_to "$PROBE" refs/heads/master); echo "push probe -> master after restart: $p"
check_prefix "protected push after restart is staged" "rejected: staged as ci candidate" "$p"
p=$(push_to "$OID" "refs/heads/side-$phase"); echo "push -> side-$phase after restart: $p"
check "unprotected push after restart" ok "$p"
end_row H14
fi

if has h15; then
row "H15 [$phase]: sign a GET for %untrusted from the %trusted attempt -> ~; same class -> a URL under ci/"
aid="${AID:-$ATTEMPT}"
echo "-- attempt $aid trust=$(att_trust "$aid")"
u=$(sign_get "$aid" untrusted); echo "sign-get untrusted -> $u"
check "cross-class" '~' "$u"
t=$(sign_get "$aid" trusted); echo "sign-get trusted -> $t"
# the endpoint is the store fixture's when P2's store.sh started it (H2
# pointed %storage at it), else the P0 fixture value
endpoint="http://127.0.0.1:1"; [ -s "$TMP/store.env" ] && endpoint="http://127.0.0.1:${STORE_PORT:-8363}"
check_prefix "same-class URL" "[~ '$endpoint/ci-bucket/ci/ci-fixture/" "$t"
end_row H15
fi

# ---- phase verdict ------------------------------------------------------------
echo
echo "== $phase: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# }) of $((NPASS+NFAIL)) rows; build: $("$HERE/mutants.sh" status | tail -1)"
case "$phase" in
  red)   if [ "$NPASS" = 0 ] && [ "$NFAIL" -gt 0 ]; then echo "RED: every row fails under the mutant build"; exit 0
         else echo "NOT RED: rows still passing under the mutant:${PASSED}"; exit 1; fi ;;
  green) if [ "$NFAIL" = 0 ] && [ "$NPASS" -gt 0 ]; then echo "GREEN: every row passes on the real build"; exit 0
         else echo "NOT GREEN: rows failing on the real build:${FAILED}"; exit 1; fi ;;
esac
