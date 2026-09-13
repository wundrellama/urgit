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
"$dojo" ':urgit-ci|mint-enroll-token' 60 6 | tee "$TMP/h6-mint.txt" | tail -4
TOKEN=$(grep -o 'ci-enroll-token 0v[0-9a-v.]*' "$TMP/h6-mint.txt" | tail -1 | sed 's/.* //')
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
WRONG="${BEARER%?}0"
echo "== poll with a wrong bearer (401)"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$WRONG"
echo "== poll with the bearer (204 after ~25 s)"
start=$(date +%s)
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER"
echo "poll took $(( $(date +%s) - start )) s"
echo "== assign the fast-forward candidate to the daemon by poke"
"$dojo" ":urgit-ci &ci-action [%assign $CAND $DAEMON ~]" 60 3 | tail -2
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
