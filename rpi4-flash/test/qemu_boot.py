#!/usr/bin/env python3
"""Boot a flashed image in QEMU's raspi4b machine and check it from the inside.

QEMU does not run the VideoCore firmware (start4.elf), so it plays the
firmware's part: it loads the image's own kernel8.img (U-Boot, aarch64) or
kernel7*.img (armv7) with the image's bcm2711-rpi-4-b.dtb. From there the
image's own boot chain runs: U-Boot -> boot.scr -> Image + initramfs -> systemd,
which mounts /boot through /etc/fstab (the step the wiki's sed breaks).

usage: qemu_boot.py IMAGE BOOTDIR LOG [aarch64|armv7]
  BOOTDIR: copy of the image's boot partition (kernel + dtb are taken from it;
  the image itself must not be mounted while QEMU runs).
Exit 0 when every in-guest check passes.
"""
import os, re, select, socket, subprocess, sys, tempfile, time

img, bootdir, log_path = sys.argv[1:4]
arch = sys.argv[4] if len(sys.argv) > 4 else "aarch64"
TIMEOUT = 900

tmp = tempfile.mkdtemp()
socks = [os.path.join(tmp, f"uart{i}") for i in (0, 1)]
if arch == "aarch64":
    kernel = os.path.join(bootdir, "kernel8.img")
    extra = []
else:
    kernel = next(os.path.join(bootdir, k) for k in ("kernel7l.img", "kernel7.img")
                  if os.path.exists(os.path.join(bootdir, k)))
    cmdline = open(os.path.join(bootdir, "cmdline.txt")).read().strip()
    # the firmware would also append its own console=; use the PL011
    extra = ["-append", cmdline + " console=ttyAMA0,115200",
             "-initrd", os.path.join(bootdir, "initramfs-linux.img")]

qemu = ["qemu-system-aarch64", "-M", "raspi4b", "-m", "2G", "-smp", "4",
        "-kernel", kernel, "-dtb", os.path.join(bootdir, "bcm2711-rpi-4-b.dtb"),
        "-drive", f"file={img},if=sd,format=raw",
        "-display", "none", "-monitor", "none", "-no-reboot",
        "-chardev", f"socket,id=u0,path={socks[0]},server=on,wait=off",
        "-chardev", f"socket,id=u1,path={socks[1]},server=on,wait=off",
        "-serial", "chardev:u0", "-serial", "chardev:u1"] + extra
if arch == "armv7":
    qemu += ["-cpu", "cortex-a72"]  # AArch32 kernel on the A72
print(" ".join(qemu))
proc = subprocess.Popen(qemu)
log = open(log_path, "wb")

conns = []
for p in socks:
    for _ in range(100):
        try:
            s = socket.socket(socket.AF_UNIX); s.connect(p); conns.append(s); break
        except OSError:
            time.sleep(0.1)
buf = {c: b"" for c in conns}
console = None
start = time.time()


def pump(until, t=TIMEOUT):
    """Read both UARTs until regex `until` is seen on one; return that socket."""
    deadline = time.time() + t
    while time.time() < deadline and proc.poll() is None:
        r, _, _ = select.select(conns, [], [], 1)
        for c in r:
            data = c.recv(65536)
            if not data:
                continue
            log.write(data); log.flush()
            buf[c] = (buf[c] + data)[-20000:]
            if re.search(until, buf[c]):
                buf[c] = b""
                return c
    raise TimeoutError(f"waiting for {until!r} ({time.time()-start:.0f}s)")


def send(c, s):
    for ch in s.encode():  # slow typing, the emulated UART has no flow control
        c.send(bytes([ch])); time.sleep(0.01)


rc = 1
try:
    console = pump(rb"login: $|emergency mode|Kernel panic|Give root password")
    txt = open(log_path, "rb").read()
    if re.search(rb"emergency mode|Give root password|Kernel panic", txt):
        raise RuntimeError("boot failed (emergency mode / panic)")
    print(f"login prompt after {time.time()-start:.0f}s")
    send(console, "root\n"); pump(rb"Password: ", 60)
    send(console, "root\n"); pump(rb"# $", 120)
    checks = r"""
set +e; f=0; ck() { if eval "$2" >/dev/null 2>&1; then echo "QA PASS $1"; else echo "QA FAIL $1"; f=1; fi; }
echo "QA uname: $(uname -rv)"; echo "QA cmdline: $(cat /proc/cmdline)"
echo "QA boot: $(findmnt -no SOURCE,FSTYPE /boot)"; echo "QA swap: $(swapon --noheadings --show=NAME,SIZE | tr '\n' ' ')"
ck "/boot mounted (vfat)" 'findmnt -no FSTYPE /boot | grep -qx vfat'
ck "/boot holds the kernel" 'test -s /boot/initramfs-linux.img'
ck "no failed units" 'test -z "$(systemctl --failed --no-legend --plain)"'
ck "root mounted rw" 'findmnt -no OPTIONS / | grep -q "^rw"'
ck "fstab has no /dev/mmcblk" '! grep -q "^/dev/mmcblk" /etc/fstab'
systemctl --failed --no-legend --plain | sed 's/^/QA failed unit: /'
echo "QA_RESULT=$f"
"""
    for line in checks.strip().splitlines():
        send(console, line + "\n"); time.sleep(0.2)
    pump(rb"QA_RESULT=\d", 180)
    time.sleep(1)
    out = open(log_path, "rb").read().decode(errors="replace")
    qa = [l for l in out.splitlines() if l.startswith("QA ") or l.startswith("QA_RESULT")]
    print("\n".join(qa))
    # the typed command echoes "QA_RESULT=$f"; only the real output has a digit
    rc = 0 if re.search(r"^QA_RESULT=0", out, re.M) else 1
    send(console, "poweroff\n")
    try:
        proc.wait(60)
    except subprocess.TimeoutExpired:
        pass
except Exception as e:
    print(f"FAIL: {e}")
finally:
    if proc.poll() is None:
        proc.kill()
    log.close()
sys.exit(rc)
