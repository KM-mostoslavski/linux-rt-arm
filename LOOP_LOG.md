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

### Iteration 1 — completion (2026-09-28 16:58)

- Native validation (`harness/native-validate.sh`): **PASS**. Unmodified
  PKGBUILD from `git archive HEAD`, pristine ALARM rootfs (qemu-user,
  unprivileged user namespace), base-devel + `.SRCINFO` makedepends only,
  `makepkg` as a normal user with dep + PGP checks: built in 5315 s
  (`work/logs/native-20260928-151935.log`). So the makedepends list is
  sufficient.
- Package test with the **natively built** packages (kernel + headers):
  **PASS** (pacman -U, reboot from disk, RT QA all pass, 1 known deviation).
- Docs: `README.md` written (how it works, usage, config provenance,
  RT-removed features, lost ALARM patches, status aarch64/armv7h,
  troubleshooting, maintenance).

## FINAL REPORT — loop stopped after 1 iteration

Status: **all green**, with one item that needs a human decision.

| Check | Result |
|---|---|
| Cross build (dev) | PASS, 572 s |
| Smoke boot (direct -kernel) | PASS, `SMP PREEMPT_RT`, no splats |
| pacman -U + reboot from disk (UEFI/systemd-boot) | PASS (cross and native packages) |
| `uname -v` PREEMPT_RT, config RT=y / DYNAMIC unset | PASS |
| chrt -f 99, SCHED_FIFO/RR semantics, cyclictest, pi_stress | PASS (functional only — TCG, no latency claims) |
| `/sys/kernel/realtime` | **DEVIATION**: not in mainline (see iteration 1) |
| Native makepkg, unmodified PKGBUILD, clean chroot | PASS, 88 min under qemu-user |

Human decisions needed:
1. `/sys/kernel/realtime`: accept the deviation (current state, documented in
   README) or carry the 12-line `sysfs__Add__sys_kernel_realtime_entry.patch`
   (would violate "no out-of-tree RT patches").
2. Review & merge `wip/loop` → `main`.

Not done / out of scope: real-hardware boot and latency benchmarks; armv7h
(postponed, documented); RPi vendor kernel (separate phase).

### Post-report update (2026-09-29, interactive)

- Human **approved** decision #1: no out-of-tree patch, so
  `/sys/kernel/realtime` stays absent and is a documented deviation.
- Human verdict on the deliverable: **insightful, not yet useful**. It is
  proven only in QEMU, and it cannot be used as-is on the boards people
  actually run (see "Raspberry Pi 4" below).

## Findings, troubleshooting and challenges (detailed record)

This is everything non-obvious found during iteration 1, including what
went wrong and how it was fixed. Log and run paths are under the gitignored
`work/`.

### A. Kernel / Kconfig findings

1. **Mainline 7.2 allows `PREEMPT_DYNAMIC` together with `PREEMPT_RT`.**
   `PREEMPT_DYNAMIC` has no `!PREEMPT_RT` dependency and defaults to `y` on
   arm64 (`HAVE_PREEMPT_DYNAMIC_KEY`). Arch's own x86_64 `linux-rt` ships
   `PREEMPT_RT=y` **and** `PREEMPT_DYNAMIC=y`. Turning on RT alone would
   therefore have left dynamic preemption enabled. The fragment disables it
   explicitly, and `prepare()` fails the build if it comes back.
2. **`uname -v` cannot tell RT+DYNAMIC apart from pure RT.**
   `init/Makefile` assigns `preempt-flag-$(CONFIG_PREEMPT_BUILD)`, then
   `$(CONFIG_PREEMPT_DYNAMIC)`, then `$(CONFIG_PREEMPT_RT)`, and the last one
   wins. A dynamic RT kernel still prints `PREEMPT_RT`. The "no dynamic
   preempt" requirement can only be checked through `/proc/config.gz` or
   the absence of `/sys/kernel/debug/sched/preempt`. `qa/rt-qa.sh` does both.
