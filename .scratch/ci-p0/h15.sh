#!/bin/bash
# Row H15: signing a GET for the %untrusted namespace from the %trusted
# attempt returns ~; the attempt's own class signs a URL under the ci/ prefix.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
echo "== attempt $ATTEMPT is %trusted:"
"$dojo" ".^((unit attempt:ci) %gx /=urgit-ci=/attempt/$ATTEMPT/noun)" 60 20 | grep -E '^\s*trust='
echo "== sign-get %untrusted from the %trusted attempt (expect ~):"
"$dojo" ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/untrusted/cache.tar/noun)" 60 3 | tail -2
echo "== sign-get %trusted from the %trusted attempt (expect a URL under ci/):"
"$dojo" ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/trusted/cache.tar/noun)" 60 3 | tail -2
