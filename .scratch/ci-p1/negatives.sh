#!/bin/bash
# usage: negatives.sh red|green|all
# The mutant phase for the P1 negative rows (P1, P7-P12, P14, P17-P20):
#   red    mutants.sh apply -> rebuild the desk and the daemon -> every row
#          must FAIL *and* its log must carry the mutant's own tripwire
#          (mutants.sh tripwire <row>): a row that fails without it failed
#          for the wrong reason and is NOT RED
#   green  mutants.sh revert -> rebuild both -> every row must PASS
# The red phase reverts the working tree on EXIT however it ends (a
# trap, the way astra's mutants.py does it in a finally), so a phase that
# dies mid-row never leaves the mutants applied; the installed desk and
# the daemon binary stay mutated until the green phase rebuilds them,
# which is the normal path. Each phase gets its own fresh repository
# (ci-p1-<phase>), CI-protected by row P1's second half, and its own
# daemon enrollment (P14 wipes the ship's state at the end of a phase).
# The rows are the same scripts the main table runs, one row at a time.
source "$(dirname "$0")/env.sh"
set +e
phase="${1:?usage: negatives.sh red|green|all}"
if [ "$phase" = all ]; then
  "$0" red; r=$?; "$0" green; g=$?
  echo; echo "== negatives: red exit $r, green exit $g"; exit $(( r || g ))
fi
case "$phase" in
  red)
    want=FAIL; "$P1/mutants.sh" apply || exit 1
    trap 'rc=$?; echo "== red phase exit ($rc): reverting the mutants in the working tree"; "$P1/mutants.sh" revert; exit $rc' EXIT
    ;;
  green) want=PASS; "$P1/mutants.sh" revert || exit 1 ;;
  *) echo "usage: negatives.sh red|green|all" >&2; exit 2 ;;
esac
# rebuild.sh waits for both agents to reload; when the installed desk
# already equals the working tree (a previous phase left it there) the
# commit is a no-op and prints no reload, which is fine for this phase
marker="neg-$phase-$(date +%s)"; "$P0/dojo.sh" "'$marker'" 60 2 >/dev/null
"$P0/rebuild.sh" "neg-$phase" urgit urgit-ci | tail -3 || echo "(no reload: the installed desk already matches this phase's tree)"
if herdr pane read "$PANE" --lines 400 | awk -v m="'$marker'" 'index($0, m) { f = 1; next } f' | grep -q 'crud: %into event failed'; then
  echo "negatives.sh: the $phase build failed to commit; stopping" >&2; exit 1
fi
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) || exit 1
echo "== build under test: $("$P1/mutants.sh" status | tail -1); rows must $want"
# daemon a for this phase at capacity 1. It keeps its identity when the
# ship still knows it: a re-enrollment leaves the old record as a ghost
# that wins the oldest-enrolled tie for ~m5 and swallows the first plan
# (the author's D6 deviation; met again when the red phase followed P20
# within 20 s and P7's push went to the stopped daemon's record, whose
# plan the P11 mutant then closed %success at its deadline). Only when
# the state is gone (the previous phase's P14 wiped the ship's daemons)
# does the restarted daemon exit `enrollment lost`, and then it enrolls
# afresh with no ghost to meet.
"$P1/runner.sh" stop a >/dev/null 2>&1
fresh=yes
if [ -f "$RUNNER_HOME/a/state.json" ]; then
  "$P1/runner.sh" config a 1 >/dev/null
  "$P1/runner.sh" start a | head -1
  sleep 3
  if [ "$("$P1/runner.sh" status a | head -1)" = "not running" ]; then
    echo "-- the ship no longer knows daemon a ($(grep -o 'enrollment lost.*' "$RUNNER_HOME/a/daemon.log" | tail -1)): enrolling afresh"
  else
    fresh=no; echo "-- daemon a kept its identity; capacity 1 reported on its poll"
  fi
fi
if [ "$fresh" = yes ]; then
  rm -rf "$RUNNER_HOME/a"
  TOKEN=$("$P1/mint.sh") || exit 1
  "$P1/runner.sh" start a 1 "$TOKEN" | head -1
