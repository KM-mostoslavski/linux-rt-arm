# Maintainer: David Mostoslavski <david.mostoslavski@km-robota.com>
# Based on Arch Linux ARM's linux-aarch64 (Kevin Mihelich, graysky) and
# Arch Linux's linux-rt (David Runge).
#
# Mainline PREEMPT_RT kernel for aarch64. Since Linux 6.12 PREEMPT_RT is in
# mainline: RT is enabled purely through Kconfig (see config.rt-fragment).
# No out-of-tree RT patch set and no board patches are applied.

pkgbase=linux-rt-arm
_srcname=linux-7.2.8
_desc="AArch64 multi-platform, PREEMPT_RT"
pkgver=7.2.8
pkgrel=1
arch=('aarch64')
url="https://github.com/KM-mostoslavski/linux-rt-arm"
license=('GPL-2.0-only')
makedepends=('bc' 'kmod' 'libelf' 'openssl' 'perl' 'python' 'tar' 'xz')
options=('!strip' '!debug')
source=("https://cdn.kernel.org/pub/linux/kernel/v7.x/${_srcname}.tar."{xz,sign}
        'config'
        'linux.preset')
validpgpkeys=(
  'ABAF11C65A2970B130ABE3C479BE3E4300411886'  # Linus Torvalds
  '647F28654894E3BD457199BE38DBBDC86092693E'  # Greg Kroah-Hartman
)
sha256sums=('12e8d5a973d1ad7c5a5c69882e4022b131ed715db7003fdcd760ddf8c3e51941'
            'SKIP'
            'd259b7fdaa9c3e84ea26324bcb12bd0fa90804114ccde6e75c84e26e784190a0'
            '73fdca55e375b3f44831b010242a0c28dd389d8085c06abb4b5d75b2ed4c2372')

export KBUILD_BUILD_HOST=archlinuxarm
export KBUILD_BUILD_USER=$pkgbase
export KBUILD_BUILD_TIMESTAMP="$(date -Ru${SOURCE_DATE_EPOCH:+d @$SOURCE_DATE_EPOCH})"

prepare() {
  cd $_srcname

  echo "Setting version..."
  echo "-$pkgrel" > localversion.10-pkgrel
  echo "${pkgbase#linux}" > localversion.20-pkgname

  echo "Setting config..."
  cp ../config .config
  make olddefconfig
  diff -u ../config .config || :

  # Hard realtime or nothing: refuse to build a non-RT or dynamic-preempt kernel.
  echo "Checking PREEMPT_RT..."
  grep -qx 'CONFIG_PREEMPT_RT=y' .config \
    || { error "CONFIG_PREEMPT_RT=y missing after olddefconfig"; return 1; }
  ! grep -q '^CONFIG_PREEMPT_DYNAMIC=' .config \
    || { error "CONFIG_PREEMPT_DYNAMIC must not be set"; return 1; }

  make -s kernelrelease > version
  echo "Prepared $pkgbase version $(<version)"
}

build() {
  cd $_srcname
  unset LDFLAGS
  make Image Image.gz modules
  # Device tree blobs with symbols, so U-Boot can apply overlays
  make DTC_FLAGS="-@" dtbs
}

_package() {
  pkgdesc="The Linux Kernel and modules - ${_desc}"
  depends=('coreutils' 'kmod' 'mkinitcpio>=0.7')
  optdepends=(
    'linux-firmware: firmware images needed for some devices'
    'wireless-regdb: to set the correct wireless channels of your country'
    'rt-tests: cyclictest and other realtime test tools'
  )
  provides=("linux=${pkgver}" "KSMBD-MODULE" "WIREGUARD-MODULE")
  # Same /boot/Image{,.gz}, /boot/dtbs and initramfs-linux.img as linux-aarch64,
  # so existing board boot setups keep working unchanged.
  conflicts=('linux-aarch64' 'linux')
  install=${pkgbase}.install

  cd $_srcname
  local kernver="$(<version)"
  local modulesdir="$pkgdir/usr/lib/modules/$kernver"

  echo "Installing boot image and dtbs..."
  install -Dm644 arch/arm64/boot/Image{,.gz} -t "$pkgdir/boot"
  make INSTALL_DTBS_PATH="$pkgdir/boot/dtbs" dtbs_install

  echo "Installing modules..."
  make INSTALL_MOD_PATH="$pkgdir/usr" INSTALL_MOD_STRIP=1 \
    DEPMOD=/doesnt/exist modules_install  # depmod runs via kmod's pacman hook

  # remove build link
  rm "$modulesdir"/build

  echo "Installing mkinitcpio preset..."
  sed "s|%PKGBASE%|${pkgbase}|g;s|%KERNVER%|${kernver}|g" ../linux.preset |
    install -Dm644 /dev/stdin "$pkgdir/etc/mkinitcpio.d/$pkgbase.preset"

  # Trigger mkinitcpio's 90-mkinitcpio-install.hook (Target: usr/lib/initcpio/*)
  # instead of shipping our own hook, as linux-aarch64 does.
  echo "dummy file to trigger mkinitcpio to run" |
    install -Dm644 /dev/stdin "$pkgdir/usr/lib/initcpio/$kernver"
}

