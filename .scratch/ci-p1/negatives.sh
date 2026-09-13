#!/bin/bash
# usage: negatives.sh red|green|all
# The mutant phase for the P1 negative rows (P1, P7-P12, P14, P17-P20):
#   red    mutants.sh apply -> rebuild the desk and the daemon -> every row must FAIL
#   green  mutants.sh revert -> rebuild both -> every row must PASS
# Each phase gets its own fresh repository (ci-p1-<phase>), CI-protected
# by row P1's second half, and its own daemon enrollment (P14 wipes the
# ship's state at the end of a phase). The rows are the same scripts the
# main table runs, invoked one row at a time.
source "$(dirname "$0")/env.sh"
set +e
phase="${1:?usage: negatives.sh red|green|all}"
if [ "$phase" = all ]; then
  "$0" red; r=$?; "$0" green; g=$?
  echo; echo "== negatives: red exit $r, green exit $g"; exit $(( r || g ))
fi
case "$phase" in
  red)   want=FAIL; "$P1/mutants.sh" apply  || exit 1 ;;
  green) want=PASS; "$P1/mutants.sh" revert || exit 1 ;;
  *) echo "usage: negatives.sh red|green|all" >&2; exit 2 ;;
esac
"$P0/rebuild.sh" "neg-$phase" urgit urgit-ci | tail -3 || exit 1
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner ) || exit 1
echo "== build under test: $("$P1/mutants.sh" status | tail -1); rows must $want"
# a fresh enrollment for this phase (the previous phase's P14 wiped the
# ship's daemons; the CI-protected set went with it)
"$P1/runner.sh" stop a >/dev/null 2>&1
rm -rf "$RUNNER_HOME/a"
TOKEN=$("$P1/mint.sh")
"$P1/runner.sh" start a 1 "$TOKEN" | head -1
export REPO="ci-p1-$phase-$(date +%H%M%S)"   # unique per phase run: row P1 needs a repo with no tip
"$P0/api.sh" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}" | cut -c1-30
CLONE="$TMP/clone-$REPO"; rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .; git config user.name neg; git config user.email neg@example; git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
NPASS=0; NFAIL=0; PASSED=""; FAILED=""
run_row() {  # <name> <script> [args]
  local name="$1"; shift
  local log="$TMP/neg-$phase-$name.log"
  "$@" > "$log" 2>&1
  local verdict; verdict=$(grep -E "^$name: (PASS|FAIL)$" "$log" | tail -1 | awk '{print $2}')
  [ -z "$verdict" ] && verdict=FAIL
  grep -E '^  .*: (PASS|FAIL)' "$log" | cut -c1-150
  echo "$name [$phase]: $verdict"
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
echo "== $phase: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# }) of $((NPASS+NFAIL)) rows; build: $("$P1/mutants.sh" status | tail -1)"
case "$phase" in
  red)   if [ "$NPASS" = 0 ] && [ "$NFAIL" -gt 0 ]; then echo "RED: every row fails under the mutant build"; exit 0
         else echo "NOT RED: rows still passing under the mutant:${PASSED}"; exit 1; fi ;;
  green) if [ "$NFAIL" = 0 ] && [ "$NPASS" -gt 0 ]; then echo "GREEN: every row passes on the real build"; exit 0
         else echo "NOT GREEN: rows failing on the real build:${FAILED}"; exit 1; fi ;;
esac
