#!/bin/bash
# P0 starts with fresh CI state and no configured storage endpoint.
# Keep the fixture piers and repositories, and archive P2 evidence first.
source "$(dirname "$0")/env.sh"
set -euo pipefail
"$P1/q-mutants.sh" status | grep -qx 'real build'
"$P0/mutants.sh" status | grep -qx 'real build'
"$P1/mutants.sh" status | grep -qx 'real build'
for name in a b; do "$P1/runner.sh" stop "$name"; done
python3 - <<'PY'
import os,pathlib,shutil,time
root=pathlib.Path(os.environ['TMP'])
archive=root/('before-s6-'+str(time.time_ns()))
archive.mkdir(mode=0o700)
for file in root.iterdir():
    if file.is_file() and file.suffix in ('.log','.json','.jsonl','.env','.png','.txt'):
        shutil.copy2(file,archive/file.name)
print('Prior stage evidence archived:',archive.name)
PY
"$P1/nuke-revive.sh" s6-p0
"$P0/poke.sh" storage storage-action "[%set-endpoint '']" > "$TMP/s6-storage-unconfigured.log"
echo 'Fresh state-0 CI controller; storage endpoint cleared for H1.'
