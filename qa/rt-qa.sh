#!/bin/bash
# In-guest functional RT QA for linux-rt-arm. Run as root on the target.
#
# Proves the kernel is a working PREEMPT_RT kernel. It does NOT measure
# latency: under QEMU/TCG any cyclictest number measures the emulator, not
# the kernel. Real latency benchmarks need real hardware.
#
# exit 0 = all checks passed
set -uo pipefail
PATH=/usr/local/bin:$PATH
fails=0
pass() { printf 'PASS  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*"; fails=$((fails+1)); }
check() { local d=$1; shift; if "$@"; then pass "$d"; else fail "$d"; fi; }

echo "== linux-rt-arm RT QA on $(uname -r) ($(uname -m)) =="
echo "uname -v: $(uname -v)"

# 1. uname -v
check "uname -v contains PREEMPT_RT" grep -q 'PREEMPT_RT' <<<"$(uname -v)"

# 2. running config
cfg=$(zcat /proc/config.gz 2>/dev/null)
check "/proc/config.gz readable" test -n "$cfg"
check "CONFIG_PREEMPT_RT=y" grep -qx 'CONFIG_PREEMPT_RT=y' <<<"$cfg"
check "CONFIG_PREEMPT_DYNAMIC not set" bash -c '! grep -q "^CONFIG_PREEMPT_DYNAMIC=" <<<"$1"' _ "$cfg"
# PREEMPT_DYNAMIC off also means no preempt= switch at runtime
check "no /sys/kernel/debug/sched/preempt switch" bash -c '
  mountpoint -q /sys/kernel/debug || mount -t debugfs none /sys/kernel/debug 2>/dev/null
  [[ ! -e /sys/kernel/debug/sched/preempt ]]'

# 3. /sys/kernel/realtime
check "/sys/kernel/realtime reads 1" test "$(cat /sys/kernel/realtime 2>/dev/null)" = 1

# 4. RT hallmarks: forced IRQ threading, ktimers thread
check "threaded IRQ handlers present (irq/* kthreads)" bash -c 'ps -eo comm= | grep -q "^irq/"'
check "ktimers kthreads present" bash -c 'ps -eo comm= | grep -q "^ktimers/"'

# 5. chrt / SCHED_FIFO / SCHED_RR
check "chrt -f 99 runs a task" chrt -f 99 true
check "chrt -r 50 runs a task" chrt -r 50 true
check "SCHED_FIFO prio 99 is applied" bash -c '
  chrt -f 99 sleep 3 & p=$!; sleep 0.5
  out=$(chrt -p $p); kill $p; echo "    $out"
  grep -q SCHED_FIFO <<<"$out" && grep -q "priority: 99" <<<"$out"'
check "SCHED_RR prio 50 is applied" bash -c '
  chrt -r 50 sleep 3 & p=$!; sleep 0.5
  out=$(chrt -p $p); kill $p; echo "    $out"
  grep -q SCHED_RR <<<"$out" && grep -q "priority: 50" <<<"$out"'

# Scheduling semantics on one CPU (python: available on the test image).
# - FIFO: a higher-priority task preempts a running lower-priority one and
#   finishes first.
# - RR: two equal-priority tasks share the CPU (finish close together), while
#   equal-priority FIFO tasks run back to back.
cpu=$(( $(nproc) > 1 ? 1 : 0 ))
check "SCHED_FIFO/SCHED_RR semantics on CPU $cpu" python3 - "$cpu" <<'PY'
import os, sys, time
cpu = int(sys.argv[1])
def spin(secs):
    # burn `secs` of CPU time (not wall time)
    t0 = time.thread_time()
    while time.thread_time() - t0 < secs: pass
def child(policy, prio, work, delay, w):
    os.sched_setaffinity(0, {cpu})
    time.sleep(delay)
    os.sched_setscheduler(0, policy, os.sched_param(prio))
    spin(work)
    os.write(w, f"{time.monotonic()}\n".encode()); os._exit(0)
def race(specs):
    os.sched_setaffinity(0, {cpu})
    # parent must outrank children to keep launching them
    os.sched_setscheduler(0, os.SCHED_FIFO, os.sched_param(90))
    pipes, pids = [], []
    for s in specs:
        r, w = os.pipe(); pid = os.fork()
        if pid == 0: os.close(r); child(*s, w)
        os.close(w); pipes.append(r); pids.append(pid)
    os.sched_setscheduler(0, os.SCHED_OTHER, os.sched_param(0))
    os.sched_setaffinity(0, set(range(os.cpu_count())))
    for p in pids: os.waitpid(p, 0)
    return [float(os.read(r, 64)) for r in pipes]
ok = True
lo, hi = race([(os.SCHED_FIFO, 10, 1.5, 0.0, ), (os.SCHED_FIFO, 50, 0.3, 0.3)])
print(f"    FIFO preempt: high(50) done {hi-lo:+.2f}s vs low(10) (expect < 0)")
ok &= hi < lo
a, b = race([(os.SCHED_RR, 30, 1.0, 0.0), (os.SCHED_RR, 30, 1.0, 0.05)])
f1, f2 = race([(os.SCHED_FIFO, 30, 1.0, 0.0), (os.SCHED_FIFO, 30, 1.0, 0.05)])
print(f"    RR   equal prio: finish gap {abs(a-b):.2f}s (expect small, interleaved)")
print(f"    FIFO equal prio: finish gap {abs(f1-f2):.2f}s (expect ~work time, back to back)")
ok &= abs(a-b) < abs(f1-f2) * 0.6
sys.exit(0 if ok else 1)
PY

# 6. cyclictest: functional run only
echo "-- cyclictest (FUNCTIONAL CHECK ONLY: numbers under QEMU/TCG measure the emulator, NOT kernel latency) --"
if command -v cyclictest >/dev/null; then
  check "cyclictest runs to completion" bash -c '
    cyclictest -m -S -p 90 -i 1000 -D 20 -q > /tmp/cyclictest.out 2>&1; rc=$?
    sed "s/^/    [TCG, not a benchmark] /" /tmp/cyclictest.out; exit $rc'
else
  fail "cyclictest installed"
fi
if command -v pi_stress >/dev/null; then
  check "pi_stress (priority inheritance) runs clean" bash -c '
    pi_stress --duration 10 --quiet > /tmp/pi_stress.out 2>&1; rc=$?; tail -3 /tmp/pi_stress.out | sed "s/^/    /"; exit $rc'
fi

# 7. kernel log sanity (RT-specific splats)
check "no BUG/sleeping-in-atomic splats in dmesg" bash -c '
  ! dmesg | grep -E "BUG: (sleeping function|scheduling while atomic|spinlock)|Oops|Kernel panic"'

echo "== RESULT: $([[ $fails -eq 0 ]] && echo ALL PASS || echo "$fails FAILED") =="
exit $(( fails > 0 ))
