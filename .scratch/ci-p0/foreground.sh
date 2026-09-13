#!/bin/bash
# The five foreground suites: four %say vectors in the dojo (each prints
# %.y) and `cd fe && npm test`. Tails are pasted into the live table.
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
for v in ci-event-vector ci-storage-vector git-migration-vector git-access-vector; do
  echo "== +urgit!$v"
  "$dojo" "+urgit!$v" 600 4 | tail -3
done
echo "== cd fe && npm test"
( cd "$ROOT/fe" && npm test 2>&1 | tail -12 )
