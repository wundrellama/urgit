#!/bin/bash
# The guest image recipe (BRIEF-CI-P4 D2, S1.3): every input pinned and
# hashed, every bootstrap download inventoried, one manifest out.
#
#   usage: build.sh <out-dir> [work-dir]
#   needs: podman (rootless, with subuids), skopeo, mke2fs/resize2fs/e2fsck,
#          go (this tree), the kernel built by kernel/build.sh in <out-dir>,
#          SOURCE_MANIFEST_SHA256 (the source identity, below),
#          firecracker (the pinned release under .scratch/ci-p4/tools, or
#          $FIRECRACKER) for the provisioning boot
#
# Steps:
#   1. urgit-guest helper: CGO_ENABLED=0 go build from this tree (hashed).
#   2. act 0.2.89: CGO_ENABLED=0 go install of the pinned module (hashed).
#   3. Docker static release: downloaded once, verified against the sha256
#      pinned below (the first build recorded it; a re-download must match).
#   4. Job image: skopeo copies catthehacker/ubuntu:act-latest AT ITS PINNED
#      DIGEST into a docker-archive tar (no daemon involved).
#   5. Rootfs: podman build (rootless) of rootfs/Containerfile from the
#      pinned base digest and dated apt snapshot; podman export; the tar
#      extracted inside `podman unshare` (uids kept) and turned into ext4
#      by mke2fs -d.
#   6. Provisioning boot: Firecracker (this user, no jailer, no NIC, no
#      vsock — a build tool, not the execution path) boots the kernel +
#      stage-1 rootfs with a second drive carrying the job image tar; the
#      helper's provision mode `docker load`s it into /var/lib/docker and
#      powers off. The result is the golden rootfs.
#   7. e2fsck, shrink to used+margin, record sha256s, sizes, package list,
#      tool versions and the digests in manifest.json.
#
# Bit-identical output is NOT claimed (Docker's data root has random ids);
# `build.sh --compare <a> <b>` reports whether two manifests' component
# hashes agree, which is the reproducibility statement the record makes.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
if [ "${1:-}" = --compare ]; then
  python3 - "$2" "$3" <<'PY'
import json, sys
a, b = (json.load(open(p)) for p in sys.argv[1:3])
keys = [("kernel","sha256"),("helper","sha256"),("act","sha256"),("docker","tarball_sha256"),("act_image","digest"),("rootfs","base_image"),("rootfs","apt_snapshot"),("rootfs","packages_sha256"),("rootfs","sha256")]
same = True
for k1, k2 in keys:
    va, vb = a.get(k1, {}).get(k2), b.get(k1, {}).get(k2)
    print(f"{k1}.{k2}: {'same' if va == vb else 'DIFFERENT'}  {va} | {vb}")
    if va != vb and (k1, k2) != ("rootfs", "sha256"): same = False
print("inputs identical:", same, "; rootfs bytes identical:", a['rootfs']['sha256'] == b['rootfs']['sha256'])
PY
  exit 0
