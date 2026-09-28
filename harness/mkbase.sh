#!/bin/bash
# One-time: build work/vm/base.qcow2 (Arch Linux ARM aarch64, UEFI +
# systemd-boot, ESP mounted at /boot) WITHOUT root and without booting it.
#
# Provisioning (pacman-key, extra packages, rt-tests) runs in a qemu-user
# chroot inside an unprivileged user namespace (unshare --map-auto), so file
# ownership inside the image is correct. The image is then assembled from a
# FAT ESP (mtools) and an ext4 root (mke2fs -d) on a GPT disk (sfdisk).
set -euo pipefail
source "$(dirname "$0")/common.sh"

[[ -e $BASE && ${1:-} != --force ]] && { echo "$BASE exists (use --force)"; exit 0; }
mkdir -p "$VM"
[[ -f $SSHKEY ]] || ssh-keygen -q -t ed25519 -N '' -C linux-rt-arm-harness -f "$SSHKEY"

stage=$VM/stage
rm -rf "$stage" 2>/dev/null || unshare --map-auto --map-root-user rm -rf "$stage"
mkdir -p "$stage"

# --- Inside the user namespace: extract, provision, make ext4 --------------
unshare --map-auto --map-root-user --mount --pid --fork \
  env STAGE="$stage" TARBALL="$ROOTFS_TARBALL" PUBKEY="$(cat "$SSHKEY.pub")" \
      KCMD="$KCMDLINE_BASE" XFER_TAG="$XFER_TAG" bash -euo pipefail <<'NS'
R=$STAGE/rootfs; mkdir -p "$R" "$STAGE/esp"
echo ">> extracting rootfs"
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

echo ">> provisioning in qemu-user chroot"
chroot "$R" /bin/bash -euo pipefail <<'CH'
sed -i 's/^CheckSpace/#CheckSpace/' /etc/pacman.conf
pacman-key --init
pacman-key --populate archlinuxarm
PAC="pacman --noconfirm --disable-sandbox"
$PAC -Syu
$PAC -S --needed base-devel git numactl python openssh
if pacman -Si rt-tests &>/dev/null; then
  $PAC -S rt-tests
else
  echo ">> rt-tests not packaged in ALARM: building from source"
  git clone --depth 1 https://git.kernel.org/pub/scm/utils/rt-tests/rt-tests.git /tmp/rt-tests
  make -C /tmp/rt-tests -j"$(nproc)" cyclictest hackbench pi_stress signaltest
  install -m755 /tmp/rt-tests/{cyclictest,hackbench,pi_stress,signaltest} /usr/local/bin/
fi
sed -i 's/^#CheckSpace/CheckSpace/' /etc/pacman.conf
ssh-keygen -A
rm -rf /var/cache/pacman/pkg/*
CH

echo ">> configuring image"
umount "$R/usr/bin/qemu-aarch64-static"; rm -f "$R/usr/bin/qemu-aarch64-static"
for m in dev sys proc tmp run; do umount -R "$R/$m" 2>/dev/null || umount -Rl "$R/$m"; done
for m in dev sys proc tmp run; do
  ! mountpoint -q "$R/$m" || { echo "still mounted: $R/$m" >&2; exit 1; }
done
kver=$(ls "$R/usr/lib/modules" | head -1)
[[ -s $R/boot/initramfs-linux.img ]] || { echo "no initramfs in base" >&2; exit 1; }
[[ -f $R/usr/lib/systemd/boot/efi/systemd-bootaa64.efi ]] || { echo "no systemd-boot EFI binary" >&2; exit 1; }
echo ">> base kernel: $kver"
install -dm700 "$R/root/.ssh"
echo "$PUBKEY" > "$R/root/.ssh/authorized_keys"; chmod 600 "$R/root/.ssh/authorized_keys"
echo linux-rt-test > "$R/etc/hostname"
cat >> "$R/etc/fstab" <<'F'
/dev/vda2  /      ext4  rw,relatime  0 1
/dev/vda1  /boot  vfat  rw,relatime,fmask=0022,dmask=0022  0 2
F
cat > "$R/etc/systemd/system/harness-marker.service" <<'S'
[Unit]
Description=Print harness boot marker on the console
After=multi-user.target sshd.service
[Service]
Type=oneshot
ExecStart=/bin/sh -c 'echo "HARNESS-BOOT-OK uname-r=$(uname -r) uname-v=$(uname -v)" > /dev/ttyAMA0'
[Install]
WantedBy=multi-user.target
S
ln -sf /etc/systemd/system/harness-marker.service \
  "$R/etc/systemd/system/multi-user.target.wants/harness-marker.service"
ln -sf /usr/lib/systemd/system/sshd.service \
  "$R/etc/systemd/system/multi-user.target.wants/sshd.service"
# 9p share for artifacts in / logs out
echo "$XFER_TAG  /mnt/xfer  9p  trans=virtio,version=9p2000.L,msize=1048576,nofail,x-systemd.automount  0 0" >> "$R/etc/fstab"
mkdir -p "$R/mnt/xfer"

# ESP content: kernel+initramfs+dtbs (package-owned files), systemd-boot
mv "$R"/boot/* "$STAGE/esp/"
mkdir -p "$STAGE/esp/EFI/BOOT" "$STAGE/esp/loader/entries"
cp "$R/usr/lib/systemd/boot/efi/systemd-bootaa64.efi" "$STAGE/esp/EFI/BOOT/BOOTAA64.EFI"
printf 'default arch.conf\ntimeout 0\nconsole-mode keep\n' > "$STAGE/esp/loader/loader.conf"
printf 'title Arch Linux ARM\nlinux /Image\ninitrd /initramfs-linux.img\noptions %s\n' "$KCMD" \
  > "$STAGE/esp/loader/entries/arch.conf"

echo ">> building ext4 root"
mke2fs -q -t ext4 -L root -E root_owner=0:0 -d "$R" "$STAGE/root.ext4" 15G
chown -R 0:0 "$STAGE/esp"
NS

# --- Outside: FAT ESP, GPT disk, qcow2 ---------------------------------------
echo ">> building FAT ESP"
mkfs.fat -F 32 -n ESP -C "$stage/esp.img" $((1024*1024)) >/dev/null   # 1 GiB
(cd "$stage/esp" && mcopy -s -i "$stage/esp.img" ./* ::/)

echo ">> assembling GPT disk"
raw=$stage/disk.raw
truncate -s 17G "$raw"
sfdisk -q "$raw" <<'P'
label: gpt
start=1MiB, size=1GiB, type=C12A7328-F81F-11D2-BA4B-00A0C93EC93B, name=esp
start=1025MiB, size=15GiB, type=B921B045-1DF0-41C3-AF44-4C6F280D3FAE, name=root
P
dd if="$stage/esp.img" of="$raw" bs=1M seek=1 conv=notrunc,sparse status=none
dd if="$stage/root.ext4" of="$raw" bs=1M seek=1025 conv=notrunc,sparse status=none
qemu-img convert -O qcow2 "$raw" "$BASE.tmp" && mv "$BASE.tmp" "$BASE"
chmod a-w "$BASE"   # never boot or modify the base directly
unshare --map-auto --map-root-user rm -rf "$stage"
echo ">> done: $BASE"
