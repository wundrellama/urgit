#!/bin/bash
# Q8 uses the exact action handler in an isolated synthetic-bowl probe.
# Q7 supplies its owner-positive case through the actual agent poke.
source "$(dirname "$0")/env.sh"
set -e
cleanup() {
  "$P1/q-mutants.sh" revert
  "$P0/dojo.sh" '|rein %urgit [%.y %urgit]' 120 4 > "$TMP/q8-restore.log"
}
trap cleanup EXIT
"$P1/q-mutants.sh" apply Q8
"$P1/p2-reload.sh"
"$P1/p2-approval-probe.sh" red > "$TMP/q8-red.log"
"$P0/dojo.sh" '|rein %urgit [%.n %urgit]' 120 4 > "$TMP/q8-suspend-red.log"
source "$P1/lib.sh"
set -e
test "$(dojo_value '.^(? %gu /=urgit=/$)')" = '%.n'
"$P1/p2-approval-probe.sh" red > "$TMP/q8-unavailable-red.log"
"$P0/dojo.sh" '|rein %urgit [%.y %urgit]' 120 4 > "$TMP/q8-restore-red.log"
"$P1/q-mutants.sh" revert
"$P1/p2-reload.sh"
"$P1/p2-approval-probe.sh" green > "$TMP/q8-green.log"
"$P0/dojo.sh" '|rein %urgit [%.n %urgit]' 120 4 > "$TMP/q8-suspend.log"
test "$(dojo_value '.^(? %gu /=urgit=/$)')" = '%.n'
"$P1/p2-approval-probe.sh" unavailable > "$TMP/q8-unavailable.log"
echo 'Q8 RED and GREEN passed; unavailable writer read refused'
