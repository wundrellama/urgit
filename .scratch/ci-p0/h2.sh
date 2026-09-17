#!/bin/bash
# Row H2: configure %storage (six %storage-action pokes; fixture endpoint
# http://127.0.0.1:1, no request is made in P0), then %set-ci-protected is
# accepted and the D3 scry answers %.y. %set-current-bucket also adds the
# bucket to the set, so no separate %add-bucket is needed.
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
poke="$HERE/poke.sh"
# P2: when the store fixture is up (store.sh start wrote $TMP/store.env),
# %storage points at it, so the P1 rows that follow upload real logs;
# without it the P0 fixture values stand (no request is made in P0)
if [ -s "$TMP/store.env" ]; then
  echo "== configure %storage (the RustFS fixture)"
  "$(dirname "$0")/../ci-p1/store.sh" configure
else
echo "== configure %storage"
for act in \
  "[%set-endpoint 'http://127.0.0.1:1']" \
  "[%set-access-key-id 'P0EXAMPLEKEY']" \
  "[%set-secret-access-key 'p0-example-secret']" \
  "[%set-current-bucket 'ci-bucket']" \
  "[%set-region 'local-1']" \
  "[%toggle-service %credentials]"; do
  printf '%s -> ' "$act"; "$poke" storage storage-action "$act" | tail -1
done
fi
echo "== %set-ci-protected ci-fixture refs/heads/master (expect >=)"
"$dojo" ":urgit-ci &ci-action [%set-ci-protected 'ci-fixture' 'refs/heads/master' %.y]" 60 3 | tail -2
echo "== D3 scry (expect %.y)"
"$dojo" ".^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)" 60 3 | tail -2
echo "== D3 scry for an unprotected ref (expect %.n)"
"$dojo" ".^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/side')/noun)" 60 3 | tail -2
