# CLAUDE.md — new-PKGBUILD-linux-rt-rpi

## Current work: `rpi4-flash/`

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

## Status (2026-09-30): built and tested, waiting for the user's decisions

The e2e matrix passed 3/3 on the final code (commit 14707e7 + docs): aarch64/7.2.7/swap
partition, aarch64/latest/swap file, armv7/latest/no swap. Logs:
`/var/cache/rpi4-flash/e2e-logs-2026-09-30/`. The final report was given to the user the
same day. What is left depends on their answers:

- whether the kernel-choice reading is right ("7.2.7" = local linux-rt-arm package via
  `pacman -U`, "latest" = `pacman -Syu`), or whether they want the PKGBUILD built from
  kernel.org sources instead;
- whether to keep the extra erase confirmation and the hiding of system disks;
- whether to merge `decision/kernel-rt-pkg-vs-alarm-latest` into `main`. After the merge,
  delete `rpi4-flash/HANDOFF.md` and this section;
- a boot on a real Pi 4 is the one thing QEMU could not verify (also the armv7
  `config.txt` change). If the user reports a real-hardware failure, start from the serial
  console output; `README.md` lists every change made to the stock boot files.

To rerun the tests (≈1 h, run in the background, wait with an until-loop):
```sh
cd rpi4-flash && go build -o rpi4-flash . && go test ./... && cd ..
sudo rpi4-flash/test/matrix.sh <scratchpad>/e2e --cache-dir /var/cache/rpi4-flash
```
Output files are root-owned (`sudo chown -R $USER` first). To stop a run: kill `matrix.sh`
and `e2e.sh` by pid, then SIGTERM the `rpi4-flash` pid (`pgrep -x rpi4-flash`); it
unmounts by itself.

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
