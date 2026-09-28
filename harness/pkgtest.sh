#!/bin/bash
# Real test: fresh overlay of base.qcow2, UEFI (edk2) + systemd-boot from the
# guest ESP, `pacman -U` the package inside the guest (mkinitcpio regenerates
# the initramfs via pacman hook), power off, boot again from disk, verify
# PREEMPT_RT and run qa/rt-qa.sh. Every boot has a hard timeout.
#
# usage: harness/pkgtest.sh <linux-rt-arm-*.pkg.tar.zst> [more pkgs...]
set -euo pipefail
source "$(dirname "$0")/common.sh"
(( $# )) || { echo "usage: $0 pkg..."; exit 2; }

run=$RUNS/pkgtest-$(ts); mkdir -p "$run/xfer"
cp "$@" "$REPO/qa/rt-qa.sh" "$run/xfer/"
qemu-img create -q -f qcow2 -b "$BASE" -F qcow2 "$run/run.qcow2"
cp "$EFI_VARS" "$run/vars.fd"
ln -sfn "$(basename "$run")" "$RUNS/pkgtest-latest"

boot() {  # boot <name>: start VM in background, console -> $run/console-<name>.log
  timeout -k 10 "$BOOT_TIMEOUT" \
    qemu-system-aarch64 -machine virt -cpu max,pauth-impdef=on -smp 8 -m 4G \
      -nographic \
      -drive if=pflash,format=raw,readonly=on,file="$EFI_CODE" \
      -drive if=pflash,format=raw,file="$run/vars.fd" \
      -drive if=virtio,file="$run/run.qcow2",format=qcow2 \
      -netdev user,id=n0,hostfwd=tcp::$SSH_PORT-:22 -device virtio-net-pci,netdev=n0 \
      -virtfs local,path="$run/xfer",mount_tag=$XFER_TAG,security_model=none,id=xfer \
      -serial "file:$run/console-$1.log" -monitor none -display none &
  QPID=$!
}
wait_marker() {  # wait_marker <name>
  local deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
  while kill -0 $QPID 2>/dev/null && (( $(date +%s) < deadline )); do
    grep -q HARNESS-BOOT-OK "$run/console-$1.log" 2>/dev/null && return 0
    sleep 3
  done
  return 1
}
shutdown_vm() {
  gssh 'systemctl poweroff' 2>/dev/null || true
  for _ in $(seq 60); do kill -0 $QPID 2>/dev/null || break; sleep 2; done
  kill $QPID 2>/dev/null || true; wait $QPID 2>/dev/null || true
}
die() { echo "FAIL: $*" | tee -a "$run/summary.txt"; kill $QPID 2>/dev/null || true; exit 1; }

# --- boot 1: stock base kernel, install package --------------------------------
log "boot 1 (base kernel): $run"
boot 1
wait_marker 1 || die "boot 1: no marker within ${BOOT_TIMEOUT}s"
wait_ssh 60 || die "boot 1: no ssh"
grep -m1 HARNESS-BOOT-OK "$run/console-1.log" | tee -a "$run/summary.txt"
pkgs=$(cd "$run/xfer" && ls *.pkg.tar.* | sed 's|^|/mnt/xfer/|' | tr '\n' ' ')
log "installing: $pkgs"
# linux-rt-arm conflicts with linux-aarch64: answer the removal prompt
gssh "ls /mnt/xfer >/dev/null && yes | pacman -U $pkgs" > "$run/pacman.log" 2>&1 \
  || die "pacman -U failed (see pacman.log)"
gssh 'ls -l /boot /boot/loader/entries; cat /boot/loader/entries/*.conf; pacman -Q | grep -E "^linux"' \
  >> "$run/pacman.log" 2>&1
grep -q 'initramfs-linux.img' "$run/pacman.log" || die "initramfs not regenerated (see pacman.log)"
shutdown_vm

# --- boot 2: from disk with the packaged kernel -----------------------------------
log "boot 2 (packaged kernel)"
boot 2
wait_marker 2 || die "boot 2: no marker within ${BOOT_TIMEOUT}s"
marker=$(grep -m1 HARNESS-BOOT-OK "$run/console-2.log")
echo "$marker" | tee -a "$run/summary.txt"
grep -q 'uname-v=.*PREEMPT_RT' <<<"$marker" || die "uname -v lacks PREEMPT_RT"
grep -q 'uname-r=.*-rt-arm' <<<"$marker" || die "not running the linux-rt-arm kernel"
wait_ssh 60 || die "boot 2: no ssh"
log "running RT QA"
set +e
gssh 'bash /mnt/xfer/rt-qa.sh' > "$run/qa.log" 2>&1; qa=$?
set -e
cat "$run/qa.log"
gssh 'dmesg' > "$run/dmesg-2.log" 2>&1 || true
shutdown_vm
rm -f "$run/run.qcow2" "$run/vars.fd"
(( qa == 0 )) || die "RT QA failed (qa.log)"
echo "PASS: package install + reboot + RT QA" | tee -a "$run/summary.txt"
