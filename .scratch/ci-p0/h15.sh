#!/bin/bash
# Row H15: signing a GET for the %untrusted namespace from the %trusted
# attempt returns ~; the attempt's own class signs a URL under the ci/ prefix.
# The object name goes through (scot %t 'cache.tar'): a bare dotted segment
# is a dojo path syntax error that leaves the pane on a continuation prompt.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
echo "== attempt $ATTEMPT is %trusted:"
"$dojo" ".^((unit attempt:ci) %gx /=urgit-ci=/attempt/$ATTEMPT/noun)" 60 20 | grep -E '^\s*trust='
echo "== sign-get %untrusted from the %trusted attempt (expect ~):"
"$dojo" ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/untrusted/(scot %t 'cache.tar')/noun)" 60 3 | tail -2
echo "== sign-get %trusted from the %trusted attempt (expect a URL under ci/):"
"$dojo" ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/trusted/(scot %t 'cache.tar')/noun)" 60 3 | tail -2
