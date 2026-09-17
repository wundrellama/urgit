#!/bin/bash
# usage: q-negatives.sh red|green|all [rows...]
# The mutant phase for the P2 negative rows, P1's negatives.sh shape:
#   red    q-mutants.sh apply -> rebuild the desk and the daemon -> every
#          row must FAIL *and* its log must carry the mutant's tripwire
#          (q-mutants.sh tripwire <row>); a row that fails without it
#          failed for the wrong reason and the phase is NOT RED
#   green  q-mutants.sh revert -> rebuild both -> every row must PASS
# The red phase reverts the working tree on EXIT however it ends. Each
# phase gets its own fresh repository (ci-p2-<phase>-<hhmmss>), seeded
# and CI-protected, and keeps daemon a's identity when the ship still
# knows it (the P1 ghost lesson), enrolling afresh only when it does not.
# A prep step (Q2's push) gives the rows an attempt to work with; it is
# not counted as a row.
source "$(dirname "$0")/lib.sh"
set +e
phase="${1:?usage: q-negatives.sh red|green|all [rows...]}"; shift
if [ "$phase" = all ]; then
  "$0" red "$@"; r=$?; "$0" green "$@"; g=$?
  echo; echo "== q-negatives: red exit $r, green exit $g"; exit $(( r || g ))
fi
ROWS=("$@"); [ ${#ROWS[@]} = 0 ] && ROWS=(Q4)
case "$phase" in
  red)
    want=FAIL; "$P2/q-mutants.sh" apply || exit 1
    trap 'rc=$?; echo "== red phase exit ($rc): reverting the mutants in the working tree"; "$P2/q-mutants.sh" revert; exit $rc' EXIT
    ;;
  green) want=PASS; "$P2/q-mutants.sh" revert || exit 1 ;;
  *) echo "usage: q-negatives.sh red|green|all" >&2; exit 2 ;;
esac
marker="qneg-$phase-$(date +%s)"; "$P0/dojo.sh" "'$marker'" 60 2 >/dev/null
"$P0/rebuild.sh" "qneg-$phase" urgit urgit-ci | tail -3 || echo "(no reload: the installed desk already matches this phase's tree)"
if herdr pane read "$PANE" --lines 400 | awk -v m="'$marker'" 'index($0, m) { f = 1; next } f' | grep -q 'crud: %into event failed'; then
  echo "q-negatives.sh: the $phase build failed to commit; stopping" >&2; exit 1
fi
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) || exit 1
echo "== build under test: $("$P2/q-mutants.sh" status | tail -1); rows must $want"
"$P1/runner.sh" stop a >/dev/null 2>&1
fresh=yes
if [ -f "$RUNNER_HOME/a/state.json" ]; then
  "$P1/runner.sh" config a 2 >/dev/null
  "$P1/runner.sh" start a | head -1
  sleep 3
  if [ "$("$P1/runner.sh" status a | head -1)" = "not running" ]; then
    echo "-- the ship no longer knows daemon a: enrolling afresh"
  else
    fresh=no; echo "-- daemon a kept its identity; capacity 2 reported on its poll"
  fi
fi
if [ "$fresh" = yes ]; then
  rm -rf "$RUNNER_HOME/a"
  TOKEN=$("$P1/mint.sh") || exit 1
  "$P1/runner.sh" start a 2 "$TOKEN" | head -1
