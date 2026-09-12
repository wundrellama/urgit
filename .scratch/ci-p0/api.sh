#!/bin/bash
# usage: api.sh <METHOD> <path-under-/apps/urgit/api> [json-body-or-@file] [bearer]
# Logs the ship session in once (cookie jar), or uses a daemon bearer when
# given as the fourth argument (pass "-" for no session cookie and no bearer).
# Prints "<status> <body>".
source "$(dirname "$0")/env.sh"
method="$1"; path="$2"; body="${3:-}"; bearer="${4:-}"
if [ "$bearer" = "" ] && [ ! -s "$JAR" ]; then
  curl -s -o /dev/null -c "$JAR" -X POST "$URL/~/login" --data-urlencode "password=$CODE"
fi
args=(-s -X "$method" "$URL/apps/urgit/api$path" -w '\n%{http_code}')
case "$bearer" in
  "") args+=(-b "$JAR") ;;
  -)  ;;
  *)  args+=(-H "x-ci-bearer: $bearer") ;;
esac
if [ -n "$body" ]; then
  args+=(-H 'content-type: application/json')
  case "$body" in
    @*) args+=(--data-binary "$body") ;;
    *)  args+=(--data "$body") ;;
  esac
fi
out=$(curl "${args[@]}")
status=$(printf '%s' "$out" | tail -n1)
printf '%s ' "$status"
printf '%s' "$out" | sed '$d'
echo
