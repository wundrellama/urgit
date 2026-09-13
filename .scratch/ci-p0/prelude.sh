#!/bin/bash
# Dojo prelude, run exactly once by setup.sh before any row: bind `ci` to the
# built sur file so the `candidate:ci` / `attempt:ci` casts in the row scries
# resolve. The path is /=urgit=/sur/ci/hoon, not %/sur/ci/hoon: in a fresh
# dojo `%` is %base and the build fails with -find.ci.
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
echo "== =ci -build-file /=urgit=/sur/ci/hoon"
"$dojo" '=ci -build-file /=urgit=/sur/ci/hoon' 180 3 | tail -2
echo "== probe: a candidate:ci cast on an unknown id must print ~"
"$dojo" '.^((unit candidate:ci) %gx /=urgit-ci=/candidate/0v0/noun)' 60 3 | tail -2
