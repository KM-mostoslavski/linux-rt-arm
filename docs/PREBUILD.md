# Pre-building linux-rt-arm for the fileserver

This is how to build the release packages of `linux-rt-arm` on the build host
(`KM-R-G-Cluster`) and publish them to the fileserver, so that machines
download a ready kernel instead of compiling it.

The sibling project `linux-rpi-rt` has the same procedure in its own
`docs/PREBUILD.md`. Each project writes only into its own `out/` folder:

| Project | Repo on the build host | Output folder |
|---|---|---|
| `linux-rt-arm` (this) | `/home/aiadmin/Projects/PKGBUILD-linux-rt` | `PKGBUILD-linux-rt/out/<pkgver>-<pkgrel>/` |
| `linux-rpi-rt` | `/home/aiadmin/Projects/PKGBUILD-linux-rpi-rt` | `PKGBUILD-linux-rpi-rt/out/<pkgver>-<pkgrel>/` |

## TL;DR

```sh
cd /home/aiadmin/Projects/PKGBUILD-linux-rt
git switch wip/loop                 # the branch that holds the finished package
tmux new-session -d -s prebuild-rt 'harness/prebuild.sh; exec bash'
tmux attach -t prebuild-rt          # watch it (Ctrl-b d to detach); ~90 min
ls out/latest/                      # the packages, SHA256SUMS, BUILDINFO, build.log
```

