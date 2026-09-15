#!/bin/bash
# Builds the statically linked act 0.2.89 that the daemon copies into
# every sandbox ($TMP/act-static/act, named by runner.sh's config and
# asserted by p-setup.sh). Brew's act is dynamically linked against
# linuxbrew's glibc and cannot execute inside the container, so this is
# `CGO_ENABLED=0 go install` of the pinned upstream module into GOBIN.
# Idempotent: a binary that already answers `--version` 0.2.89 and that
# `file` calls statically linked is kept.
source "$(dirname "$0")/env.sh"
DEST="$TMP/act-static"
VERSION=0.2.89
good() {
  [ -x "$DEST/act" ] \
    && [ "$("$DEST/act" --version 2>/dev/null)" = "act version $VERSION" ] \
    && file "$DEST/act" | grep -qF 'statically linked'
}
if good; then
  echo "act-static: $DEST/act is act version $VERSION, statically linked (kept)"
  exit 0
fi
mkdir -p "$DEST"
echo "== CGO_ENABLED=0 GOBIN=$DEST go install github.com/nektos/act@v$VERSION"
CGO_ENABLED=0 GOBIN="$DEST" go install "github.com/nektos/act@v$VERSION"
v=$("$DEST/act" --version)
[ "$v" = "act version $VERSION" ] || { echo "act-static: built $v, expected act version $VERSION" >&2; exit 1; }
file "$DEST/act" | grep -qF 'statically linked' || { echo "act-static: not statically linked: $(file -b "$DEST/act")" >&2; exit 1; }
echo "act-static: $v, $(file -b "$DEST/act" | cut -d, -f1-4) -> $DEST/act"
