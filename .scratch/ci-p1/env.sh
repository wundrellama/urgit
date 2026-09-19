#!/bin/bash
# P1/P2/P3 harness environment: the P0 env (ship, port, pier, tmux
# session, +code) plus the rootless Docker socket, the runner daemon paths
# and the object store fixture from the launch footer of BRIEF-CI-P3.md
# (the P2 close-out's state dir was /run/user/1000/ci-p2-closeout and its
# store port 8365; the P2 build's /run/user/1000/ci-p2-opus and 8363; the
# P1 close-out's /run/user/1000/ci-p1-closeout, the P1 build's
# /run/user/1000/ci-p1-opus). Source this from every P1, P2 and P3 script.
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p0/env.sh"
export P0="$ROOT/.scratch/ci-p0"
export P1="$ROOT/.scratch/ci-p1"
export DOCKER_STATE=/run/user/1000/ci-p3-opus
export DOCKER_DATA="$TMP/docker-data"
export DOCKER_SOCK="$DOCKER_STATE/docker.sock"
export RUNNER_BIN="$ROOT/runner/urgit-runner"
export RUNNER_HOME="$TMP/runner"
export ACT_IMAGE=catthehacker/ubuntu:act-latest
# the capacity daemon a is enrolled at (footer; BRIEF-CI-P2-CLOSEOUT T1):
# p2-setup enrolls at it once and q18 refuses to run below 3 — the
# eight-job row on a capacity-2 daemon is CI-DELIVERY-1.1's t+0 squeeze
# (an attempt with events and no result until the ~h1 deadline), P3's
export DAEMON_CAPACITY="${DAEMON_CAPACITY:-3}"
# the RustFS object-store fixture (BRIEF-CI-P2 §5): one rootless container
# on the harness Docker daemon, its own port from the footer, data under
# $TMP; store.sh writes the fixture's access key to $TMP/store.env
export STORE_PORT=8392
export STORE_DATA="$TMP/store-data"
export STORE_NAME="urgit-ci-store-$SHIP"
export STORE_IMAGE=rustfs/rustfs:1.0.0
# the harness reads the store on the loopback; the container binds
# 0.0.0.0:$STORE_PORT (BRIEF-CI-P3 §5) and the ship's %storage endpoint
# names $STORE_ADVERTISE, the address every viewer's browser and the
# daemon must reach (D5): 127.0.0.1 by default so the P0-P2 rows are
# unchanged, the host's LAN address (the footer's 192.168.1.229) for the
# P3 rows, and 127.0.0.1 again as R7's RED case
export STORE_URL="http://127.0.0.1:$STORE_PORT"
export STORE_ADVERTISE="${STORE_ADVERTISE:-127.0.0.1}"
export STORE_ENDPOINT="http://$STORE_ADVERTISE:$STORE_PORT"
export STORE_BUCKET=ci-bucket
export STORE_REGION=local-1
export P2="$ROOT/.scratch/ci-p2"
# git must never open a credential dialog from a row (this desktop sets
# SSH_ASKPASS=ksshaskpass, which blocks on a 401): answer with nothing
export GIT_ASKPASS=/bin/true
export GIT_TERMINAL_PROMPT=0
unset SSH_ASKPASS
