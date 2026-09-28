#!/bin/bash
# Final validation: build the shipped PKGBUILD UNMODIFIED, natively for
# aarch64, the way an ALARM user would: a pristine Arch Linux ARM rootfs,
# base-devel + the PKGBUILD's makedepends only, `makepkg` as a normal user
# (dependency check on, PGP check on). aarch64 binaries run through
# qemu-user (binfmt); the chroot lives in an unprivileged user namespace
# (unshare --map-auto), so no root on the host.
#
# Output: work/native/pkgdest/*.pkg.tar.zst, log work/logs/native-<ts>.log
set -euo pipefail
source "$(dirname "$0")/common.sh"

nat=$WORK/native
logf=$WORK/logs/native-$(ts).log
[[ -d $nat ]] && unshare --map-auto --map-root-user rm -rf "$nat"
mkdir -p "$nat"
ln -sf "$(basename "$logf")" "$WORK/logs/native-latest.log"
log "native validation -> $logf"

# package sources exactly as they would be published (tracked files only)
mkdir -p "$nat/pkgsrc"
git -C "$REPO" archive HEAD | tar -x -C "$nat/pkgsrc"

unshare --map-auto --map-root-user --mount --pid --fork \
  env NAT="$nat" TARBALL="$ROOTFS_TARBALL" SRCTAR="$WORK/srcdest" JOBS="$(nproc)" \
  bash -euo pipefail <<'NS' 2>&1 | tee "$logf"
R=$NAT/root; mkdir -p "$R"
bsdtar -xpf "$TARBALL" -C "$R"
mount --bind "$R" "$R"
mount -t proc proc "$R/proc"
mount --rbind /sys "$R/sys"
mount --rbind /dev "$R/dev"
mount -t tmpfs tmpfs "$R/tmp"
mount -t tmpfs tmpfs "$R/run"
touch "$R/usr/bin/qemu-aarch64-static"
mount --bind /usr/bin/qemu-aarch64-static "$R/usr/bin/qemu-aarch64-static"
rm -f "$R/etc/resolv.conf"; cp -L /etc/resolv.conf "$R/etc/resolv.conf"
mkdir -p "$R/build/pkg" "$R/build/srcdest"
cp -a "$NAT/pkgsrc/." "$R/build/pkg/"
# pre-seed the upstream tarball (checksum + signature are still verified)
cp "$SRCTAR"/linux-*.tar.{xz,sign} "$R/build/srcdest/"

chroot "$R" /usr/bin/env JOBS="$JOBS" /bin/bash -euo pipefail <<'CH'
echo "== uname -m in chroot: $(uname -m)"
sed -i 's/^CheckSpace/#CheckSpace/' /etc/pacman.conf
pacman-key --init >/dev/null
pacman-key --populate archlinuxarm >/dev/null
PAC="pacman --noconfirm --disable-sandbox"
$PAC -Syu
$PAC -S --needed base-devel
cd /build/pkg
# makedepends straight from .SRCINFO (what an AUR helper would install)
mapfile -t mk < <(sed -n 's/^\tmakedepends = //p' .SRCINFO)
echo "== makedepends: ${mk[*]}"
$PAC -S --needed --asdeps "${mk[@]}"
useradd -m builder
chown -R builder: /build
sed -i "s/^#\?MAKEFLAGS=.*/MAKEFLAGS=\"-j$JOBS\"/" /etc/makepkg.conf
su builder -c 'for k in keys/pgp/*.asc; do gpg --import "$k"; done'
echo "== makepkg (native aarch64, unmodified PKGBUILD)"
start=$(date +%s)
su builder -c 'SRCDEST=/build/srcdest PKGDEST=/build/out makepkg --noconfirm --nosign'
echo "== makepkg OK in $(( $(date +%s) - start ))s"
ls -l /build/out
CH
mkdir -p "$NAT/pkgdest"; cp "$R"/build/out/*.pkg.tar.* "$NAT/pkgdest/"
chown -R 0:0 "$NAT/pkgdest"
NS
rc=${PIPESTATUS[0]}
log "native validation rc=$rc"
exit "$rc"
