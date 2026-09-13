#!/bin/bash
# usage: poke.sh <agent> <mark> '<hoon noun>'
# Pokes an agent on the ship through a khan strand; a nack surfaces as the
# strand's failure output, which is the RED evidence for refusal rows.
source "$(dirname "$0")/env.sh"
agent="${1#%}"; agent="%$agent"; mark="${2#%}"; mark="%$mark"; noun="$3"
file="$TMP/poke-$$.hoon"
cat > "$file" <<HOON
=/  m  (strand ,vase)
;<  our=@p  bind:m  get-our
;<  ~  bind:m  (poke [our $agent] $mark !>($noun))
(pure:m !>([%poked $agent $mark]))
HOON
"$CLICK" -k -i "$file" "$PIER" 2>/dev/null
rm -f "$file"