_package-headers() {
  pkgdesc="Headers and scripts for building modules for the Linux kernel - ${_desc}"
  depends=("$pkgbase=$pkgver-$pkgrel")
  provides=("linux-headers=${pkgver}")
  conflicts=('linux-aarch64-headers' 'linux-headers')

  cd $_srcname
  local builddir="$pkgdir/usr/lib/modules/$(<version)/build"

  echo "Installing build files..."
  install -Dt "$builddir" -m644 .config Makefile Module.symvers System.map \
    localversion.* version vmlinux
  install -Dt "$builddir/kernel" -m644 kernel/Makefile
  install -Dt "$builddir/arch/arm64" -m644 arch/arm64/Makefile
  cp -t "$builddir" -a scripts

  # add xfs and shmem for aufs building
  mkdir -p "$builddir"/{fs/xfs,mm}

  echo "Installing headers..."
  cp -t "$builddir" -a include
  cp -t "$builddir/arch/arm64" -a arch/arm64/include
  install -Dt "$builddir/arch/arm64/kernel" -m644 arch/arm64/kernel/asm-offsets.s
  mkdir -p "$builddir/arch/arm"
  cp -t "$builddir/arch/arm" -a arch/arm/include

  install -Dt "$builddir/drivers/md" -m644 drivers/md/*.h
  install -Dt "$builddir/net/mac80211" -m644 net/mac80211/*.h

  # https://bugs.archlinux.org/task/13146
  install -Dt "$builddir/drivers/media/i2c" -m644 drivers/media/i2c/msp3400-driver.h

  # https://bugs.archlinux.org/task/20402
  install -Dt "$builddir/drivers/media/usb/dvb-usb" -m644 drivers/media/usb/dvb-usb/*.h
  install -Dt "$builddir/drivers/media/dvb-frontends" -m644 drivers/media/dvb-frontends/*.h
  install -Dt "$builddir/drivers/media/tuners" -m644 drivers/media/tuners/*.h

  # https://bugs.archlinux.org/task/71392
  install -Dt "$builddir/drivers/iio/common/hid-sensors" -m644 drivers/iio/common/hid-sensors/*.h

  echo "Installing KConfig files..."
  find . -name 'Kconfig*' -exec install -Dm644 {} "$builddir/{}" \;

  echo "Removing unneeded architectures..."
  local arch
  for arch in "$builddir"/arch/*/; do
    [[ $arch = */arm64/ || $arch == */arm/ ]] && continue
    echo "Removing $(basename "$arch")"
    rm -r "$arch"
  done

  echo "Removing documentation..."
  rm -r "$builddir/Documentation"

  echo "Removing broken symlinks..."
  find -L "$builddir" -type l -printf 'Removing %P\n' -delete

  echo "Removing loose objects..."
  find "$builddir" -type f -name '*.o' -printf 'Removing %P\n' -delete

  echo "Stripping build tools..."
  local file
  while read -rd '' file; do
    case "$(file -bi "$file")" in
      application/x-sharedlib\;*)      # Libraries (.so)
        strip -v $STRIP_SHARED "$file" ;;
      application/x-archive\;*)        # Libraries (.a)
        strip -v $STRIP_STATIC "$file" ;;
      application/x-executable\;*)     # Binaries
        strip -v $STRIP_BINARIES "$file" ;;
      application/x-pie-executable\;*) # Relocatable binaries
        strip -v $STRIP_SHARED "$file" ;;
    esac
  done < <(find "$builddir" -type f -perm -u+x ! -name vmlinux -print0)

  echo "Adding symlink..."
  mkdir -p "$pkgdir/usr/src"
  ln -sr "$builddir" "$pkgdir/usr/src/$pkgbase"
}

pkgname=("$pkgbase" "$pkgbase-headers")
for _p in "${pkgname[@]}"; do
  eval "package_$_p() {
    $(declare -f "_package${_p#$pkgbase}")
    _package${_p#$pkgbase}
  }"
done

# vim:set ts=8 sts=2 sw=2 et:
