#!/bin/bash
# Finish Q13 from its retained authentic delivery, including after a driver stop.
source "$(dirname "$0")/env.sh"
set -euo pipefail
python3 - <<'PY'
import json,os,pathlib,time
expiry=json.loads((pathlib.Path(os.environ['TMP'])/'p2-q13-hold.json').read_text())['expiry']
print('Waiting for authentic grant expiry:',expiry,flush=True)
while time.time() <= expiry:
    time.sleep(min(10,max(0.1,expiry-time.time()+1)))
PY
for phase in red green; do
  printf '\n################ q13-%s (%s)\n' "$phase" "$(date -Is)"
  "$P1/p2-signing.sh" q13 "$phase" 2>&1 | tee "$TMP/s6-q13-$phase.log"
done
"$P1/q-mutants.sh" status | grep -qx 'real build'
echo "Q13 expiry regression passed ($(date -Is))"
