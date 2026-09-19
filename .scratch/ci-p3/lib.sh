#!/bin/bash
# Shared readers and drivers for the P3 rows (BRIEF-CI-P3 §5): sources the
# P2 lib (which sources P1's, which sources the P0 env and lib), and adds
# the operator surface's routes — the Runners panel's GET ci/runners and
# POST ci/runners/mint, the fact paths through Eyre's channel, the storage
# probe — and the daemon-record readers the P3 rows assert against.
# The store the P3 rows use is advertised on the host's LAN address
# (STORE_ADVERTISE, the footer's 192.168.1.229) unless a caller set it:
# a 127.0.0.1 endpoint is R7's RED case, not the default.
export STORE_ADVERTISE="${STORE_ADVERTISE:-192.168.1.229}"
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p2/lib.sh"
P3="$ROOT/.scratch/ci-p3"
# the P3 rows have their own repository unless the caller names one
REPO="${P3_REPO:-ci-p3}"
CLONE="$TMP/clone-$REPO"
[ -s "$TMP/p3.env" ] && source "$TMP/p3.env"
# ---- the Runners panel's routes --------------------------------------------------
runners_json() { body_of "$(ci_get /runners)"; }
# runner_field <id> <jq-expr>: one field of one daemon record as the
# panel reads it (GET ci/runners); empty when the record is absent
runner_field() { runners_json | jq -r --arg id "$1" ".runners[] | select(.id == \$id) | $2" 2>/dev/null; }
runner_state() { runner_field "$1" .state; }
runner_count() { runners_json | jq -r '.runners | length'; }
# wait_runner_state <id> <state-regex> <seconds>
wait_runner_state() {
  local s; for _ in $(seq 1 "$3"); do s=$(runner_state "$1"); if [[ "$s" =~ ^($2)$ ]]; then echo "$s"; return 0; fi; sleep 1; done
  echo "$s"; return 1
}
# mint: POST ci/runners/mint; prints the token, keeps the whole answer in
# $TMP/mint-last.json (ci-p1/mint.sh)
mint_token() { "$P1/mint.sh"; }
mint_id() { jq -r .id "$TMP/mint-last.json"; }
# ---- daemon records on the ship ---------------------------------------------------
# daemon_field <id> <field>: a field of the daemon record by scry
daemon_field() { dojo_value "$2:(need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$1/noun))" 60; }
daemon_exists() { dojo_value "!=(~ .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$1/noun))" | one '^%\.[yn]$'; }
daemon_id_of() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["daemon_id"])' "$RUNNER_HOME/$1/state.json"; }
# every daemon record the scheduler could still select (live: enrolled,
# bearer, unrevoked, unrefused, seen within ~m5) besides the named ones
live_daemons_besides() {
  local d skip="$*" out=""
  for d in $(daemon_ids); do
    case " $skip " in *" $d "*) continue;; esac
    [ "$(dojo_value "=/  d  (need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$d/noun))  ?&(?=(^ enrolled.d) ?=(^ bearer-hash.d) ?=(~ revoked.d) ?=(~ refused.d) ?=(^ last-seen.d) (lte (sub now (min now (need last-seen.d))) ~m5))" | one '^%\.[yn]$')" = '%.y' ] && out="$out $d"
  done
  echo "$out"
}
# wait_live_only <ids...> [-- seconds]: until no daemon record but the
# named ones is selectable (a stopped daemon's record stays live for
# stale-after ~m5 — the P1 ghost; a row that stops a test daemon revokes
# its record instead, so this normally passes at once)
wait_live_only() {
  local t=400 i=0 pending
  while :; do
    pending=$(live_daemons_besides "$@")
    [ -z "${pending// /}" ] && { echo "-- no live daemon record besides $* ($(date -Is))"; return 0; }
    [ "$i" -ge "$t" ] && { echo "-- wait_live_only: still live after $t s:$pending" >&2; return 1; }
    echo "-- live daemon records besides $*:$pending; waiting 15 s ($(date -Is))"
    sleep 15; i=$((i+15))
  done
}
# retire_daemon <name>: stop the test daemon and revoke its record, so
# the scheduler never hands it work again (then remove the record)
retire_daemon() {
  local id; id=$(daemon_id_of "$1" 2>/dev/null)
  "$P1/runner.sh" stop "$1" >/dev/null 2>&1
  [ -n "$id" ] || return 0
  ci_action "{\"action\":\"revoke-daemon\",\"id\":\"$id\"}" >/dev/null
  ci_action "{\"action\":\"expire-token\",\"id\":\"$id\"}" >/dev/null
  echo "-- daemon $1 ($id) retired: revoked and removed"
}
# start_daemon <name> <capacity> [labels-toml-body] [extra-toml-lines]:
# mint a token on the ship and start the real daemon with it; records
# DAEMON_<NAME> in $TMP/p3.env
start_daemon() {
  local name="$1" cap="${2:-1}" token
  "$P1/runner.sh" stop "$name" >/dev/null 2>&1
  rm -rf "$RUNNER_HOME/$name"
  token=$(mint_token) || return 1
  RUNNER_LABELS="${3:-}" RUNNER_EXTRA="${4:-}" "$P1/runner.sh" start "$name" "$cap" "$token" | head -1
  local id; id=$(daemon_id_of "$name")
  [ -n "$id" ] || { echo "start_daemon: $name did not enroll" >&2; return 1; }
  sed -i "/^export DAEMON_${name^^}=/d" "$TMP/p3.env" 2>/dev/null
  echo "export DAEMON_${name^^}=$id" >> "$TMP/p3.env"
  eval "export DAEMON_${name^^}=$id"
  echo "-- daemon $name enrolled as $id (capacity $cap${3:+, labels [$3]})"
}
# ---- the channel (D4) ---------------------------------------------------------------
# channel_open <path> <file> [seconds]: subscribe the ship session to
# %urgit-ci's fact path through Eyre's channel and stream the SSE events
# into <file> in the background for <seconds> (default 300); prints the
# curl pid. The client acks nothing: Eyre keeps the events, which is fine
# for a row that reads them once.
channel_open() {
  local path="$1" file="$2" secs="${3:-300}"
  local ch="$URL/~/channel/$(date +%s)-p3$RANDOM"
  [ -s "$JAR" ] || "$api" GET /repositories >/dev/null
  local code; code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X PUT "$ch" -H 'content-type: application/json' \
    --data "[{\"id\":1,\"action\":\"subscribe\",\"ship\":\"$SHIP\",\"app\":\"urgit-ci\",\"path\":\"$path\"}]")
  [ "$code" = 204 ] || { echo "channel_open: subscribe PUT answered $code" >&2; return 1; }
  : > "$file"
  ( curl -s -N --max-time "$secs" -b "$JAR" "$ch" >> "$file" 2>/dev/null ) &
  echo "$!"
}
# channel_facts <file>: the `json` of every diff event in the stream, one
# JSON object per line, in order
channel_facts() { grep -E '^data:' "$1" | sed 's/^data: *//' | jq -c 'select(.response == "diff") | .json' 2>/dev/null; }
# wait_fact <file> <jq-test> <seconds>: until a fact satisfies the test
wait_fact() {
  local i; for i in $(seq 1 "$3"); do
    if channel_facts "$1" | jq -e "$2" >/dev/null 2>&1; then return 0; fi; sleep 1
  done; return 1
}
