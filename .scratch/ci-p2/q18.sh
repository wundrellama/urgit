#!/bin/bash
# Row Q18: ERPit for real through P2 — a real ERPit revision pushed as a
# writer into a fresh CI-protected repository: 8/8 jobs pass under the
# daemon, every job's log lands in the private bucket under the
# attempt's /trusted/ key with the sha256 the ship recorded, and the
# candidate lands. P1's p15.sh does the push and the eight-job
# verification; this row adds the store's evidence. Runs on daemon a as
# p2-setup enrolled it (the footer's DAEMON_CAPACITY) and REFUSES below
# capacity 3 rather than restarting the daemon: the eight jobs on a
# capacity-2 daemon are CI-DELIVERY-1.1's t+0 squeeze — an attempt with
# events and no result is not re-offered (CI-DELIVERY-1 covers zero
# activity only) and waits for the ~h1 job deadline; that is P1 code and
# P3's first item, not this row's to paper over (BRIEF-CI-P2-CLOSEOUT T1).
source "$(dirname "$0")/lib.sh"
row "Q18: full ERPit — eight jobs, eight logs in the bucket under /trusted/, landed"
# the capacity daemon a was enrolled at is the one in its config (p2-setup
# wrote it from DAEMON_CAPACITY; Q12/Q13 restart it with that config)
enrolled=$(sed -n 's/^capacity = \([0-9]*\)$/\1/p' "$RUNNER_HOME/a/config.toml" 2>/dev/null)
echo "-- daemon a: enrolled capacity ${enrolled:-<no config>}; DAEMON_CAPACITY=$DAEMON_CAPACITY"
if [ -z "$enrolled" ] || [ "$enrolled" -lt 3 ]; then
  echo "Q18: SKIPPED — capacity ${enrolled:-0} < 3; CI-DELIVERY-1.1: a t+0 squeeze leaves an attempt with events and no result until ~h1"
  exit 0
fi
[ "$("$P1/runner.sh" status a | head -1 | cut -d' ' -f1)" = running ] || "$P1/runner.sh" start a | head -1
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
end_row Q18
[ "$NFAIL" = 0 ]
