# CLAUDE.md — new-PKGBUILD-linux-rt-rpi (linux-rt-arm)

This repository is the `linux-rt-arm` kernel package only (origin:
`KM-mostoslavski/linux-rt-arm`). `README.md` is the user's description of where it is
going: two PKGBUILDs under `PKGBUILDs/` (`linux-rt-arm`, built from source, and
`linux-rt-arm-bin`, a prebuilt kernel), for aarch64 and armv7h.

## State (2026-09-30)

- The flasher (`rpi4-flash`) was moved, with its history, to its own repository:
  `../arch-linux-preempt-rt-rpi-flasher` (origin
  `KM-mostoslavski/arch-linux-preempt-rt-rpi-flasher`). Its own `CLAUDE.md` has its state.
  Nothing of it is left here.
- The PKGBUILD is not on this branch. It is on `origin/wip/loop` (aarch64 only, 7.2.8,
  at the repository root, with `harness/`, `qa/rt-qa.sh`, `keys/pgp`), a history unrelated
  to this branch. GitHub's `main` has only LICENSE + README.
- The current branch, `decision/kernel-rt-pkg-vs-alarm-latest`, now only holds the user's
  README rewrite and the removal of the flasher. `PKGBUILDs/` exists but is empty.
- Next work, in the user's words: update the package to support both aarch64 and armv7h.

## What the flasher expects from this repository

The flasher compiles nothing. It installs the already built package file it finds in this
checkout, which must stay beside the flasher's directory:

- `linux-rt-arm-<ver>-<rel>-aarch64.pkg.tar.*` at the root (today: the user's 7.2.8-1
  build, gitignored) or in `PKGBUILDs/linux-rt-arm/`;
- findings from booting that package on the Pi 4 image: it ships no
  `usr/lib/modules/<ver>/pkgbase`; its `Image` is ~50 MB (U-Boot's default load addresses
  cannot hold it; the flasher moves them); `8250_bcm2835aux` is a module, so there is no
  early output on the Pi's default serial console (ttyS1).

## How the user wants this run

- Don't stop to ask. When a decision is needed: commit, create a `decision/<topic>`
  branch, decide, keep going, and report it at the end.
- Commits: Conventional Commits, ending with the co-author line from the system reminder.
  Don't commit the user's `rt-qa.sh` or the `*.pkg.tar.*` package.
- Do only what was asked. Never start a kernel compile or another job nobody asked for.

## Environment traps

- zsh aliases: `cat` → bat and `tmux` → herdr. Use `/usr/bin/cat` and `/usr/bin/tmux`.
  zsh also doesn't word-split `$var`, so write loops as bash scripts.
- `pkill -f <pattern>` kills the calling shell. Use `pgrep -x qemu-system-aar` / `kill`.
- `/tmp` (and the scratchpad) is a RAM tmpfs that is wiped at shutdown.
- `sudo` works without a password. Host tools are installed: qemu-system-aarch64,
  qemu-user-static + binfmt, arch-install-scripts, aarch64-linux-gnu-gcc (no ccache, no
  armv7 cross toolchain).
