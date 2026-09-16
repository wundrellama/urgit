#!/bin/bash
# Row H5: a diverging push from a second clone is staged; materializing it
# yields a two-parent merge candidate (first parent = destination tip, second
# = pushed head) while master stays unchanged. The candidate is exposed on a
# scratch ref (refs/ci/candidate) with the existing %set-ref poke so a clone
# can inspect it: upload-pack only serves objects reachable from a ref.
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
B="$TMP/clone-b"
rm -rf "$B"
git clone -q "$URL/git/ci-fixture" "$B"
cd "$B"
git config user.name harness-b; git config user.email b@example
git config http.cookieFile "$JAR"
echo "== clone-b master: $(git rev-parse master) (expect TWO=$TWO)"
# a truly divergent head: based on commit one, so it does not descend from
# the destination tip (two); git needs --force to send old=two new=head
git reset -q --hard "$ONE"
echo "diverging" > b.txt
git add -A && git commit -qm "b: diverging commit from a second clone"
DIVERGE=$(git rev-parse HEAD)
echo "DIVERGE=$DIVERGE"
git push --force origin master 2>&1 | tee "$TMP/h5-push.log" | grep -E 'rejected|staged' || true
CAND2=$(grep -o 'staged as ci candidate 0v[0-9a-v.]*' "$TMP/h5-push.log" | head -1 | sed 's/.*candidate //')
echo "CAND2=$CAND2"
echo "== materialize (divergent: expect a merge commit)"
"$dojo" ":urgit-ci &ci-action [%materialize $CAND2]" 60 3 | tail -2
sleep 3
"$dojo" ".^((unit candidate:ci) %gx /=urgit-ci=/candidate/$CAND2/noun)" 60 40 | tail -38 | tee "$TMP/h5-candidate.txt"
MERGE_UX=$(grep -o 'candidate=\[~ 0x[0-9a-f.]*' "$TMP/h5-candidate.txt" | tail -1 | sed 's/candidate=\[~ //')
echo "MERGE_UX=$MERGE_UX"
echo "== expose the merge object on refs/ci/candidate and inspect it from clone-b"
"$dojo" ":urgit &git-action [%set-ref 'ci-fixture' 'refs/ci/candidate' $MERGE_UX]" 60 3 | tail -2
git fetch -q origin refs/ci/candidate
MERGE=$(git rev-parse FETCH_HEAD)
echo "MERGE=$MERGE"
git cat-file -p FETCH_HEAD | head -6
echo "== parents (expect first=TWO second=DIVERGE)"
git rev-list --parents -n 1 FETCH_HEAD
echo "== master still unchanged?"
"$api" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print("master oid:", [r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"], "objectCount:", d["objectCount"])'
cat >> "$TMP/oids.env" <<EOF
export DIVERGE=$DIVERGE
export CAND2=$CAND2
export MERGE=$MERGE
EOF
