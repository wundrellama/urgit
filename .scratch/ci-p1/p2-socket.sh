#!/bin/bash
# Q19: inspect an actual act child created by the real runner.
source "$(dirname "$0")/env.sh"
exec python3 "$P1/p2-socket.py" "$@"
