#!/bin/bash
# S1 storage rows. Q4 starts on a one-line mutant before the real assertion.
source "$(dirname "$0")/lib.sh"
set -e
STATE="$TMP/p2-storage.env"
case "${1:-}" in
  setup)
    REPO=${P2_STORAGE_REPO:-ci-p2-storage}
    CLONE="$TMP/clone-$REPO"
    "$P0/prelude.sh" > "$TMP/p2-prelude.log"
    created=$("$api" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}")
    [[ "$created" == 201* ]] || { echo "$created"; exit 1; }
    mkdir -p "$CLONE/.github/workflows"
    git -C "$CLONE" init -q -b master
    git -C "$CLONE" config user.name p2
    git -C "$CLONE" config user.email p2@example
    git -C "$CLONE" config http.cookieFile "$JAR"
    git -C "$CLONE" remote add origin "$URL/git/$REPO"
    cp "$ROOT/desk/tests/ci/fixture-pass.yml" "$CLONE/.github/workflows/"
    git -C "$CLONE" add -A
    git -C "$CLONE" commit -qm 'p2 storage fixture seed'
    git -C "$CLONE" push -q origin master
    "$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 3 >/dev/null
    echo 'storage probe' > "$CLONE/README.md"
    push_commit 'p2 storage candidate'
    [ -n "$CID" ]
    TOKEN=$("$P1/mint.sh")
    enrolled=$("$api" POST /ci/daemon/enroll "{\"token\":\"$TOKEN\",\"capacity\":1,\"sandbox\":\"storage-probe\"}" -)
    DAEMON=$(printf '%s' "${enrolled#* }" | python3 -c 'import sys,json; print(json.load(sys.stdin)["daemon-id"])')
    BEARER=$(printf '%s' "${enrolled#* }" | python3 -c 'import sys,json; print(json.load(sys.stdin)["bearer"])')
    assigned=$("$api" GET "/ci/daemon/$DAEMON/assignment" '' "$BEARER")
    ATTEMPT=$(printf '%s' "${assigned#* }" | python3 -c 'import sys,json; print(json.load(sys.stdin)["assignment"]["attempt"])')
    printf 'REPO=%q\nCID=%q\nATTEMPT=%q\nDAEMON=%q\nBEARER=%q\n' "$REPO" "$CID" "$ATTEMPT" "$DAEMON" "$BEARER" > "$STATE"
    chmod 600 "$STATE"
    echo "storage fixture: candidate $CID, running plan attempt $ATTEMPT"
    ;;
  q4)
    source "$STATE"
    phase=${2:?red or green}
    if [ "$phase" = red ]; then
      trap '"$P1/q-mutants.sh" revert' EXIT
      "$P1/q-mutants.sh" apply Q4
    elif [ "$phase" = green ]; then
      "$P1/q-mutants.sh" revert
    else exit 2; fi
    "$P1/p2-reload.sh"
    reply=$("$api" POST "/ci/attempt/$ATTEMPT/upload" '{"name":"../x","contentType":"application/x-ndjson","size":0,"sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}' "$BEARER")
    status=${reply%% *}
    echo "upload ../x -> $status"
    if [ "$phase" = red ]; then
      [ "$status" = 200 ] || { echo "Q4 NOT RED: wrong failure ($reply)"; exit 1; }
      echo "Q4 RED: 400 assertion failed; tripwire: $("$P1/q-mutants.sh" tripwire Q4)"
    else
      [ "$status" = 400 ] || { echo "Q4 FAIL: $reply"; exit 1; }
      echo 'Q4 GREEN: unsafe upload name refused'
    fi
    ;;
  q2)
    source "$TMP/p15-run.env"
    REPO=erpit-p15
    ATTEMPT=$(wait_for_attempt "$CID" plan 180)
    [ -n "$ATTEMPT" ]
    status=$(wait_att "$ATTEMPT" '%passed|%failed|%infrastructure-error' 240)
    [ "$status" = '%passed' ] || { echo "ERPit plan attempt $ATTEMPT: $status"; exit 1; }
    printf 'ATTEMPT=%q\n' "$ATTEMPT" > "$TMP/q2.env"
    reply=$("$api" GET "/ci/attempt/$ATTEMPT")
    [ "${reply%% *}" = 200 ]
    printf '%s' "${reply#* }" > "$TMP/q2-attempt.json"
    status=$(curl --silent --show-error --location --cookie "$JAR" \
      --dump-header "$TMP/q2-headers.txt" --output "$TMP/q2-log.jsonl" \
      --write-out '%{http_code}' "$URL/apps/urgit/api/ci/attempt/$ATTEMPT/log")
    echo "log redirect followed without SigV4 headers -> $status"
    if [ "$status" != 200 ]; then cat "$TMP/q2-log.jsonl"; exit 1; fi
    python3 - "$TMP" <<'PY'
