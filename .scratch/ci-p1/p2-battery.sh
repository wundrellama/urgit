#!/bin/bash
# Complete P2 rows after the P1 regression, using the same ships and store.
source "$(dirname "$0")/env.sh"
set -euo pipefail
step() {
  local label=$1; shift
  printf '\n################ %s (%s)\n' "$label" "$(date -Is)"
  "$@" 2>&1 | tee "$TMP/s6-$label.log"
}
for name in a b; do "$P1/runner.sh" stop "$name"; done
step p2-reset "$P1/nuke-revive.sh" s6-p2
step p2-store "$P1/store.sh" configure
step p2-enroll "$P1/p15-prep.sh" 3
export ERPIT_REPO=${P2_ERPIT_REPO:-erpit-p2-s6}
step p2-erpit "$P1/p15.sh"
source "$TMP/p15-run.env"
step q14 "$P1/p2-web.sh" q14 "$CID"
step q18 "$P1/p2-web.sh" q18 "$CID"
step q15-setup "$P1/p2-web.sh" setup
step q15 "$P1/p2-web.sh" q15
step q16 "$P1/p2-session-regression.sh" "$CID"
step q2 "$P1/p2-storage.sh" q2
for phase in red green; do step "q3-$phase" "$P1/p2-storage.sh" q3 "$phase"; done
for phase in red green; do step "q2-missing-$phase" "$P1/p2-storage.sh" q2-missing "$phase"; done
"$P1/runner.sh" stop a
step q4-reset "$P1/nuke-revive.sh" s6-q4
export P2_STORAGE_REPO=ci-p2-storage-s6
step q4-setup "$P1/p2-storage.sh" setup
for phase in red green; do step "q4-$phase" "$P1/p2-storage.sh" q4 "$phase"; done
step q5a-reset "$P1/nuke-revive.sh" s6-q5a
for phase in red green; do step "q5a-$phase" "$P1/p2-trust.sh" q5a "$phase"; done
step q5-setup "$P1/p2-trust.sh" q5-setup
for phase in red green; do step "q5-$phase" "$P1/p2-trust.sh" q5 "$phase"; done
step q8 "$P1/p2-approval.sh"
step q6-setup "$P1/p2-trust.sh" q6-setup
for phase in red green; do step "q6-$phase" "$P1/p2-trust.sh" q6 "$phase"; done
step q7 "$P1/p2-trust.sh" q7
step q9 "$P1/p2-credentials.sh" q9
for phase in red green; do step "q10-$phase" "$P1/p2-credentials.sh" q10 "$phase"; done
for phase in red green; do step "q11-$phase" "$P1/p2-credentials.sh" q11 "$phase"; done
for name in a b; do "$P1/runner.sh" stop "$name"; done
step signing-reset "$P1/nuke-revive.sh" s6-signing
step q13-hold "$P1/p2-signing.sh" hold
for phase in red green; do step "q12-$phase" "$P1/p2-signing.sh" q12 "$phase"; done
step signing-positive "$P1/p2-signing.sh" positive
step signing-vectors "$P1/p2-signing-vectors.sh" check
step q13 "$P1/p2-expiry.sh"
"$P1/q-mutants.sh" status | grep -qx 'real build'
echo "P2 battery passed ($(date -Is))"