Then upload `out/<version>/` (see [Uploading](#uploading-to-the-fileserver)).

## How the build works

The packages are built **natively for aarch64**, the same way an Arch Linux
ARM user building the PKGBUILD on their board would:

1. `git archive` of the chosen commit, so only committed files are built.
2. A fresh Arch Linux ARM aarch64 rootfs is unpacked into `work/native/root`.
3. That rootfs runs as a chroot on the x86_64 host. Each aarch64 binary in it
   runs through `qemu-aarch64-static` (binfmt_misc). The chroot lives in an
   unprivileged user namespace (`unshare --map-auto --map-root-user`), so the
   host needs **no root and no sudo**.
4. Inside: `pacman -Syu`, `base-devel`, the `makedepends` from `.SRCINFO`, then
   `makepkg` as a normal user, unmodified, with source checksum and PGP
   signature verification on.
5. The packages land in `work/native/pkgdest/`. `harness/prebuild.sh` then
   checks them and publishes them to `out/<pkgver>-<pkgrel>/`.

`harness/prebuild.sh` drives the whole thing. Steps 1–4 are
`harness/native-validate.sh`, the script that produced the validated 7.2.8-1
build. `prebuild.sh` adds input preparation, checks and the output folder.

### Why not the cross-compiler?

`harness/cross-build.sh` is about 9× faster (10 min vs. 90 min), but it is a
**dev tool only. Never upload its output.** Its `-headers` package contains
x86_64 host tools under `scripts/`, so building out-of-tree modules (DKMS) on
a board fails. Its kernel is also built with a different `makepkg.conf` than
users get. Only the native build gives packages identical to what the
PKGBUILD produces on a board.

## Requirements on the build host

All of these are present on `KM-R-G-Cluster` and were checked on 2026-10-12:

| Need | Check |
|---|---|
| `qemu-user-static` with aarch64 binfmt registered | `cat /proc/sys/fs/binfmt_misc/qemu-aarch64` shows `enabled` and flags `PF` |
| subordinate UID/GID range for the build user | `grep "^$USER:" /etc/subuid /etc/subgid` (here `165536:65536`) |
| `unshare` (util-linux), `bsdtar` (libarchive), `git`, `curl`, `makepkg` (pacman) | `command -v unshare bsdtar git curl makepkg` |
| Disk | about 10 GB in `work/native` per build, ~100 MB per release in `out/` |
| Network | ALARM mirrors (`pacman -Syu` in the chroot) and `cdn.kernel.org` |

With flags `PF` (no `F`), the interpreter is not loaded in advance, so the
build script bind-mounts `/usr/bin/qemu-aarch64-static` into the chroot.
Don't remove that line.

## Step by step

### 1. Pick the commit to build

The finished package is on the branch **`wip/loop`**. `main` does not contain
it yet. Build from the branch, or from `main` once `wip/loop` is merged:

```sh
cd /home/aiadmin/Projects/PKGBUILD-linux-rt
git fetch && git switch wip/loop && git pull --ff-only
grep -E '^pkg(ver|rel)=' PKGBUILD       # what you are about to build
```

`prebuild.sh` builds **the commit**, never the working tree. Uncommitted edits
to the PKGBUILD, the config or the install scriptlet are ignored, with a
warning. Commit first. You can also name any ref without checking it out:
`harness/prebuild.sh origin/main`, `harness/prebuild.sh v7.2.8-1`.

### 2. Run it

```sh
tmux new-session -d -s prebuild-rt 'harness/prebuild.sh; exec bash'
```

Use tmux (or `nohup`): the build takes **about 90 minutes** (the reference
build ran `makepkg` for 88 min) and must survive an ssh logout. The full log
goes to `work/logs/native-<timestamp>.log`, and `work/logs/native-latest.log`
always points at the newest one:

```sh
tail -f work/logs/native-latest.log
```

Options (environment variables):

| Variable | Default | Meaning |
|---|---|---|
| `JOBS` | `nproc` (64) | `make -j` inside the chroot. Lower it when sharing the host, e.g. `JOBS=32`. |
| `FORCE` | `0` | `1` replaces an existing `out/<version>/`. See [Versions are immutable](#versions-are-immutable). |

### 3. What the script does

1. Reads `pkgbase`, `pkgver`, `pkgrel` and the package names from `.SRCINFO`
   of that commit, and stops if `.SRCINFO` disagrees with the `PKGBUILD`.
   A stale `.SRCINFO` means wrong makedepends in the chroot.
2. Stops **before** building if `out/<pkgver>-<pkgrel>/` already exists.
3. Takes a lock (`work/prebuild.lock`) so two prebuilds of this repo cannot
   run at once (they would both wipe `work/native`).
4. Downloads the ALARM rootfs to `work/vm/ArchLinuxARM-aarch64-latest.tar.gz`
   if it is missing, and checks its md5.
5. Downloads missing sources into `work/srcdest/` (`linux-<ver>.tar.xz` and
   `.sign` from kernel.org) and verifies them against the PKGBUILD's
   `sha256sums`. The PGP signature is verified again inside the chroot against
   the keys in `keys/pgp/` (Linus Torvalds, Greg Kroah-Hartman).
6. Wipes `work/native` and runs the native build (above).
7. Checks the result: every package in `.SRCINFO` exists with the right
   version, and the kernel config in the headers package has
   `CONFIG_PREEMPT_RT=y` and does not have `CONFIG_PREEMPT_DYNAMIC=y`.
8. Writes everything to `out/.<version>.tmp/`, renames it to
   `out/<version>/` in one step, and points `out/latest` at it. A failed or
   interrupted run never leaves a half-filled release folder.

### 4. The output folder

```
out/
├── 7.2.8-1/
│   ├── linux-rt-arm-7.2.8-1-aarch64.pkg.tar.xz           kernel (~65 MB)
│   ├── linux-rt-arm-headers-7.2.8-1-aarch64.pkg.tar.xz   headers (~27 MB)
│   ├── SHA256SUMS                                        sha256 of both packages
│   ├── BUILDINFO                                         provenance, see below
│   └── build.log                                         full native build log
└── latest -> 7.2.8-1
```

`BUILDINFO` records what to know when someone asks "where did this kernel come
from?":

```
pkgbase        linux-rt-arm
version        7.2.8-1
kernel_release 7.2.8-1-rt-arm
packages       linux-rt-arm-7.2.8-1-aarch64.pkg.tar.xz linux-rt-arm-headers-7.2.8-1-aarch64.pkg.tar.xz
git_ref        HEAD
git_commit     <full sha>
git_describe   <tag or short sha>
built_utc      2026-10-12T12:02:09Z
build_host     KM-R-G-Cluster (x86_64, qemu-user aarch64 chroot)
chroot_rootfs  ArchLinuxARM-aarch64-latest.tar.gz (md5 …), pacman -Syu at build time
chroot_tools   binutils-… gcc-… glibc-…
makepkg_time   5315s (total …s)
preempt        CONFIG_PREEMPT_RT=y, CONFIG_PREEMPT_DYNAMIC not set (checked)
signed         no (makepkg --nosign)
```

`out/` is gitignored. The packages are not committed.

## Building both projects

The two repos are independent: separate `work/` directories, separate locks,
separate `out/` folders. You can run them **one after the other** (simplest,
each gets the whole machine) or **at the same time**:

```sh
# one after the other, about 2 h 20 min total
tmux new-session -d -s prebuild \
  'cd /home/aiadmin/Projects/PKGBUILD-linux-rt && harness/prebuild.sh;
   cd /home/aiadmin/Projects/PKGBUILD-linux-rpi-rt && harness/prebuild.sh;
   exec bash'

# at the same time: split the 64 cores
tmux new-session -d -s prebuild-rt  'cd /home/aiadmin/Projects/PKGBUILD-linux-rt     && JOBS=32 harness/prebuild.sh; exec bash'
tmux new-session -d -s prebuild-rpi 'cd /home/aiadmin/Projects/PKGBUILD-linux-rpi-rt && JOBS=32 harness/prebuild.sh; exec bash'
```

Each build uses about 10 GB of disk in its `work/native`. The host has 125 GB
of RAM, so memory is not the limit.

## Uploading to the fileserver

Upload the **whole version folder**. The packages and `SHA256SUMS` are what
clients need. `BUILDINFO` and `build.log` are cheap and make the release
auditable. Suggested layout on the server, one directory per package and
version:

```
<fileserver>/linux-rt-arm/7.2.8-1/{*.pkg.tar.xz,SHA256SUMS,BUILDINFO,build.log}
<fileserver>/linux-rpi-rt/6.18.53-1/{…}
```

Example with rsync over ssh (replace the host and path with the real ones):

```sh
v=$(readlink out/latest)
rsync -av --chmod=F644,D755 "out/$v/" "fileserver:/srv/pkgs/linux-rt-arm/$v/"
ssh fileserver "cd /srv/pkgs/linux-rt-arm/$v && sha256sum -c SHA256SUMS"
```

Always run the `sha256sum -c` on the server side: it proves the upload is
complete.

### On the client side

A board can install straight from the fileserver:

```sh
pacman -U https://<fileserver>/linux-rt-arm/7.2.8-1/linux-rt-arm-7.2.8-1-aarch64.pkg.tar.xz
```

A PKGBUILD that downloads the pre-built package instead of compiling it should
pin the hashes from `SHA256SUMS` in its `sha256sums=()`. Because of that pin,
a file once published must never change on the server (next section).

## Versions are immutable

Once `out/<version>/` is uploaded, treat that version as frozen. A rebuild of
the same commit is **not** bit-identical: the chroot runs `pacman -Syu`, so the
compiler can differ, and timestamps differ. Its checksums would no longer
match anything that pinned them.

- New build of the same kernel (packaging fix, toolchain refresh): bump
  `pkgrel` in the PKGBUILD, regenerate `.SRCINFO`, commit, prebuild.
- `FORCE=1` is only for a version that was never uploaded (for example, a
  build you want to redo before releasing it).

## Releasing a new kernel version

For a new 7.x stable point release (e.g. 7.2.9):

1. In `PKGBUILD`: `pkgver=7.2.9`, `_srcname=linux-7.2.9`, `pkgrel=1`, and
   update the first entry of `sha256sums` (the `.tar.xz`). Get it with
   `makepkg -g -A` or from `https://cdn.kernel.org/pub/linux/kernel/v7.x/sha256sums.asc`.
2. For a new ALARM base config, regenerate `config` with
   `tools/genconfig.sh <linux-src-dir> <alarm-linux-aarch64-config>` and
   review the diff (README, "Maintaining / updating" and "Config
   provenance").
3. `makepkg --printsrcinfo > .SRCINFO`, commit.
4. Recommended before the long native build: a quick cross-build and QEMU
   test (`harness/cross-build.sh`, `harness/pkgtest.sh`), as in `LOOP_LOG.md`.
5. `harness/prebuild.sh`, then upload.

Old tarballs in `work/srcdest/` can be deleted once nothing builds them.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `out/<version> already exists` | That version was already built. Bump `pkgrel`, or `FORCE=1` if it was never uploaded. |
| `.SRCINFO (…) does not match PKGBUILD (…)` | `makepkg --printsrcinfo > .SRCINFO`, commit, retry. |
| `another prebuild is running` | A prebuild of this repo is still running (`tmux ls`). If it died, the lock is released automatically. |
| `warning: uncommitted changes are NOT built` | Expected if you have local edits. Commit them if they belong in the build. |
| `source download/verification failed` | kernel.org unreachable, or a `sha256sums` entry is wrong after a version bump. Delete a partially downloaded file in `work/srcdest/` and retry. |
| `pacman` fails in the chroot with `Operation too slow` or a 404 | ALARM mirror trouble. Retry later. A very old `ArchLinuxARM-aarch64-latest.tar.gz` makes `-Syu` large: delete it and the script downloads a fresh one. |
| `exec format error` in the chroot | binfmt for aarch64 is not registered (e.g. after a host change). Check `/proc/sys/fs/binfmt_misc/qemu-aarch64`, `systemctl restart systemd-binfmt` (needs an admin). |
| `unshare: … map` / `newuidmap` errors | The build user has no `/etc/subuid` / `/etc/subgid` range. An admin must add one. |
| PGP `unknown public key` | The kernel was signed by a key not in `keys/pgp/`. Add the signer's key (`<fingerprint>.asc`) and the fingerprint to `validpgpkeys`. |
| `kernel config: CONFIG_PREEMPT_RT is not y` | The config lost RT (Kconfig dependency change in a new kernel). Fix the `config`, see `LOOP_LOG.md`. Nothing was published. |
| Can't delete `work/native` (`Permission denied`) | The chroot files belong to sub-UIDs. Use `unshare --map-auto --map-root-user rm -rf work/native`. |
| Any other build failure | Read the end of `work/logs/native-latest.log`. The chroot stays in `work/native/root` for inspection until the next run. |
