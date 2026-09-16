#!/bin/bash
# Mints an enrollment token on the ship and prints it (the only place the
# raw token appears is this output and the daemon's config file). The
# generator's ~& prints `[%ci-enroll-token 0v…]` BEFORE the dojo echoes
# the command, and a narrow dojo pretty-prints it over three lines with
# the token on its own; read 30 lines, take the last token after a
# `ci-enroll-token` line, and fail loudly rather than hand back nothing.
source "$(dirname "$0")/env.sh"
token=$("$P0/dojo.sh" ':urgit-ci|mint-enroll-token' 60 30 \
  | awk '/ci-enroll-token/ { f = 1 } f && match($0, /0v[0-9a-v.]+/) { t = substr($0, RSTART, RLENGTH); f = 0 } END { printf "%s", t }')
[ -n "$token" ] || { echo "mint.sh: no token in the dojo's output" >&2; exit 1; }
echo "$token"
