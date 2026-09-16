#!/bin/bash
# usage: docker-rootless.sh start|stop|status|info
# The per-chair rootless Docker daemon (§8): state dir $DOCKER_STATE under
# /run/user/1000 (short Unix socket paths), data root and logs under $TMP
# (git-ignored). Never touches the host's rootful daemon.
# `start` waits until the socket answers and prints the security options,
# which must include name=rootless; `info` copies the act image in when
# missing. A data root belongs to one dockerd at a time: `start` refuses
# when another dockerd (an earlier run's state dir) still holds
# $DOCKER_DATA, naming it, rather than starting a second daemon on it.
source "$(dirname "$0")/env.sh"
LOG="$TMP/docker-rootless.log"
PIDFILE="$DOCKER_STATE/dockerd.pid"
# pids of every dockerd whose /proc/<pid>/cmdline names our data root
data_root_holders() {
  local p cmd
  for p in /proc/[0-9]*; do
    cmd=$({ tr '\0' ' ' < "$p/cmdline"; } 2>/dev/null)
    case "$cmd" in dockerd\ *"--data-root $DOCKER_DATA "*) basename "$p" ;; esac
  done
}
case "${1:-status}" in
  start)
    mkdir -p "$DOCKER_STATE" "$DOCKER_DATA"
    if [ -S "$DOCKER_SOCK" ] && docker --host "unix://$DOCKER_SOCK" info >/dev/null 2>&1; then
      echo "already running: $DOCKER_SOCK"
    elif [ -n "$(data_root_holders)" ]; then
      for p in $(data_root_holders); do
        echo "docker-rootless.sh: dockerd pid $p already holds $DOCKER_DATA: $(tr '\0' ' ' < "/proc/$p/cmdline" | grep -oE -- '--exec-root [^ ]+')" >&2
      done
      echo "docker-rootless.sh: stop it first (docker-rootless.sh stop under its own DOCKER_STATE)" >&2
      exit 1
    else
      echo '{}' > "$TMP/docker-config.json"
      XDG_RUNTIME_DIR="$DOCKER_STATE" \
      DOCKERD_ROOTLESS_ROOTLESSKIT_STATE_DIR="$DOCKER_STATE/rootlesskit" \
        setsid nohup dockerd-rootless.sh \
          --config-file "$TMP/docker-config.json" \
          --data-root "$DOCKER_DATA" \
          --exec-root "$DOCKER_STATE/exec" \
          --pidfile "$PIDFILE" \
          --host "unix://$DOCKER_SOCK" \
          > "$LOG" 2>&1 < /dev/null &
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
    [ -f "$PIDFILE" ] || { echo "no pidfile at $PIDFILE"; exit 0; }
    p=$(cat "$PIDFILE")
    echo "dockerd pid $p: $(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null | cut -c1-120)"
    kill "$p" && echo "sent TERM to $p"
    for _ in $(seq 1 60); do [ -d "/proc/$p" ] || break; sleep 1; done
    [ -d "/proc/$p" ] && { echo "docker-rootless.sh: dockerd $p still running after 60 s" >&2; exit 1; }
    echo "dockerd $p gone; $DOCKER_DATA released"
    ;;
esac
