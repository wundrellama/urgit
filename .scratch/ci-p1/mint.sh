#!/bin/bash
# Mints an enrollment token on the ship and prints it (the only place the
# raw token appears is this output and the daemon's config file). P3 D1:
# the ship mints it — `POST ci/runners/mint` with the ship session — and
# answers it exactly once; the dojo generator is gone. The whole answer
# (id, token, the config snippet, the record) is kept in $TMP/mint-last.json
# for the rows that read it; only the token is printed.
source "$(dirname "$0")/env.sh"
answer=$("$P0/api.sh" POST /ci/runners/mint '{}')
status=${answer%% *}
[ "$status" = 200 ] || { echo "mint.sh: POST ci/runners/mint answered $answer" >&2; exit 1; }
printf '%s' "${answer#* }" > "$TMP/mint-last.json"
token=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$TMP/mint-last.json")
[ -n "$token" ] || { echo "mint.sh: no token in the mint answer" >&2; exit 1; }
echo "$token"
