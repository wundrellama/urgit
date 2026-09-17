#!/bin/bash
# usage: q-mutants.sh apply | revert | status | tripwire <row>
# One-line sabotage per P2 negative row (Q4, Q5a, Q5, Q6, Q8, Q10, Q11,
# Q12, Q13, Q16) that must turn the row RED, in P1's mutants.sh shape:
# `apply` edits the working tree (each old text must occur exactly once,
# or nothing is written) and refuses on a dirty file; `revert` is
# `git checkout --` of the files; `tripwire <row>` prints the substring
# the sabotaged build must produce in the row's log (alternatives one per
# line), and q-negatives.sh red counts a row RED only when it FAILS *and*
# carries it. Hoon mutants need a rebuild; Go mutants need `go build`
# and a daemon restart (q-negatives.sh does both). Never committed.
#
#   Q4   lib/ci-storage.hoon   upload-name-allowed: every name passes
#   Q5a  app/urgit.hoon        the web merge writes the ref even when the
#                              target is CI-protected (the gate is skipped)
#   Q5   app/urgit.hoon        the web merge stages every actor as %trusted
#   Q6   app/urgit-ci.hoon     a passed %untrusted candidate is asked to land
#   Q8   app/urgit-ci.hoon     %approve-candidate accepts any actor
#   Q10  app/urgit-ci.hoon     grants are released to an %untrusted job too
#   Q11  app/urgit-ci.hoon     handle-event keeps the event unscrubbed: act's
#                              set-output line carries the raw value in `arg`
#                              (measured), so the attempt's outputs would hold it
#   Q12  runner daemon.go      the assignment signature is not verified
#   Q13  runner daemon.go      an expired grant is still passed to act
#   Q16  app/urgit-ci.hoon     the ci/* session check is skipped
source "$(dirname "$0")/lib.sh"
cd "$ROOT"
git rev-parse --is-inside-work-tree >/dev/null || { echo "q-mutants.sh: $PWD is not a git work tree"; exit 1; }
FILES=(desk/lib/ci-storage.hoon desk/app/urgit-ci.hoon desk/app/urgit.hoon runner/internal/daemon/daemon.go)
case "${1:-}" in
  apply)
    if ! git diff --quiet -- "${FILES[@]}"; then
      echo "q-mutants.sh: uncommitted changes in ${FILES[*]}; commit them first (revert is git checkout --)" >&2
      exit 1
    fi
    python3 - <<'PY'
import sys
edits = [
 ("Q4", "desk/lib/ci-storage.hoon",
  "  ?:  =('log.jsonl' name)  %.y\n  ?:  =('summary.md' name)  %.y\n",
  "  ?:  =('log.jsonl' name)  %.y\n  ?:  %.y  %.y\n"),
 ("Q5a", "desk/app/urgit.hoon",
  "    ?:  p.ci-protected\n      =/  =trust:ci\n",
  "    ?:  %.n\n      =/  =trust:ci\n"),
 ("Q5", "desk/app/urgit-ci.hoon",
  "          ~  %.n  %pending  ~  ~  ~  ~  actor.act  via.act  trust.act  pull.act\n",
  "          ~  %.n  %pending  ~  ~  ~  ~  actor.act  via.act  %trusted  pull.act\n"),
 ("Q6", "desk/app/urgit-ci.hoon",
  "  ?&  =(%passed status.c)\n      =(%trusted trust.c)\n      ?=(^ candidate.c)\n",
  "  ?&  =(%passed status.c)\n      %.y\n      ?=(^ candidate.c)\n"),
 ("Q8", "desk/app/urgit-ci.hoon",
  "    ?.  =(actor.act our.bowl)\n      ~|  'only a writer can approve a candidate'\n",
  "    ?.  %.y\n      ~|  'only a writer can approve a candidate'\n"),
 ("Q10", "desk/app/urgit-ci.hoon",
  "    ?.  ?&(?=(%job kind.assignment) =(%trusted trust.assignment))  ~\n",
  "    ?.  ?=(%job kind.assignment)  ~\n"),
 ("Q11", "desk/app/urgit-ci.hoon",
  "  =/  =event:ci  (scrub:ci-event p.parsed (credential-values (candidate-repo candidate.u.found)))\n",
  "  =/  =event:ci  p.parsed\n"),
 ("Q12", "runner/internal/daemon/daemon.go",
  "\t\tif err := d.verifyAssignment(assignment); err != nil {\n",
  "\t\tif err := d.verifyAssignment(assignment); err != nil && false {\n"),
 ("Q13", "runner/internal/daemon/daemon.go",
  "\t\tif g.Expiry <= now {\n",
  "\t\tif false {\n"),
]
texts = {}
for row, path, old, new in edits:
    s = texts.get(path) or open(path).read()
    n = s.count(old)
    if n != 1:
        sys.exit(f"q-mutants.sh: {row}: expected exactly one match in {path}, found {n}; nothing written")
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
      Q4)  echo "upload name=../x -> 400: FAIL (observed: 200" ;;
      Q5a) echo "merge of a PR to the CI-protected branch -> 202: FAIL (observed: 200" ;;
      Q5)  echo "candidate is %untrusted: FAIL (observed: %trusted" ;;
      Q6)  echo "master unmoved (restricted check cannot land): FAIL (observed:" ;;
      Q8)  echo "approval by ~sampel-palnet refused: FAIL (observed: accepted (>=)" ;;
      Q10) echo "grants=~ on the untrusted assignment: FAIL (observed: grants 1 (TOKEN)" ;;
      Q11) echo "the attempt's recorded output is the scrubbed leak: FAIL (observed: 'q9-hunter2-" ;;
      Q12) echo "daemon b prepared no sandbox (no work): FAIL (observed: 1" ;;
      Q13) echo "expired grant refused by the daemon: FAIL (observed: 0" ;;
      *) echo "q-mutants.sh: no tripwire for row '${2:-}'" >&2; exit 2 ;;
    esac
    ;;
  *) echo "usage: q-mutants.sh apply|revert|status|tripwire <row>" >&2; exit 2 ;;
esac