fi
# the provisioning boot's console names the images it loaded (C5: grep;
# `ug` is not installed on this host, and its exit 127 would have been read
# as "the boot did not load the image"); `build.sh --check-console <log>`
# runs the same check alone
console_loaded() { grep -q 'provision: images:' "$1"; }
if [ "${1:-}" = --check-console ]; then console_loaded "${2:?console log}"; exit; fi
OUT="${1:?out-dir}"; WORK="${2:-$OUT/../build/rootfs}"
# ---- the source identity (C4) ---------------------------------------------
# SOURCE_MANIFEST_SHA256 names the source this image is built from: the
# sha256 of the source tree's own file manifest, one `<sha256>  runner/<path>`
# line per regular file under $ROOT/runner, sorted by path. It is an
# explicit input, recomputed here: missing, or not this tree's, the build
# refuses before anything is done. No Git is run, and no commit is recorded:
# manifest.json carries it as the helper's source_manifest_sha256.
source_manifest_sha256() {
  ( cd "$ROOT" && find runner -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum ) | sha256sum | cut -d' ' -f1
}
[ -n "${SOURCE_MANIFEST_SHA256:-}" ] || { echo "build.sh: SOURCE_MANIFEST_SHA256, the sha256 of the source tree's file manifest, is required; refusing" >&2; exit 1; }
SOURCE_ID=$(source_manifest_sha256)
[ "$SOURCE_ID" = "$SOURCE_MANIFEST_SHA256" ] || { echo "build.sh: the source tree's file manifest is sha256 $SOURCE_ID, not the given SOURCE_MANIFEST_SHA256 $SOURCE_MANIFEST_SHA256; refusing" >&2; exit 1; }
mkdir -p "$OUT" "$WORK/downloads" "$WORK/docker"
# ---- pins ----------------------------------------------------------------
BASE_DIGEST=sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251   # docker.io/library/debian:bookworm-slim, read 2026-09-20
APT_SNAPSHOT=20260919T000000Z
DOCKER_VERSION=29.8.1
DOCKER_URL="https://download.docker.com/linux/static/stable/x86_64/docker-$DOCKER_VERSION.tgz"
DOCKER_SHA256="${DOCKER_SHA256:-$(cat "$HERE/rootfs/docker-$DOCKER_VERSION.sha256" 2>/dev/null || true)}"
ACT_VERSION=0.2.89
ACT_IMAGE_REF=docker.io/catthehacker/ubuntu:act-latest
ACT_IMAGE_DIGEST=sha256:c58e2b364da03b0c804c7d660f2ecbedf2f221a382b9baa0b344b0144780ff43   # read 2026-09-20 via skopeo inspect
KERNEL="$OUT/vmlinux-6.1.188"
FIRECRACKER="${FIRECRACKER:-$ROOT/.scratch/ci-p4/tools/release-v1.17.0-x86_64/firecracker-v1.17.0-x86_64}"
STAGE1_MIB=${STAGE1_MIB:-8192}
[ -s "$KERNEL" ] || { echo "build.sh: no kernel at $KERNEL; run kernel/build.sh first" >&2; exit 1; }
[ -x "$FIRECRACKER" ] || { echo "build.sh: no firecracker at $FIRECRACKER" >&2; exit 1; }
started=$(date -Is)
# ---- 1. the helper --------------------------------------------------------
echo "== 1. urgit-guest helper"
( cd "$ROOT/runner" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$WORK/urgit-guest" ./cmd/urgit-guest )
HELPER_SHA=$(sha256sum "$WORK/urgit-guest" | cut -d' ' -f1)
# ---- 2. act ---------------------------------------------------------------
echo "== 2. act $ACT_VERSION"
if [ ! -x "$WORK/act" ] || [ "$("$WORK/act" --version 2>/dev/null)" != "act version $ACT_VERSION" ]; then
  CGO_ENABLED=0 GOBIN="$WORK" GOFLAGS=-trimpath go install "github.com/nektos/act@v$ACT_VERSION"
fi
[ "$("$WORK/act" --version)" = "act version $ACT_VERSION" ]
ACT_SHA=$(sha256sum "$WORK/act" | cut -d' ' -f1)
# ---- 3. docker static -----------------------------------------------------
echo "== 3. docker $DOCKER_VERSION static release"
DTGZ="$WORK/downloads/docker-$DOCKER_VERSION.tgz"
if [ ! -s "$DTGZ" ]; then curl -fsSL --max-time 900 -o "$DTGZ.part" "$DOCKER_URL"; mv "$DTGZ.part" "$DTGZ"; fi
got=$(sha256sum "$DTGZ" | cut -d' ' -f1)
if [ -z "$DOCKER_SHA256" ]; then
  echo "$got" > "$HERE/rootfs/docker-$DOCKER_VERSION.sha256"; DOCKER_SHA256=$got
  echo "   first download: sha256 $got pinned into rootfs/docker-$DOCKER_VERSION.sha256"
elif [ "$got" != "$DOCKER_SHA256" ]; then
  echo "build.sh: $DTGZ sha256 $got does not match the pinned $DOCKER_SHA256; refusing" >&2; exit 1
fi
rm -rf "$WORK/docker"; mkdir -p "$WORK/docker"; tar -xzf "$DTGZ" -C "$WORK/docker" --strip-components=1
/bin/ls "$WORK/docker"
# ---- 4. the job image at its digest --------------------------------------
echo "== 4. job image $ACT_IMAGE_REF@$ACT_IMAGE_DIGEST"
IMGTAR="$WORK/downloads/act-image.tar"
if [ ! -s "$IMGTAR" ]; then
  skopeo copy --override-os linux --override-arch amd64 "docker://${ACT_IMAGE_REF%%:*}@$ACT_IMAGE_DIGEST" "docker-archive:$IMGTAR:catthehacker/ubuntu:act-latest"
fi
IMGTAR_SHA=$(sha256sum "$IMGTAR" | cut -d' ' -f1)
# the provisioning drive: an ext4 image holding the tar
PROV="$WORK/provision.ext4"
rm -f "$PROV"; truncate -s $(( $(stat -c %s "$IMGTAR") / 1048576 + 64 ))M "$PROV"
mkdir -p "$WORK/prov-root"; ln -sf "$IMGTAR" "$WORK/prov-root/act-image.tar" 2>/dev/null || cp "$IMGTAR" "$WORK/prov-root/act-image.tar"
# mke2fs -d follows the symlink? it archives the link; copy instead
rm -f "$WORK/prov-root/act-image.tar"; cp --reflink=auto "$IMGTAR" "$WORK/prov-root/act-image.tar"
mke2fs -q -F -t ext4 -d "$WORK/prov-root" -L provision "$PROV"
# ---- 5. rootfs ------------------------------------------------------------
echo "== 5. rootfs (podman build, base $BASE_DIGEST, apt snapshot $APT_SNAPSHOT)"
CTX="$WORK/ctx"; rm -rf "$CTX"; mkdir -p "$CTX"
cp "$HERE/rootfs/Containerfile" "$CTX/"; cp -r "$WORK/docker" "$CTX/docker"; cp "$WORK/act" "$CTX/act"; cp "$WORK/urgit-guest" "$CTX/urgit-guest"
# the base image by digest only (a tag is never resolved here)
podman image exists "docker.io/library/debian@$BASE_DIGEST" || podman pull -q "docker.io/library/debian@$BASE_DIGEST" >/dev/null
podman build --pull=never --no-cache --build-arg BASE_DIGEST="$BASE_DIGEST" --build-arg APT_SNAPSHOT="$APT_SNAPSHOT" -t urgit-guest-rootfs:build "$CTX" 2>&1 | tail -40
cid=$(podman create urgit-guest-rootfs:build /bin/true)
podman export "$cid" -o "$WORK/rootfs.tar"; podman rm "$cid" >/dev/null
STAGE1="$WORK/stage1.ext4"; rm -f "$STAGE1"; truncate -s "${STAGE1_MIB}M" "$STAGE1"
# inside the user namespace the tar's uids map to subuids and mke2fs
# records them as the guest sees them (root is root)
podman unshare bash -c "rm -rf '$WORK/root' && mkdir -p '$WORK/root' && tar -xpf '$WORK/rootfs.tar' -C '$WORK/root' --numeric-owner && mke2fs -q -F -t ext4 -d '$WORK/root' -L urgit-guest -E lazy_itable_init=1 '$STAGE1' && cat '$WORK/root/etc/urgit-guest-packages.txt' > '$WORK/packages.txt' && rm -rf '$WORK/root'"
PACKAGES_SHA=$(sha256sum "$WORK/packages.txt" | cut -d' ' -f1)
# ---- 6. provisioning boot -------------------------------------------------
echo "== 6. provisioning boot (firecracker $(basename "$FIRECRACKER"), this user, no jailer, no network, no vsock)"
cat > "$WORK/provision-vm.json" <<JSON
{
  "boot-source": {"kernel_image_path": "$KERNEL", "boot_args": "console=ttyS0 reboot=k panic=1 pci=off nomodule random.trust_cpu=on ipv6.disable=1 init=/sbin/urgit-guest urgit.provision=1"},
  "drives": [
    {"drive_id": "rootfs", "path_on_host": "$STAGE1", "is_root_device": true, "is_read_only": false},
    {"drive_id": "provision", "path_on_host": "$PROV", "is_root_device": false, "is_read_only": true}
  ],
  "machine-config": {"vcpu_count": 4, "mem_size_mib": 4096, "smt": false}
}
JSON
rm -f "$WORK/provision-console.log"
timeout 900 "$FIRECRACKER" --no-api --config-file "$WORK/provision-vm.json" > "$WORK/provision-console.log" 2>&1 || { echo "build.sh: provisioning boot failed; console:" >&2; tail -40 "$WORK/provision-console.log" >&2; exit 1; }
console_loaded "$WORK/provision-console.log" || { echo "build.sh: the provisioning boot did not load the image; console:" >&2; tail -40 "$WORK/provision-console.log" >&2; exit 1; }
tail -6 "$WORK/provision-console.log"
# ---- 7. finish ------------------------------------------------------------
echo "== 7. fsck, shrink, hash"
e2fsck -fy "$STAGE1" >/dev/null 2>&1 || true
resize2fs -M "$STAGE1" >/dev/null 2>&1
USED_MIB=$(( $(stat -c %s "$STAGE1") / 1048576 ))
# margin so the guest's own writes (Docker state, tmp) never hit the
# baseline; the per-attempt drive grows by disk_mib on top of this
resize2fs "$STAGE1" "$(( USED_MIB + 512 ))M" >/dev/null 2>&1
e2fsck -fy "$STAGE1" >/dev/null 2>&1 || true
FINAL="$OUT/urgit-guest.ext4"; mv "$STAGE1" "$FINAL"
ROOTFS_SHA=$(sha256sum "$FINAL" | cut -d' ' -f1)
KERNEL_SHA=$(cut -d' ' -f1 "$OUT/vmlinux-6.1.188.sha256")
cp "$WORK/urgit-guest" "$OUT/urgit-guest"; cp "$WORK/packages.txt" "$OUT/packages.txt"; cp "$WORK/provision-console.log" "$OUT/provision-console.log"
finished=$(date -Is)
python3 - "$OUT" "$KERNEL_SHA" "$ROOTFS_SHA" "$USED_MIB" "$BASE_DIGEST" "$APT_SNAPSHOT" "$PACKAGES_SHA" "$DOCKER_VERSION" "$DOCKER_SHA256" "$ACT_VERSION" "$ACT_SHA" "$ACT_IMAGE_REF" "$ACT_IMAGE_DIGEST" "$IMGTAR_SHA" "$HELPER_SHA" "$SOURCE_ID" "$started" "$finished" "$(podman --version)" "$(skopeo --version)" "$(mke2fs -V 2>&1 | head -1)" "$(go version)" "$("$FIRECRACKER" --version | head -1)" <<'PY'
import json, sys, os, platform
(out, ksha, rsha, used, base, snap, pkgsha, dver, dsha, aver, asha, iref, idig, itar, hsha, srcman, started, finished, podman, skopeo, mke2fs, gov, fc) = sys.argv[1:]
kb = json.load(open(os.path.join(out, "kernel-build.json")))
m = {
  "version": 1,
  "kernel": {"name": "vmlinux-6.1.188", "sha256": ksha, "source": kb["inputs"]["source"], "config": kb["inputs"]["config"], "toolchain": kb["toolchain"]},
  "rootfs": {"name": "urgit-guest.ext4", "sha256": rsha, "baseline_mib": int(used) + 512, "used_mib": int(used), "base_image": "docker.io/library/debian:bookworm-slim@" + base, "apt_snapshot": snap, "packages": "packages.txt", "packages_sha256": pkgsha},
  "docker": {"version": dver, "tarball": f"docker-{dver}.tgz", "tarball_sha256": dsha, "url": f"https://download.docker.com/linux/static/stable/x86_64/docker-{dver}.tgz"},
  "act": {"version": aver, "sha256": asha, "module": f"github.com/nektos/act@v{aver}"},
  "act_image": {"reference": iref, "digest": idig, "archive_sha256": itar},
  "helper": {"version": 1, "sha256": hsha, "source_manifest_sha256": srcman},
  "boot_args": "console=ttyS0 reboot=k panic=1 pci=off nomodule random.trust_cpu=on ipv6.disable=1 init=/sbin/urgit-guest",
  "notices": "runner/guest/NOTICES.md",
  "bootstrap_downloads": [
    {"what": "kernel source", "url": kb["inputs"]["source"]["url"], "sha256": kb["inputs"]["source"]["sha256"]},
    {"what": "docker static release", "url": f"https://download.docker.com/linux/static/stable/x86_64/docker-{dver}.tgz", "sha256": dsha},
    {"what": "act module", "url": f"proxy.golang.org github.com/nektos/act@v{aver}", "sha256": asha, "note": "go.sum-verified module; the binary's sha256 is recorded"},
    {"what": "job OCI image", "url": iref, "digest": idig, "archive_sha256": itar},
    {"what": "debian base image", "url": "docker.io/library/debian:bookworm-slim", "digest": base},
    {"what": "apt packages", "url": f"https://snapshot.debian.org/archive/debian/{snap}", "packages_sha256": pkgsha},
  ],
  "provisioning_boot": {"vmm": fc, "privilege": "unprivileged user, no jailer, no NIC, no vsock; build tool only", "console": "provision-console.log"},
  "built": {"started": started, "finished": finished, "host": platform.node(), "host_kernel": platform.release(), "tools": {"podman": podman, "skopeo": skopeo, "mke2fs": mke2fs, "go": gov}},
  "bit_identical_claim": False,
}
json.dump(m, open(os.path.join(out, "manifest.json"), "w"), indent=2)
print(json.dumps({k: m[k] for k in ("kernel", "rootfs", "helper", "act_image")}, indent=1))
PY
( cd "$OUT" && sha256sum manifest.json > manifest.json.sha256 )
echo "== manifest: $(cat "$OUT/manifest.json.sha256")"
