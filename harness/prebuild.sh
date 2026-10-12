#!/bin/bash
# Pre-build the release packages for the fileserver (docs/PREBUILD.md).
#
# Runs the native build (harness/native-validate.sh: the committed PKGBUILD,
# unmodified, in a clean Arch Linux ARM aarch64 chroot under qemu-user), checks
# the result, and publishes it to out/<pkgver>-<pkgrel>/:
#   *.pkg.tar.xz   the packages (kernel + headers)
#   SHA256SUMS     checksums of the packages
#   BUILDINFO      what was built, from which commit, with which toolchain
#   build.log      the full native build log
# out/latest points at the newest one. Nothing is published if a step fails.
#
# Usage: harness/prebuild.sh [<git-ref>]      (default: HEAD)
# Env:   JOBS=<n>  parallel make jobs in the chroot (default: native-validate.sh's)
#        FORCE=1   replace an existing out/<pkgver>-<pkgrel>/
set -euo pipefail
source "$(dirname "$0")/common.sh"

die() { log "error: $*"; exit 1; }
ROOTFS=${GENERIC_TARBALL:-$ROOTFS_TARBALL}   # generic ALARM aarch64 rootfs
ROOTFS_URL=http://os.archlinuxarm.org/os/ArchLinuxARM-aarch64-latest.tar.gz
OUT=$REPO/out

ref=${1:-HEAD}
commit=$(git -C "$REPO" rev-parse --verify "$ref^{commit}") || die "unknown git ref: $ref"
if [[ $ref == HEAD && -n $(git -C "$REPO" status --porcelain --untracked-files=no) ]]; then
  log "warning: uncommitted changes are NOT built; building commit $commit"
fi

# --- what will be built (from the commit, not the working tree) -------------
srcinfo=$(git -C "$REPO" show "$commit:.SRCINFO")
field() { sed -n "s/^\t*$1 = //p" <<<"$srcinfo" | head -1; }
pkgbase=$(field pkgbase); pkgver=$(field pkgver); pkgrel=$(field pkgrel)
mapfile -t pkgnames < <(sed -n 's/^pkgname = //p' <<<"$srcinfo")
pb_ver=$(git -C "$REPO" show "$commit:PKGBUILD" | sed -n 's/^pkgver=//p')
pb_rel=$(git -C "$REPO" show "$commit:PKGBUILD" | sed -n 's/^pkgrel=//p')
[[ $pb_ver == "$pkgver" && $pb_rel == "$pkgrel" ]] \
  || die ".SRCINFO ($pkgver-$pkgrel) does not match PKGBUILD ($pb_ver-$pb_rel): run makepkg --printsrcinfo > .SRCINFO and commit"
version=$pkgver-$pkgrel
dest=$OUT/$version
[[ ! -e $dest || ${FORCE:-0} == 1 ]] || die "$dest already exists (FORCE=1 to replace it; bump pkgrel for a new release)"

mkdir -p "$WORK/logs" "$WORK/srcdest" "$OUT"
exec 9>"$WORK/prebuild.lock"
flock -n 9 || die "another prebuild is running in $REPO"
log "prebuild $pkgbase $version from $ref ($commit)"

# --- inputs: ALARM rootfs and upstream sources -------------------------------
if [[ ! -f $ROOTFS ]]; then
  log "downloading $ROOTFS_URL"
  mkdir -p "$(dirname "$ROOTFS")"
  curl -fL --retry 3 -o "$ROOTFS.part" "$ROOTFS_URL"
  curl -fsSL --retry 3 -o "$ROOTFS.md5" "$ROOTFS_URL.md5"
  [[ $(md5sum <"$ROOTFS.part" | cut -d' ' -f1) == $(cut -d' ' -f1 "$ROOTFS.md5") ]] \
    || die "rootfs md5 mismatch"
  mv "$ROOTFS.part" "$ROOTFS"
fi

# Download what is missing into work/srcdest and verify the PKGBUILD's
# checksums. The PGP signature is checked again inside the chroot.
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
git -C "$REPO" archive "$commit" | tar -x -C "$tmp"
(cd "$tmp" && SRCDEST=$WORK/srcdest makepkg --verifysource --skippgpcheck -A -d >&2) \
  || die "source download/verification failed"

# --- build ---------------------------------------------------------------------
start=$(date +%s)
REF=$commit "$REPO/harness/native-validate.sh" || die "native build failed, see $WORK/logs/native-latest.log"
buildlog=$(readlink -f "$WORK/logs/native-latest.log")

# --- check -------------------------------------------------------------------
pkgdest=$WORK/native/pkgdest
files=()
for n in "${pkgnames[@]}"; do
  f=$(compgen -G "$pkgdest/$n-$version-aarch64.pkg.tar.*" | head -1) || die "missing package $n-$version"
  [[ $(bsdtar -xOf "$f" .PKGINFO | sed -n 's/^pkgver = //p') == "$version" ]] || die "$f: wrong pkgver in .PKGINFO"
  files+=("$f")
done
hdr=$(compgen -G "$pkgdest/$pkgbase-headers-$version-aarch64.pkg.tar.*" | head -1) || die "no headers package"
config=$(bsdtar -xOf "$hdr" 'usr/lib/modules/*/build/.config')
grep -qx 'CONFIG_PREEMPT_RT=y' <<<"$config" || die "kernel config: CONFIG_PREEMPT_RT is not y"
grep -qx 'CONFIG_PREEMPT_DYNAMIC=y' <<<"$config" && die "kernel config: CONFIG_PREEMPT_DYNAMIC is set"
kernrel=$(bsdtar -tf "$hdr" | sed -n 's|^usr/lib/modules/\([^/]*\)/build/$|\1|p' | head -1)
chroot_pkgs=$(ls "$WORK/native/root/var/lib/pacman/local" | grep -E '^(gcc|binutils|glibc)-[0-9]' | tr '\n' ' ' || true)
makepkg_s=$(sed -n 's/^== makepkg OK in \([0-9]*\)s$/\1/p' "$buildlog" | tail -1)

# --- publish (staging dir, then one rename) ----------------------------------
stage=$OUT/.$version.tmp
rm -rf "$stage"; mkdir -p "$stage"
cp "${files[@]}" "$stage/"
cp "$buildlog" "$stage/build.log"
(cd "$stage" && sha256sum -- *.pkg.tar.* > SHA256SUMS)
cat > "$stage/BUILDINFO" <<EOF
pkgbase        $pkgbase
version        $version
kernel_release $kernrel
packages       $(cd "$stage" && echo *.pkg.tar.*)
git_ref        $ref
git_commit     $commit
git_describe   $(git -C "$REPO" describe --always --tags "$commit")
built_utc      $(date -u +%Y-%m-%dT%H:%M:%SZ)
build_host     $(hostname) ($(uname -m), qemu-user aarch64 chroot)
chroot_rootfs  $(basename "$ROOTFS") (md5 $(md5sum <"$ROOTFS" | cut -d' ' -f1)), pacman -Syu at build time
chroot_tools   $chroot_pkgs
makepkg_time   ${makepkg_s:-?}s (total $(( $(date +%s) - start ))s)
preempt        CONFIG_PREEMPT_RT=y, CONFIG_PREEMPT_DYNAMIC not set (checked)
signed         no (makepkg --nosign)
EOF
rm -rf "$dest"; mv "$stage" "$dest"
ln -sfn "$version" "$OUT/latest"
log "published $dest"
ls -l "$dest" >&2
