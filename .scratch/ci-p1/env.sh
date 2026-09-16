#!/bin/bash
# P1 harness environment: the P0 env (ship, port, pier, pane, +code) plus
# the rootless Docker socket and the runner daemon paths from the launch
# footer of BRIEF-CI-P2.md. Source this from every P1/P2 script.
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p0/env.sh"
export P0="$ROOT/.scratch/ci-p0"
export P1="$ROOT/.scratch/ci-p1"
export DOCKER_STATE=/run/user/1000/ci-p2-astra
export DOCKER_DATA="$TMP/docker-data"
export DOCKER_SOCK="$DOCKER_STATE/docker.sock"
export RUNNER_BIN="$ROOT/runner/urgit-runner"
export RUNNER_HOME="$TMP/runner"
export ACT_IMAGE=catthehacker/ubuntu:act-latest
export STORE_PORT=8362
export STORE_DATA="$TMP/store-data"
export STORE_URL="http://127.0.0.1:$STORE_PORT"
export STORE_BUCKET=ci-bucket
export STORE_REGION=us-east-1
# git must never open a credential dialog from a row (this desktop sets
# SSH_ASKPASS=ksshaskpass, which blocks on a 401): answer with nothing
export GIT_ASKPASS=/bin/true
export GIT_TERMINAL_PROMPT=0
unset SSH_ASKPASS
