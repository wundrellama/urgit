#!/bin/bash
# Row H3: push a third commit to the CI-protected master. Expect
# `ng refs/heads/master staged as ci candidate <id>; checks pending`, the ref
# unchanged, and the pushed objects kept (objectCount grows).
source "$(dirname "$0")/env.sh"
source "$HERE/lib.sh"
source "$TMP/oids.env"
echo "== before: objectCount=$(object_count) master=$(master_oid) (TWO=$TWO)"
cd "$TMP/clone-a"
echo "three" >> README.md
git commit -qam "three: direct push to a ci-protected branch"
THREE=$(git rev-parse HEAD)
echo "THREE=$THREE"
echo "== push commit three"
git push origin master 2>&1 | tee "$TMP/h3-push.log" | grep -E 'rejected|staged|master' || true
echo "== after:  objectCount=$(object_count) master=$(master_oid) (must still be TWO)"
CAND=$(grep -o 'staged as ci candidate 0v[0-9a-v.]*' "$TMP/h3-push.log" | head -1 | sed 's/.*candidate //')
echo "CAND=$CAND"
echo "export THREE=$THREE" >> "$TMP/oids.env"
echo "export CAND=$CAND" >> "$TMP/oids.env"
