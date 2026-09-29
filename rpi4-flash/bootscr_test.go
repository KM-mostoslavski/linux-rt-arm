package main

import (
	"strings"
	"testing"
)

// boot.txt from ArchLinuxARM-rpi-aarch64-latest.tar.gz (uboot-raspberrypi 2025.01)
const tarballBootTxt = `# After modifying, run ./mkscr

# Set root partition to the second partition of boot device
part uuid ${devtype} ${devnum}:2 uuid

setenv bootargs console=ttyS1,115200 console=tty0 root=PARTUUID=${uuid} rw rootwait smsc95xx.macaddr="${usbethaddr}"

if load ${devtype} ${devnum}:${bootpart} ${kernel_addr_r} /Image; then
  if load ${devtype} ${devnum}:${bootpart} ${fdt_addr_r} /dtbs/${fdtfile}; then
    booti ${kernel_addr_r} - ${fdt_addr_r};
  fi;
fi
`

func TestPatchBootTxt(t *testing.T) {
	out, changed := patchBootTxt(tarballBootTxt)
	if !changed {
		t.Fatal("not patched")
	}
	if strings.Index(out, "setenv ramdisk_addr_r") > strings.Index(out, "if load ") {
		t.Fatalf("addresses must be set before the first load:\n%s", out)
	}
	if again, changed := patchBootTxt(out); changed || again != out {
		t.Fatal("patch is not idempotent")
	}
	if _, changed := patchBootTxt("booti 0 - 0\n"); changed {
		t.Fatal("patched an unknown layout")
	}
}

func TestArmv7ConfigTxt(t *testing.T) {
	in := "initramfs initramfs-linux.img followkernel\n[cm5]\ndtoverlay=dwc2,dr_mode=host\n[all]\n"
	out, changed := armv7ConfigTxt(in, "kernel7.img")
	if !changed || !strings.HasSuffix(out, "[all]\narm_64bit=0\nkernel=kernel7.img\n") {
		t.Fatalf("got:\n%s", out)
	}
	if again, changed := armv7ConfigTxt(out, "kernel7.img"); changed || again != out {
		t.Fatal("not idempotent")
	}
}
