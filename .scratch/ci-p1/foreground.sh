#!/bin/bash
# The P1 foreground suites: every +urgit!*-vector generator in desk/gen
# (each prints %.y), `cd fe && npm test` (untouched), and in runner/:
# `go test ./...`, `go test -race ./...`, the live Docker boundary test
# against the harness's rootless socket (URGIT_DOCKER_HOST), then the
# static binary and `file` on it (as astra's foreground-go.sh does).
# Tails are pasted into the live table.
source "$(dirname "$0")/env.sh"
set +e
dojo="$P0/dojo.sh"
fails=0
# a vector that ends in a loobean asserts (%.y required); the others dump
# values for inspection and must only build and print (no crash)
for f in "$ROOT"/desk/gen/*-vector.hoon; do
  v=$(basename "$f" .hoon)
  printf '== +urgit!%s -> ' "$v"
  out=$("$dojo" "+urgit!$v" 900 6)
  if grep -qE '^%\.y$|^\?~  failures|^%\.n$' "$f"; then
    verdict=$(printf '%s\n' "$out" | grep -oE '^%\.[yn]$' | tail -1)
    echo "${verdict:-<no verdict>}"; [ "$verdict" = "%.y" ] || fails=$((fails+1))
  else
    if printf '%s\n' "$out" | grep -qE 'generator-build-fail|dojo: hoon expression failed|bail:'; then echo "FAILED to build or run"; fails=$((fails+1))
    else echo "printed (value dump, no loobean verdict): $(printf '%s\n' "$out" | grep -v "^~$SHIP" | grep -v '^>' | grep -vE '^(gall|behn|clay|ames|eyre|dill|kiln|iris|jael|khan|lick|arvo): ' | tail -1 | cut -c1-60)"; fi
  fi
done
echo "== cd fe && npm test"
( cd "$ROOT/fe" && set -o pipefail; npm test 2>&1 | tail -8 ) || fails=$((fails+1))
# the Go suites: every step's exit code counts, so a red suite or a
# non-static binary fails the foreground like a red vector does
gofails=0
step() {  # <title> <command...>: run in runner/, count a non-zero exit
  local title="$1"; shift
  echo "== $title"
  ( cd "$ROOT/runner" && "$@" ) 2>&1 | grep -v 'no test files'
  [ "${PIPESTATUS[0]}" = 0 ] || { echo "   -> FAILED: $title"; gofails=$((gofails+1)); }
}
step "go test ./... (runner/)" go test ./...
step "go test -race ./..." go test -race ./...
step "live Docker boundary (URGIT_DOCKER_HOST=unix://$DOCKER_SOCK)" env "URGIT_DOCKER_HOST=unix://$DOCKER_SOCK" go test -count=1 -run TestDockerLiveBoundary -v ./internal/sandbox
step "go vet, gofmt" bash -c 'go vet ./... && [ -z "$(gofmt -l .)" ] && echo "vet ok, gofmt clean"'
step "static binary" env CGO_ENABLED=0 go build -trimpath -o "$TMP/urgit-runner-checked" ./cmd/urgit-runner
step "file" bash -c 'file "$1" && file "$1" | grep -qF "statically linked"' _ "$TMP/urgit-runner-checked"
echo "== vectors failing: $fails; go steps failing: $gofails"
[ "$fails" = 0 ] && [ "$gofails" = 0 ]