3. **The default preemption model in 7.x is `PREEMPT_LAZY`** on arches with
   `ARCH_HAS_PREEMPT_LAZY` (arm64 has it). ALARM had picked `PREEMPT`. We
   pin `PREEMPT` (full) and explicitly unset `PREEMPT_LAZY`: lazy delays
   preemption of SCHED_NORMAL tasks, which is not the hard-RT model asked
   for. RT tasks are preempted immediately either way.
4. **`/sys/kernel/realtime` is not a mainline interface.** It comes only from
   `sysfs__Add__sys_kernel_realtime_entry.patch` (Clark Williams) in the
   out-of-tree queue `patches-7.2-rt5` (20 patches remain out of tree in
   7.2). Verified by downloading the queue and grepping mainline
   `kernel/ksysfs.c`. The SOW's QA list assumed the pre-6.12 patched world.
   Tools such as `tuned` that probe this file will not detect RT on this
   kernel.
5. **RT removes features through `depends on !PREEMPT_RT`** (full list in
   `docs/config-rt-diff.txt`):
   - `NETFILTER_XTABLES_LEGACY`, which takes every `IP_NF_*`/`IP6_NF_*`
     table and target, arptables and legacy ebtables with it
     (`iptables-legacy` breaks, `iptables-nft` is fine),
   - `LEDS_TRIGGER_CPU`,
   - `IR_GPIO_TX`,
   - some debug options.

   `olddefconfig` drops all of these **silently**. That is why
   `tools/genconfig.sh` diffs the fragment and why the diff is committed.
6. **`NO_HZ_FULL` has side effects.** It forces `VIRT_CPU_ACCOUNTING_GEN`
   and `CONTEXT_TRACKING_USER`, which add a small cost on every kernel entry
   and exit even when `nohz_full=` is not used. We accepted this to match
   Arch `linux-rt` and to allow CPU isolation. If hardware benchmarks show
   the cost matters, revisit it.
7. **Host toolchain values leak into the committed `config`.**
   `CC_VERSION_TEXT` (the cross gcc), `RUSTC_VERSION=109801` and
   `PAHOLE_VERSION=132` were detected on the x86_64 host when
   `genconfig.sh` ran. This is harmless because `prepare()` re-runs
   `olddefconfig` and `CONFIG_RUST` stays `n`. The native build showed only
   those lines changing. It is noise in diffs, though, so regenerate the
   config natively on aarch64 if you want a clean diff.
8. **The baseline was confirmed from ALARM's own kernel.** The stock ALARM
   kernel in the test VM reports `7.2.8-1-aarch64-ARCH ... SMP
   PREEMPT_DYNAMIC`. That comes from ALARM's `LOCALVERSION="-ARCH"`, which
   we cleared so our release string reads `7.2.8-1-rt-arm`.

### B. Packaging findings

1. **mkinitcpio triggering.** ALARM does not use Arch's `vmlinuz` +
   `pkgbase` convention. It ships a dummy `/usr/lib/initcpio/<kver>` so that
   `90-mkinitcpio-install.hook` fires and runs the presets. I first added an
   Arch-style `/usr/lib/modules/<kver>/pkgbase` file, then removed it: the
   upstream hook script's `generate_presets()` creates presets from a
   template for every modules dir that has a `pkgbase` file, which would
   conflict with ALARM's layout, where there is no vmlinuz in the modules
   dir. Verified in the guest: `initramfs-linux.img` is regenerated for
   `-k 7.2.8-1-rt-arm`.
2. **The pacman conflict prompt defaults to N.** `pacman -U --noconfirm`
   therefore *aborts* when replacing `linux-aarch64`. Scripts must pipe
   `yes |` or remove the old kernel first.
3. **Dropping `Image.gz` would have broken boards.** The first PKGBUILD
   shipped only `/boot/Image`. The reviewer caught that boot scripts loading
   `Image.gz` would break after replacing `linux-aarch64`, so we now ship
   both. provides/conflicts were also aligned with `linux-aarch64`.
4. **The makedepends list is minimal and proven:** `bc kmod libelf openssl
   perl python tar xz` on top of base-devel. It was validated by the clean
   chroot build (no hidden dependency on git/dtc/pahole/cpio: the in-tree dtc
   uses base-devel's flex/bison, `DEBUG_INFO_NONE` means no pahole,
   `IKHEADERS=n` means no cpio).
