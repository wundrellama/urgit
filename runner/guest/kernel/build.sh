#!/bin/bash
# The guest kernel recipe (BRIEF-CI-P4 D2, S1.3): one pinned source
# tarball from kernel.org, one vendored configuration (Firecracker's
# microvm-kernel-ci-x86_64-6.1.config at v1.17.0, Apache-2.0), one
# `make vmlinux`. Every input is named and hashed here; the output's
# sha256 and the toolchain that produced it are recorded beside it. A
# second clean build from the same inputs must produce a bootable kernel;
# bit-identity is NOT claimed unless two outputs are compared and found
# equal (the manifest says which).
#
#   usage: build.sh <out-dir> [work-dir]
#   writes: <out-dir>/vmlinux-<version>, <out-dir>/vmlinux-<version>.sha256,
#           <out-dir>/kernel-build.json (inputs, hashes, toolchain, host)
#
# Bootstrap download, inventoried (D2 "inventory every bootstrap
# download"): the kernel source tarball, verified against the sha256
# pinned below before anything is extracted. Nothing else is fetched.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${1:?out-dir}"; WORK="${2:-$OUT/../build/kernel}"
VERSION=6.1.188
TARBALL="linux-$VERSION.tar.xz"
TARBALL_URL="https://cdn.kernel.org/pub/linux/kernel/v6.x/$TARBALL"
# from https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc, read 2026-09-20
TARBALL_SHA256=ed4d0acb1307c235230c89efc094e210e6290593f94a7e617f28b1001101a33a
CONFIG="$HERE/microvm-kernel-ci-x86_64-6.1.config"
# firecracker v1.17.0 resources/guest_configs/microvm-kernel-ci-x86_64-6.1.config
CONFIG_SHA256=153ca1b40f3312bfb40b7587b471ea548a915f98f480f0d0892a1537957a2147
JOBS="${JOBS:-$(nproc)}"
mkdir -p "$OUT" "$WORK"
echo "$CONFIG_SHA256  $CONFIG" | sha256sum -c - >/dev/null || { echo "build.sh: the vendored config does not match its pinned sha256" >&2; exit 1; }
if [ ! -s "$WORK/$TARBALL" ]; then
  echo "== fetching $TARBALL_URL"
  curl -fsSL --max-time 900 -o "$WORK/$TARBALL.part" "$TARBALL_URL"
  mv "$WORK/$TARBALL.part" "$WORK/$TARBALL"
fi
echo "$TARBALL_SHA256  $WORK/$TARBALL" | sha256sum -c - || { echo "build.sh: $TARBALL does not match the pinned sha256; refusing" >&2; exit 1; }
SRC="$WORK/linux-$VERSION"
if [ ! -d "$SRC" ]; then
  echo "== extracting $TARBALL"
  tar -C "$WORK" -xJf "$WORK/$TARBALL"
fi
cp "$CONFIG" "$SRC/.config"
cd "$SRC"
# the vendored config is complete for its kernel line; olddefconfig only
# answers options this point release added, with their defaults, and the
# resulting .config is hashed into the build record
make -s olddefconfig
echo "== make -j$JOBS vmlinux (linux-$VERSION, $(gcc --version | head -1))"
started=$(date -Is)
make -s -j"$JOBS" vmlinux 2>&1 | tail -5
finished=$(date -Is)
cp vmlinux "$OUT/vmlinux-$VERSION"
( cd "$OUT" && sha256sum "vmlinux-$VERSION" > "vmlinux-$VERSION.sha256" )
python3 - "$OUT" "$VERSION" "$TARBALL_SHA256" "$CONFIG_SHA256" "$started" "$finished" "$(sha256sum .config | cut -d' ' -f1)" "$(gcc --version | head -1)" "$(ld --version | head -1)" "$(make --version | head -1)" <<'PY'
import json, sys, platform, os
out, version, tar_sha, cfg_sha, started, finished, effective_cfg, gcc, ld, make = sys.argv[1:]
rec = {
  "artifact": f"vmlinux-{version}",
  "sha256": open(os.path.join(out, f"vmlinux-{version}.sha256")).read().split()[0],
  "inputs": {
    "source": {"name": f"linux-{version}.tar.xz", "url": f"https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-{version}.tar.xz", "sha256": tar_sha},
    "config": {"name": "microvm-kernel-ci-x86_64-6.1.config", "origin": "firecracker-microvm/firecracker v1.17.0 resources/guest_configs", "sha256": cfg_sha, "effective_sha256_after_olddefconfig": effective_cfg},
  },
  "toolchain": {"gcc": gcc, "ld": ld, "make": make, "host_kernel": platform.release(), "host": platform.node()},
  "started": started, "finished": finished,
  "bit_identical_claim": False,
}
json.dump(rec, open(os.path.join(out, "kernel-build.json"), "w"), indent=2)
print(json.dumps(rec, indent=2))
PY
echo "== $OUT/vmlinux-$VERSION: $(cat "$OUT/vmlinux-$VERSION.sha256")"
