#!/bin/bash
# usage: store.sh start|stop|status|configure|head <key>|get <key> <file>|ls [prefix]|anon <key>
# The object-store fixture (BRIEF-CI-P2 §5, astra §3): RustFS (Apache-2.0,
# SigV4; the store NativePlanet ships by default) as ONE rootless container
# on the harness Docker daemon, on the footer's port ($STORE_PORT), data
# under $STORE_DATA, one PRIVATE bucket ($STORE_BUCKET: no anonymous policy,
# so an unsigned read answers 403), and an access key minted once per
# fixture into $TMP/store.env (mode 0600; git-ignored). Every read the rows
# make of the bucket goes through this script's signed curl
# (`--aws-sigv4`), never through the container's filesystem.
#   start      pull the image if missing, run the container, wait for the
#              S3 port, create the bucket (idempotent), prove it is private
#   stop       remove the container (part of shutdown); the data dir stays
#   status     container state and the bucket's object count
#   configure  the %storage-action pokes that point the fixture ship at it
#              (the P0 recipe: endpoint, both keys, bucket, region, service)
#   head/get/ls/anon  signed HEAD, signed GET to a file, signed list of a
#              prefix (keys one per line), and an UNSIGNED GET (the status)
source "$(dirname "$0")/env.sh"
DK="docker --host unix://$DOCKER_SOCK"
ENVF="$TMP/store.env"
mint_keys() {
  [ -s "$ENVF" ] && return 0
  local key secret
  key="CI$(head -c 9 /dev/urandom | base32 | tr -d '=' | tr 'a-z' 'A-Z' | cut -c1-18)"
  secret="$(head -c 24 /dev/urandom | base64 | tr -d '=/+' | cut -c1-32)"
  ( umask 077; printf 'export STORE_KEY=%s\nexport STORE_SECRET=%s\n' "$key" "$secret" > "$ENVF" )
  echo "minted a fixture access key into $ENVF (mode $(stat -c %a "$ENVF"))"
}
load_keys() { [ -s "$ENVF" ] || { echo "store.sh: no $ENVF; run store.sh start first" >&2; exit 1; }; source "$ENVF"; }
# signed <method> <url> [curl args...]: SigV4 with the fixture's key
signed() { local m="$1" u="$2"; shift 2; curl -s --aws-sigv4 "aws:amz:$STORE_REGION:s3" --user "$STORE_KEY:$STORE_SECRET" -X "$m" "$u" "$@"; }
wait_port() { for _ in $(seq 1 60); do curl -s -o /dev/null "$STORE_URL/" && return 0; sleep 1; done; echo "store.sh: $STORE_URL never answered" >&2; return 1; }
case "${1:-status}" in
  start)
    mkdir -p "$STORE_DATA"; mint_keys; load_keys
    if [ "$($DK inspect -f '{{.State.Running}}' "$STORE_NAME" 2>/dev/null)" = true ]; then
      echo "already running: $STORE_NAME on $STORE_URL"
    else
      $DK rm -f "$STORE_NAME" >/dev/null 2>&1 || true
      if ! $DK image inspect "$STORE_IMAGE" >/dev/null 2>&1; then
        echo "== pulling $STORE_IMAGE into the harness daemon"
        $DK pull -q "$STORE_IMAGE" | tail -1
      fi
      # the image's own user (rustfs, uid 10001) maps to a subuid under
      # rootless Docker and cannot write a bind-mounted host directory;
      # container root maps to this user, which owns $STORE_DATA
      $DK run -d --name "$STORE_NAME" --user 0:0 \
        -p "127.0.0.1:$STORE_PORT:9000" \
        -e "RUSTFS_ACCESS_KEY=$STORE_KEY" -e "RUSTFS_SECRET_KEY=$STORE_SECRET" \
        -e RUSTFS_CONSOLE_ENABLE=false -e RUSTFS_VOLUMES=/data -e RUSTFS_OBS_LOGGER_LEVEL=warn \
        -v "$STORE_DATA:/data" "$STORE_IMAGE" >/dev/null
      echo "started $STORE_NAME ($STORE_IMAGE) on $STORE_URL, data $STORE_DATA"
    fi
    wait_port || exit 1
    code=$(signed PUT "$STORE_URL/$STORE_BUCKET" -o /dev/null -w '%{http_code}')
    case "$code" in 200|409) echo "bucket $STORE_BUCKET: $code (created or already present)";; *) echo "store.sh: bucket create answered $code" >&2; signed PUT "$STORE_URL/$STORE_BUCKET" | head -c 400; echo; exit 1;; esac
    anon=$(curl -s -o /dev/null -w '%{http_code}' "$STORE_URL/$STORE_BUCKET/?list-type=2")
    echo "anonymous list of $STORE_BUCKET: $anon (private: must be 403)"
    [ "$anon" = 403 ] || { echo "store.sh: the bucket is not private" >&2; exit 1; }
    ;;
  stop)
    if $DK inspect "$STORE_NAME" >/dev/null 2>&1; then
      $DK rm -f "$STORE_NAME" >/dev/null && echo "removed $STORE_NAME (data kept under $STORE_DATA)"
    else echo "no container $STORE_NAME"; fi
    ;;
  status)
    printf 'container %s: ' "$STORE_NAME"; $DK inspect -f '{{.State.Status}} {{.Config.Image}} port {{(index (index .NetworkSettings.Ports "9000/tcp") 0).HostPort}}' "$STORE_NAME" 2>&1 | head -1
    if [ -s "$ENVF" ]; then load_keys; printf 'objects under ci/: %s\n' "$("$0" ls ci/ | wc -l)"; fi
    ;;
  configure)
    load_keys
    poke="$P0/poke.sh"
    for act in \
      "[%set-endpoint '$STORE_URL']" \
      "[%set-access-key-id '$STORE_KEY']" \
      "[%set-secret-access-key '$STORE_SECRET']" \
      "[%set-current-bucket '$STORE_BUCKET']" \
      "[%set-region '$STORE_REGION']" \
      "[%toggle-service %credentials]"; do
      printf '%s -> ' "$(printf '%s' "$act" | sed "s/'$STORE_SECRET'/'…'/")"; "$poke" storage storage-action "$act" | tail -1
    done
    ;;
  head) load_keys; signed HEAD "$STORE_URL/$STORE_BUCKET/$2" -o /dev/null -w '%{http_code} %header{content-length} %header{etag}\n' ;;
  get)  load_keys; signed GET "$STORE_URL/$STORE_BUCKET/$2" -o "$3" -w '%{http_code}\n' ;;
  anon) curl -s -o /dev/null -w '%{http_code}\n' "$STORE_URL/$STORE_BUCKET/$2" ;;
  ls)   load_keys; signed GET "$STORE_URL/$STORE_BUCKET/?list-type=2&prefix=${2:-}" | python3 -c 'import sys,re; print("\n".join(re.findall(r"<Key>([^<]+)</Key>", sys.stdin.read())))' | sed '/^$/d' ;;
  *) echo "usage: store.sh start|stop|status|configure|head <key>|get <key> <file>|ls [prefix]|anon <key>" >&2; exit 2 ;;
esac
