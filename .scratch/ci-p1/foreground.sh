#!/bin/bash
# The P1 foreground suites: every +urgit!*-vector generator in desk/gen
# (each prints %.y), `cd fe && npm test` (untouched), and `go test ./...`
# in runner/. Tails are pasted into the live table.
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
    else echo "printed (value dump, no loobean verdict): $(printf '%s\n' "$out" | grep -v '^~ryp' | grep -v '^>' | tail -1 | cut -c1-60)"; fi
  fi
done
echo "== cd fe && npm test"
( cd "$ROOT/fe" && npm test 2>&1 | tail -8 )
echo "== go test ./... (runner/)"
( cd "$ROOT/runner" && go test ./... 2>&1 | grep -v 'no test files' )
echo "== go vet, gofmt"
( cd "$ROOT/runner" && go vet ./... && [ -z "$(gofmt -l .)" ] && echo "vet ok, gofmt clean" )
echo "== vectors failing: $fails"
[ "$fails" = 0 ]
