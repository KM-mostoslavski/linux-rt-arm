#!/usr/bin/env bash
# End-to-end test: flash a disk image through a loop device with rpi4-flash,
# then boot it in QEMU's raspi4b machine and run in-guest checks.
#
#   sudo test/e2e.sh WORKDIR ARCH KERNEL SWAP [extra rpi4-flash flags...]
#   e.g. sudo test/e2e.sh /tmp/e2e aarch64 7.2.7 partition --cache-dir /var/cache/rpi4-flash
#
# The image is never attached to the host while QEMU runs: the boot files
# are copied out first and the loop device is detached.
set -o errexit -o nounset -o pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$1 arch=$2 kernel=$3 swap=$4
shift 4
name="${arch}-${kernel}-${swap}"
img="${work}/${name}.img"
mkdir -p "${work}"
rm -f "${img}"
truncate --size 8G "${img}" # power of two: QEMU's SD card model requires it

loop=$(losetup --find --show --partscan "${img}")
mnt=$(mktemp -d)
cleanup() {
  mountpoint -q "${mnt}" && umount "${mnt}"
  losetup --detach "${loop}" 2>/dev/null || true
  rmdir "${mnt}"
}
trap cleanup EXIT

"${here}/../rpi4-flash" --arch "${arch}" --device "${loop}" --kernel "${kernel}" \
  --swap "${swap}" --yes --log "${work}/${name}.flash.log" "$@"

rm -rf "${work}/${name}.boot"
mount "${loop}p1" "${mnt}"
# QEMU raspi4b cannot boot the stock Pi 4 device tree (see qemu_dtb.sh):
# patch the test image's DTBs (test-only change, like the QA script below).
for dtb in "${mnt}/bcm2711-rpi-4-b.dtb" "${mnt}/dtbs/broadcom/bcm2711-rpi-4-b.dtb"; do
  if [[ -f "${dtb}" ]]; then
    "${here}/qemu_dtb.sh" "${dtb}"
  fi
done
cp -r "${mnt}" "${work}/${name}.boot"
umount "${mnt}"
# In-guest checks for qemu_boot.py (test image only).
mount "${loop}p2" "${mnt}"
install -m 0755 "${here}/qa_guest.sh" "${mnt}/root/rpi4-flash-qa.sh"
umount "${mnt}"
losetup --detach "${loop}"

python3 "${here}/qemu_boot.py" "${img}" "${work}/${name}.boot" "${work}/${name}.serial.log" "${arch}"
