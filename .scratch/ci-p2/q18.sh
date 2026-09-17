#!/bin/bash
# Row Q18: ERPit for real through P2 — a real ERPit revision pushed as a
# writer into a fresh CI-protected repository: 8/8 jobs pass under the
# daemon, every job's log lands in the private bucket under the
# attempt's /trusted/ key with the sha256 the ship recorded, and the
# candidate lands. P1's p15.sh does the push and the eight-job
# verification; this row adds the store's evidence. Needs daemon a at
# capacity 3 (p2-setup enrolls it at 2: this row restarts it at 3).
source "$(dirname "$0")/lib.sh"
row "Q18: full ERPit — eight jobs, eight logs in the bucket under /trusted/, landed"
"$P1/runner.sh" stop a >/dev/null 2>&1
"$P1/runner.sh" config a 3 >/dev/null
"$P1/runner.sh" start a | head -1
sleep 3
# p15.sh uses REPO=erpit-p15; a second run on the same ship needs a fresh name
export P15_REPO="erpit-q18-$(date +%H%M%S)"
# the copy sources the P1 lib by its absolute path (it no longer sits beside it)
sed -e "s|^source \"\$(dirname \"\$0\")/lib.sh\"|source \"$P1/lib.sh\"|" \
    -e "s/^REPO=erpit-p15; CLONE=\"\$TMP\/clone-erpit\"/REPO=\$P15_REPO; CLONE=\"\$TMP\/clone-erpit-q18\"/" "$P1/p15.sh" > "$TMP/q18-p15.sh"
chmod +x "$TMP/q18-p15.sh"
"$TMP/q18-p15.sh" 2>&1 | tee "$TMP/q18-p15.log" | grep -E 'PASS|FAIL|^--|^P15' | cut -c1-200
source "$TMP/p15.env"
check "P15's eight-job verification passed" "P15: PASS" "$(grep -E '^P15: (PASS|FAIL)$' "$TMP/q18-p15.log" | tail -1)"
REPO="$P15_REPO"
n_logs=0; n_trusted=0; n_match=0
for a in $(cand_attempt_ids "$ERPIT_CID"); do
  [ "$(att_kind "$a")" = "%job" ] || continue
  key=$(att_log_key "$a"); [ -n "$key" ] || continue
  n_logs=$((n_logs+1))
  case "$key" in "ci/$REPO/$ERPIT_CID/$a/trusted/log.jsonl") n_trusted=$((n_trusted+1));; esac
  rm -f "$TMP/q18-one.jsonl"; "$store" get "$key" "$TMP/q18-one.jsonl" >/dev/null
  [ "$(sha_of "$TMP/q18-one.jsonl")" = "$(att_log_sha "$a")" ] && n_match=$((n_match+1))
done
check "eight job attempts recorded a log handle" "8" "$n_logs"
check "every log key is the attempt's own, under /trusted/" "8" "$n_trusted"
check "every bucket object's sha256 matches the ship's handle" "8" "$n_match"
check "the bucket lists eight logs for the candidate" "8" "$("$store" ls "ci/$REPO/$ERPIT_CID/" | grep -c '/trusted/log.jsonl$')"
check "no object under /untrusted/ for the candidate" "0" "$("$store" ls "ci/$REPO/$ERPIT_CID/" | grep -c '/untrusted/')"
check "unsigned read of one log (private bucket)" "403" "$("$store" anon "$key")"
check "landed: master = candidate OID" "$ERPIT_OID" "$(repo_master)"
echo "export Q18_REPO=$REPO; export Q18_CID=$ERPIT_CID; export Q18_T=$ERPIT_T" > "$TMP/q18.env"
"$P1/runner.sh" stop a >/dev/null 2>&1
"$P1/runner.sh" config a 2 >/dev/null
"$P1/runner.sh" start a | head -1
end_row Q18
[ "$NFAIL" = 0 ]
