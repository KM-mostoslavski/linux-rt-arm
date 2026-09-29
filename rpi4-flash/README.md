# rpi4-flash

Flashes Arch Linux ARM onto an SD card / USB disk for a **Raspberry Pi 4**,
following the [ALARM Raspberry Pi 4 guide](https://archlinuxarm.org/platforms/armv8/broadcom/raspberry-pi-4)
with its outdated/wrong steps fixed. Four questions ([huh](https://github.com/charmbracelet/huh) forms), the rest is automatic.

```sh
cd rpi4-flash && go build -o rpi4-flash .
sudo ./rpi4-flash            # interactive
```

| Question | Choices | Default |
|---|---|---|
| Build | `aarch64`, `armv7h` | aarch64 |
| Device | every whole disk **except** the ones holding the running system (`/`, `/boot`, `/efi`, swap…) | first listed |
| Kernel | `7.2.7` (linux-rt-arm, PREEMPT_RT), `latest` | 7.2.7 |
| Swap | none, 1 GiB swap file, 1 GiB swap partition | none |

A final "erase everything on /dev/X?" confirmation follows (skip with `--yes`).
Every question can also be answered by a flag (`--arch --device --kernel --swap`),
which is how the end-to-end test runs it. `--verbose` streams command output; the full
log is always in `<cache-dir>/rpi4-flash.log`.

### What it does

1. Preflight, before anything is erased: root, tools, target is a whole non-system disk
   and is big enough, the kernel choice can be satisfied, qemu binfmt is available.
2. Downloads `ArchLinuxARM-rpi-<arch>-latest.tar.gz` into `--cache-dir`
   (`/var/cache/rpi4-flash`), verified against the published `.md5` (re-downloaded when
   upstream changed).
3. Unmounts / swapoffs anything on the target, wipes signatures.
4. MBR: `p1` 1 GiB FAT32 LBA (`0x0c`) `/boot`, `p2` ext4 `/`, `p3` 1 GiB swap (only
   with the swap-partition choice; root **must** stay partition 2, U-Boot's `boot.scr`
   hard-codes `:2`).
5. Extracts the rootfs to ext4, moves `boot/` onto the FAT partition, then mounts the
   FAT partition at `root/boot` (like on the Pi) for the next steps.
6. Pins `/etc/fstab` to filesystem UUIDs and `cmdline.txt` (armv7) to the root PARTUUID.
7. Swap file (`/swapfile`, 1 GiB) or partition, added to fstab.
8. Kernel, inside the rootfs via `arch-chroot` + qemu-user: pacman keyring init, then
   - `7.2.7`: removes the stock kernel and `pacman -U` the local `linux-rt-arm` package
     (`--rt-pkg`, else searched in the working dir, next to the binary, cache dir;
     7.2.7 preferred, otherwise the newest with a warning). aarch64 only; armv7 falls back
     to `latest`.
   - `latest`: `pacman -Syu` (full upgrade: never a partial one).
   Then rebuilds the initramfs without host autodetection (see below) and checks that
   `/boot` contains the whole boot chain.
9. aarch64: moves U-Boot's load addresses out of the kernel's way in `boot.txt` and
   recompiles `boot.scr` (see below).
10. sync, unmount.

### Where the wiki is wrong / outdated

| Wiki step | Problem | Here |
|---|---|---|
| `sed -i 's/mmcblk0/mmcblk1/g' root/etc/fstab` (aarch64) | The SD card name is not stable. The mainline `bcm2711-rpi-4-b.dtb` has **no `mmc0/mmc1` aliases**, so the number follows probe order; the downstream kernel calls it `mmcblk0`; from USB it is `sda`. A hard-coded name makes `/boot` fail to mount → emergency mode. | fstab uses `UUID=`; root comes from `root=PARTUUID=` (U-Boot `part uuid` on aarch64, rewritten `cmdline.txt` on armv7, which ships `root=/dev/mmcblk0p2`). |
| interactive `fdisk` | not scriptable | `sfdisk` script, 1 MiB aligned |
| `mkfs.vfat /dev/sdX1` | lets mkfs choose FAT12/16/32 | `mkfs.vfat -F 32` to match type `0x0c` |
| `wget http://…tar.gz` | no integrity check | md5 checked, cached |
| `mv root/boot/* boot` | fine, but `mv` to FAT warns on every file | `cp --no-preserve=ownership,mode` then delete |
| `pacman-key --init` on the Pi | pacman unusable until then | done while flashing (needed for the kernel step anyway) |
| auto-mounted card | `mkfs` fails "device busy" | unmounted/swapoff'd first |

The fstab failure was reproduced in QEMU: the same image with the wiki's line
(`/dev/mmcblk1p1 /boot`) gets `Timed out waiting for device /dev/mmcblk1p1` →
`Dependency failed for /boot` → **Emergency Mode** (the card is `mmcblk0` there); with
`UUID=` it reaches `Multi-User System`.

### Problems not in the wiki, found by booting the result

- **U-Boot load addresses.** U-Boot's rpi defaults put the fdt at `0x02600000` and the
  initramfs at `0x02700000`, ~38 MiB after the kernel. `boot.txt` loads `Image` first, then
  the fdt and initramfs over its tail. `linux-rt-arm`'s `Image` is ~50 MiB: U-Boot stops
  with `ERROR: RD image overlaps OS image (OS=200000..3340000)`. The stock `linux-aarch64`
  `Image` (44 MiB) is also past that limit. `boot.txt` now sets `kernel_addr_r=0x200000`,
  `fdt_addr_r=0x8000000`, `ramdisk_addr_r=0x8100000` (126 MiB for the kernel; fits a
  1 GiB Pi).
- **mkinitcpio autodetects the flashing PC.** `arch-chroot` bind-mounts the host's
  `/sys`, so `autodetect` saw this PC's NVIDIA GPU: `nouveau.ko` + all NVIDIA firmware went
  into the Pi's initramfs (133 MiB instead of 23). The initramfs is rebuilt with
  `-S autodetect,kms` (host-independent); the first kernel update on the Pi regenerates it
  with real autodetection.
- **pacman's sandbox under qemu-user.** `pacman -Syu` fails with `Landlock is not
  supported by the kernel`; the flash-time run uses `--disable-sandbox`.

### Testing

`go test ./...` covers the fstab/cmdline/boot.txt rewriting.

`sudo test/matrix.sh WORKDIR` runs `test/e2e.sh` for aarch64/7.2.7/swap partition,
aarch64/latest/swap file and armv7/latest/no swap: each flashes an 8 GiB image through a
loop device, then boots it in QEMU's `raspi4b` machine (`test/qemu_boot.py`):

1. **U-Boot stage** (aarch64): QEMU stands in for the VideoCore firmware and starts the
   image's `kernel8.img` (U-Boot), which must run the image's `boot.scr`, load kernel, DTB
   and initramfs without overlap, and the kernel must mount root through the PARTUUID U-Boot
   computed.
2. **Linux stage**: QEMU boots the image's own kernel/initramfs/DTB with the image's
   command line (+ a PL011 console); the test logs in on the serial console and checks
   `/boot` mounted (vfat, by UUID), root rw, swap as in fstab, no `/dev/mmcblk` in fstab,
   pacman keyring present, no failed systemd units.

QEMU 11's `raspi4b` cannot run the stock Pi 4 device tree: `test/qemu_dtb.sh` patches the
**test image's** DTBs only (disables the unemulated AON/HDMI block at `0x7ef00000`, PCIe,
GENET, RNG, thermal, and turns the SDHCI QEMU wires the SD card to into a plain SD host).
After U-Boot's hand-off, QEMU's SD controller also degrades (RCU stalls, I/O errors) while
the same image is clean when QEMU loads the kernel directly: that is why stage 2 exists.
None of this touches what `rpi4-flash` writes for a real Pi, but it also means a real Pi 4
boot is the remaining unverified step.

Host requirements: `qemu-user-static qemu-user-static-binfmt arch-install-scripts
dosfstools e2fsprogs libarchive uboot-tools`; `qemu-system-aarch64 dtc python` for the tests.
