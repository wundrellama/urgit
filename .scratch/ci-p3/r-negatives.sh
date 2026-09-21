#!/bin/bash
# usage: r-negatives.sh red|green|all [rows...]
# The mutant phase for the P3 negative rows, P2's q-negatives.sh shape:
#   red    r-mutants.sh apply -> rebuild the desk and the daemon -> every
#          row must FAIL *and* its log must carry the mutant's tripwire
#          (r-mutants.sh tripwire <row>); a row that fails without it
#          failed for the wrong reason and the phase is NOT RED
#   green  r-mutants.sh revert -> rebuild both -> every row must PASS
# The red phase reverts the working tree on EXIT however it ends. Each
# phase gets its own fresh repository (ci-p3-<phase>-<hhmmss>), seeded
# and CI-protected through the web action route, and a freshly enrolled
# daemon a at the footer's DAEMON_CAPACITY (R5 rotates the key and
# re-enrolls a, so a phase never inherits a's identity); the previous a's
# record is revoked and removed first so it can never be a ghost.
source "$(dirname "$0")/lib.sh"
set +e
phase="${1:?usage: r-negatives.sh red|green|all [rows...]}"; shift
if [ "$phase" = all ]; then
  "$0" red "$@"; r=$?; "$0" green "$@"; g=$?
  echo; echo "== r-negatives: red exit $r, green exit $g"; exit $(( r || g ))
fi
ROWS=("$@"); [ ${#ROWS[@]} = 0 ] && ROWS=(R3)
case "$phase" in
  red)
    want=FAIL; "$P3/r-mutants.sh" apply "${ROWS[@]}" || exit 1
    trap 'rc=$?; echo "== red phase exit ($rc): reverting the mutants in the working tree"; "$P3/r-mutants.sh" revert; exit $rc' EXIT
    ;;
  green) want=PASS; "$P3/r-mutants.sh" revert || exit 1 ;;
  *) echo "usage: r-negatives.sh red|green|all" >&2; exit 2 ;;
esac
marker="rneg-$phase-$(date +%s)"; "$P0/dojo.sh" "'$marker'" 60 2 >/dev/null
# the reload to wait for is the mutated agent's: R16's, R14's and R17's
# mutants are %urgit's, every other row's is %urgit-ci's; a phase mixing
# them waits for both
agents=(); case " ${ROWS[*]} " in *" R16 "*|*" R14 "*|*" R17 "*) agents+=(urgit) ;; esac
case " ${ROWS[*]} " in *" R3 "*|*" R4 "*|*" R4b "*|*" R5 "*|*" R5b "*|*" R6a "*|*" R9 "*|*" R10 "*|*" R11b "*|*" R15b "*|*" R18 "*) agents+=(urgit-ci) ;; esac
"$P0/rebuild.sh" "rneg-$phase" "${agents[@]}" | tail -3 || echo "(no reload: the installed desk already matches this phase's tree)"
if tty_read 400 | awk -v m="'$marker'" 'index($0, m) { f = 1; next } f' | grep -q 'crud: %into event failed'; then
  echo "r-negatives.sh: the $phase build failed to commit; stopping" >&2; exit 1
fi
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) || exit 1
echo "== build under test: $("$P3/r-mutants.sh" status | tail -1); rows must $want"
for d in a b; do retire_daemon "$d"; done
: > "$TMP/p3.env"
start_daemon a "$DAEMON_CAPACITY"
export P3_REPO="ci-p3-$phase-$(date +%H%M%S)"
export REPO="$P3_REPO"; CLONE="$TMP/clone-$REPO"
"$P0/api.sh" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}" | cut -c1-30
rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .; git config user.name rneg; git config user.email rneg@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
set_workflows fixture-pass.yml
echo "seed" > README.md; git add -A && git commit -qm "seed"; git push -q origin master 2>&1 | tail -1
"$P0/api.sh" POST "/repository/$REPO/branches/default" '{"name":"master"}' >/dev/null
ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$REPO\",\"ref\":\"refs/heads/master\",\"protected\":true}" | cut -c1-40
echo "-- repository $REPO seeded and CI-protected"
NPASS=0; NFAIL=0; PASSED=""; FAILED=""; NRED=0; NWRONG=0; RED=""; WRONG=""
run_row() {  # <name> <script> [args]
  local name="$1"; shift
  local log="$TMP/rneg-$phase-$name.log"
  "$@" > "$log" 2>&1
  # R4b and R5b re-run R4's and R5's scripts under their own mutants;
  # R11b is a row of its own
  local label="$name"; case "$name" in R4b) label=R4 ;; R5b) label=R5 ;; esac
  local verdict; verdict=$(grep -E "^$label: (PASS|FAIL)$" "$log" | tail -1 | awk '{print $2}')
  [ -z "$verdict" ] && verdict=FAIL
  grep -E '^  .*: (PASS|FAIL)' "$log" | cut -c1-150
  if [ "$phase" = red ]; then
    local tripwires hit=""
    tripwires=$("$P3/r-mutants.sh" tripwire "$name") || { echo "r-negatives.sh: no tripwire for $name" >&2; exit 2; }
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
for r in "${ROWS[@]}"; do
  case "$r" in
    R3)  run_row R3  "$P3/r1-r5.sh" r3 ;;
    R4)  run_row R4  "$P3/r1-r5.sh" r4 ;;
    R4b) run_row R4b "$P3/r1-r5.sh" r4 ;;
    R5)  run_row R5  "$P3/r1-r5.sh" r5 ;;
    R5b) run_row R5b "$P3/r1-r5.sh" r5 ;;
    R6a) run_row R6a "$P3/r2-r6.sh" r6a ;;
    R15b) run_row R15b "$P3/r14-r16.sh" r15b ;;
    R18) run_row R18 "$P3/r18.sh" ;;
    R9)  run_row R9  "$P3/r9-r11.sh" r9 ;;
    R10) run_row R10 "$P3/r9-r11.sh" r10 ;;
    R11b) run_row R11b "$P3/r9-r11.sh" r11b ;;
    R16) run_row R16 "$P3/r14-r16.sh" r16 ;;
    R14) run_row R14 "$P3/r14-r16.sh" r14 ;;
    R17) run_row R17 "$P3/r17.sh" ;;
    *) echo "r-negatives.sh: unknown row $r" >&2 ;;
  esac
  # a row that re-enrolled a (R5) rewrote p3.env; the next row reads it
  source "$TMP/p3.env"
done
for d in a b; do "$P1/runner.sh" stop "$d" >/dev/null 2>&1; done
echo
case "$phase" in
  red)
    echo "== red: FAIL-with-tripwire=$NRED (${RED# }) FAIL-wrong-reason=$NWRONG (${WRONG# }) PASS=$NPASS (${PASSED# }) of $((NPASS+NFAIL)) rows; build: $("$P3/r-mutants.sh" status | tail -1)"
    if [ "$NPASS" = 0 ] && [ "$NWRONG" = 0 ] && [ "$NRED" -gt 0 ]; then echo "RED: every row fails under the mutant build, each for its own mutant's reason"; exit 0
    else echo "NOT RED: passing under the mutant:${PASSED}; failing for the wrong reason:${WRONG}"; exit 1; fi ;;
  green)
    echo "== green: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# }) of $((NPASS+NFAIL)) rows; build: $("$P3/r-mutants.sh" status | tail -1)"
    if [ "$NFAIL" = 0 ] && [ "$NPASS" -gt 0 ]; then echo "GREEN: every row passes on the real build"; exit 0
    else echo "NOT GREEN: rows failing on the real build:${FAILED}"; exit 1; fi ;;
esac
