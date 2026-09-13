#!/bin/bash
# Row H1: before %storage is configured, %set-ci-protected is refused with
# the D9 message and the D3 scry stays %.n. The poke goes through the dojo so
# the refusal is the quoted crash line plus `dojo: app poke failed`.
source "$(dirname "$0")/env.sh"
dojo="$HERE/dojo.sh"
echo "== %set-ci-protected ci-fixture refs/heads/master (expect the D9 refusal)"
"$dojo" ":urgit-ci &ci-action [%set-ci-protected 'ci-fixture' 'refs/heads/master' %.y]" 60 8 | tail -7
echo "== D3 scry (expect %.n)"
"$dojo" ".^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)" 60 3 | tail -2