import datetime, hashlib, json, pathlib, re, sys, urllib.parse
p=pathlib.Path(sys.argv[1]); a=json.loads((p/'q2-attempt.json').read_text()); log=a['log']
data=(p/'q2-log.jsonl').read_bytes()
assert len(data)==log['size'] and hashlib.sha256(data).hexdigest()==log['sha256'], log
assert '/trusted/' in log['key'], log
headers=(p/'q2-headers.txt').read_text()
assert re.search(r'^HTTP/\S+ 302',headers,re.M), headers
url=re.search(r'^location: (.+)$',headers,re.I|re.M).group(1).strip()
q=urllib.parse.parse_qs(urllib.parse.urlsplit(url).query)
assert 1<=int(q['X-Amz-Expires'][0])<=900, q
assert q['X-Amz-SignedHeaders']==['host'], q
at=datetime.datetime.strptime(q['X-Amz-Date'][0],'%Y%m%dT%H%M%SZ').replace(tzinfo=datetime.timezone.utc)
(p/'q2-expiry').write_text(str(int(at.timestamp())+int(q['X-Amz-Expires'][0])+2))
(p/'q2-url.txt').write_text(url)
print(f"attempt {a['attempt']}: {log['key']}, {len(data)} bytes, sha256 {log['sha256']}")
print('JSONL lines:',len(data.splitlines()))
for line in data.splitlines(): json.loads(line)
PY
    # Wait for the expiry the actual returned URL names, not a forged URL.
    while [ "$(date +%s)" -lt "$(cat "$TMP/q2-expiry")" ]; do sleep 1; done
    status=$(curl --silent --show-error --output "$TMP/q2-expired.xml" --write-out '%{http_code}' "$(cat "$TMP/q2-url.txt")")
    echo "same URL after expiry -> $status"
    cat "$TMP/q2-expired.xml"; echo
    [ "$status" = 403 ]
    grep -q 'Request has expired' "$TMP/q2-expired.xml"
    echo 'Q2 GREEN: real ERPit plan log uploaded, redirected, hashed, expired'
    ;;
  q3)
    source "$TMP/q2.env"
    phase=${2:?red or green}
    if [ "$phase" = red ]; then
      trap '"$P1/q-mutants.sh" revert' EXIT
      "$P1/q-mutants.sh" apply Q3
    elif [ "$phase" = green ]; then
      "$P1/q-mutants.sh" revert
    else exit 2; fi
    "$P1/p2-reload.sh"
    value=$(dojo_value ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/untrusted/(scot %t 'log.jsonl')/noun)" | unit_join)
    if [ "$phase" = red ]; then
      [[ "$value" == *'/untrusted/log.jsonl'* ]] || { echo "Q3 NOT RED: wrong failure ($value)"; exit 1; }
      echo "cross-trust read refused: FAIL (observed a signed /untrusted/log.jsonl URL)"
      echo 'Q3 RED: trust sabotage produced its tripwire'
    else
      [ "$value" = '~' ] || { echo "Q3 FAIL: $value"; exit 1; }
      echo 'Q3 GREEN: cross-trust scry returned ~; stored key has /trusted/'
    fi
    ;;
  q2-missing)
    phase=${2:?red or green}
    if [ "$phase" = red ]; then
      source "$TMP/p15-run.env"
      ATTEMPT=$(att_of_job "$CID" erasure)
      reply=$("$api" GET "/ci/attempt/$ATTEMPT")
      key=$(printf '%s' "${reply#* }" | python3 -c 'import sys,json; a=json.load(sys.stdin); assert a["status"]=="passed"; print(a["log"]["key"])')
      printf 'ATTEMPT=%q\n' "$ATTEMPT" > "$TMP/q2-missing.env"
      status=$(curl --silent --show-error --config "$TMP/store-config/scoped.curl" \
        --request DELETE --output "$TMP/q2-delete.xml" --write-out '%{http_code}' "$STORE_URL/$STORE_BUCKET/$key")
      [ "$status" = 204 ]
      trap '"$P1/q-mutants.sh" revert' EXIT
      "$P1/q-mutants.sh" apply Q2-missing
    elif [ "$phase" = green ]; then
      source "$TMP/q2-missing.env"
      "$P1/q-mutants.sh" revert
    else exit 2; fi
    "$P1/p2-reload.sh"
    reply=$("$api" GET "/ci/attempt/$ATTEMPT/log")
    [ "${reply%% *}" = 404 ] || { echo "$reply"; exit 1; }
    reply=$("$api" GET "/ci/attempt/$ATTEMPT")
    printf '%s' "${reply#* }" > "$TMP/q2-missing-$phase.json"
    python3 - "$phase" "$TMP/q2-missing-$phase.json" <<'PY'
import json, sys
phase=sys.argv[1]; a=json.load(open(sys.argv[2]))
assert a['status']=='passed', a
if phase=='red':
    assert a['log'] is not None, 'wrong RED reason: log already cleared'
    print('missing object cleared: FAIL (metadata retained; verdict passed)')
    print('Q2-missing RED: stale-handle sabotage produced its tripwire')
else:
    assert a['log'] is None, a
    print('Q2-missing GREEN: GET 404, log=null, verdict still passed')
PY
    ;;
  *) echo 'usage: p2-storage.sh setup|q4 red|q4 green|q2|q3 red|q3 green|q2-missing red|q2-missing green' >&2; exit 2 ;;
esac
