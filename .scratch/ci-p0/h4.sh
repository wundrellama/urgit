#!/bin/bash
# Row H4: %materialize the fast-forward candidate from H3. %urgit-ci pokes
# %urgit, which answers %candidate-ready with candidate == head (THREE),
# conflict %.n, status still %pending, and no ref moves.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$HERE/api.sh"
dojo="$HERE/dojo.sh"
echo "== materialize $CAND (head THREE=$THREE)"
"$dojo" ":urgit-ci &ci-action [%materialize $CAND]" 60 3 | tail -2
sleep 3
"$dojo" ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CAND/noun)" 60 40 | tail -38 | tee "$TMP/h4-candidate.txt"
echo "== candidate == head?"
CAND_UX=$(grep -o 'candidate=\[~ 0x[0-9a-f.]*' "$TMP/h4-candidate.txt" | tail -1 | sed 's/candidate=\[~ //')
echo "candidate=$CAND_UX"
echo "head as @ux: 0x$(printf '%s' "$THREE" | sed 's/^0*//' | rev | sed 's/\(....\)/\1./g' | rev | sed 's/^\.//')"
echo "== master still TWO?"
"$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print("master oid:", [r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'
echo "expected TWO=$TWO"
