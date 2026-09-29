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
   Then checks that `/boot` contains the whole boot chain.
9. sync, unmount.

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

### Testing

`go test ./...` covers the fstab/cmdline rewriting. `test/e2e.sh` flashes an 8 GiB image
through a loop device and boots it in QEMU's `raspi4b` machine (`test/qemu_boot.py`),
logs in on the serial console and checks `/boot` is mounted, no failed units, etc.

QEMU does not run the VideoCore firmware, so it stands in for `start4.elf`: it loads the
image's own `kernel8.img` (U-Boot) and `bcm2711-rpi-4-b.dtb`; everything after that
(U-Boot, `boot.scr`, kernel, initramfs, systemd, fstab) is the image's own boot chain.

Host requirements: `qemu-user-static qemu-user-static-binfmt arch-install-scripts
dosfstools e2fsprogs libarchive`, and `qemu-system-aarch64` + `python` for the e2e test.
