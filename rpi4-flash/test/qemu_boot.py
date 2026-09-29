#!/usr/bin/env python3
"""Boot a flashed image in QEMU's raspi4b machine and check it from the inside.

QEMU does not run the VideoCore firmware (start4.elf); it plays the firmware's
part by loading a kernel image and a DTB itself. The test runs in two stages:

 1. aarch64 only - U-Boot: QEMU loads the image's kernel8.img (U-Boot). It must
    find the SD card, run the image's boot.scr and load Image, the DTB and the
    initramfs without overlap, up to "Starting kernel ...".
    The kernel log shows on ttyS1 once its driver loads; the stage ends when
    the kernel has mounted root through the PARTUUID U-Boot computed.
 2. Linux: QEMU loads the image's own kernel, initramfs and DTB directly, with
    the kernel command line of the image (boot.txt bootargs / cmdline.txt),
    its console= arguments replaced by a PL011 console. systemd then mounts / and /boot, swap, etc. from the
    image's /etc/fstab: the part the wiki's sed breaks. We log in on the serial
    console and run checks.

usage: qemu_boot.py IMAGE BOOTDIR LOG [aarch64|armv7]
  BOOTDIR: copy of the image's boot partition (the image itself must not be
  mounted while QEMU runs).
Exit 0 when every stage and in-guest check passes.
"""
import os, re, select, socket, subprocess, sys, tempfile, time

img, bootdir, log_path = sys.argv[1:4]
arch = sys.argv[4] if len(sys.argv) > 4 else "aarch64"
log = open(log_path, "wb")
sys.stdout.reconfigure(line_buffering=True)


class VM:
    def __init__(self, kernel, dtb, extra, stage):
        tmp = tempfile.mkdtemp()
        socks = [os.path.join(tmp, f"uart{i}") for i in (0, 1)]
        cmd = ["qemu-system-aarch64", "-M", "raspi4b", "-m", "2G", "-smp", "4",
               "-kernel", kernel, "-dtb", dtb,
               "-drive", f"file={img},if=sd,format=raw",
               "-display", "none", "-monitor", "none", "-no-reboot",
               "-chardev", f"socket,id=u0,path={socks[0]},server=on,wait=off",
               "-chardev", f"socket,id=u1,path={socks[1]},server=on,wait=off",
               "-serial", "chardev:u0", "-serial", "chardev:u1"] + extra
        log.write(f"\n##### stage {stage}: {' '.join(cmd)}\n".encode()); log.flush()
        self.proc = subprocess.Popen(cmd, stderr=subprocess.DEVNULL)
        self.conns = []
        for p in socks:
            for _ in range(100):
                try:
                    s = socket.socket(socket.AF_UNIX); s.connect(p)
                    self.conns.append(s); break
                except OSError:
                    time.sleep(0.1)
        self.buf = {c: b"" for c in self.conns}
        self.start = time.time()

    def pump(self, until, t, fail=None):
        """Read both UARTs until regex `until` shows up; return that socket."""
        deadline = time.time() + t
        while time.time() < deadline and self.proc.poll() is None:
            r, _, _ = select.select(self.conns, [], [], 1)
            for c in r:
                data = c.recv(65536)
                if not data:
                    continue
                log.write(data); log.flush()
                self.buf[c] = (self.buf[c] + data)[-20000:]
                if fail and re.search(fail, self.buf[c]):
                    m = re.search(fail, self.buf[c]).group(0).decode(errors="replace")
                    raise RuntimeError(f"saw {m!r}")
                if re.search(until, self.buf[c]):
                    self.buf[c] = b""
                    return c
        raise TimeoutError(f"no {until!r} after {time.time()-self.start:.0f}s")

    def send(self, c, s):
        c.settimeout(30)  # a guest that stops reading must not hang us
        for ch in s.encode():  # type slowly: the emulated UART has no flow control
            c.send(bytes([ch])); time.sleep(0.01)

    def run_line(self, c, line, t=60):
        """Type one shell line and wait for its echo before the next one."""
        self.send(c, line + "\n")
        self.pump(re.escape(line[-12:].encode()), t)

    def stop(self):
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait()


def partuuid(n):
    out = subprocess.run(["sfdisk", "--disk-id", img], capture_output=True, text=True).stdout
    return f"{out.strip().removeprefix('0x')}-{n:02d}"


