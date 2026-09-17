#!/bin/bash
# Rider 4, before retry: inspect the failed attempt's actual Docker daemon.
source "$(dirname "$0")/env.sh"
set -euo pipefail
dock=(docker --host "unix://$DOCKER_SOCK")
name="ci-p2-tmp-probe-$SHIP"
network="$name-network"
volume="$name-work"
cleanup() {
  "${dock[@]}" rm -f "$name" >/dev/null 2>&1 || true
  "${dock[@]}" volume rm "$volume" >/dev/null 2>&1 || true
  "${dock[@]}" network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT
python3 - "$RUNNER_HOME/a/config.toml" "$DOCKER_SOCK" <<'PY'
import pathlib, sys, tomllib
cfg = tomllib.loads(pathlib.Path(sys.argv[1]).read_text())
assert cfg['docker_host'] == 'unix://' + sys.argv[2]
print('Failed erasure ran on daemon a; Docker endpoint:', cfg['docker_host'])
PY
echo 'Exact Prepare mount lines:'
nl -ba "$ROOT/runner/internal/sandbox/docker.go" | sed -n '79,84p'
echo 'Destroy removes attached containers, own container, work volume and network:'
nl -ba "$ROOT/runner/internal/sandbox/docker.go" | sed -n '174,193p'
"${dock[@]}" image inspect "$ACT_IMAGE" | python3 -c 'import json,sys; x=json.load(sys.stdin)[0]; print("Image",x["Id"],"declared volumes",x["Config"].get("Volumes"))'
"${dock[@]}" network create "$network" >/dev/null
"${dock[@]}" volume create "$volume" >/dev/null
for phase in initial replacement; do
  # Same mounts as Prepare. The replacement even reuses /work, so the
  # marker would expose a /tmp alias into that volume.
  "${dock[@]}" run --detach --name "$name" --network "$network" \
    -v "$volume:/work" -v "$DOCKER_SOCK:/var/run/docker.sock" \
    -e DOCKER_HOST=unix:///var/run/docker.sock "$ACT_IMAGE" tail -f /dev/null >/dev/null
  echo "$phase container:"
  "${dock[@]}" inspect "$name" --format 'Mounts={{json .Mounts}} Binds={{json .HostConfig.Binds}} Tmpfs={{json .HostConfig.Tmpfs}}'
  "${dock[@]}" exec "$name" sh -c 'test ! -e /tmp/erasewes; ls -ld /tmp; echo "/tmp/erasewes: absent"; findmnt -T /tmp -o TARGET,FSTYPE,SOURCE -n'
  if [ "$phase" = initial ]; then
    "${dock[@]}" exec "$name" sh -c 'mkdir /tmp/erasewes; echo stale-pier-probe > /tmp/erasewes/marker; test -f /tmp/erasewes/marker'
  fi
  "${dock[@]}" rm -f "$name" >/dev/null
done
echo 'No shared /tmp found; the deliberate marker did not survive container replacement.'
