#!/bin/bash
# Shared readers for the row scripts (source after env.sh). The dojo prints
# a noun on one line when it fits the pane and pretty-prints it over many
# lines when it does not, so grepping `field=` out of a whole-noun print is
# not reliable: an attempt with no outputs fits on one line and the grep
# then finds an older candidate's `status=` instead. Ask for one field at a
# time and take exactly what the last command printed.
dojo="$HERE/dojo.sh"
# dojo_value '<expr>': the line(s) the dojo printed for <expr>, nothing
# else. A pane narrower than the echoed command wraps the echo over rows;
# a row that keeps `> <line>` a prefix of what was sent is the echo's
# tail, not the value, and is skipped; so is a vane's slog (`gall: got
# old %wake …`), which no dojo value ever looks like.
dojo_value() {
  "$dojo" "$1" "${2:-60}" 8 | DOJO_ECHO="> $1" awk -v p="~$SHIP:dojo>" '
    BEGIN { e = ENVIRON["DOJO_ECHO"] }
    index($0, "> ") == 1 { f = 1; acc = $0; buf = ""; next }
    index($0, p) == 1    { f = 0; next }
    /^(gall|behn|clay|ames|eyre|dill|kiln|iris|jael|khan|lick|arvo): / { next }
    f && acc != "" && index(e, acc $0) == 1 { acc = acc $0; next }
    f { acc = ""; buf = buf $0 "\n" }
    END { printf "%s", buf }'
}
# dojo_unit_cord '<expr>': a (unit @t) answer as ONE line, `~` or
# `[~ '…']`. A pane narrower than the value (this close-out ran in 106
# columns) makes the dojo pretty-print the unit over several lines
# (`[ ~`, `  '…`, `…'`, `]`) and hard-wrap the cord; the echoed command
# wraps too. Read 10 lines, start at the first line of the value, join.
dojo_unit_cord() {
  "$dojo" "$1" "${2:-60}" 10 | awk -v p="~$SHIP:dojo>" '
    index($0, "> ") == 1 { f = 1; on = 0; buf = ""; next }
    index($0, p) == 1    { f = 0; next }
    /^(gall|behn|clay|ames|eyre|dill|kiln|iris|jael|khan|lick|arvo): / { next }
    f && !on && ($0 ~ /^(~|\[)/) { on = 1 }
    f && on { buf = buf $0 }
    END { printf "%s\n", buf }' | sed "s/^\[ ~ */[~ /; s/' *\]$/']/"
}
cand_field() { dojo_value "$2:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))"; }   # <cid> <field>
att_field()  { dojo_value "$2:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))"; }       # <aid> <field>
cand_state() { echo "candidate $1: status=$(cand_field "$1" status) candidate=$(cand_field "$1" candidate)"; }
att_state()  { echo "attempt $1: status=$(att_field "$1" status) events=$(att_field "$1" events) job-result=$(att_field "$1" job-result)"; }
# the ship's view of the repository: master's oid and the object count
master_oid() { "$HERE/api.sh" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print([r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'; }
object_count() { "$HERE/api.sh" GET /repository/ci-fixture | grep -o '"objectCount":[0-9]*' | cut -d: -f2; }
