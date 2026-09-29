package main

import (
	"strings"
	"testing"
)

// The /etc/fstab shipped in ArchLinuxARM-rpi-{aarch64,armv7}-latest.tar.gz.
const tarballFstab = `# Static information about the filesystems.
# See fstab(5) for details.

# <file system> <dir> <type> <options> <dump> <pass>
/dev/mmcblk0p1  /boot   vfat    defaults        0       0
`

func TestRewriteFstabReplacesDeviceNames(t *testing.T) {
	out := rewriteFstab(tarballFstab, []fstabEntry{
		{Source: "UUID=aaaa", Mount: "/", FSType: "ext4", Options: "defaults", Pass: 1},
		{Source: "UUID=BBBB-CCCC", Mount: "/boot", FSType: "vfat", Options: "defaults", Pass: 2},
	})
	if strings.Contains(out, "mmcblk") {
		t.Fatalf("device name left in fstab:\n%s", out)
	}
	if !strings.HasPrefix(out, "# Static information") {
		t.Fatalf("comments lost:\n%s", out)
	}
	for _, want := range []string{"UUID=BBBB-CCCC", "UUID=aaaa"} {
		if strings.Count(out, want) != 1 {
			t.Fatalf("want exactly one %s:\n%s", want, out)
		}
	}
}

func TestRewriteFstabIsIdempotent(t *testing.T) {
	e := []fstabEntry{
		{Source: "UUID=BBBB-CCCC", Mount: "/boot", FSType: "vfat", Options: "defaults", Pass: 2},
		{Source: "/swapfile", Mount: "none", FSType: "swap", Options: "defaults"},
	}
	once := rewriteFstab(tarballFstab, e)
	if twice := rewriteFstab(once, e); twice != once {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, twice)
	}
}

func TestRewriteFstabKeepsOtherSwap(t *testing.T) {
	in := tarballFstab + "/dev/zram0 none swap defaults 0 0\n"
	out := rewriteFstab(in, []fstabEntry{{Source: "/swapfile", Mount: "none", FSType: "swap", Options: "defaults"}})
	if !strings.Contains(out, "/dev/zram0") || !strings.Contains(out, "/swapfile") {
		t.Fatalf("swap entries wrong:\n%s", out)
	}
}

func TestRewriteCmdline(t *testing.T) {
	// cmdline.txt from ArchLinuxARM-rpi-armv7-latest.tar.gz
	in := "root=/dev/mmcblk0p2 rw rootwait console=serial0,115200 console=tty1 fsck.repair=yes\n"
	got := rewriteCmdline(in, "1234abcd-02")
	want := "root=PARTUUID=1234abcd-02 rw rootwait console=serial0,115200 console=tty1 fsck.repair=yes\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := rewriteCmdline("rw rootwait", "x-02"); got != "rw rootwait root=PARTUUID=x-02\n" {
		t.Fatalf("append: got %q", got)
	}
}
