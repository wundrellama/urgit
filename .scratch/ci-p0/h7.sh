#!/bin/bash
# Row H7: run the real act against a checkout of the candidate OID with the
# fixture-pass workflow and relay every --json line to the attempt's event
# route with a shell loop. Assert outputs.suite == 'true' on the attempt.
# usage: h7.sh [workflow-name] [job]   (default fixture-pass / pass)
source "$(dirname "$0")/env.sh"
source "$TMP/oids.env"
api="$ROOT/.scratch/ci-p0/api.sh"
dojo="$ROOT/.scratch/ci-p0/dojo.sh"
wf="${1:-fixture-pass}"; job="${2:-pass}"; tag="${3:-h7}"
cd "$TMP/clone-a"
git checkout -q "$THREE"
echo "== act on $(git rev-parse HEAD) with $wf/$job"
act push -W ".github/workflows/$wf.yml" -j "$job" -P ubuntu-latest=catthehacker/ubuntu:act-latest \
  --network bridge --json --pull=false > "$TMP/$tag-act.log" 2> "$TMP/$tag-act.err" || true
echo "act exit=$? lines=$(wc -l < "$TMP/$tag-act.log")"
n=0; : > "$TMP/$tag-relay.log"
while IFS= read -r line; do
  printf '%s' "$line" > "$TMP/$tag-line.json"
  "$api" POST "/ci/attempt/$ATTEMPT/event" "@$TMP/$tag-line.json" "$BEARER" >> "$TMP/$tag-relay.log"
  n=$((n+1))
done < "$TMP/$tag-act.log"
echo "relayed $n lines; last answer:"
tail -n 2 "$TMP/$tag-relay.log"
echo "== attempt state"
"$dojo" ".^((unit attempt:ci) %gx /=urgit-ci=/attempt/$ATTEMPT/noun)" 60 20 | grep -E 'status|events|outputs|job-result' | tee "$TMP/$tag-attempt.txt"
git checkout -q master
