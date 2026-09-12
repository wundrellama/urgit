#!/bin/bash
# Row H6 (channel half): with the daemon enrolled and one assignment made
# by poke, the long-poll answers the assignment once, a wrong bearer is
# refused, and an empty channel closes 204 after the poll window.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
echo "== poll with the bearer: the pending assignment"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER" | tee "$TMP/h6-assignment.txt"
ATTEMPT=$(grep -o '"attempt":"[^"]*"' "$TMP/h6-assignment.txt" | head -1 | cut -d'"' -f4 || true)
echo "ATTEMPT=$ATTEMPT"
WRONG="${BEARER%?}0"
echo "== poll with a wrong bearer ($WRONG)"
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$WRONG"
echo "== poll again with the bearer: nothing pending, 204 after the window"
start=$(date +%s)
"$api" GET "/ci/daemon/$DAEMON/assignment" "" "$BEARER"
echo "poll took $(( $(date +%s) - start )) s"
sed -i '/^export ATTEMPT=/d' "$TMP/oids.env"
echo "export ATTEMPT=$ATTEMPT" >> "$TMP/oids.env"
