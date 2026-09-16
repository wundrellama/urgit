#!/bin/bash
# Row P2: the daemon starts with enroll_token in its config -> enrolls,
# writes the state file mode 0600, prints the sandbox banner; a restart
# with the state file -> no re-enrollment, polls.
source "$(dirname "$0")/lib.sh"
row "P2: enroll from config, state file 0600, banner; restart without re-enrolling"
TOKEN=$("$P1/mint.sh") || exit 1; echo "-- minted token (${#TOKEN} chars)"
rm -rf "$RUNNER_HOME/a"
"$P1/runner.sh" start a 1 "$TOKEN" >/dev/null
sleep 4
log=$(runner_log a)
check_contains "enrolled" "enrolled as daemon" "$log"
check_contains "banner" "sandbox: docker-rootless (container isolation; microvm backend pending)" "$log"
check "state file mode" "-rw-------" "$(stat -c %A "$RUNNER_HOME/a/state.json")"
check "state file holds no raw token" "0" "$(grep -c -F "$TOKEN" "$RUNNER_HOME/a/state.json" || true)"
DAEMON_A=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/a/state.json")
echo "-- daemon a = $DAEMON_A"
check "ship shows the daemon enrolled with capacity 1 and its sandbox" "1 'docker-rootless'" \
  "$(dojo_value "[capacity sandbox]:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$DAEMON_A/noun))" | tr -d '\n' | sed 's/^\[//; s/\]$//')"
echo "-- restart with the state file"
"$P1/runner.sh" stop a | tail -1
before=$(grep -c 'enrolled as daemon' "$RUNNER_HOME/a/daemon.log")
"$P1/runner.sh" start a >/dev/null
sleep 3
after=$(grep -c 'enrolled as daemon' "$RUNNER_HOME/a/daemon.log")
check "no re-enrollment on restart" "$before" "$after"
check_contains "restart reads the state file" "state file $RUNNER_HOME/a/state.json present" "$(runner_log a)"
sleep 2
check_contains "daemon is polling (last-seen set on the ship)" "[~ ~" "$(dojo_value "last-seen:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$DAEMON_A/noun))" | tr -d '\n')"
echo "export DAEMON_A=$DAEMON_A" > "$TMP/p2.env"
end_row P2
# a failed row fails the script, so battery.sh stops at it
[ "$NFAIL" = 0 ]