5. **A cross-built `-headers` package is unusable on target.** Its
   `scripts/` are x86_64 binaries. It is dev-only; only natively built
   headers are real. The native headers package installed fine. Building an
   external module against it was **not** tested.

### C. QEMU / harness challenges

1. **TCG only.** The host is x86_64, so `/dev/kvm` does not help an aarch64
   guest. Every boot is emulated.
   - Package test boots took about 55 s each to the marker.
   - `pacman -U` plus mkinitcpio took about 2 min in the guest.
   - The whole package test takes about 4–5 min.
2. **Building `base.qcow2` without root and without booting it.** Instead of
   a slow provisioning boot under TCG, the rootfs is provisioned in a
   qemu-user chroot inside `unshare --map-auto --map-root-user`, which uses
   /etc/subuid, so ownership is real inside the namespace. The disk is then
   assembled with `mke2fs -d`, `mkfs.fat` + `mcopy` and `sfdisk` on a raw
   file, then `qemu-img convert`. Since the base is never booted, the SOW
   rule "never boot the base directly" holds trivially. The image is also
   made read-only (`chmod a-w`). Build time was about 15 min, mostly
   `pacman -Syu` under qemu-user.
3. **binfmt is registered with flags `PF`, not `F`.** CLAUDE.md said `F`, but
   iteration 0 recorded `PF`. Without `F` the interpreter is resolved inside
   the chroot, so `/usr/bin/qemu-aarch64-static` has to be bind-mounted into
   every chroot. Both mkbase and native-validate do this.
4. **pacman inside the chroot** needs `CheckSpace` disabled (mount detection
   fails in a chroot) and `--disable-sandbox` (pacman 7's landlock/alpm
   download-user sandbox under qemu-user in a user namespace).
5. **mkinitcpio inside a chroot autodetects the HOST.** During provisioning
   `pacman -Syu` upgraded linux-aarch64, and mkinitcpio `autodetect` scanned
   the x86 host: `ERROR: binary not found: fsck.xfs` because the host root
   is xfs. The base initramfs still boots only because `VIRTIO_BLK` and
   `EXT4` are built in. `/sys` was not even bind-mounted in the first
   version (a review finding, now fixed). The same errors show in the native
   validation log and are harmless there.
6. **Unmounting before `mke2fs -d`.** `umount -R dev` failed on
   `dev/hugepages`, and the first version ignored the failure with `|| true`.
   If `/dev` or `/proc` had stayed mounted, `mke2fs -d` would have walked
   live pseudo-filesystems into the image. It is now unmounted strictly,
   with a lazy fallback, and the script asserts that nothing is still
   mounted.
7. **Smoke boot without an initrd.** Direct `-kernel` boots the fresh Image
   against the base rootfs, whose modules belong to a different kernel.
   This works only because virtio/ext4 are built in. 9p (a module) is
   unavailable in smoke boots, so smoke uses only the serial console.
8. **Pass/fail detection.**
   - A systemd oneshot `harness-marker.service` writes
     `HARNESS-BOOT-OK uname-r=… uname-v=…` to `/dev/ttyAMA0`, and the host
     greps the console file.
   - Bug found: the panic grep matched `panic=10` in the logged kernel
     command line. It could have aborted a good boot early, so it now matches
     only `Kernel panic`.
   - `qemu-system-aarch64: terminating on signal 15 (timeout)` in the logs
     is our own cleanup kill, not a failure.
9. **Every boot has a hard 300 s `timeout -k 10`.** None were hit this
   iteration.
10. **UEFI.** `QEMU_EFI.fd` and `QEMU_VARS.fd` are 64 MiB pflash images, and
    the vars are copied per run. systemd-boot needs no NVRAM entry because it
    sits at the removable path `EFI/BOOT/BOOTAA64.EFI`.
11. **9p share.** `security_model=none` works unprivileged. The guest fstab
    uses `x-systemd.automount,nofail`, so a boot without the share, or
    without a matching 9p module, never hangs.
12. **rt-tests is not packaged in ALARM.** It is built from
    git.kernel.org in the base image (cyclictest, hackbench, pi_stress,
    signaltest).
