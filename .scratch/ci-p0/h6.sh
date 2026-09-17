#!/bin/bash
# Row H6: mint an enrollment token, enroll a daemon (re-enrolling the same
# token is refused), long-poll the assignment channel without credentials
# (401), with a wrong bearer (401), with the bearer (204 after the 25 s
# window), assign the fast-forward candidate by poke, long-poll again (the
# assignment, once), and once more (204: it is not delivered twice).
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$HERE/api.sh"
dojo="$HERE/dojo.sh"
echo "== mint"
"$ROOT/.scratch/ci-p1/p2-key.sh"
# the generator's ~& prints `[%ci-enroll-token 0v…]` before the echoed
# command, pretty-printed over three lines when the pane is narrow: read
# 30 lines and take the last token after a `ci-enroll-token` line
"$dojo" ':urgit-ci|mint-enroll-token' 60 30 | tee "$TMP/h6-mint.txt" | tail -4
TOKEN=$(awk '/ci-enroll-token/ { f = 1 } f && match($0, /0v[0-9a-v.]+/) { t = substr($0, RSTART, RLENGTH); f = 0 } END { printf "%s", t }' "$TMP/h6-mint.txt")
[ -n "$TOKEN" ] || { echo "h6.sh: no token in the dojo's output" >&2; exit 1; }
echo "TOKEN=$TOKEN"
echo "== enroll (no session, token only)"
ENROLL=$("$api" POST /ci/daemon/enroll "{\"token\":\"$TOKEN\"}" -)
echo "$ENROLL"
DAEMON=$(echo "$ENROLL" | grep -o '"daemon-id":"[^"]*"' | cut -d'"' -f4)
BEARER=$(echo "$ENROLL" | grep -o '"bearer":"[^"]*"' | cut -d'"' -f4)
echo "DAEMON=$DAEMON"
echo "== enroll again with the same token (must be refused)"
"$api" POST /ci/daemon/enroll "{\"token\":\"$TOKEN\"}" -
echo "== poll with no credentials (401)"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" -
case "${BEARER: -1}" in 0) WRONG="${BEARER%?}1" ;; *) WRONG="${BEARER%?}0" ;; esac
echo "== poll with a wrong bearer $WRONG (401)"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$WRONG"
echo "== poll with the bearer (P0: 204 after ~25 s; P1: the scheduler hands this daemon the automatic PLAN assignment of a pending candidate first, D4/D6)"
start=$(date +%s)
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER"
echo "poll took $(( $(date +%s) - start )) s"
echo "== assign the fast-forward candidate's fixture-pass/pass job to the daemon by poke (P1 %assign shape: kind, workflow, job, deadline)"
"$dojo" ":urgit-ci &ci-action [%assign $CAND $DAEMON %job \`'fixture-pass.yml' \`'pass' ~]" 60 3 | tail -2
echo "== poll again: the assignment"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER" | tee "$TMP/h6-assignment.txt"
ATTEMPT=$(grep -o '"attempt":"[^"]*"' "$TMP/h6-assignment.txt" | head -1 | cut -d'"' -f4 || true)
echo "ATTEMPT=$ATTEMPT"
echo "== poll once more: nothing pending, 204 after the window (delivered once)"
start=$(date +%s)
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER"
echo "poll took $(( $(date +%s) - start )) s"
cat >> "$TMP/oids.env" <<EOF2
export TOKEN=$TOKEN
export DAEMON=$DAEMON
export BEARER=$BEARER
export ATTEMPT=$ATTEMPT
EOF2
