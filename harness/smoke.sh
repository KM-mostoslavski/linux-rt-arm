#!/bin/bash
# Fast inner loop: direct-boot a freshly built Image (no firmware, no
# packaging) on a throwaway overlay of base.qcow2. No initrd: virtio-blk and
# ext4 are built in. Modules on the base rootfs don't match this kernel, which
# is fine for a smoke test (boot to multi-user + marker).
#
# usage: harness/smoke.sh [path/to/Image]   -> exit 0 on PASS
set -euo pipefail
source "$(dirname "$0")/common.sh"

img=${1:-$WORK/build/linux-rt-arm/src/linux-7.2.8/arch/arm64/boot/Image}
[[ -f $img ]] || { echo "no Image at $img"; exit 2; }
run=$RUNS/smoke-$(ts); mkdir -p "$run"
qemu-img create -q -f qcow2 -b "$BASE" -F qcow2 "$run/run.qcow2"
cp "$img" "$run/Image"
log "smoke boot: $run (timeout ${BOOT_TIMEOUT}s)"

timeout -k 10 "$BOOT_TIMEOUT" \
  qemu-system-aarch64 -machine virt -cpu max,pauth-impdef=on -smp 8 -m 4G \
    -nographic -no-reboot \
    -kernel "$run/Image" -append "$KCMDLINE_BASE panic=10" \
    -drive if=virtio,file="$run/run.qcow2",format=qcow2 \
    -netdev user,id=n0 -device virtio-net-pci,netdev=n0 \
    -serial "file:$run/console.log" -monitor none -display none \
  &
qpid=$!
verdict=FAIL
deadline=$(( $(date +%s) + BOOT_TIMEOUT ))
while kill -0 $qpid 2>/dev/null && (( $(date +%s) < deadline )); do
  if grep -q 'HARNESS-BOOT-OK' "$run/console.log" 2>/dev/null; then verdict=PASS; break; fi
  if grep -qE 'Kernel panic|end Kernel panic' "$run/console.log" 2>/dev/null; then break; fi
  sleep 3
done
kill $qpid 2>/dev/null; wait $qpid 2>/dev/null || true

{
  echo "verdict: $verdict"
  grep -m1 'Linux version' "$run/console.log" || echo "no 'Linux version' line"
  grep -m1 'HARNESS-BOOT-OK' "$run/console.log" || true
  grep -m1 -E 'PREEMPT_RT' "$run/console.log" >/dev/null && echo "PREEMPT_RT in banner: yes" || echo "PREEMPT_RT in banner: NO"
  grep -iE 'BUG:|WARNING:|Oops|Kernel panic|Call trace' "$run/console.log" | head -20 || true
} | tee "$run/summary.txt"
rm -f "$run/run.qcow2" "$run/Image"
ln -sfn "$(basename "$run")" "$RUNS/smoke-latest"
[[ $verdict == PASS ]] && grep -q 'PREEMPT_RT in banner: yes' "$run/summary.txt"
