#!/bin/bash
# P1 harness environment: the P0 env (ship, port, pier, pane, +code) plus
# the rootless Docker socket and the runner daemon paths from the launch
# footer of BRIEF-CI-P1.md (chair opus). Source this from every P1 script.
source "$(dirname "${BASH_SOURCE[0]}")/../ci-p0/env.sh"
export P0="$ROOT/.scratch/ci-p0"
export P1="$ROOT/.scratch/ci-p1"
export DOCKER_STATE=/run/user/1000/ci-p1-opus
export DOCKER_DATA="$TMP/docker-data"
export DOCKER_SOCK="$DOCKER_STATE/docker.sock"
export RUNNER_BIN="$ROOT/runner/urgit-runner"
export RUNNER_HOME="$TMP/runner"
export ACT_IMAGE=catthehacker/ubuntu:act-latest
