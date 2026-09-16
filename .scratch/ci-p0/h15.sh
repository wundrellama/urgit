#!/bin/bash
# Row H15: signing a GET for the %untrusted namespace from the %trusted
# attempt returns ~; the attempt's own class signs a URL under the ci/ prefix.
# The object name goes through (scot %t 'cache.tar'): a bare dotted segment
# is a dojo path syntax error that leaves the pane on a continuation prompt.
source "$(dirname "$0")/env.sh"
source "$HERE/lib.sh"
source "$TMP/oids.env"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
echo "== attempt $ATTEMPT is %trusted:"
echo "trust=$(att_field "$ATTEMPT" trust)"
# the (unit @t) is read joined: a narrow pane pretty-prints it over lines
echo "== sign-get %untrusted from the %trusted attempt (expect ~):"
dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/untrusted/(scot %t 'cache.tar')/noun)"
echo "== sign-get %trusted from the %trusted attempt (expect a URL under ci/):"
dojo_unit_cord ".^((unit @t) %gx /=urgit-ci=/sign-get/$ATTEMPT/trusted/(scot %t 'cache.tar')/noun)"
