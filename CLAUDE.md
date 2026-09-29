# CLAUDE.md — new-PKGBUILD-linux-rt-rpi

## Current work: `rpi4-flash/` (paused 2026-09-29 16:25, resume next session)

A Go + charmbracelet/huh tool that flashes Arch Linux ARM onto an SD card or USB disk
for a Raspberry Pi 4. It asks exactly 4 questions (build, device, kernel, swap) and
automates the rest. It is based on the ALARM Pi 4 wiki, with that page's wrong steps fixed.
The user asked for the finished tool explicitly, so build it; the tutor mode in the global
CLAUDE.md does not apply to delivering it.

- Branch: `decision/kernel-rt-pkg-vs-alarm-latest` holds all the work. `main` is only the
  baseline, with the kernel step left as a stub.
- `rpi4-flash/HANDOFF.md` has the full state: what's verified, the decision taken alone,
  and the traps already hit. Read it before doing anything; don't re-derive it.
- `rpi4-flash/README.md` is the user-facing documentation.

## Tomorrow, in order

1. **Rerun the e2e matrix** (≈1 h). The armv7 test fix (`systemd.default_device_timeout_sec=600`
   in `test/qemu_boot.py`) has not been through a full run yet. Expected result: 3× PASS.
   ```sh
   cd rpi4-flash && go build -o rpi4-flash . && go test ./... && cd ..
   sudo rpi4-flash/test/matrix.sh <scratchpad>/e2e --cache-dir /var/cache/rpi4-flash
   ```
   - Start it with `run_in_background`. Wait with an until-loop on the output file; never
     use foreground sleeps.
   - Per-config output goes to `<scratchpad>/e2e/<arch>-<kernel>-<swap>.out`, plus
     `.serial.log` and `.flash.log`. The files are root-owned: `sudo chown -R $USER`
     before reading or editing them.
   - Yesterday's logs for comparison: `/var/cache/rpi4-flash/e2e-logs-2026-09-29/`.
2. **If armv7 still fails**, read its `.serial.log` (the stage 2 part) before changing
   anything. Known causes and their fixes are in the docstrings of `test/qemu_boot.py` and
   `test/qemu_dtb.sh`. If it's a real problem, fix it; if it's a new decision, put it on its
   own branch (see "How the user wants this run" below).
3. **Optional review** of `flash.go` for error, cleanup and unmount paths (`os.RemoveAll`
   must never run while something is still mounted) and of device validation. Nothing is
   known broken.
4. **Final report to the user.** They want to discuss the decisions only at the end.
   Cover, briefly:
   - the kernel-choice decision ("7.2.7" = local linux-rt-arm package via `pacman -U`,
     "latest" = `pacman -Syu`), the alternative reading, and the extra erase confirm;
   - the wiki flaws and the bugs found by booting (README, "Problems not in the wiki");
   - findings for their PKGBUILD:
     - no `usr/lib/modules/<ver>/pkgbase` file;
     - ~50 MB `Image`;
     - `8250_bcm2835aux` is a module, so the default `ttyS1` console shows no early output;
     - the package is 7.2.8, not 7.2.7;
   - what is unverified: booting a real Pi 4, and the armv7 `config.txt` change;
   - ask whether to merge the decision branch into `main`. Once that's settled, delete
     `HANDOFF.md` and this section.

## How the user wants this run

- Don't stop to ask. When a decision is needed: commit, create a `decision/<topic>`
  branch, decide, keep going, and report it at the end.
- Commits: Conventional Commits, ending with the co-author line from the system reminder.
  Don't commit the user's `rt-qa.sh` or the `*.pkg.tar.*` package.
- Never confirm the erase prompt against `/dev/sda`: it's the user's real USB card
  reader. Tests use loop devices only (`--device /dev/loopN --yes`).

## Environment traps

- zsh aliases: `cat` → bat and `tmux` → herdr. Use `/usr/bin/cat` and `/usr/bin/tmux`.
  zsh also doesn't word-split `$var`, so write loops as bash scripts.
- `pkill -f <pattern>` kills the calling shell. Use `pgrep -x qemu-system-aar` / `kill`.
- `/tmp` (and the scratchpad) is a RAM tmpfs that is wiped at shutdown. Persistent cache:
  `/var/cache/rpi4-flash` (md5-verified ALARM tarballs).
- `sudo` works without a password. Host tools are installed: qemu-system-aarch64 (with a
  `raspi4b` machine), qemu-user-static + binfmt, arch-install-scripts, dosfstools,
  uboot-tools, dtc.
