#!/bin/bash
# Shared readers and drivers for the P2 rows (source after nothing: it
# sources the P1 lib, which sources the P0 env and lib). Everything a P2
# row reads comes through here: the ci/* routes with the ship session, the
# store fixture through store.sh's signed curl, the attempt's log handle
# and the daemon's saved streams.
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p1/lib.sh"
# the P1 lib defaulted REPO to ci-p1; the P2 rows have their own
# repository unless the caller names one in P2_REPO (the negatives do)
REPO="${P2_REPO:-ci-p2}"
CLONE="$TMP/clone-$REPO"
store="$P1/store.sh"
[ -s "$TMP/store.env" ] && source "$TMP/store.env"
# ---- ci/* routes -----------------------------------------------------------------
# ci_get <path-under-/apps/urgit/api/ci> [bearer|-]: "<status> <body>"
ci_get() { "$api" GET "/ci$1" "" "${2:-}"; }
# ci_post <path> <json> [bearer|-]
ci_post() { "$api" POST "/ci$1" "$2" "${3:-}"; }
# ci_action '<json poke>' : POST ci/action with the ship session
ci_action() { "$api" POST /ci/action "$1"; }
# status_of "<status> <body>" -> status; body_of -> body
status_of() { printf '%s' "$1" | cut -d' ' -f1; }
body_of() { printf '%s' "$1" | cut -d' ' -f2-; }
# jq_of "<status> <body>" '<jq filter>'
jq_of() { body_of "$1" | jq -r "$2" 2>/dev/null; }
# a raw curl of a ci route with the session cookie, printing status and
# the Location header (the 302 the log route answers)
ci_location() {  # <path> -> "<status> <location>"
  [ -s "$JAR" ] || "$api" GET /repositories >/dev/null
  curl -s -o /dev/null -b "$JAR" -w '%{http_code} %{redirect_url}\n' "$URL/apps/urgit/api/ci$1"
}
# ---- attempt log handle ----------------------------------------------------------------
# att_log <aid>: `~` or `[~ [key=… size=… sha256=…]]` squeezed to one line
att_log() { dojo_value "log:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" 120 | tr -d '\n' | sed 's/  */ /g; s/^\[ ~ */[~ /; s/ *\]$/]/'; }
att_log_key() { att_log "$1" | grep -oE "key='[^']*'" | cut -d"'" -f2; }
att_log_sha() { att_log "$1" | grep -oE "sha256='[^']*'" | cut -d"'" -f2; }
att_log_size() { att_log "$1" | grep -oE 'size=[0-9.]+' | cut -d= -f2 | tr -d .; }
att_trust() { dojo_value "trust:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))" | one '^%[a-z]+$'; }
cand_trust() { dojo_value "trust:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | one '^%[a-z]+$'; }
ci_protected() { dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t '$1')/noun)" | one '^%\.[yn]$'; }   # <ref> -> %.y/%.n
cand_pull() { dojo_value "pull:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | unit_join | sed 's/^\[~ //; s/\]$//'; }
# presign_scry <aid> <trusted|untrusted> <name> <seconds>: the URL or ~
presign_scry() { dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/presign-get/$1/$2/(scot %t '$3')/$4/noun)" | sed "s/^\[~ '//; s/'\]$//"; }
sign_get_scry() { dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/sign-get/$1/$2/(scot %t '$3')/noun)"; }
# the daemon's saved stream for an attempt
daemon_stream() { echo "$RUNNER_HOME/${2:-a}/work/$1.act.jsonl"; }
sha_of() { sha256sum "$1" | cut -d' ' -f1; }
# wait_log <aid> <seconds>: until the attempt has a log handle
wait_log() { local l; for _ in $(seq 1 "$2"); do l=$(att_log "$1"); case "$l" in "[~ "*) echo "$l"; return 0;; esac; sleep 1; done; echo "$l"; return 1; }
# cand_object_hex <cid>: the materialized object's 40-hex oid (the dojo
# prints @ux with dots and without leading zeros)
cand_object_hex() {
  local o; o=$(cand_object "$1" | sed 's/^\[~ //; s/\]$//; s/^0x//; s/\.//g')
  [ -n "$o" ] && printf '%040s' "$o" | tr ' ' 0
}
cand_head_hex() {
  local o; o=$(dojo_value "head:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))" | one '^0x[0-9a-f.]+$' | sed 's/^0x//; s/\.//g')
  [ -n "$o" ] && printf '%040s' "$o" | tr ' ' 0
}
# ---- daemon records (the P1 ghost, thrice — Deviations) ------------------------------
# daemon_ids: every daemon record the ship holds
daemon_ids() { dojo_value "\`(list @uv)\`~(tap in ~(key by .^((map @uv daemon:ci) %gx /=urgit-ci=/daemons/noun)))" 120 | tr -d '\n' | grep -oE '0v[0-9a-v.]+'; }
# daemon_stale <id>: %.y once the scheduler would no longer select it —
# last-seen older than ~m5 (D6, select-daemon) — or it was never seen
daemon_stale() { dojo_value "=/  d  (need .^((unit daemon:ci) %gx /=urgit-ci=/daemon/$1/noun))  ?~(last-seen.d %.y (gte (sub now u.last-seen.d) ~m5))" | one '^%\.[yn]$'; }
# wait_ghosts_stale [seconds]: until every daemon record but a is stale
# (default 400 s: a record goes stale ~m5 after its daemon's last poll).
# A row whose work exceeds daemon a's capacity must call this first: a
# record the ship saw within ~m5 is still selected although its process
# is gone, an assignment is offered only to its own daemon, and one never
# fetched waits out the ~h1 deadline (urgit-ci.hoon select-daemon,
# handle-assignment-poll, stale-delivery).
wait_ghosts_stale() {
  local t="${1:-400}" i=0 d pending
  while :; do
    pending=""
    for d in $(daemon_ids); do [ "$d" = "${DAEMON_A:-}" ] && continue; [ "$(daemon_stale "$d")" = '%.y' ] || pending="$pending $d"; done
    [ -z "$pending" ] && { echo "-- every daemon record but a ($DAEMON_A) is stale or absent ($(date -Is))"; return 0; }
    [ "$i" -ge "$t" ] && { echo "-- wait_ghosts_stale: still selectable after $t s:$pending" >&2; return 1; }
    echo "-- daemon records the ship would still select besides a:$pending; waiting 15 s ($(date -Is))"
    sleep 15; i=$((i+15))
  done
}
