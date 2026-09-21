#!/bin/bash
# usage: mutants.sh apply | revert | status | tripwire <row>
# One-line sabotage per negative row (P1, P7-P12, P14, P17-P20) that must
# turn the row RED. `apply` edits the working tree (each old text must
# occur exactly once, or nothing is written) and refuses on a dirty file;
# `revert` is `git checkout --` of the files. Hoon mutants need
# rebuild.sh; Go mutants need `go build` and a daemon restart
# (negatives.sh does both). The mutations are never committed.
# `tripwire <row>` prints the exact substring the sabotaged build must
# produce in the row's log (the row's observation line carrying the
# mutant's own answer, or the product's own message); negatives.sh red
# counts a row RED only when it FAILS *and* carries its tripwire.
#
#   P1   app/urgit-ci.hoon   %set-ci-protected: the no-tip refusal accepts silently
#   P7   app/urgit-ci.hoon   close-attempt: every job result is %passed (P0's H11)
#   P8   lib/ci-plan.hoon    validate: a matrix job is not refused
#   P9   lib/ci-plan.hoon    validate: an unsupported if is accepted (the refusal line
#                            continues the walk; the evaluator then skips the job)
#   P10  app/urgit-ci.hoon   /abandon closes %job-result %success
#   P11  app/urgit-ci.hoon   the deadline wake closes %job-result %success (P0's H10)
#   P12  runner daemon.go    quarantine does not lower the capacity
#   P13-overlap runner plan.go  the projection leaves name: unprefixed (act's
#                            container name collides across attempts)
#   P14  runner ship/client.go  the poll's 401 reads as "nothing assigned"
#   P17  app/urgit.hoon      land-candidate skips the expected-tip compare
#                            (apply-receive's own old-tip check still refuses, with the wrong reason)
#   P18  app/urgit.hoon      handle-receive-pack skips write-authorized
#   (P19's mutant — the desk-linked refusal skipped — is gone: P3 D9 lands a
#   desk-linked repository through the desk, and P19 is a positive row)
#   P20  runner daemon.go    act runs on the unprojected workflow
source "$(dirname "$0")/env.sh"
cd "$ROOT"
# never mutate anything but a git work tree: reached through a symlinked
# desk/ this once edited a tree that `git checkout --` could not restore
git rev-parse --is-inside-work-tree >/dev/null || { echo "mutants.sh: $PWD is not a git work tree"; exit 1; }
FILES=(desk/app/urgit-ci.hoon desk/lib/ci-plan.hoon desk/app/urgit.hoon runner/internal/daemon/daemon.go runner/internal/ship/client.go runner/internal/plan/plan.go)
case "${1:-}" in
  apply)
    if ! git diff --quiet -- "${FILES[@]}"; then
      echo "mutants.sh: uncommitted changes in ${FILES[*]}; commit them first (revert is git checkout --)" >&2
      exit 1
    fi
    python3 - <<'PY'
import sys
edits = [
 ("P1",  "desk/app/urgit-ci.hoon",
  "    ?~  u.tip\n      ~|  no-tip-refusal\n      !!\n",
  "    ?~  u.tip  (emit ~)\n"),
 ("P7",  "desk/app/urgit-ci.hoon",
  "      %job-result            ?:(=(%success result.attempt-result) %passed %failed)\n",
  "      %job-result            %passed\n"),
 ("P8",  "desk/lib/ci-plan.hoon",
  "    ?:  matrix.wire-job\n",
  "    ?:  %.n\n"),
 ("P9",  "desk/lib/ci-plan.hoon",
  "      [%| (rap 3 ~[name ': unsupported if expression ' (quote raw.u.cond.wire-job)])]\n",
  "      $(remaining t.remaining, seen (~(put in seen) k))\n"),
 ("P10", "desk/app/urgit-ci.hoon",
  "    (close-attempt attempt [%infrastructure-error (rap 3 ~['abandoned: ' reason])])\n",
  "    (close-attempt attempt [%job-result %success])\n"),
 ("P11", "desk/app/urgit-ci.hoon",
  "    %+  close-attempt  attempt\n    :-  %infrastructure-error\n    ?:(silent-before 'runner went silent' 'no result arrived before the deadline')\n",
  "    %+  close-attempt  attempt\n    [%job-result %success]\n"),
 ("P12", "runner/internal/daemon/daemon.go",
  "\td.capacity--\n",
  "\td.capacity = d.capacity\n"),
 ("P13-overlap", "runner/internal/plan/plan.go",
  "\treturn attempt + \"/\" + original\n",
  "\treturn original\n"),
 ("P14", "runner/internal/ship/client.go",
  "\t\treturn nil, ErrUnauthorized\n\tcase http.StatusOK:\n\t\tvar answer struct {\n\t\t\tAssignment Assignment `json:\"assignment\"`\n",
  "\t\treturn nil, nil\n\tcase http.StatusOK:\n\t\tvar answer struct {\n\t\t\tAssignment Assignment `json:\"assignment\"`\n"),
 ("P17", "desk/app/urgit.hoon",
  "  ?.  =(`expected (~(get by refs.u.found) ref))\n    (refuse 'destination moved; rebase and push again')\n",
  "  ?.  %.y\n    (refuse 'destination moved; rebase and push again')\n"),
 ("P18", "desk/app/urgit.hoon",
  "  ?.  (write-authorized u.found req)\n    :_  this\n    %-  give-http\n    :*  eyre-id\n        401\n        ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm=\"git\"']]\n        `(text:git-codec 'repository authentication required\\0a')\n",
  "  ?.  %.y\n    :_  this\n    %-  give-http\n    :*  eyre-id\n        401\n        ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm=\"git\"']]\n        `(text:git-codec 'repository authentication required\\0a')\n"),
 ("P20", "runner/internal/daemon/daemon.go",
  "\tprojected, err := plan.Project(original, a.Job, a.Attempt, a.Workflow)\n",
  "\tprojected, err := original, error(nil)\n"),
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
  tripwire)
    # what the sabotaged build itself produces, as the row logs it; a RED
    # without it failed for the wrong reason. Alternatives one per line.
    # P13-overlap: two acts on one container name; the loser's shape
    # depends on the race — force-removed mid-run (exitcode '137', the
    # brief's live proof), the name in use at create time (Sep 13), or
    # removed between create and copy/exec (`No such container`,
    # `is not running`; the close-out run).
    case "${2:-}" in
      P1)  echo "%set-ci-protected on a ref with no tip: accepted (>=)" ;;
      P7)  echo "fail job %failed: FAIL (observed: %passed" ;;
      P8)  echo "no job attempt ran: FAIL (observed: 0v" ;;
      P9)  echo "candidate %failed: FAIL (observed: %passed" ;;
      P10) echo "attempt %infrastructure-error: FAIL (observed: %passed" ;;
      P11) echo "closed at the deadline: FAIL (observed: %passed" ;;
      P12) echo "advertised capacity now 2" ;;
      P13-overlap) printf '%s\n' "exitcode '137'" "is already in use by container" "No such container" "is not running" ;;
      P14) echo "daemon exited non-zero: FAIL (observed: running pid" ;;
      P17) echo "verdict-reason: FAIL (observed: 'candidate object is missing from the store'" ;;
      P18) echo "no credentials -> 401: FAIL (observed: 200" ;;
      P20) echo "event job does not match the assignment" ;;
      *) echo "mutants.sh: no tripwire for row '${2:-}'" >&2; exit 2 ;;
    esac
    ;;
  *) echo "usage: mutants.sh apply|revert|status|tripwire <row>" >&2; exit 2 ;;
esac
