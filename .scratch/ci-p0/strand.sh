#!/bin/bash
# usage: strand.sh '<hoon producing a vase, with our=@p now=@da bound>'
# Runs a khan strand on the ship through click -k and prints the result.
# The expression sees `our` and `now`; wrap the value you want in !>(...).
source "$(dirname "$0")/env.sh"
file="$TMP/strand-$$.hoon"
cat > "$file" <<HOON
=/  m  (strand ,vase)
;<  our=@p  bind:m  get-our
;<  now=@da  bind:m  get-time
(pure:m $1)
HOON
"$CLICK" -k -i "$file" "$PIER"
rm -f "$file"
