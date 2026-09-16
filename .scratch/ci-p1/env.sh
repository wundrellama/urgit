#!/bin/bash
# P1/P2 harness environment: the P0 env (ship, port, pier, pane, +code)
# plus the rootless Docker socket, the runner daemon paths and the object
# store fixture from the launch footer of BRIEF-CI-P2.md (the P1 close-out's
# state dir was /run/user/1000/ci-p1-closeout, the P1 build's
# /run/user/1000/ci-p1-opus). Source this from every P1 and P2 script.
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p0/env.sh"
export P0="$ROOT/.scratch/ci-p0"
export P1="$ROOT/.scratch/ci-p1"
export DOCKER_STATE=/run/user/1000/ci-p2-opus
export DOCKER_DATA="$TMP/docker-data"
export DOCKER_SOCK="$DOCKER_STATE/docker.sock"
export RUNNER_BIN="$ROOT/runner/urgit-runner"
export RUNNER_HOME="$TMP/runner"
export ACT_IMAGE=catthehacker/ubuntu:act-latest
# the RustFS object-store fixture (BRIEF-CI-P2 §5): one rootless container
# on the harness Docker daemon, its own port from the footer, data under
# $TMP; store.sh writes the fixture's access key to $TMP/store.env
export STORE_PORT=8363
export STORE_DATA="$TMP/store-data"
export STORE_NAME="urgit-ci-store-$SHIP"
export STORE_IMAGE=rustfs/rustfs:1.0.0
export STORE_URL="http://127.0.0.1:$STORE_PORT"
export STORE_BUCKET=ci-bucket
export STORE_REGION=local-1
export P2="$ROOT/.scratch/ci-p2"
# git must never open a credential dialog from a row (this desktop sets
# SSH_ASKPASS=ksshaskpass, which blocks on a 401): answer with nothing
export GIT_ASKPASS=/bin/true
export GIT_TERMINAL_PROMPT=0
unset SSH_ASKPASS
