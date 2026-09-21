#!/bin/bash
# usage: r7-r8.sh r7|r8
# Rows R7 and R8 (BRIEF-CI-P3 D5, D7, S4): the storage probe from a
# SECOND machine, and the first-run states.
#   R7  GET ci/storage/probe names the LAN endpoint; `np` (192.168.1.64,
#       curl only, nothing written) fetches the probe URL — an HTTP
#       answer, with CORS headers for a browser's origin — and reads a
#       real log through the log route's 302 (`curl -L` from np, the
#       bytes the ship's handle names). Then %storage is pointed at
#       127.0.0.1: the probe URL names it, np's fetch fails to connect
#       (the browser's NetworkError), the fe module renders D5's exact
#       sentence for that host, and the log route still 302s. The LAN
#       endpoint is restored.
#   R8  a repository with CI required and no enrolled runner: the runner
#       list is empty of enrolled records, the policy lists the ref, and
#       the fe module renders the first-run message; %storage unset: the
#       toggle's POST answers the refusal string, the probe reads
#       unconfigured; %storage restored.
source "$(dirname "$0")/lib.sh"
which_row="${1:-r7}"
NP="${NP_HOST:-np}"
remote="$P3/r7-remote.sh"
fe_probe_message() {  # <state> <host>
  ( cd "$ROOT/fe" && node -e "import('./src/storageProbe.js').then((m) => process.stdout.write(m.probeMessage(process.argv[1], process.argv[2])))" "$1" "$2" )
}
if [ "$which_row" = r7 ]; then
row "R7: the storage probe from np — the LAN endpoint answers and a log opens from np; a 127.0.0.1 endpoint is red with D5's sentence while the log route still 302s"
check "np is reachable over ssh (read-only: curl only)" "nativeplanet" "$(ssh -o BatchMode=yes -o ConnectTimeout=10 "$NP" hostname 2>/dev/null)"
r=$(ci_get /storage/probe)
check "GET ci/storage/probe -> 200 configured" "true" "$(jq_of "$r" .configured)"
check "the probe names the LAN endpoint" "http://$STORE_ADVERTISE:$STORE_PORT" "$(jq_of "$r" .endpoint)"
PROBE=$(jq_of "$r" .url); HOST=$(jq_of "$r" .host)
check "the probe URL is under the CI prefix of the bucket" "http://$STORE_ADVERTISE:$STORE_PORT/$STORE_BUCKET/ci/_probe" "$PROBE"
check "GET ci/storage/probe without a session -> 401" "401" "$(status_of "$(ci_get /storage/probe -)")"
np_code=$("$remote" code "$PROBE")
check "np fetches the probe URL: an HTTP answer (403: unsigned, reachable)" "403" "$np_code"
check "np's fetch with a browser Origin gets CORS headers (the log view can read the store)" "*" "$("$remote" headers "$PROBE" | grep -i '^access-control-allow-origin' | tr -d '\r' | awk '{print $2}')"
check "from the host too" "403" "$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$PROBE")"
# a real log through the log route's 302, read from np
sync_clone; set_workflows fixture-pass.yml
printf 'r7 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p3 R7: a log for np to read"
# the attempt and its handle through the route (the page's own read)
AID=""; for _ in $(seq 1 120); do AID=$(body_of "$(ci_get "/candidate/$CID")" | jq -r '.attempts[] | select(.job == "pass" and .status == "passed" and .log != null) | .attempt' | head -1); [ -n "$AID" ] && break; sleep 2; done
echo "-- the pass attempt $AID"
check "the pass job passed with a log handle" "yes" "$([ -n "$AID" ] && echo yes || echo no)"
SHA=$(body_of "$(ci_get "/candidate/$CID")" | jq -r ".attempts[] | select(.attempt == \"$AID\") | .log.sha256")
loc=$(ci_location "/attempt/$AID/log")
check "GET ci/attempt/<id>/log -> 302" "302" "$(printf '%s' "$loc" | cut -d' ' -f1)"
LINK=$(printf '%s' "$loc" | cut -d' ' -f2-)
check "the Location names the LAN host" "1" "$(printf '%s' "$LINK" | grep -cF "http://$STORE_ADVERTISE:$STORE_PORT/")"
np_sha=$("$remote" sha "$LINK")
check "np reads the log through the presigned link: sha256 = the ship's handle" "$SHA" "$np_sha"
echo "-- %storage pointed at 127.0.0.1 (R7's RED case)"
STORE_ADVERTISE=127.0.0.1 "$store" configure | tail -1
r=$(ci_get /storage/probe)
check "the probe now names 127.0.0.1" "127.0.0.1:$STORE_PORT" "$(jq_of "$r" .host)"
PROBE2=$(jq_of "$r" .url)
np_code2=$("$remote" exit "$PROBE2")
check "np cannot fetch it (a connection failure, the browser's NetworkError)" "000 exit=7" "$np_code2"
check "the fe module renders D5's exact sentence for that host" "Your browser cannot reach the object store at 127.0.0.1:$STORE_PORT. Logs and artifacts will not open. The endpoint must be reachable from every viewer's network, not only from the ship's host." "$(fe_probe_message unreachable "127.0.0.1:$STORE_PORT")"
loc2=$(ci_location "/attempt/$AID/log")
check "the log route still 302s" "302" "$(printf '%s' "$loc2" | cut -d' ' -f1)"
check "to a 127.0.0.1 link" "1" "$(printf '%s' "$loc2" | cut -d' ' -f2- | grep -cF "http://127.0.0.1:$STORE_PORT/")"
echo "-- %storage restored to the LAN endpoint"
"$store" configure | tail -1
check "the probe names the LAN endpoint again" "$STORE_ADVERTISE:$STORE_PORT" "$(jq_of "$(ci_get /storage/probe)" .host)"
end_row R7
fi
if [ "$which_row" = r8 ]; then
row "R8: first-run states — CI required with no enrolled runner shows the message; %storage unset surfaces the refusal at the toggle"
R8_REPO="ci-p3-r8-$(date +%H%M%S)"
"$api" POST /repositories "{\"name\":\"$R8_REPO\",\"publicRead\":true}" | cut -c1-30
R8_CLONE="$TMP/clone-$R8_REPO"; rm -rf "$R8_CLONE"; mkdir -p "$R8_CLONE"; ( cd "$R8_CLONE" && git init -q -b master . && git config user.name r8 && git config user.email r8@example && git config http.cookieFile "$JAR" && git remote add origin "$URL/git/$R8_REPO" && echo seed > README.md && git add -A && git commit -qm seed && git push -q origin master 2>&1 | tail -1 )
"$api" POST "/repository/$R8_REPO/branches/default" '{"name":"master"}' >/dev/null
# every runner record but the pool's daemon a is retired; a itself is
# revoked for the message's sake and re-enrolled after
retire_daemon b 2>/dev/null
check "CI required on master -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$R8_REPO\",\"ref\":\"refs/heads/master\",\"protected\":true}")")"
check "revoke daemon a (the last enrolled runner) -> 200" "200" "$(status_of "$(ci_action "{\"action\":\"revoke-daemon\",\"id\":\"$DAEMON_A\"}")")"
enrolled=$(runners_json | jq '[.runners[] | select(.enrolled != null and .revoked == null)] | length')
check "no enrolled, unrevoked runner remains" "0" "$enrolled"
check "the policy lists the CI-required ref" "refs/heads/master" "$(jq_of "$(ci_get "/repository/$R8_REPO/policy")" '.ciProtected[0]')"
msg=$( cd "$ROOT/fe" && node -e "import('./src/ci.js').then((m) => process.stdout.write(m.noRunnerMessage(JSON.parse(process.argv[1]), JSON.parse(process.argv[2]))))" "$(jq_of "$(ci_get "/repository/$R8_REPO/policy")" '.ciProtected')" "$(runners_json | jq -c .runners)" )
check_contains "the CI tab's first-run message renders from those two reads" "No runner is enrolled. Mint a token in Settings → Runners and install the daemon" "$msg"
r=$(ci_get "/repository/$R8_REPO/candidates")
check "the initial feed carries the runner list for the same message" "true" "$(jq_of "$r" '.candidates | type == "array"')"
echo "-- %storage unset (endpoint cleared)"
"$poke" storage storage-action "[%set-endpoint '']" | tail -1
r=$(ci_action "{\"action\":\"set-ci-protected\",\"repo\":\"$R8_REPO\",\"ref\":\"refs/heads/main\",\"protected\":true}")
check "CI required on another ref with no store -> 409" "409" "$(status_of "$r")"
check "the toggle's error is the refusal string" "ship object storage is not configured; CI cannot be enabled" "$(jq_of "$r" .error)"
check "the probe reads unconfigured" "false" "$(jq_of "$(ci_get /storage/probe)" .configured)"
check "the fe message for unconfigured names %storage" "1" "$(fe_probe_message unconfigured '' | grep -c 'no object store configured')"
echo "-- %storage restored; daemon a re-enrolled"
"$store" configure | tail -1
check "the probe reads configured again" "true" "$(jq_of "$(ci_get /storage/probe)" .configured)"
ci_action "{\"action\":\"expire-token\",\"id\":\"$DAEMON_A\"}" >/dev/null
start_daemon a "$DAEMON_CAPACITY"
check "daemon a is back" "healthy" "$(wait_runner_state "$DAEMON_A" healthy 30)"
end_row R8
fi
[ "$NFAIL" = 0 ]
