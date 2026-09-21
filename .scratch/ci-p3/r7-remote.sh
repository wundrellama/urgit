#!/bin/bash
# usage: r7-remote.sh code <url> | headers <url> | sha <url> | exit <url>
# R7's browser-side fetch, run from the SECOND machine the footer names
# (`np`, 192.168.1.64): a probe from the ship's own host is not evidence
# that a viewer's browser reaches the store. Read-only use of that box —
# curl only, nothing written there. Prints what the row asserts on:
#   code     the HTTP status (000 when nothing answered)
#   headers  the response headers (for the CORS answer)
#   sha      the sha256 of the body (a presigned log link)
#   exit     the status and curl's exit code (7 = connection refused)
NP="${NP_HOST:-np}"
# the remote shell re-parses the command line: every argument is quoted
# for it (printf %q), so a header with a space stays one argument
np() { local q=""; local a; for a in "$@"; do q="$q $(printf '%q' "$a")"; done; ssh -o BatchMode=yes -o ConnectTimeout=10 "$NP" "curl -s --max-time 20$q"; }
case "${1:?usage}" in
  code)    np -o /dev/null -w '%{http_code}' "$2"; echo ;;
  headers) np -D - -o /dev/null -H 'Origin: http://viewer.example' "$2" ;;
  sha)     np "$2" | sha256sum | cut -d' ' -f1 ;;
  exit)    np -o /dev/null -w '%{http_code}' "$2"; echo " exit=$?" ;;
  *) echo "usage: r7-remote.sh code|headers|sha|exit <url>" >&2; exit 2 ;;
esac
