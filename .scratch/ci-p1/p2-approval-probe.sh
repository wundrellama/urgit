#!/bin/bash
# Q8's isolated action-handler probe: compile the current controller helper
# door verbatim with a synthetic bowl.src. No cards are dispatched and no
# state is saved. The production poke entry still admits only the owner.
source "$(dirname "$0")/env.sh"
source "$P1/lib.sh"
set -e
phase=${1:?red or green}
actor=${2:-$SHIP2}
cid=$(python3 - "$TMP/p2-q5-green.json" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))['cid'])
PY
)
probe="$PIER/urgit/gen/p2-approval-probe.hoon"
python3 - "$ROOT/desk/app/urgit-ci.hoon" "$probe" <<'PY'
import pathlib,sys
source=pathlib.Path(sys.argv[1]).read_text()
preamble=source[:source.index('=|  state-0:ci\n')]
helper=source[source.index('|_  =bowl:gall\n+$  out'):]
helper=helper.split('\n',1)[1]
wrapper='''|=  [snapshot=state-0:ci =bowl:gall act=action:ci]
=+  snapshot
=*  state  -
=|  polls=(map daemon-id:ci poll)
|^
=/  checked=(each out tang)  (mule |.((handle-action act)))
?:  ?=(%& -.checked)  [%accepted ~]
[%refused (murn p.checked |=(t=tank ?:(?=(%leaf -.t) `(crip p.t) ~)))]
'''
path=pathlib.Path(sys.argv[2]); path.parent.mkdir(parents=True,exist_ok=True)
path.write_text(preamble+wrapper+helper)
print('Probe uses the current action-handler/helper source verbatim')
PY
"$P0/dojo.sh" '|commit %urgit' 120 4 >/dev/null
binding="q8-probe-$(date +%s%N)"
"$P0/dojo.sh" "=$binding -build-file /=urgit=/gen/p2-approval-probe/hoon" 180 12 > "$TMP/q8-build-$phase.log"
expr="=/  s=state-0:ci  *state-0:ci  =.  s  s(candidates .^((map candidate-id:ci candidate:ci) %gx /=urgit-ci=/candidates/noun), daemons .^((map daemon-id:ci daemon:ci) %gx /=urgit-ci=/daemons/noun), assignments .^((map assignment-id:ci assignment:ci) %gx /=urgit-ci=/assignments/noun), attempts .^((map attempt-id:ci attempt:ci) %gx /=urgit-ci=/attempts/noun))  =/  b=bowl:gall  *bowl:gall  ($binding s b(our ~$SHIP, src ~$actor, now now) [%approve-candidate $cid])"
value=$(dojo_value "$expr" 180)
printf '%s\n' "$value"
case "$phase" in
  red)
    [[ "$value" == *'%accepted'* ]] || { echo 'Q8 NOT RED: wrong reason' >&2; exit 1; }
    echo 'Q8 RED: non-writer approval accepted by the action handler'
    ;;
  green)
    [[ "$value" == *'%refused'* && "$value" == *'actor cannot write '* ]]
    echo 'Q8 GREEN: second-galaxy actor refused by the writer peek'
    ;;
  unavailable)
    [[ "$value" == *'%refused'* && "$value" == *'writer read is unavailable'* ]]
    echo 'Q8 GREEN: unavailable writer read refused'
    ;;
  *) exit 2 ;;
esac
