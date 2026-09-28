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
