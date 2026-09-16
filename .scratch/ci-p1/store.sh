#!/bin/bash
# Private RustFS fixture on this chair's rootless daemon. Configuration follows
# https://docs.rustfs.com/en/installation/container and the native IAM API at
# https://docs.rustfs.com/en/security-compliance/iam/policies .
source "$(dirname "$0")/env.sh"
umask 077
IMAGE=rustfs/rustfs:1.0.0@sha256:8cc9801755448b71a786705ce76692c77e14936cccd87cf2fc31842e58f4d1ff
NAME="urgit-ci-store-$SHIP"
CONFIG="$TMP/store-config"
dock=(docker --host "unix://$DOCKER_SOCK")

request() {
  local role=$1 method=$2 path=$3 data=${4:-} status
  local args=(--config "$CONFIG/$role.curl" --request "$method")
  [ -z "$data" ] || args+=(--header 'content-type: application/json' --data-binary "@$data")
  status=$(curl --silent --show-error --max-time 30 "${args[@]}" \
    --output "$TMP/store-response.xml" --write-out '%{http_code}' "$STORE_URL$path")
  case "$status" in
    2??) return 0 ;;
    409) if [ "$method:$path" = "PUT:/$STORE_BUCKET" ] && \
      grep -q 'BucketAlreadyOwnedByYou' "$TMP/store-response.xml"; then return 0; fi ;;
  esac
  echo "store.sh: $method $path -> $status" >&2
  cat "$TMP/store-response.xml" >&2
  return 1
}

configure_ship() {
  source "$CONFIG/scoped.env"
  local action answer
  for action in \
    "[%set-endpoint '$STORE_URL']" \
    "[%set-access-key-id '$STORE_ACCESS_KEY']" \
    "[%set-secret-access-key '$STORE_SECRET_KEY']" \
    "[%add-bucket '$STORE_BUCKET']" \
    "[%set-current-bucket '$STORE_BUCKET']" \
    "[%set-region '$STORE_REGION']" \
    "[%toggle-service %credentials]"; do
    answer=$("$P0/poke.sh" storage storage-action "$action")
    if [[ "$answer" != *'%poked %storage %storage-action'* ]]; then
      echo 'store.sh: %storage configuration poke failed' >&2
      return 1
    fi
  done
  echo "ship %storage configured: $STORE_URL/$STORE_BUCKET (scoped CI key)"
}

case "${1:-status}" in
  start)
    security=$("${dock[@]}" info --format '{{.SecurityOptions}}')
    [[ "$security" == *name=rootless* ]] || { echo 'rootless Docker required' >&2; exit 1; }
    mkdir -p "$STORE_DATA" "$CONFIG"
    if [ ! -f "$CONFIG/root.env" ]; then
      python3 - "$CONFIG" "$STORE_REGION" "$STORE_BUCKET" <<'PY'
import json, pathlib, secrets, sys
p, region, bucket = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
root_key, root_secret = secrets.token_hex(10).upper(), secrets.token_hex(32)
key, secret = secrets.token_hex(10).upper(), secrets.token_hex(32)
(p/'root.env').write_text(f'RUSTFS_ACCESS_KEY={root_key}\nRUSTFS_SECRET_KEY={root_secret}\n')
(p/'scoped.env').write_text(f'STORE_ACCESS_KEY={key}\nSTORE_SECRET_KEY={secret}\n')
for name, access, password in [('root', root_key, root_secret), ('scoped', key, secret)]:
    (p/f'{name}.curl').write_text(f'aws-sigv4 = "aws:amz:{region}:s3"\nuser = "{access}:{password}"\n')
(p/'user.json').write_text(json.dumps({'secretKey': secret, 'status': 'enabled'}))
(p/'policy.json').write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
    {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation'], 'Resource': [f'arn:aws:s3:::{bucket}']},
    {'Effect': 'Allow', 'Action': ['s3:ListBucket'], 'Resource': [f'arn:aws:s3:::{bucket}'],
     'Condition': {'StringLike': {'s3:prefix': ['ci/*']}}},
    {'Effect': 'Allow', 'Action': ['s3:GetObject', 's3:PutObject', 's3:DeleteObject'],
     'Resource': [f'arn:aws:s3:::{bucket}/ci/*']}
]}))
PY
    fi
    source "$CONFIG/scoped.env"
    if "${dock[@]}" container inspect "$NAME" >/dev/null 2>&1; then
      "${dock[@]}" start "$NAME" >/dev/null
    else
      # Root inside this rootless user namespace is the invoking host user;
      # keep the fixture data owned by that user without privileged chown.
      "${dock[@]}" run --detach --name "$NAME" --user 0:0 \
        --label "urgit-ci.fixture=$SHIP" \
        --publish "127.0.0.1:$STORE_PORT:9000" \
        --mount "type=bind,src=$STORE_DATA,dst=/data" \
        --env-file "$CONFIG/root.env" --env RUSTFS_CONSOLE_ENABLE=false \
        "$IMAGE" /data > "$TMP/store-container.id"
    fi
    ready=false
    for _ in $(seq 1 60); do
      if curl --silent --fail --max-time 2 "$STORE_URL/health" >/dev/null; then ready=true; break; fi
      sleep 1
    done
    $ready || { echo 'store.sh: RustFS never became healthy' >&2; "${dock[@]}" logs --tail 30 "$NAME"; exit 1; }
    request root PUT "/$STORE_BUCKET"
    request root PUT '/rustfs/admin/v3/add-canned-policy?name=urgit-ci' "$CONFIG/policy.json"
    request root PUT "/rustfs/admin/v3/add-user?accessKey=$STORE_ACCESS_KEY" "$CONFIG/user.json"
    request root PUT "/rustfs/admin/v3/set-user-or-group-policy?policyName=urgit-ci&userOrGroup=$STORE_ACCESS_KEY&isGroup=false"
    printf 'private CI fixture\n' > "$TMP/store-probe.txt"
    request scoped PUT "/$STORE_BUCKET/ci/fixture-probe" "$TMP/store-probe.txt"
    request scoped GET "/$STORE_BUCKET/ci/fixture-probe"
    cmp "$TMP/store-probe.txt" "$TMP/store-response.xml"
    status=$(curl --silent --show-error --output "$TMP/store-anonymous.xml" --write-out '%{http_code}' \
      "$STORE_URL/$STORE_BUCKET/ci/fixture-probe")
    [ "$status" = 403 ] || { echo "store.sh: private object returned $status anonymously" >&2; exit 1; }
    request scoped DELETE "/$STORE_BUCKET/ci/fixture-probe"
    configure_ship
    echo "RustFS private bucket ready: $STORE_URL/$STORE_BUCKET; anonymous GET -> 403"
    ;;
  stop)
    if "${dock[@]}" container inspect "$NAME" >/dev/null 2>&1; then
      "${dock[@]}" stop --time 20 "$NAME" >/dev/null
      running=$("${dock[@]}" inspect --format '{{.State.Running}}' "$NAME")
      [ "$running" = false ]
      echo "RustFS $NAME stopped; data retained at $STORE_DATA"
    else
      echo "RustFS $NAME absent"
    fi
    ;;
  status)
    "${dock[@]}" inspect --format '{{.Name}} running={{.State.Running}} image={{.Config.Image}}' "$NAME"
    ;;
  *) echo 'usage: store.sh start|stop|status' >&2; exit 2 ;;
esac
