#!/bin/bash
# usage: r-mutants.sh apply [rows...] | revert | status | tripwire <row>
# One-line sabotage per P3 negative row, in P2's q-mutants.sh shape:
# `apply` edits the working tree (each old text must occur exactly once,
# or nothing is written) and refuses on a dirty file; `revert` is
# `git checkout --` of the files; `tripwire <row>` prints the substring
# the sabotaged build must produce in the row's log, and r-negatives.sh
# red counts a row RED only when it FAILS *and* carries it. Never
# committed. The rows are grouped so that no row is turned red by another
# row's mutant (r-negatives.sh runs the groups in turn):
#
#   R3   app/urgit-ci.hoon   %expire-token deletes an enrolled daemon's record
#                            (the 409 is skipped)
#   R4   app/urgit-ci.hoon   a revoked daemon's poll is not told why: the
#                            401 is the generic one, the daemon exits as
#                            "enrollment lost", never "revoked by the ship"
#   R4b  app/urgit-ci.hoon   %revoke-daemon leaves the daemon's running
#                            attempts running (no re-offer; they wait ~h1)
#   R5   app/urgit-ci.hoon   %rotate-ci-key keeps the old key: the enrolled
#                            daemon's next assignment verifies
#   R5b  app/urgit-ci.hoon   an abandon over a signature/key reason does not
#                            mark the daemon refused (CI-DELIVERY-1.1 b): the
#                            scheduler keeps it eligible
source "$(dirname "$0")/lib.sh"
cd "$ROOT"
git rev-parse --is-inside-work-tree >/dev/null || { echo "r-mutants.sh: $PWD is not a git work tree"; exit 1; }
FILES=(desk/app/urgit-ci.hoon desk/lib/ci-plan.hoon desk/lib/ci-event.hoon runner/internal/daemon/daemon.go runner/internal/relay/relay.go)
case "${1:-}" in
  apply)
    if ! git diff --quiet -- "${FILES[@]}"; then
      echo "r-mutants.sh: uncommitted changes in ${FILES[*]}; commit them first (revert is git checkout --)" >&2
      exit 1
    fi
    python3 - "${@:2}" <<'PY'
import sys
only = set(sys.argv[1:])
edits = [
 ("R3", "desk/app/urgit-ci.hoon",
  "    ?:  ?&(?=(^ enrolled.u.found) ?=(~ revoked.u.found))\n      ~|  'daemon is enrolled; revoke it instead'\n",
  "    ?:  %.n\n      ~|  'daemon is enrolled; revoke it instead'\n"),
 ("R4", "desk/app/urgit-ci.hoon",
  "  ?:  ?&(?=(^ revoked.u.found) ?=(^ (presented-bearer-hash req)))\n    (emit (give-error eyre-id 401 revoked-refusal))\n",
  "  ?:  %.n\n    (emit (give-error eyre-id 401 revoked-refusal))\n"),
 ("R4b", "desk/app/urgit-ci.hoon",
  "      (reoffer-attempt attempt 'daemon revoked; re-offered')\n",
  "      state\n"),
 ("R5", "desk/app/urgit-ci.hoon",
  "      %rotate-ci-key\n    =.  signing  `fresh-signing-key\n",
  "      %rotate-ci-key\n    =.  signing  signing\n"),
 ("R5b", "desk/app/urgit-ci.hoon",
  "  =?  daemons  refusal\n",
  "  =?  daemons  %.n\n"),
]
texts = {}
for row, path, old, new in edits:
    if only and row not in only:
        continue
    s = texts.get(path) or open(path).read()
    n = s.count(old)
    if n != 1:
        sys.exit(f"r-mutants.sh: {row}: expected exactly one match in {path}, found {n}; nothing written")
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
  tripwire)
    case "${2:-}" in
      R3)  echo "expire enrolled -> 409: FAIL (observed: 200" ;;
      R4)  echo "daemon b logged the revocation on its next poll: FAIL" ;;
      R4b) echo "the attempt on b is re-offered, not left running: FAIL (observed: %running" ;;
      R5)  echo "daemon a refused the assignment after the rotation: FAIL (observed: 0" ;;
      R5b) echo "the ship de-listed a: the panel reads refused: FAIL (observed: healthy" ;;
      *) echo "r-mutants.sh: no tripwire for row '${2:-}'" >&2; exit 2 ;;
    esac
    ;;
  *) echo "usage: r-mutants.sh apply|revert|status|tripwire <row>" >&2; exit 2 ;;
esac
