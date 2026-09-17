#!/bin/bash
# Preserve prior evidence and piers, then boot the footer's two fresh ships.
# The composed battery runs after this prelude, with its own transcripts.
source "$(dirname "$0")/env.sh"
set -euo pipefail
for check in "$P0/mutants.sh" "$P1/mutants.sh" "$P1/q-mutants.sh"; do
  "$check" status | grep -qx 'real build'
done
"$P1/p2-shutdown.sh"
python3 - <<'PY'
import os, pathlib, shutil, time
root = pathlib.Path(os.environ['TMP'])
stamp = str(time.time_ns())
archive = root / ('before-cold-' + stamp)
archive.mkdir(mode=0o700)
piers = [pathlib.Path(os.environ[key]) for key in ('PIER', 'PIER2')]
for proc in pathlib.Path('/proc').glob('[0-9]*/cmdline'):
    try:
        args = proc.read_bytes().split(b'\0')
    except (OSError, ProcessLookupError):
        continue
    if args and args[0] == os.fsencode(os.environ['URBIT']):
        if any(os.fsencode(p) in args for p in piers):
            raise SystemExit('Refusing to move an active pier: ' + str(proc))
for scope in (root, root / 'peer'):
    target = archive if scope == root else archive / 'peer'
    target.mkdir(mode=0o700, exist_ok=True)
    for file in scope.iterdir():
        if file.is_file():
            shutil.copy2(file, target / file.name)
for pier in piers:
    if pier.exists():
        saved = pier.with_name(pier.name + '-before-cold-' + stamp)
        pier.rename(saved)
        print('Prior pier retained:', saved, flush=True)
(root / 'cold-archive.txt').write_text(str(archive) + '\n')
print('Prior evidence retained:', archive, flush=True)
PY
"$P0/boot.sh"
CI_PEER=1 "$P0/boot.sh"
"$P1/store.sh" start
echo "Cold ships ready ($(date -Is)); run battery.sh all."
