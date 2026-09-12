#!/bin/bash
# Row H6: mint an enrollment token, enroll a daemon, long-poll the
# assignment channel (204 after the window), assign the fast-forward
# candidate by poke, long-poll again (the assignment).
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
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
echo "== poll with no credentials (401), then with the bearer (204 after ~25 s)"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" -
start=$(date +%s)
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER"
echo "poll took $(( $(date +%s) - start )) s"
echo "== assign the fast-forward candidate to the daemon by poke"
"$dojo" ":urgit-ci &ci-action [%assign $CAND $DAEMON ~]" 60 3 | tail -2
echo "== poll again: the assignment"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER" | tee "$TMP/h6-assignment.txt"
ATTEMPT=$(grep -o '"attempt":"[^"]*"' "$TMP/h6-assignment.txt" | head -1 | cut -d'"' -f4 || true)
echo "ATTEMPT=$ATTEMPT"
cat >> "$TMP/oids.env" <<EOF
export TOKEN=$TOKEN
export DAEMON=$DAEMON
export BEARER=$BEARER
export ATTEMPT=$ATTEMPT
EOF
