#!/bin/bash
# Row Q19 (CI-SANDBOX-1.1, re-freeze 4): the socket act binds into every
# job container is the configured ROOTLESS daemon's, never the host's
# /var/run/docker.sock. A pushed fixture-docker job runs `docker info` and
# sleeps; while it runs, `docker inspect` of the live job container on
# the rootless daemon shows the docker.sock bind's source; the job's
# stream shows what daemon it reached. RED on the unfixed daemon: the
# bind source is /var/run/docker.sock (the host's rootful socket, refused
# by its permissions) and the job fails; GREEN: the source is $DOCKER_SOCK
# and `docker info` inside the job reports name=rootless. The second
# negative (BRIEF-CI-P2-CLOSEOUT T5, astra's p2-sandbox-inspect.sh) walks
# EVERY mount and bind of the live job container and asserts the host
# socket is absent from all of them — whatever the destination — not
# only that the one docker.sock mount's source is the rootless socket.
source "$(dirname "$0")/lib.sh"
HOST_SOCK=/var/run/docker.sock
HOST_SOCK_REAL=$(readlink -f "$HOST_SOCK" 2>/dev/null || echo "$HOST_SOCK")
row "Q19: the job container's docker.sock is the rootless daemon's, by construction"
sync_clone
set_workflows fixture-docker.yml
printf 'q19 %s\n' "$(date -Is)" >> README.md
push_commit "ci-p2 Q19: a job that asks which daemon it can reach"
AID=$(wait_for_attempt "$CID" docker 180)
check "a job attempt for docker exists" "yes" "${AID:+yes}"
# the job container act creates is named act-<attempt with dots as dashes>-…
prefix="act-$(printf '%s' "$AID" | tr '.' '-')"
cname=""
for _ in $(seq 1 60); do cname=$($DK ps --format '{{.Names}}' | grep -F "$prefix" | head -1); [ -n "$cname" ] && break; sleep 1; done
echo "-- job container: ${cname:-<none seen>}"
check "the job container was seen live" "yes" "${cname:+yes}"
binds=$($DK inspect "$cname" --format '{{json .HostConfig.Binds}}' 2>/dev/null)
source_of=$($DK inspect "$cname" --format '{{range .Mounts}}{{if eq .Destination "/var/run/docker.sock"}}{{.Source}}{{end}}{{end}}' 2>/dev/null)
echo "-- Binds=$binds; docker.sock mount source=$source_of"
check "the job container's socket bind is the rootless socket" "$DOCKER_SOCK:/var/run/docker.sock" "$(printf '%s' "$binds" | jq -r '.[] | select(endswith(":/var/run/docker.sock"))')"
check "the docker.sock mount's source is the configured rootless socket" "$DOCKER_SOCK" "$source_of"
check "the host's rootful socket is not bound" "0" "$(printf '%s' "$binds" | grep -c '"/var/run/docker.sock:/var/run/docker.sock"')"
# every mount and bind of the live container, whatever its destination
mounts=$($DK inspect "$cname" --format '{{range .Mounts}}{{.Type}} {{.Source}} -> {{.Destination}}{{"\n"}}{{end}}' 2>/dev/null | sed '/^$/d')
n_mounts=$(printf '%s\n' "$mounts" | grep -c .)
printf '%s\n' "$mounts" | sed 's/^/-- mount: /'
host_in_mounts=$(printf '%s\n' "$mounts" | awk -v h="$HOST_SOCK" -v r="$HOST_SOCK_REAL" '$2 == h || $2 == r' | grep -c .)
host_in_binds=$(printf '%s' "$binds" | jq -r '.[]?' 2>/dev/null | awk -F: -v h="$HOST_SOCK" -v r="$HOST_SOCK_REAL" '$1 == h || $1 == r' | grep -c .)
check "the host socket is absent from every mount and bind of the job container ($n_mounts mounts walked)" "0" "$((host_in_mounts + host_in_binds))"
st=$(wait_att "$AID" '%passed|%failed|%infrastructure-error' 240)
check "the job finished" '%passed' "$st"
stream=$(daemon_stream "$AID")
check_contains "docker info inside the job reports name=rootless" "name=rootless" "$(grep -o 'daemon security=[^"]*' "$stream" | head -1)"
check "no permission denied on the socket" "0" "$(grep -c 'permission denied' "$stream")"
end_row Q19
[ "$NFAIL" = 0 ]
