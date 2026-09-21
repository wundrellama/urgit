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
# P3 D1: the ship mints the token through the session-authorized
# POST ci/runners/mint (ci-p1/mint.sh); the dojo generator is gone
TOKEN=$("$ROOT/.scratch/ci-p1/mint.sh") || { echo "h6.sh: mint failed" >&2; exit 1; }
cut -c1-120 "$TMP/mint-last.json" | tee "$TMP/h6-mint.txt"; echo
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
