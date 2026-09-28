# Shared settings for the dev/test harness. Sourced, not executed.
# Host-side everything runs unprivileged.
REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$REPO/work
VM=$WORK/vm
RUNS=$WORK/runs
ROOTFS_TARBALL=$VM/ArchLinuxARM-aarch64-latest.tar.gz
BASE=$VM/base.qcow2
SSHKEY=$VM/harness_key
EFI_CODE=/usr/share/edk2/aarch64/QEMU_EFI.fd
EFI_VARS=/usr/share/edk2/aarch64/QEMU_VARS.fd
BOOT_TIMEOUT=${BOOT_TIMEOUT:-300}   # hard timeout per boot attempt (s)
XFER_TAG=xfer                        # 9p mount_tag, also in guest fstab
SSH_PORT=${SSH_PORT:-2222}
KCMDLINE_BASE="root=/dev/vda2 rw console=ttyAMA0 loglevel=6"

ts() { date +%Y%m%d-%H%M%S; }
log() { printf '[%s] %s\n' "$(date +%T)" "$*" >&2; }

# ssh into the running guest
gssh() {
  ssh -i "$SSHKEY" -p "$SSH_PORT" -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
      -o ConnectTimeout=10 -o BatchMode=yes root@localhost "$@"
}

# wait until guest sshd answers (or deadline passes)
wait_ssh() {
  local deadline=$(( $(date +%s) + $1 ))
  while (( $(date +%s) < deadline )); do
    gssh true 2>/dev/null && return 0
    sleep 5
  done
  return 1
}
