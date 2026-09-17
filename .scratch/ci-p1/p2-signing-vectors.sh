#!/bin/bash
source "$(dirname "$0")/env.sh"
exec python3 "$P1/p2-signing-vectors.py" vectors
