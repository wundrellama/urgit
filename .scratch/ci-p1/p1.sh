#!/bin/bash
# Row P1: %set-ci-protected on a ref with no tip -> refused with the
# CI-EMPTY-REF-1-A reason; after the seed push the same poke is accepted.
source "$(dirname "$0")/lib.sh"
row "P1: CI-protect before any tip -> refused; after the seed push -> accepted"
out=$("$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 8)
check_contains "refused before the seed push" "ref has no tip; push a commit before CI-protecting it" "$out"
check "not protected" '%.n' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
set_workflows fixture-pass.yml fixture-chain.yml
echo "seed" > README.md
push_commit "seed: fixture-pass and fixture-chain workflows"
check "seed push landed (ref not CI-protected yet)" "$OID" "$(repo_master)"
out=$("$dojo" ":urgit-ci &ci-action [%set-ci-protected '$REPO' 'refs/heads/master' %.y]" 60 4)
check "protected after the seed push" '%.y' "$(dojo_value ".^(? %gx /=urgit-ci=/ci-protected/(scot %t '$REPO')/(scot %t 'refs/heads/master')/noun)" | one '^%\.[yn]$')"
echo "export SEED=$OID" > "$TMP/p1.env"
end_row P1
