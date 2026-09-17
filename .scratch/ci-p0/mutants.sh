#!/bin/bash
# usage: mutants.sh apply | revert | status
# The seven one-line guard removals that must turn every negative row RED
# (H9-H15). `apply` edits the working tree (each old text must occur exactly
# once, or nothing is written). `revert` restores a snapshot of the current
# working tree, preserving uncommitted P2 work. The mutations are never committed.
#
#   H9   app/urgit-ci.hoon  handle-result: the "no jobResult event was relayed"
#                           409 becomes acceptance (close passed, answer 200)
#   H10  app/urgit-ci.hoon  on-arvo %deadline wake: closes %job-result %success
#                           instead of %infrastructure-error
#   H11  app/urgit-ci.hoon  close-attempt: every job result is %passed
#   H12  app/urgit-ci.hoon  attempt-authorized: the daemon bearer check is %.y
#   H13  lib/ci-event.hoon  max-line 65.536 -> 10.000.000
#   H14  app/urgit.hoon     ci-gate-error: the liveness outage branch returns ~
#                           (fail open) instead of the refusal
#   H15  lib/ci-storage.hoon sign-get: the trust-class check ?. =(requester trust)
#                           becomes ?. %.y (never refuses)
source "$(dirname "$0")/env.sh"
cd "$ROOT"
# never mutate anything but a git work tree: reached through a symlinked
# desk/ this once edited a tree that `git checkout --` could not restore
git rev-parse --is-inside-work-tree >/dev/null || { echo "mutants.sh: $PWD is not a git work tree"; exit 1; }
FILES=(desk/app/urgit-ci.hoon desk/lib/ci-event.hoon desk/app/urgit.hoon desk/lib/ci-storage.hoon)
case "${1:-}" in
  apply)
    python3 - <<'PY'
import json, os, pathlib, sys
snapshot = pathlib.Path(os.environ['TMP']) / 'ci-p0-mutants.json'
if snapshot.exists(): raise SystemExit('mutants already applied; revert first')
edits = [
 ("H9",  "desk/app/urgit-ci.hoon",
  "      (emit (give-error eyre-id 409 'no jobResult event was relayed for this attempt'))\n",
  "      =.(state (close-attempt u.found [%job-result u.result]) (emit (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found)))))\n"),
 ("H10", "desk/app/urgit-ci.hoon",
  "      (close-attempt:hc u.found [%infrastructure-error 'no result arrived before the deadline'])\n",
  "      (close-attempt:hc u.found [%job-result %success])\n"),
 ("H11", "desk/app/urgit-ci.hoon",
  "      %job-result            ?:(=(%success result.attempt-result) %passed %failed)\n",
  "      %job-result            %passed\n"),
 ("H12", "desk/app/urgit-ci.hoon",
  "  ?~  found  authenticated.req\n  (daemon-authorized req u.found)\n",
  "  ?~  found  authenticated.req\n  %.y\n"),
 ("H13", "desk/lib/ci-event.hoon",
  "++  max-line    65.536\n",
  "++  max-line    10.000.000\n"),
 ("H14", "desk/app/urgit.hoon",
  "  ?.  ?&(?=(%& -.live) p.live)\n    `[outage ~]\n",
  "  ?.  ?&(?=(%& -.live) p.live)\n    ~\n"),
 ("H15", "desk/lib/ci-storage.hoon",
  "  ?.  =(requester trust)  ~\n",
  "  ?.  %.y  ~\n"),
]
texts = {}
originals = {}
for row, path, old, new in edits:
    s = texts.get(path) or open(path).read()
    originals.setdefault(path, s)
    n = s.count(old)
    expected = 2 if row == 'H15' else 1
    if n != expected:
        sys.exit(f"mutants.sh: {row}: expected {expected} match(es) in {path}, found {n}; nothing written")
    texts[path] = s.replace(old, new, 1)
snapshot.write_text(json.dumps({'originals': originals, 'changed': texts}))
for path, s in texts.items():
    open(path, "w").write(s)
    print(f"mutated {path}")
PY
    ;;
  revert)
    python3 - <<'PY'
import json, os, pathlib
snapshot = pathlib.Path(os.environ['TMP']) / 'ci-p0-mutants.json'
if snapshot.exists():
    saved = json.loads(snapshot.read_text())
    for name, original in saved['originals'].items():
        if pathlib.Path(name).read_text() not in (original, saved['changed'][name]):
            raise SystemExit('source changed while mutated; refusing to overwrite ' + name)
    for name, original in saved['originals'].items():
        pathlib.Path(name).write_text(original)
    snapshot.unlink()
print('reverted: original working tree restored')
PY
    ;;
  status)
    if [ -f "$TMP/ci-p0-mutants.json" ]; then echo MUTATED; else echo 'real build'; fi
    ;;
  *) echo "usage: mutants.sh apply|revert|status" >&2; exit 2 ;;
esac
