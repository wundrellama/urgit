#!/bin/bash
# Shared readers for the row scripts (source after env.sh). The dojo prints
# a noun on one line when it fits the pane and pretty-prints it over many
# lines when it does not, so grepping `field=` out of a whole-noun print is
# not reliable: an attempt with no outputs fits on one line and the grep
# then finds an older candidate's `status=` instead. Ask for one field at a
# time and take exactly what the last command printed.
dojo="$HERE/dojo.sh"
# dojo_value '<expr>': the line(s) the dojo printed for <expr>, nothing else
dojo_value() {
  "$dojo" "$1" "${2:-60}" 8 | awk -v p="~$SHIP:dojo>" '
    index($0, "> ") == 1 { f = 1; buf = ""; next }
    index($0, p) == 1    { f = 0; next }
    f { buf = buf $0 "\n" }
    END { printf "%s", buf }'
}
cand_field() { dojo_value "$2:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/$1/noun))"; }   # <cid> <field>
att_field()  { dojo_value "$2:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/$1/noun))"; }       # <aid> <field>
cand_state() { echo "candidate $1: status=$(cand_field "$1" status) candidate=$(cand_field "$1" candidate)"; }
att_state()  { echo "attempt $1: status=$(att_field "$1" status) events=$(att_field "$1" events) job-result=$(att_field "$1" job-result)"; }
# the ship's view of the repository: master's oid and the object count
master_oid() { "$HERE/api.sh" GET /repository/ci-fixture | sed 's/^[0-9]* //' | python3 -c 'import sys,json; d=json.load(sys.stdin); print([r for r in d["refs"] if r["name"]=="refs/heads/master"][0]["oid"])'; }
object_count() { "$HERE/api.sh" GET /repository/ci-fixture | grep -o '"objectCount":[0-9]*' | cut -d: -f2; }
