#!/bin/bash
# usage: docker-rootless.sh start|stop|status|info
# The per-chair rootless Docker daemon (§8): state dir under
# /run/user/1000/ci-p1-opus (short Unix socket paths), data root and logs
# under $TMP (git-ignored). Never touches the host's rootful daemon. `start`
# waits until the socket answers and prints the security options, which
# must include name=rootless; `info` copies the act image in when missing.
source "$(dirname "$0")/env.sh"
LOG="$TMP/docker-rootless.log"
PIDFILE="$DOCKER_STATE/dockerd.pid"
case "${1:-status}" in
  start)
    mkdir -p "$DOCKER_STATE" "$DOCKER_DATA"
    if [ -S "$DOCKER_SOCK" ] && docker --host "unix://$DOCKER_SOCK" info >/dev/null 2>&1; then
      echo "already running: $DOCKER_SOCK"
    else
      echo '{}' > "$TMP/docker-config.json"
      XDG_RUNTIME_DIR="$DOCKER_STATE" \
      DOCKERD_ROOTLESS_ROOTLESSKIT_STATE_DIR="$DOCKER_STATE/rootlesskit" \
        nohup dockerd-rootless.sh \
          --config-file "$TMP/docker-config.json" \
          --data-root "$DOCKER_DATA" \
          --exec-root "$DOCKER_STATE/exec" \
          --pidfile "$PIDFILE" \
          --host "unix://$DOCKER_SOCK" \
          > "$LOG" 2>&1 &
      echo "started (log $LOG)"
      for _ in $(seq 1 60); do
        docker --host "unix://$DOCKER_SOCK" info >/dev/null 2>&1 && break; sleep 1
      done
    fi
    printf 'security options: '; docker --host "unix://$DOCKER_SOCK" info --format '{{.SecurityOptions}}'
    docker --host "unix://$DOCKER_SOCK" info --format 'server version {{.ServerVersion}}, root {{.DockerRootDir}}'
    ;;
  info)
    if ! docker --host "unix://$DOCKER_SOCK" image inspect "$ACT_IMAGE" >/dev/null 2>&1; then
      echo "== copying $ACT_IMAGE from the host daemon (docker save | load)"
      docker save "$ACT_IMAGE" | docker --host "unix://$DOCKER_SOCK" load
    fi
    docker --host "unix://$DOCKER_SOCK" images "$ACT_IMAGE"
    ;;
  status)
    docker --host "unix://$DOCKER_SOCK" info --format 'rootless daemon: {{.SecurityOptions}}' 2>&1 | head -1
    [ -f "$PIDFILE" ] && printf 'pid %s: ' "$(cat "$PIDFILE")" && { tr '\0' ' ' < "/proc/$(cat "$PIDFILE")/cmdline" 2>/dev/null | cut -c1-120; echo; }
    ;;
  stop)
    [ -f "$PIDFILE" ] && kill "$(cat "$PIDFILE")" && echo "sent TERM to $(cat "$PIDFILE")"
    ;;
esac
