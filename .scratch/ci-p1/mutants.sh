#!/bin/bash
# usage: mutants.sh apply | revert | status
# One-line sabotage per negative row (P1, P7-P12, P14, P17-P20) that must
# turn the row RED. `apply` edits the working tree (each old text must
# occur exactly once, or nothing is written) and refuses on a dirty file;
# `revert` is `git checkout --` of the files. Hoon mutants need
# rebuild.sh; Go mutants need `go build` and a daemon restart
# (negatives.sh does both). The mutations are never committed.
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
#   P19  app/urgit-ci.hoon   %set-ci-protected accepts a desk-linked repo
#   P20  runner daemon.go    act runs on the unprojected workflow
source "$(dirname "$0")/env.sh"
cd "$ROOT"
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
  "  =.  state  (close-attempt u.found [%infrastructure-error (rap 3 ~['abandoned: ' u.reason])])\n",
  "  =.  state  (close-attempt u.found [%job-result %success])\n"),
 ("P11", "desk/app/urgit-ci.hoon",
  "      (close-attempt:hc u.found [%infrastructure-error 'no result arrived before the deadline'])\n",
  "      (close-attempt:hc u.found [%job-result %success])\n"),
 ("P12", "runner/internal/daemon/daemon.go",
  "\td.capacity--\n",
  "\td.capacity = d.capacity\n"),
 ("P13-overlap", "runner/internal/plan/plan.go",
  "\treturn attempt + \"/\" + original\n",
  "\treturn original\n"),
 ("P14", "runner/internal/ship/client.go",
  "\tcase http.StatusUnauthorized:\n\t\treturn nil, ErrUnauthorized\n\tcase http.StatusOK:\n\t\tvar answer struct {\n\t\t\tAssignment Assignment `json:\"assignment\"`\n",
  "\tcase http.StatusUnauthorized:\n\t\treturn nil, nil\n\tcase http.StatusOK:\n\t\tvar answer struct {\n\t\t\tAssignment Assignment `json:\"assignment\"`\n"),
 ("P17", "desk/app/urgit.hoon",
  "  ?.  =(`expected (~(get by refs.u.found) ref))\n    (refuse 'destination moved; rebase and push again')\n",
  "  ?.  %.y\n    (refuse 'destination moved; rebase and push again')\n"),
 ("P18", "desk/app/urgit.hoon",
  "  ?.  (write-authorized u.found req)\n    :_  this\n    %-  give-http\n    :*  eyre-id\n        401\n        ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm=\"git\"']]\n        `(text:git-codec 'repository authentication required\\0a')\n",
  "  ?.  %.y\n    :_  this\n    %-  give-http\n    :*  eyre-id\n        401\n        ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm=\"git\"']]\n        `(text:git-codec 'repository authentication required\\0a')\n"),
 ("P19", "desk/app/urgit-ci.hoon",
  "    ?:  linked.u.u.tip\n      ~|  linked-refusal\n      !!\n",
  "    ?:  %.n\n      ~|  linked-refusal\n      !!\n"),
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
  *) echo "usage: mutants.sh apply|revert|status" >&2; exit 2 ;;
esac
