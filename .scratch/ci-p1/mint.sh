#!/bin/bash
# Mints an enrollment token on the ship and prints it (the only place the
# raw token appears is this output and the daemon's config file).
source "$(dirname "$0")/env.sh"
"$P0/dojo.sh" ':urgit-ci|mint-enroll-token' 60 6 | grep -o 'ci-enroll-token 0v[0-9a-v.]*' | tail -1 | sed 's/.* //'