fi
export REPO="ci-p1-$phase-$(date +%H%M%S)"   # unique per phase run: row P1 needs a repo with no tip
"$P0/api.sh" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}" | cut -c1-30
CLONE="$TMP/clone-$REPO"; rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .; git config user.name neg; git config user.email neg@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
NPASS=0; NFAIL=0; PASSED=""; FAILED=""
# red: NRED rows failed for the mutant's reason (verdict FAIL + tripwire
# in the log); NWRONG rows failed without their tripwire
NRED=0; NWRONG=0; RED=""; WRONG=""
run_row() {  # <name> <script> [args]
  local name="$1"; shift
  local log="$TMP/neg-$phase-$name.log"
  "$@" > "$log" 2>&1
  local verdict; verdict=$(grep -E "^$name: (PASS|FAIL)$" "$log" | tail -1 | awk '{print $2}')
  [ -z "$verdict" ] && verdict=FAIL
  grep -E '^  .*: (PASS|FAIL)' "$log" | cut -c1-150
  if [ "$phase" = red ]; then
    # the named-assertion oracle: the row's log must contain one of the
    # mutant's tripwire strings (alternatives one per line)
    local tripwires hit=""
    tripwires=$("$P1/mutants.sh" tripwire "$name") || { echo "negatives.sh: no tripwire for $name" >&2; exit 2; }
    while IFS= read -r t; do [ -n "$t" ] && grep -qF -- "$t" "$log" && { hit="$t"; break; }; done <<< "$tripwires"
    if [ "$verdict" = FAIL ] && [ -n "$hit" ]; then
      echo "$name [red]: FAIL, tripwire present: $hit"
      NRED=$((NRED+1)); RED="$RED $name"
    elif [ "$verdict" = FAIL ]; then
      echo "$name [red]: FAIL for the WRONG reason, tripwire absent: $(printf '%s' "$tripwires" | tr '\n' '|')"
      NWRONG=$((NWRONG+1)); WRONG="$WRONG $name"
    else
      echo "$name [red]: PASS (not red)"
    fi
  else
    echo "$name [$phase]: $verdict"
  fi
  if [ "$verdict" = PASS ]; then NPASS=$((NPASS+1)); PASSED="$PASSED $name"; else NFAIL=$((NFAIL+1)); FAILED="$FAILED $name"; fi
}
run_row P1  "$P1/p1.sh"
# the daemon state file for p2's env (rows read DAEMON_A from it)
python3 -c 'import json,sys; print("export DAEMON_A="+json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/a/state.json" > "$TMP/p2.env"
run_row P7  "$P1/p6-9.sh" p7
run_row P8  "$P1/p6-9.sh" p8
run_row P9  "$P1/p6-9.sh" p9
run_row P10 "$P1/p10-14.sh" p10
run_row P11 "$P1/p10-14.sh" p11
run_row P12 "$P1/p10-14.sh" p12
run_row P13-overlap "$P1/p10-14.sh" p13b
run_row P17 "$P1/p16-20.sh" p17
run_row P18 "$P1/p16-20.sh" p18
run_row P19 "$P1/p16-20.sh" p19
run_row P20 "$P1/p16-20.sh" p20
run_row P14 "$P1/p10-14.sh" p14
"$P1/runner.sh" stop a >/dev/null 2>&1
echo
case "$phase" in
  red)
    echo "== red: FAIL-with-tripwire=$NRED (${RED# }) FAIL-wrong-reason=$NWRONG (${WRONG# }) PASS=$NPASS (${PASSED# }) of $((NPASS+NFAIL)) rows; build: $("$P1/mutants.sh" status | tail -1)"
    if [ "$NPASS" = 0 ] && [ "$NWRONG" = 0 ] && [ "$NRED" -gt 0 ]; then echo "RED: every row fails under the mutant build, each for its own mutant's reason"; exit 0
    else echo "NOT RED: passing under the mutant:${PASSED}; failing for the wrong reason:${WRONG}"; exit 1; fi ;;
  green)
    echo "== green: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# }) of $((NPASS+NFAIL)) rows; build: $("$P1/mutants.sh" status | tail -1)"
    if [ "$NFAIL" = 0 ] && [ "$NPASS" -gt 0 ]; then echo "GREEN: every row passes on the real build"; exit 0
    else echo "NOT GREEN: rows failing on the real build:${FAILED}"; exit 1; fi ;;
esac