def stage_uboot():
    vm = VM(os.path.join(bootdir, "kernel8.img"),
            os.path.join(bootdir, "bcm2711-rpi-4-b.dtb"), [], "1 (U-Boot)")
    try:
        vm.pump(rb"Starting kernel \.\.\.", 300,
                fail=rb"overlaps [^\n]*|ERROR: [^\n]*|Bad Linux ARM64 Image magic|U-Boot> $")
        print(f"PASS  U-Boot ran boot.scr and started the kernel ({time.time()-vm.start:.0f}s)")
        # The log shows up on ttyS1 once its driver is loaded (the kernel
        # replays its buffer). Root is mounted by the initramfs from the
        # root=PARTUUID U-Boot computed.
        vm.pump(rb"EXT4-fs \(mmcblk\dp2\): mounted filesystem", 420,
                fail=rb"Kernel panic[^\n]*|Internal error[^\n]*|Timed out waiting for device[^\n]*")
        print(f"PASS  kernel booted by U-Boot mounted root by PARTUUID ({time.time()-vm.start:.0f}s)")
        # Past this point the image's console=ttyS1 makes QEMU's SDHCI time
        # out (RCU stalls, SD I/O errors), so userland is checked in stage 2.
    finally:
        vm.stop()


def stage_linux():
    if arch == "aarch64":
        txt = open(os.path.join(bootdir, "boot.txt")).read()
        args = re.search(r"^setenv bootargs (.*)$", txt, re.M).group(1)
        args = args.replace("${uuid}", partuuid(2))
        args = re.sub(r"\S*\$\{\w+\}\S*", "", args)  # U-Boot-only variables
        kernel = os.path.join(bootdir, "Image")
        dtb = os.path.join(bootdir, "dtbs", "broadcom", "bcm2711-rpi-4-b.dtb")
    else:
        args = open(os.path.join(bootdir, "cmdline.txt")).read().strip()
        kernel = next(os.path.join(bootdir, k) for k in ("kernel7l.img", "kernel7.img")
                      if os.path.exists(os.path.join(bootdir, k)))
        dtb = os.path.join(bootdir, "bcm2711-rpi-4-b.dtb")
    # Keep the image's arguments but its consoles: under QEMU the mini-UART
    # console (ttyS1) starves the SDHCI of interrupts ("mmc0: Timeout waiting
    # for hardware interrupt", then ext4 I/O errors), and with tty0 around
    # systemd starts no serial getty. A lone PL011 console avoids both.
    args = " ".join(a for a in args.split() if not a.startswith("console="))
    # The PL011 is ttyAMA0 for mainline, ttyAMA1 for the downstream armv7
    # kernel (its DT aliases serial1 = PL011). systemd's getty generator did
    # not pick it up by itself here: ask for it.
    tty = "ttyAMA1" if arch == "armv7" else "ttyAMA0"
    args += f" console={tty},115200 systemd.wants=serial-getty@{tty}.service"
    # Emulated 32-bit boots are slow: /boot's device shows up after ~2.5 min,
    # past systemd's default 90 s device timeout (-> emergency mode).
    args += " systemd.default_device_timeout_sec=600"
    print(f"      kernel command line: {args}")
    extra = ["-initrd", os.path.join(bootdir, "initramfs-linux.img"), "-append", args]
    if arch == "armv7":
        extra += ["-cpu", "cortex-a72,aarch64=off"]  # AArch32 kernel
    vm = VM(kernel, dtb, extra, "2 (Linux)")
    try:
        con = vm.pump(rb"login: $", 900,
                      fail=rb"Emergency Mode|emergency mode|Kernel panic[^\n]*|Give root password")
        print(f"PASS  login prompt ({time.time()-vm.start:.0f}s)")
        vm.send(con, "root\n"); vm.pump(rb"Password: ", 60)
        # the shell prompt is followed by OSC 3008 escapes (systemd >= 258)
        vm.send(con, "root\n"); vm.pump(rb"\]# ", 120)
        # the checks live in the test image (e2e.sh): long typed lines get lost
        vm.run_line(con, "sh /root/rpi4-flash-qa.sh")
        vm.pump(rb"\nQA_RESULT=\d", 300)
        time.sleep(1)
        out = open(log_path, "rb").read().decode(errors="replace").replace("\r", "")
        out = out[out.rfind("##### stage 2"):]
        out = re.sub(r"\x1b\][^\x1b]*\x1b\\|\x1b\[[0-9;?]*[a-zA-Z]", "", out)  # OSC/CSI
        for l in out.splitlines():
            if re.match(r"QA (PASS|FAIL|kernel|/boot|swap|fstab|failed)", l):
                print("   " + l[3:])
        ok = re.search(r"^QA_RESULT=0", out, re.M) is not None
        vm.send(con, "poweroff\n")
        try:
            vm.proc.wait(60)
        except subprocess.TimeoutExpired:
            pass
        return ok
    finally:
        vm.stop()


rc = 1
try:
    if arch == "aarch64" and not os.environ.get("SKIP_UBOOT"):
        stage_uboot()
    rc = 0 if stage_linux() else 1
except Exception as e:
    print(f"FAIL  {e}")
print("RESULT:", "PASS" if rc == 0 else "FAIL")
sys.exit(rc)
