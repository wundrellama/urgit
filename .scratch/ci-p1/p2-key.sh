#!/bin/bash
# Initialize the CI key after a greenfield CI reset, before enrollment.
# The public response is safe to retain; neither key secret is read.
source "$(dirname "$0")/env.sh"
answer=$("$P0/api.sh" GET /ci/key)
if [[ "$answer" == 503* ]]; then
  "$P0/poke.sh" urgit-ci ci-action '[%rotate-ci-key ~]' > "$TMP/ci-key-init.log"
  answer=$("$P0/api.sh" GET /ci/key)
fi
[[ "$answer" == 200* ]] || { echo "CI key initialization failed: $answer" >&2; exit 1; }
printf '%s' "${answer#200 }" > "$TMP/ci-key.json"
