#!/usr/bin/env bash
# Patch a bcm2711-rpi-4-b.dtb IN PLACE so the mainline kernel boots on QEMU's
# raspi4b machine. TEST ONLY: never apply this to a card meant for a real Pi.
#
# QEMU 11 raspi4b gaps hit by the mainline kernel (it already disables PCIe,
# GENET, RNG200 and thermal itself):
#  - the AON/HDMI block at 0x7ef00000 is not emulated: probing its L2 intc
#    (aon_intr), DVP clock gate or HDMI DDC i2c ends in a synchronous
#    external abort (the i2c one inside udev wedges it: no by-uuid links);
#  - the firmware GPIO expander is not emulated, so the regulator of emmc2
#    (the real SD slot, fe340000) never appears and emmc2 never probes;
#  - QEMU wires the SD card to the Arasan SDHCI at 7e300000 (WiFi SDIO on a
#    real Pi 4: non-removable, with a power sequence on the missing GPIOs).
set -o errexit -o nounset -o pipefail
dtb=$1

disable() {
  if fdtget "${dtb}" "$1" compatible >/dev/null 2>&1; then
    fdtput --type s "${dtb}" "$1" status disabled
  fi
}
for node in $(fdtget --list "${dtb}" /soc); do
  case "${node}" in
    *@7ef*) disable "/soc/${node}" ;; # aon_intr, dvp, hdmi0/1, their i2c/cec
  esac
done

sdhci=/soc/mmc@7e300000
if fdtget "${dtb}" "${sdhci}" compatible >/dev/null 2>&1; then
  for prop in non-removable mmc-pwrseq; do
    fdtput --delete "${dtb}" "${sdhci}" "${prop}" 2>/dev/null || true
  done
  for child in $(fdtget --list "${dtb}" "${sdhci}" 2>/dev/null); do
    fdtput --remove "${dtb}" "${sdhci}/${child}"
  done
fi
