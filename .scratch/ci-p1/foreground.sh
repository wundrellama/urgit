#!/bin/bash
# The P1 foreground suites: every +urgit!*-vector generator in desk/gen
# (each prints %.y), `cd fe && npm test` (untouched), and `go test ./...`
# in runner/. Tails are pasted into the live table.
source "$(dirname "$0")/env.sh"
dojo="$P0/dojo.sh"
fails=0
for f in "$ROOT"/desk/gen/*-vector.hoon; do
  v=$(basename "$f" .hoon)
  printf '== +urgit!%s -> ' "$v"
  out=$("$dojo" "+urgit!$v" 900 4 | grep -oE '^%\.[yn]$' | tail -1)
  echo "${out:-<no verdict>}"; [ "$out" = "%.y" ] || fails=$((fails+1))
done
echo "== cd fe && npm test"
( cd "$ROOT/fe" && npm test 2>&1 | tail -8 )
echo "== go test ./... (runner/)"
( cd "$ROOT/runner" && go test ./... 2>&1 | grep -v 'no test files' )
echo "== go vet, gofmt"
( cd "$ROOT/runner" && go vet ./... && [ -z "$(gofmt -l .)" ] && echo "vet ok, gofmt clean" )
echo "== vectors failing: $fails"
[ "$fails" = 0 ]