fi
python3 -c 'import json,sys; print("export DAEMON_A="+json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/a/state.json" > "$TMP/p2.env"
export P2_REPO="ci-p2-$phase-$(date +%H%M%S)"
export REPO="$P2_REPO"; CLONE="$TMP/clone-$REPO"
"$P0/api.sh" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}" | cut -c1-30
rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .; git config user.name qneg; git config user.email qneg@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
set_workflows fixture-pass.yml
echo "seed" > README.md; git add -A && git commit -qm "seed"; git push -q origin master 2>&1 | tail -1
"$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 3 >/dev/null
echo "-- repository $REPO seeded and CI-protected"
NPASS=0; NFAIL=0; PASSED=""; FAILED=""; NRED=0; NWRONG=0; RED=""; WRONG=""
run_row() {  # <name> <script> [args]
  local name="$1"; shift
  local log="$TMP/qneg-$phase-$name.log"
  "$@" > "$log" 2>&1
  local verdict; verdict=$(grep -E "^$name: (PASS|FAIL)$" "$log" | tail -1 | awk '{print $2}')
  [ -z "$verdict" ] && verdict=FAIL
  grep -E '^  .*: (PASS|FAIL)' "$log" | cut -c1-150
  if [ "$phase" = red ]; then
    local tripwires hit=""
    tripwires=$("$P2/q-mutants.sh" tripwire "$name") || { echo "q-negatives.sh: no tripwire for $name" >&2; exit 2; }
    while IFS= read -r t; do [ -n "$t" ] && grep -qF -- "$t" "$log" && { hit="$t"; break; }; done <<< "$tripwires"
    if [ "$verdict" = FAIL ] && [ -n "$hit" ]; then
      echo "$name [red]: FAIL, tripwire present: $hit"; NRED=$((NRED+1)); RED="$RED $name"
    elif [ "$verdict" = FAIL ]; then
      echo "$name [red]: FAIL for the WRONG reason, tripwire absent: $(printf '%s' "$tripwires" | tr '\n' '|')"; NWRONG=$((NWRONG+1)); WRONG="$WRONG $name"
    else
      echo "$name [red]: PASS (not red)"
    fi
  else
    echo "$name [$phase]: $verdict"
  fi
  if [ "$verdict" = PASS ]; then NPASS=$((NPASS+1)); PASSED="$PASSED $name"; else NFAIL=$((NFAIL+1)); FAILED="$FAILED $name"; fi
}
# prep: Q2's push gives the phase an attempt (its own verdict is not a row here)
"$P2/q2-4.sh" q2 > "$TMP/qneg-$phase-prep.log" 2>&1; echo "-- prep (Q2 push): $(grep -E '^Q2: (PASS|FAIL)$' "$TMP/qneg-$phase-prep.log")"
for r in "${ROWS[@]}"; do
  case "$r" in
    Q4)  run_row Q4  "$P2/q2-4.sh" q4 ;;
    Q5a) run_row Q5a "$P2/q5-8.sh" q5a ;;
    Q5)  run_row Q5  "$P2/q5-8.sh" q5 ;;
    Q6)  run_row Q6  "$P2/q5-8.sh" q6 ;;
    Q8)  run_row Q8  "$P2/q5-8.sh" q8 ;;
    Q10) run_row Q10 "$P2/q9-11.sh" q10 ;;
    Q11) "$P2/q9-11.sh" q9 > "$TMP/qneg-$phase-prep-q9.log" 2>&1; echo "-- prep (Q9 push): $(grep -E '^Q9: (PASS|FAIL)$' "$TMP/qneg-$phase-prep-q9.log")"
         run_row Q11 "$P2/q9-11.sh" q11 ;;
    Q12) run_row Q12 "$P2/q12-13.sh" q12 ;;
    Q13) run_row Q13 "$P2/q12-13.sh" q13 ;;
    Q16) run_row Q16 "$P2/q14-16.sh" q16 ;;
    *) echo "q-negatives.sh: unknown row $r" >&2 ;;
  esac
done
"$P1/runner.sh" stop a >/dev/null 2>&1
echo
case "$phase" in
  red)
    echo "== red: FAIL-with-tripwire=$NRED (${RED# }) FAIL-wrong-reason=$NWRONG (${WRONG# }) PASS=$NPASS (${PASSED# }) of $((NPASS+NFAIL)) rows; build: $("$P2/q-mutants.sh" status | tail -1)"
    if [ "$NPASS" = 0 ] && [ "$NWRONG" = 0 ] && [ "$NRED" -gt 0 ]; then echo "RED: every row fails under the mutant build, each for its own mutant's reason"; exit 0
    else echo "NOT RED: passing under the mutant:${PASSED}; failing for the wrong reason:${WRONG}"; exit 1; fi ;;
  green)
    echo "== green: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# }) of $((NPASS+NFAIL)) rows; build: $("$P2/q-mutants.sh" status | tail -1)"
    if [ "$NFAIL" = 0 ] && [ "$NPASS" -gt 0 ]; then echo "GREEN: every row passes on the real build"; exit 0
    else echo "NOT GREEN: rows failing on the real build:${FAILED}"; exit 1; fi ;;
esac
