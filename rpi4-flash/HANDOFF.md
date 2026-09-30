# Handoff — rpi4-flash (session of 2026-09-29, stopped 16:25 for laptop shutdown)

Notes for the next session (Claude or human). README.md is the user-facing doc;
this file is "where we are and what to do next". Delete it once finished.

## The task (user's words, condensed)

Small flash/installation tool for the Raspberry Pi 4 based on
https://archlinuxarm.org/platforms/armv8/broadcom/raspberry-pi-4 (old, partly wrong:
check every step). Go + charmbracelet/huh forms, exactly 4 questions:
1. build aarch64 | armv7h (default aarch64)
2. device (list what is there)
3. kernel "latest" | "7.2.7" (default 7.2.7, the PKGBUILD's version) — download it
4. swap: file | partition | none — max 1 GiB. /boot ALWAYS 1 GiB.
Everything else automated. The wiki's `sed -i 's/mmcblk0/mmcblk1/g' root/etc/fstab`
"fails every time": test in QEMU raspi4b before delivering.
Work rule from the user: never wait for them — when a decision is needed, commit,
branch, decide, discuss at the end. Self-paced /loop until completion.

## Git state

- `main` @ ee8ac91: baseline (forms, partitioning, rootfs, UUID fstab, swap) with the
  kernel step as a stub.
- `decision/kernel-rt-pkg-vs-alarm-latest` (current): everything else. Uncommitted work
  at stop time was committed as the last commit on this branch.
- Untracked on purpose: `linux-rt-arm-7.2.8-1-aarch64.pkg.tar.xz` (user's build, 68 MB,
  gitignored) and `rt-qa.sh` (user's file, not mine).

## Decision taken alone (to discuss with the user)

"7.2.7" = install the local `linux-rt-arm` PREEMPT_RT package (from the user's
PKGBUILD; not in any repo) via `pacman -U` in an arch-chroot; picks 7.2.7 if present,
else newest local one with a WARNING (only 7.2.8-1 exists locally). "latest" =
`pacman -Syu` from the ALARM repos (today: linux-aarch64 7.2.8 / linux-rpi 6.18.53).
armv7 + 7.2.7 -> falls back to latest (RT package is aarch64-only).
Why: the PKGBUILD itself is nowhere on disk (GitHub repo KM-mostoslavski/linux-rt-arm
has only LICENSE/README), so "download the 7.2.7 kernel" could not mean building it;
ALARM's linux-aarch64 == kernel.org latest stable (7.2.8) today.
Alternative reading to offer: download kernel.org 7.2.7/latest source and build the
PKGBUILD (needs the PKGBUILD pushed; ~30 min cross-build; out of a flasher's scope?).

Minor decision: a final "Erase /dev/X?" confirm after the 4 questions (`--yes` skips).

## Status

Verified (QEMU raspi4b, full matrix at 15:xx, logs in
/var/cache/rpi4-flash/e2e-logs-2026-09-29/):
- PASS aarch64 / 7.2.7 (linux-rt-arm 7.2.8-1 PREEMPT_RT) / swap partition — flash 4m09s
- PASS aarch64 / latest (linux-aarch64 7.2.8) / swap file — flash 8m26s
- armv7 / latest / none: FAILED in the matrix only because the emulated 32-bit boot
  is slow: /boot's device appears after ~2.5 min > systemd's 90 s timeout. A manual
  boot with `systemd.default_device_timeout_sec=600` mounted /boot and reached the login
  prompt. That parameter is now in test/qemu_boot.py (last commit) but the matrix has
  NOT been rerun with it.
- Wiki control: same image with the wiki's `/dev/mmcblk1p1 /boot` -> "Timed out waiting
  for device /dev/mmcblk1p1" -> Emergency Mode. Card is mmcblk0 under mainline aarch64,
  mmcblk1 under downstream armv7 linux-rpi (same QEMU): names are not stable -> UUIDs.
- Interactive huh forms checked in tmux (4 forms + confirm; system NVMe hidden, only
  /dev/sda offered; Ctrl+C -> "aborted, nothing was written", exit 130). NEVER confirm
  there: /dev/sda is the user's real USB card reader.
- go test ./... passes (fstab, cmdline, boot.txt, config.txt rewriting).

## Update 2026-09-30

Review done and committed (ee72950): tarball GPG signature verified with gpgv against the
embedded ALARM build key; SIGINT/SIGTERM now end through the unmount path (tested: SIGINT
during extraction, SIGTERM during `pacman -U` in the chroot -> exit 130, nothing mounted,
no temp dir, no stray process); cleanup uses `umount --recursive` and `os.Remove`;
preflight rejects non-512-byte sectors and LUKS/LVM-stacked partitions; the guest checks
also assert /boot = 1 GiB and swap <= 1 GiB. The matrix on this final code was started the
same morning; CLAUDE.md "Remaining work" is the current to-do list (it supersedes the
"Next steps" below where they differ).

## Next steps, in order

1. Rerun the matrix (expect 3x PASS, ~1 h):
   `cd rpi4-flash && go build -o rpi4-flash . && cd .. &&
    sudo rpi4-flash/test/matrix.sh <scratch>/e2e --cache-dir /var/cache/rpi4-flash`
   (tarballs are cached there, md5-verified; /tmp is tmpfs and was wiped).
   Run it with run_in_background; wait with an until-loop on the output file.
2. Optional: an adversarial review of the Go code (flash.go cleanup/unmount paths,
   device validation) — nothing known broken.
3. Final report to the user (they asked to discuss decisions at the end):
   - the kernel-choice decision above + the erase confirm;
   - wiki flaws + the extra bugs found (see README "Problems not in the wiki");
   - findings for their PKGBUILD: linux-rt-arm ships no
     usr/lib/modules/<ver>/pkgbase (Arch kernels normally do); its Image is ~50 MB
     (U-Boot default addresses can't hold it -> fixed in boot.txt here);
     8250_bcm2835aux (ttyS1 console of ALARM's boot.txt) is a module, so no early serial
     output on the Pi's default console; package is 7.2.8 while the user said 7.2.7.
   - what is NOT verified: a real Pi 4 boot (VideoCore firmware can't be emulated),
     incl. the armv7 config.txt change (arm_64bit=0 + kernel=kernel7.img, from the
     official config.txt docs).
   - cleanup offer: merge decision branch into main if they agree.

## Traps hit today (don't rediscover them)

- zsh aliases: `cat` -> bat, `tmux` -> herdr: use /usr/bin/cat, /usr/bin/tmux.
- zsh doesn't word-split `$var` in for-loops: use bash scripts (test/matrix.sh).
- `pkill -f <pattern>` kills the calling shell (pattern is in its own cmdline):
  use `pgrep -x qemu-system-aar`.
- Files created by `sudo e2e.sh` are root-owned: `sudo chown -R $USER <dir>`.
- QEMU raspi4b gaps and their workarounds are documented in test/qemu_dtb.sh and
  test/qemu_boot.py docstrings (AON/HDMI 0x7ef block aborts, PCIe enabled in the
  card's DTB, SD card on SDHCI 7e300000, bluetooth serdev on PL011, mini-UART console
  starving SDHCI, lost UART input for long lines, missing serial getty).
- pacman under qemu-user needs --disable-sandbox (Landlock).
- mkinitcpio in arch-chroot autodetects the HOST (host /sys): rebuilt with
  -S autodetect,kms.
