# Loop log

Append-only log of the agentic loop (README → "The loop"). One section per
iteration: date, what changed, build/boot/QA result, next step.
Max 10 full iterations per session.

## Iteration 0 — setup (2026-09-28, interactive session)

Environment (verified):
- Host: x86_64, 64 cores, 125 GB RAM, ~900 GB free. `/dev/kvm` present but
  irrelevant (aarch64 guest = TCG). No sudo.
- Installed: aarch64-linux-gnu-gcc, ccache, qemu-system-aarch64/arm,
  qemu-img, qemu-user-static(+binfmt, registered, flags `PF`), edk2-aarch64
  (`/usr/share/edk2/aarch64/QEMU_EFI.fd`), virtiofsd, arch-install-scripts,
  bubblewrap, mtools, expect, socat, pahole, dtc, rust/bindgen/clang, gh
  (authenticated), tmux.
- Latest stable per kernel.org on 2026-09-28: **7.2.8**.

Downloaded (gitignored, under `work/`):
- `work/srcdest/linux-7.2.8.tar.xz` + `.tar.sign` — signature verified
  (Greg Kroah-Hartman). sha256
  `12e8d5a973d1ad7c5a5c69882e4022b131ed715db7003fdcd760ddf8c3e51941`,
  b2sum matches Arch's linux-rt PKGBUILD.
- `work/vm/ArchLinuxARM-aarch64-latest.tar.gz` — md5 OK.
- `work/ref/alarm/core/linux-aarch64/` — ALARM PKGBUILD + config (7.2.8-1,
  config generated on 7.2.7).
- `work/ref/archrt/` — Arch linux-rt 7.2.8.rt3.arch1-1.

Findings:
- Arch's x86_64 `linux-rt` STILL applies an out-of-tree `v7.2.8-rt3` patch
  (the residual RT patch set). Per the statement of work we do NOT use it;
  RT is config-only. Document this difference.
- ALARM config already has `ARCH_SUPPORTS_RT=y`, `EXPERT=y`, `HZ_1000`,
  `IKCONFIG_PROC=y`, `EFI_STUB=y`, `VIRTIO_BLK/PCI=y`, `EXT4=y`,
  `SERIAL_AMBA_PL011_CONSOLE=y`; `CONFIG_PREEMPT=y` + `PREEMPT_DYNAMIC=y`.
  9p/virtiofs are modules.

Proposed plan (not yet implemented; the human saw it and did not object):
- Base on ALARM `linux-aarch64`: aarch64 config, dtbs, ALARM boot layout.
  Drop the chromebook subpackage and ALARM's 5 board patches (Rockchip/RPi5
  DTS, PPS compat) → pure mainline + config. Document what's lost.
- Source: full `linux-7.2.8.tar.xz` + `.sign` (no incremental patch).
- Drop-in replacement for linux-aarch64: same `/boot/Image`,
  `/boot/initramfs-linux.img`, `/boot/dtbs`; conflicts linux-aarch64,
  so existing board boot scripts keep working. `prepare()` asserts
  `PREEMPT_RT=y` and `PREEMPT_DYNAMIC` unset after olddefconfig.
- Dev cross-build: makepkg with a dev makepkg.conf (CARCH=aarch64,
  `ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu-` in env, `-A -d`), PATH
  wrapper dir: `aarch64-linux-gnu-gcc` → ccache masquerade symlink,
  `strip` → aarch64 strip. Headers pkg from cross build has x86 host tools:
  dev-only, fine for boot tests.
- base.qcow2 without root: GPT (sfdisk on a raw file) with FAT ESP
  (mkfs.fat + mcopy) mounted at `/boot` in the guest + ext4 root built via
  `fakeroot` bsdtar extract + `mke2fs -d`; systemd-boot
  (`systemd-bootaa64.efi` from the rootfs) as `EFI/BOOT/BOOTAA64.EFI`,
  static loader entry `/Image` + `/initramfs-linux.img`. Harness ssh key
  in root's authorized_keys. One provisioning boot: pacman-key init,
  base-devel, git, numactl (for rt-tests). Then never boot base again.
- QEMU speed: `-cpu max,pauth-impdef=on` (cheap pointer-auth emulation).

## Iteration 1 — 2026-09-28 (unattended loop)

Changes:
- `PKGBUILD` (linux-rt-arm 7.2.8-1), based on ALARM linux-aarch64: full
  upstream tarball + PGP (keys/pgp), no RT/board patches, chromebook
  subpackage dropped, ships `/boot/Image{,.gz}`, `/boot/dtbs`,
  `initramfs-linux.img` (drop-in for linux-aarch64, conflicts with it).
  `prepare()` hard-fails unless `PREEMPT_RT=y` and `PREEMPT_DYNAMIC` unset.
- `config` = ALARM config + `config.rt-fragment` via `tools/genconfig.sh`
  (olddefconfig on 7.2.8). Fallout recorded in `docs/config-rt-diff.txt`:
  RT (Kconfig `!PREEMPT_RT`) removes legacy xtables (iptables-legacy,
  ebtables/arptables legacy), LEDS_TRIGGER_CPU, IR_GPIO_TX.
- Harness (`harness/`): base image built fully unprivileged (user
  namespace + qemu-user chroot provisioning, mke2fs -d, mtools, sfdisk) —
  faster than provisioning under TCG and never boots the base.
  rt-tests is not packaged in ALARM → built from git in the base image.
- `qa/rt-qa.sh`: in-guest functional RT checks.

Review (subagent, CLAUDE.md as SOW): PASS, no blockers. Applied: ship
Image.gz, provides linux/linux-headers; mkbase: mount /sys, strict
unmount, systemd-boot presence check, shared 9p tag var. Doc items
(regressions list, lost ALARM patches, no fallback initramfs) → docs stage.

Results (logs under `work/logs/`, `work/runs/`):
- Cross build: OK, 572 s cold ccache (`work/logs/cross-build-20260928-145538.log`).
- Smoke boot (direct -kernel): PASS, `#1 SMP PREEMPT_RT`, no splats
  (`work/runs/smoke-20260928-150742`).
- Package test (UEFI + systemd-boot, pacman -U replacing linux-aarch64,
  initramfs regenerated, reboot from disk): PASS
  (`work/runs/pkgtest-20260928-151431`).
- RT QA: ALL PASS with **1 known deviation** — see below. cyclictest ran to
  completion (functional only; TCG numbers are meaningless).

**SOW conflict — needs human decision:** CLAUDE.md requires
`/sys/kernel/realtime` to exist and read 1, and also forbids out-of-tree RT
patches. Mainline 7.2.8 has no `/sys/kernel/realtime`; it is added only by
`sysfs__Add__sys_kernel_realtime_entry.patch` (Clark Williams) in the
out-of-tree queue `patches-7.2-rt5` (verified by downloading the queue and
grepping `kernel/ksysfs.c` in mainline). Decision taken: do NOT patch;
QA reports it as a labelled DEVIATION. The option for the human: carry
that one 12-line patch, or accept the deviation (userspace can test
`uname -v`/`/proc/config.gz` instead).

Next: native-PKGBUILD validation (clean ALARM chroot, qemu-user,
unprivileged) running; then docs and final report.
