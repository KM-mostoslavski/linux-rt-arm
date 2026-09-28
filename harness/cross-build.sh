#!/bin/bash
# DEV ONLY: build the unmodified PKGBUILD on the x86_64 host with the aarch64
# cross toolchain + ccache. The shipped PKGBUILD never requires this; it
# builds natively on aarch64 (see harness/native-validate.sh).
#
# Output: work/pkgdest/*.pkg.tar.zst, log in work/logs/cross-build-<ts>.log
# Note: the -headers package from a cross build contains x86_64 host tools
# (scripts/), so it is only good for boot tests, never for distribution.
set -euo pipefail
source "$(dirname "$0")/common.sh"

wrap=$WORK/crossbin
mkdir -p "$wrap" "$WORK"/{pkgdest,srcdest,build,logs,ccache}
# ccache masquerade: ccache finds the real compiler further down PATH
ln -sf /usr/bin/ccache "$wrap/aarch64-linux-gnu-gcc"

conf=$WORK/makepkg-cross.conf
cat > "$conf" <<C
source /etc/makepkg.conf
CARCH=aarch64
CHOST=aarch64-unknown-linux-gnu
CFLAGS="" CXXFLAGS="" LDFLAGS=""
MAKEFLAGS="-j$(nproc)"
BUILDENV=(!distcc color !ccache !check !sign)
OPTIONS=(!strip !docs !libtool !staticlibs emptydirs zipman purge !debug !lto)
PKGDEST=$WORK/pkgdest
SRCDEST=$WORK/srcdest
BUILDDIR=$WORK/build
PKGEXT=.pkg.tar.zst
COMPRESSZST=(zstd -c -T0 -)
C

logf=$WORK/logs/cross-build-$(ts).log
log "cross build -> $logf"
export ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- PATH="$wrap:$PATH"
export CCACHE_DIR=$WORK/ccache CCACHE_MAXSIZE=30G CCACHE_BASEDIR=$WORK/build
cd "$REPO"
start=$(date +%s)
makepkg --config "$conf" -f -d --noconfirm "$@" 2>&1 | tee "$logf" >/dev/null
rc=${PIPESTATUS[0]}
log "makepkg rc=$rc in $(( $(date +%s) - start ))s"
ccache -s | tee -a "$logf" | grep -iE 'hits|misses' >&2 || true
ln -sf "$(basename "$logf")" "$WORK/logs/cross-build-latest.log"
exit "$rc"
