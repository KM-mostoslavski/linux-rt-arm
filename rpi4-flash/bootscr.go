package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// U-Boot's rpi defaults put the fdt at 0x02600000 and the initramfs at
// 0x02700000, i.e. ~38 MiB after where Image is loaded (0x80000, moved to
// 0x200000 by booti). The stock boot.txt loads Image first, then the fdt and
// the initramfs on top of its tail: a bigger kernel (linux-rt-arm's Image is
// ~50 MiB) is silently corrupted, or U-Boot stops with
// "RD image overlaps OS image". Found booting in QEMU raspi4b; the Pi runs
// the same U-Boot, so it fails the same way.
//
// Loading at fixed higher addresses leaves 126 MiB for the kernel and still
// fits the 1 GiB Pi 4 (U-Boot relocates itself to the top of RAM).
const loadAddrMarker = "# rpi4-flash: load addresses"

const loadAddrBlock = loadAddrMarker + ` (defaults overlap kernels > ~38 MiB)
setenv kernel_addr_r 0x00200000
setenv fdt_addr_r 0x08000000
setenv ramdisk_addr_r 0x08100000
`

// patchBootTxt inserts loadAddrBlock before the first load command.
func patchBootTxt(txt string) (string, bool) {
	if strings.Contains(txt, loadAddrMarker) {
		return txt, false
	}
	i := strings.Index(txt, "if load ")
	if i < 0 {
		return txt, false
	}
	return txt[:i] + loadAddrBlock + "\n" + txt[i:], true
}

func (f *flasher) fixBootScript() error {
	if f.cfg.Arch != "aarch64" {
		return nil // armv7 is booted by the firmware directly, no U-Boot
	}
	dir := filepath.Join(f.root, "boot")
	txtPath := filepath.Join(dir, "boot.txt")
	b, err := os.ReadFile(txtPath)
	if err != nil {
		return err
	}
	out, changed := patchBootTxt(string(b))
	if !changed {
		if !strings.Contains(string(b), loadAddrMarker) {
			return fmt.Errorf("boot.txt has an unexpected layout, cannot fix its load addresses")
		}
		return nil
	}
	if err := os.WriteFile(txtPath, []byte(out), 0o644); err != nil {
		return err
	}
	// Same invocation as the image's own /boot/mkscr.
	if err := f.r.run("", "mkimage", "-A", "arm", "-O", "linux", "-T", "script", "-C", "none",
		"-n", "U-Boot boot script", "-d", txtPath, filepath.Join(dir, "boot.scr")); err != nil {
		return err
	}
	f.r.info("boot.scr: kernel/fdt/initramfs loaded at 0x200000/0x8000000/0x8100000")
	return nil
}
