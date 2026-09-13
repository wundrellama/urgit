#!/bin/bash
# Rows P6-P9, one commit each on the landed master:
#   P6 fixture-chain-off (a emits go=false) -> b %skipped with the reason;
#      candidate %passed; landed.
#   P7 fixture-fail -> job %failed, candidate %failed, push refused.
#   P8 fixture-matrix -> %plan-invalid `matrix unsupported in P1`; %failed.
#   P9 fixture-unsupported-if -> %plan-invalid with the expression quoted.
source "$(dirname "$0")/lib.sh"
rows="${*:-p6 p7 p8 p9}"
has() { case " $rows " in *" $1 "*) return 0;; *) return 1;; esac; }
plan_attempt() { for a in $(cand_attempt_ids "$1"); do [ "$(att_kind "$a")" = "%plan" ] && { echo "$a"; return; }; done; }

if has p6; then
row "P6: fixture-chain-off -> b %skipped (if false), candidate %passed, landed"
sync_clone; set_workflows fixture-pass.yml fixture-chain-off.yml
push_commit "six: chain-off"
A_ATT=$(wait_for_attempt "$CID" a 120) || true
check "a passes" '%passed' "$(wait_att "$A_ATT" '%passed' 240)"
check "candidate passed" '%passed' "$(wait_cand "$CID" '%passed' 60)"
B_ATT=$(att_of_job "$CID" b || true)
check "b is %skipped" '%skipped' "$(att_status "$B_ATT")"
check "b's reason" "if false: needs.a.outputs.go is \"false\", not \"true\"" "$(att_reason "$B_ATT")"
sleep 3
check "landed by the ship" "$OID" "$(repo_master)"
end_row P6
fi

if has p7; then
row "P7: fixture-fail -> job %failed, candidate %failed, push refused"
sync_clone; set_workflows fixture-pass.yml fixture-fail.yml
push_commit "seven: a failing job"
F_ATT=$(wait_for_attempt "$CID" fail 120) || true
check "fail job %failed" '%failed' "$(wait_att "$F_ATT" '%failed' 240)"
check "candidate %failed" '%failed' "$(wait_cand "$CID" '%failed' 60)"
check "reason" "job fail in fixture-fail.yml failed" "$(cand_reason "$CID" | tr -d "'")"
sleep 2
check "master unchanged" "$(git -C "$CLONE" rev-parse HEAD~1)" "$(repo_master)"
p=$(git -C "$CLONE" push origin master 2>&1 | grep -o 'remote rejected.*' | head -1)
check_contains "re-push refused (staged again, not landed)" "staged as ci candidate" "$p"
check_not_contains "scratch ref released at the terminal status" "refs/ci/candidate/$CID" "$(ls_remote)"
end_row P7
fi

if has p8; then
row "P8: matrix -> %plan-invalid 'matrix unsupported in P1', candidate %failed"
sync_clone; set_workflows fixture-pass.yml fixture-matrix.yml
push_commit "eight: a matrix job"
check "candidate %failed" '%failed' "$(wait_cand "$CID" '%failed' 180)"
P_ATT=$(plan_attempt "$CID")
check_contains "plan attempt reason" "plan-invalid: matrix unsupported in P1" "$(att_reason "$P_ATT")"
check_contains "candidate verdict-reason" "matrix unsupported in P1" "$(cand_reason "$CID")"
check "no job attempt ran" "" "$(att_of_job "$CID" m || true)"
end_row P8
fi

if has p9; then
row "P9: if: github.event_name == 'push' -> %plan-invalid with the expression quoted"
sync_clone; set_workflows fixture-pass.yml fixture-unsupported-if.yml
push_commit "nine: an unsupported job-level if"
check "candidate %failed" '%failed' "$(wait_cand "$CID" '%failed' 180)"
P_ATT=$(plan_attempt "$CID")
check_contains "plan attempt reason quotes the expression" "job u in fixture-unsupported-if.yml: unsupported if expression \"github.event_name == 'push'\"" "$(att_reason "$P_ATT")"
check "no job attempt ran" "" "$(att_of_job "$CID" u || true)"
end_row P9
fi
echo; echo "== rows: PASS=$NPASS (${PASSED# }) FAIL=$NFAIL (${FAILED# })"
[ "$NFAIL" = 0 ]
