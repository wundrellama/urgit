#!/bin/bash
# P1 setup: a fresh %urgit-ci state (nuke/revive: the P0 rows left staged
# candidates and two shell-driven daemons behind), the rootless Docker
# daemon up with the act image, the static act binary built by
# act-static.sh, the runner binary built, a fresh public repository
# `ci-p1` with NO commits (row P1 needs
# an empty ref), and a local clone. %storage is still configured from H2.
source "$(dirname "$0")/lib.sh"
"$P1/nuke-revive.sh" p-setup | tail -3
"$P1/docker-rootless.sh" start | tail -2
"$P1/docker-rootless.sh" info | tail -1
"$P1/act-static.sh"
[ -x "$TMP/act-static/act" ] || { echo "p-setup: $TMP/act-static/act missing (static act 0.2.89)" >&2; exit 1; }
"$TMP/act-static/act" --version
( cd "$ROOT" && zig build -Drunner 2>&1 | grep -E 'runner' )
echo "== create repository $REPO (public, no commits)"
created=$("$api" POST /repositories "{\"name\":\"$REPO\",\"publicRead\":true}")
echo "$created"
# row P1 needs a repository with no tip: an existing $REPO (a reused
# ship) would turn P1's refusal into acceptance for the wrong reason
case "$created" in 201*) ;; *) echo "p-setup: could not create $REPO ($created); the P1 table needs a fresh ship" >&2; exit 1 ;; esac
rm -rf "$CLONE"; mkdir -p "$CLONE"; cd "$CLONE"
git init -q -b master .
git config user.name p1; git config user.email p1@example
git config http.cookieFile "$JAR"
git remote add origin "$URL/git/$REPO"
echo "== prelude: ci bound in the dojo"
"$P0/prelude.sh" | tail -1
