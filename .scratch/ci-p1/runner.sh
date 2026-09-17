#!/bin/bash
# usage: runner.sh start <name> [capacity] [token]  |  stop <name>  |  kill <name>
#        runner.sh status <name>  |  log <name> [lines]  |  config <name> [capacity] [token]
# The real daemon binary against the harness ship: one directory per daemon name under
# $RUNNER_HOME with its TOML, state file, work dir and log. `start` with a
# token enrolls; without one the state file must exist (the restart path).
# `stop` sends TERM, `kill` sends KILL (P11), both by the recorded pid and
# verified through /proc/<pid>/cmdline. Extra PATH entries in $RUNNER_PATH
# come first (P12 wraps docker that way).
source "$(dirname "$0")/env.sh"
cmd="${1:?usage}"; name="${2:?name}"; runner_dir="$RUNNER_HOME/$name"
pid_of() { cat "$runner_dir/pid" 2>/dev/null || true; }
alive() { local p; p=$(pid_of); [ -n "$p" ] && [ -r "/proc/$p/cmdline" ] && { tr '\0' ' ' < "/proc/$p/cmdline"; } 2>/dev/null | grep -q "urgit-runner.*$runner_dir/config.toml"; }
write_config() {
  local capacity="${1:-1}" token="${2:-}"
  local pinned=""
  if [ -z "$token" ] && [ -f "$runner_dir/config.toml" ]; then
    pinned=$(python3 - "$runner_dir/config.toml" <<'PYCONFIG'
import sys,tomllib
with open(sys.argv[1],'rb') as f: print(tomllib.load(f).get('ci_pub',''))
PYCONFIG
    )
  fi
  mkdir -p "$runner_dir/work"
  cat > "$runner_dir/config.toml" <<TOML
ship_url = "$URL"
enroll_token = "$token"
ci_pub = "$pinned"
sandbox = "docker-rootless"
docker_host = "unix://$DOCKER_SOCK"
act_binary = "$TMP/act-static/act"
act_image = "$ACT_IMAGE"
capacity = $capacity
work_dir = "$runner_dir/work"
state_file = "$runner_dir/state.json"
TOML
  chmod 600 "$runner_dir/config.toml"
}
case "$cmd" in
  config) write_config "${3:-1}" "${4:-}"; echo "wrote $runner_dir/config.toml" ;;
  start)
    if alive; then echo "runner $name already running (pid $(pid_of))"; exit 0; fi
    [ -n "${3:-}${4:-}" ] && write_config "${3:-1}" "${4:-}"
    [ -f "$runner_dir/config.toml" ] || { echo "runner.sh: no config for $name; pass capacity and token" >&2; exit 1; }
    ( cd "$runner_dir" || exit 1
      export PATH="${RUNNER_PATH:+$RUNNER_PATH:}$PATH"
      setsid nohup "$RUNNER_BIN" -config "$runner_dir/config.toml" >> "$runner_dir/daemon.log" 2>&1 < /dev/null &
      echo $! > "$runner_dir/pid" )
    sleep 2
    echo "runner $name started: pid $(pid_of), log $runner_dir/daemon.log"
    tail -n 3 "$runner_dir/daemon.log"
    ;;
  stop|kill)
    p=$(pid_of)
    if ! alive; then echo "runner $name not running"; exit 0; fi
    sig=TERM; [ "$cmd" = kill ] && sig=KILL
    echo "runner $name: /proc/$p/cmdline = $(tr '\0' ' ' < "/proc/$p/cmdline" | cut -c1-120)"
    kill "-$sig" "$p"; for _ in $(seq 1 30); do alive || break; sleep 1; done
    alive && { echo "runner $name still alive after $sig" >&2; exit 1; }
    echo "runner $name stopped ($sig); exit status in the log:"; tail -n 2 "$runner_dir/daemon.log"
    ;;
  wait-exit)  # wait until the daemon process is gone, print its exit line
    p=$(pid_of); for _ in $(seq 1 "${3:-60}"); do alive || break; sleep 1; done
    alive && { echo "still running"; exit 1; } || { echo "exited"; tail -n 3 "$runner_dir/daemon.log"; }
    ;;
  status) if alive; then echo "running pid $(pid_of)"; else echo "not running"; fi; ls -la "$runner_dir/state.json" 2>/dev/null | awk '{print "state file mode", $1, $NF}' ;;
  log) tail -n "${3:-20}" "$runner_dir/daemon.log" ;;
  *) echo "usage: runner.sh start|stop|kill|status|log|config <name> ..." >&2; exit 2 ;;
esac
