#!/bin/bash
# After P14 wiped the ship's state: CI-protect ci-p1 again, and enroll
# daemon a afresh with capacity 3 for P15's eight jobs (D6: the ship
# records the capacity reported at enrollment).
source "$(dirname "$0")/lib.sh"
"$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 3 | tail -1
"$P1/runner.sh" stop a >/dev/null 2>&1
rm -rf "$RUNNER_HOME/a"
TOKEN=$("$P1/mint.sh") || exit 1
"$P1/runner.sh" start a "${1:-3}" "$TOKEN" | head -1
DAEMON_A=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/a/state.json")
echo "export DAEMON_A=$DAEMON_A" > "$TMP/p2.env"
echo "daemon a re-enrolled as $DAEMON_A with capacity ${1:-3}"