13. **TCG "latency" numbers.** cyclictest showed max values of 0.9–4.1 ms
    with averages of 100–230 µs. These measure QEMU's TCG scheduling jitter,
    not the kernel. They are recorded only as proof that cyclictest ran and
    must never be quoted as results.
14. **Native validation speed.** The same kernel took 88 min under
    qemu-user with `-j64`, against 9.5 min cross-compiling, a slowdown of
    about 9×. Acceptable once; unusable for iteration. That confirms the
    SOW's strategy.

### D. Process / tooling notes

- **QA script bug:** passing the whole `/proc/config.gz` as a `bash -c`
  argument hit `Argument list too long` and produced a false FAIL. It now
  reads a temp file.
- **Git slip:** `git push -u origin HEAD:wip/loop` from local `main` set
  `main` to track `origin/wip/loop`. Fixed by moving the work to a local
  `wip/loop` branch and resetting local `main` to `c742361`. Remote `main`
  was never touched.
- The adversarial review found real problems (Image.gz, /sys in the chroot,
  unmount safety). It is worth keeping as a mandatory step.
- Twice during the session, auto-mode's permission check returned no verdict
  (a transient error). Read-only file access still worked as a fallback.

### E. Why it is not yet useful: gaps before real use

1. **No hardware has run it.** Everything is QEMU `virt`: no real
   interrupt controllers, clocks, DMA or firmware, and no latency data.
2. **Raspberry Pi 4 (and Pi in general) on standard ALARM is a trap.**
   - ALARM's RPi images use `linux-rpi`, the vendor kernel. It installs
     `/boot/kernel8.img`, flat dtbs, `config.txt` and `cmdline.txt`,
     provides `linux` and conflicts with `linux`.
   - Our package also provides and conflicts with `linux`, so pacman asks
     to remove `linux-rpi`.
     - **N** (the default) aborts the install, so nothing changes.
     - **y** removes `kernel8.img` (and `config.txt` if it was unmodified),
       and the Pi firmware then finds no kernel: **the board does not
       boot**.
   - Our `/boot/Image` and `/boot/dtbs/broadcom/*.dtb` are not what the Pi
     firmware loads unless `config.txt` has `kernel=Image`,
     `device_tree=dtbs/broadcom/bcm2711-rpi-4-b.dtb` and `initramfs
     initramfs-linux.img followkernel`. That recipe is **untested**.
   - Even when it boots, mainline lacks vendor-tree Pi features (the
     overlays ecosystem, parts of camera and display support).
   - It works as a drop-in only where the board already runs
     `linux-aarch64` (U-Boot/UEFI boards).
   - Needed: an install-time guard or clear error when `linux-rpi` is
     present, and either a tested Pi recipe or the planned `linux-rpi` + RT
     phase.
3. **Dropped ALARM board patches:**
   - RK3568 PCIe3 MSI revert: possible PCIe regressions,
   - RPi5 RP1 UART console: no serial console on the header,
   - rk3399-firefly pwm0 fix,
   - PPS x86-only hack.
4. **Features lost to RT:** legacy iptables, the CPU LED trigger and IR GPIO
   TX.
5. **Not tested at all:**
   - external module builds against `-headers` (DKMS),
   - upgrades from one linux-rt-arm version to the next,
   - uninstalling back to linux-aarch64,
   - U-Boot `boot.scr`/extlinux boot paths,
   - the armv7h package (postponed).

### F. Suggested next steps (not started)

1. Hardware bring-up on one target board that runs `linux-aarch64` today:
   boot, run `qa/rt-qa.sh`, then real latency with `cyclictest`/`rtla`
   under load.
2. Raspberry Pi 4:
   - refuse to install over `linux-rpi` (or warn loudly),
   - test the `config.txt` recipe on a real Pi 4,
   - then start the `linux-rpi` + PREEMPT_RT phase.
3. Test DKMS and headers, package upgrades, and uninstalling back to
   `linux-aarch64`.
4. armv7h: toolchain (AUR cross gcc or native-only via qemu-user) and the
   config fallout review.
