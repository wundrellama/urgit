#!/bin/bash
# usage: mutants.sh apply | revert | status
# The seven one-line guard removals that must turn every negative row RED
# (H9-H15). `apply` edits the working tree (each old text must occur exactly
# once, or nothing is written); `revert` is `git checkout --` of the four
# files, so any uncommitted edit in them would be lost: apply refuses to run
# on a dirty file. The mutations are never committed.
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
    if ! git diff --quiet -- "${FILES[@]}"; then
      echo "mutants.sh: uncommitted changes in ${FILES[*]}; commit them first (revert is git checkout --)" >&2
      exit 1
    fi
    python3 - <<'PY'
import sys
edits = [
 ("H9",  "desk/app/urgit-ci.hoon",
  "      (emit (give-error eyre-id 409 'no jobResult event was relayed for this attempt'))\n",
  "      =.(state (close-attempt u.found [%job-result u.result]) (emit (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found)))))\n"),
 ("H10", "desk/app/urgit-ci.hoon",
  "    %+  close-attempt  attempt\n    :-  %infrastructure-error\n    ?:(silent-before 'runner went silent' 'no result arrived before the deadline')\n",
  "    %+  close-attempt  attempt\n    [%job-result %success]\n"),
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
for row, path, old, new in edits:
    s = texts.get(path) or open(path).read()
    n = s.count(old)
    if n != 1:
        sys.exit(f"mutants.sh: {row}: expected exactly one match in {path}, found {n}; nothing written")
    texts[path] = s.replace(old, new)
for path, s in texts.items():
    open(path, "w").write(s)
    print(f"mutated {path}")
PY
    ;;
  revert)
    git checkout -- "${FILES[@]}"
    git diff --quiet -- "${FILES[@]}" && echo "reverted: ${FILES[*]} clean"
    ;;
  status)
    git diff --stat -- "${FILES[@]}"
    git diff --quiet -- "${FILES[@]}" && echo "clean (real build)" || echo "MUTATED"
    ;;
  *) echo "usage: mutants.sh apply|revert|status" >&2; exit 2 ;;
esac
