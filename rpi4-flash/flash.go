package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sectorSize  = 512
	gib         = 1 << 30
	bootSize    = 1 * gib // always 1 GiB
	swapSize    = 1 * gib // 1 GiB max, when chosen
	minRootSize = 4 * gib // the aarch64 rootfs alone is ~2.5 GiB
	alignSector = 2048    // 1 MiB alignment
)

type config struct {
	Arch     string // aarch64 | armv7
	Device   string // /dev/sdX, /dev/mmcblkN, /dev/loopN
	Kernel   string // kernelRT | kernelLatest
	Swap     string // swapNone | swapFile | swapPartition
	CacheDir string
	RTPkg    string // explicit linux-rt-arm package, optional
	LogPath  string
}

const (
	swapNone      = "none"
	swapFile      = "file"
	swapPartition = "partition"
)

// flasher carries the state of one flashing run.
type flasher struct {
	cfg      config
	r        *runner
	work     string // temp dir holding the mountpoints
	root     string // rootfs mountpoint
	bootPart string
	rootPart string
	swapPart string
	mounted  []string // mount stack, unmounted in reverse
	kernel   *kernelPlan
}

func (f *flasher) flash() (err error) {
	f.r.step("Preflight checks")
	if err := f.preflight(); err != nil {
		return err
	}

	f.r.step("Fetching the Arch Linux ARM %s rootfs", f.cfg.Arch)
	tarball, err := fetchTarball(f.r, f.cfg.CacheDir, f.cfg.Arch)
	if err != nil {
		return err
	}

	// Everything above is read-only. From here on the target gets erased.
	f.work, err = os.MkdirTemp("", "rpi4-flash-")
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.cleanup(); cerr != nil {
			if err == nil {
				err = cerr
			} else {
				f.r.warn("cleanup: %v", cerr)
			}
		}
	}()

	steps := []struct {
		name string
		fn   func() error
	}{
		{"Releasing " + f.cfg.Device, f.release},
		{"Partitioning " + f.cfg.Device, f.partition},
		{"Creating filesystems", f.mkfs},
		{"Extracting the rootfs (takes a few minutes)", func() error { return f.extract(tarball) }},
		{"Pinning /etc/fstab and the kernel command line to UUIDs", f.pinUUIDs},
		{"Setting up swap", f.setupSwap},
		{"Installing the kernel", f.installKernel},
		{"Fixing U-Boot load addresses", f.fixBootScript},
		{"Flushing writes to " + f.cfg.Device, f.syncAll},
	}
	for _, s := range steps {
		f.r.step("%s", s.name)
		if err := s.fn(); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return nil
}

func (f *flasher) preflight() error {
	if os.Geteuid() != 0 {
		return errors.New("must run as root (partitioning and mounting need it): sudo rpi4-flash")
	}
	tools := []string{"lsblk", "wipefs", "sfdisk", "blockdev", "udevadm", "blkid",
		"mkfs.vfat", "mkfs.ext4", "mkswap", "bsdtar", "mount", "umount", "sync"}
	if f.cfg.Arch == "aarch64" {
		tools = append(tools, "mkimage")
	}
	var missing []string
	for _, t := range tools {
		if _, err := lookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing tools: %s (on Arch: pacman -S util-linux dosfstools e2fsprogs libarchive systemd uboot-tools)",
			strings.Join(missing, " "))
	}
	if _, err := validateTarget(f.cfg.Device); err != nil {
		return err
	}
	size, err := f.r.capture("blockdev", "--getsize64", f.cfg.Device)
	if err != nil {
		return err
	}
	n, _ := strconv.ParseUint(size, 10, 64)
	need := uint64(bootSize + minRootSize)
	if f.cfg.Swap == swapPartition {
		need += swapSize
	}
	if n < need {
		return fmt.Errorf("%s is %d MiB, need at least %d MiB", f.cfg.Device, n>>20, need>>20)
	}
	plan, err := planKernel(f.r, f.cfg)
	if err != nil {
		return err
	}
	f.kernel = plan
	return nil
}

// release unmounts / swapoffs anything on the target (desktop auto-mounters
// love to grab SD cards) and wipes old signatures.
func (f *flasher) release() error {
	d, err := validateTarget(f.cfg.Device)
	if err != nil {
		return err
	}
	for _, c := range d.Children {
		for _, m := range c.Mountpoints {
			switch {
			case m == "":
			case m == "[SWAP]":
				if err := f.r.run("", "swapoff", c.Path); err != nil {
					return err
				}
			default:
				f.r.info("unmounting %s (%s)", c.Path, m)
				if err := f.r.run("", "umount", c.Path); err != nil {
					return err
				}
			}
		}
		// Old partition signatures would otherwise make udev/blkid
		// report stale filesystems on the new partitions.
		_ = f.r.run("", "wipefs", "--all", "--force", c.Path)
	}
	return f.r.run("", "wipefs", "--all", "--force", f.cfg.Device)
}

func (f *flasher) partition() error {
	size, err := f.r.capture("blockdev", "--getsz", f.cfg.Device)
	if err != nil {
		return err
	}
	total, err := strconv.ParseUint(size, 10, 64)
	if err != nil {
		return err
	}
	// Layout (MBR, like the wiki: the Pi firmware reads the first FAT
	// partition, the U-Boot script expects the rootfs as partition 2):
	//   1: 1 GiB FAT32 (LBA) /boot
	//   2: ext4 /, rest of the disk
	//   3: 1 GiB Linux swap, only for the "partition" swap choice
	const bootSectors = bootSize / sectorSize
	rootStart := uint64(alignSector + bootSectors)
	script := "label: dos\n"
	script += fmt.Sprintf("start=%d, size=%d, type=c\n", alignSector, bootSectors)
	if f.cfg.Swap == swapPartition {
		const swapSectors = swapSize / sectorSize
		swapStart := (total - swapSectors) / alignSector * alignSector
		script += fmt.Sprintf("start=%d, size=%d, type=83\n", rootStart, swapStart-rootStart)
		script += fmt.Sprintf("start=%d, size=%d, type=82\n", swapStart, total-swapStart)
	} else {
		script += fmt.Sprintf("start=%d, type=83\n", rootStart)
	}
	f.r.info("sfdisk layout:\n%s", indent(script))
	if err := f.r.run(script, "sfdisk", "--wipe", "always", "--wipe-partitions", "always", f.cfg.Device); err != nil {
		return err
	}
	want := 2
	if f.cfg.Swap == swapPartition {
		want = 3
	}
	parts, err := f.waitPartitions(want)
	if err != nil {
		return err
	}
	f.bootPart, f.rootPart = parts[0], parts[1]
	if want == 3 {
		f.swapPart = parts[2]
	}
	return nil
}

// waitPartitions waits for the kernel and udev to expose the new partitions.
func (f *flasher) waitPartitions(want int) ([]string, error) {
	_ = f.r.run("", "blockdev", "--rereadpt", f.cfg.Device)
	for i := 0; i < 20; i++ {
		_ = f.r.run("", "udevadm", "settle")
		parts, err := partitions(f.cfg.Device)
		if err == nil && len(parts) == want {
			ok := true
			for _, p := range parts {
				if _, err := os.Stat(p); err != nil {
					ok = false
				}
			}
			if ok {
				return parts, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("the kernel did not expose %d partitions on %s (for loop devices use losetup --partscan)", want, f.cfg.Device)
}

func (f *flasher) mkfs() error {
	if err := f.r.run("", "mkfs.vfat", "-F", "32", "-n", "BOOT", f.bootPart); err != nil {
		return err
	}
	if err := f.r.run("", "mkfs.ext4", "-F", "-q", "-L", "root", f.rootPart); err != nil {
		return err
	}
	if f.swapPart != "" {
		if err := f.r.run("", "mkswap", "--label", "swap", f.swapPart); err != nil {
			return err
		}
	}
	return nil
}

func (f *flasher) mount(src, dst string, args ...string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := f.r.run("", "mount", append(args, src, dst)...); err != nil {
		return err
	}
	f.mounted = append(f.mounted, dst)
	return nil
}

func (f *flasher) extract(tarball string) error {
	f.root = filepath.Join(f.work, "root")
	if err := f.mount(f.rootPart, f.root); err != nil {
		return err
	}
	// Extract onto ext4 first: bsdtar -p cannot set owners/modes on FAT,
	// which is why the wiki extracts to root/ and moves boot/* afterwards.
	if err := f.r.run("", "bsdtar", "-xpf", tarball, "-C", f.root); err != nil {
		return err
	}
	stage := filepath.Join(f.work, "boot")
	if err := f.mount(f.bootPart, stage); err != nil {
		return err
	}
	rootBoot := filepath.Join(f.root, "boot")
	// cp instead of mv: mv tries to keep ownership on FAT and warns per file.
	if err := f.r.run("", "cp", "-r", "--no-preserve=ownership,mode", rootBoot+"/.", stage); err != nil {
		return err
	}
	entries, err := os.ReadDir(rootBoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(rootBoot, e.Name())); err != nil {
			return err
		}
	}
	if err := f.unmountLast(); err != nil {
		return err
	}
	// From now on /boot is the FAT partition, exactly as on the Pi, so
	// anything installed into the rootfs (kernel, initramfs) lands there.
	return f.mount(f.bootPart, rootBoot)
}

func (f *flasher) blkid(tag, dev string) (string, error) {
	v, err := f.r.capture("blkid", "--probe", "--match-tag", tag, "--output", "value", dev)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("blkid: no %s on %s", tag, dev)
	}
	return v, nil
}

func (f *flasher) pinUUIDs() error {
	bootUUID, err := f.blkid("UUID", f.bootPart)
	if err != nil {
		return err
	}
	rootUUID, err := f.blkid("UUID", f.rootPart)
	if err != nil {
		return err
	}
	rootPartUUID, err := f.blkid("PART_ENTRY_UUID", f.rootPart)
	if err != nil {
		return err
	}
	entries := []fstabEntry{
		{Source: "UUID=" + rootUUID, Mount: "/", FSType: "ext4", Options: "defaults", Dump: 0, Pass: 1},
		{Source: "UUID=" + bootUUID, Mount: "/boot", FSType: "vfat", Options: "defaults", Dump: 0, Pass: 2},
	}
	if err := f.editFstab(entries); err != nil {
		return err
	}
	f.r.info("/boot -> UUID=%s, / -> UUID=%s", bootUUID, rootUUID)

	// armv7 boots the kernel straight from the firmware, with root= taken
	// from cmdline.txt (root=/dev/mmcblk0p2 in the tarball).
	cmdline := filepath.Join(f.root, "boot", "cmdline.txt")
	if b, err := os.ReadFile(cmdline); err == nil {
		out := rewriteCmdline(string(b), rootPartUUID)
		if err := os.WriteFile(cmdline, []byte(out), 0o644); err != nil {
			return err
		}
		f.r.info("cmdline.txt: %s", strings.TrimSpace(out))
	}
	// aarch64 boots through U-Boot, whose boot.scr computes the PARTUUID of
	// partition 2 itself; make sure that assumption still holds.
	if b, err := os.ReadFile(filepath.Join(f.root, "boot", "boot.txt")); err == nil {
		if !strings.Contains(string(b), "root=PARTUUID=${uuid}") {
			f.r.warn("boot.txt no longer uses root=PARTUUID; check that the Pi finds its rootfs")
		} else {
			f.r.info("boot.scr: root=PARTUUID of partition 2 (%s), resolved by U-Boot", rootPartUUID)
		}
	}
	return nil
}

func (f *flasher) editFstab(entries []fstabEntry) error {
	path := filepath.Join(f.root, "etc", "fstab")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rewriteFstab(string(b), entries)), 0o644)
}

func (f *flasher) setupSwap() error {
	switch f.cfg.Swap {
	case swapPartition:
		uuid, err := f.blkid("UUID", f.swapPart)
		if err != nil {
			return err
		}
		f.r.info("1 GiB swap partition %s (UUID=%s)", f.swapPart, uuid)
		return f.editFstab([]fstabEntry{{Source: "UUID=" + uuid, Mount: "none", FSType: "swap", Options: "defaults"}})
	case swapFile:
		path := filepath.Join(f.root, "swapfile")
		// dd, not fallocate: a swapfile must not have holes/unwritten extents
		// on every filesystem, and 1 GiB is quick to write.
		if err := f.r.run("", "dd", "if=/dev/zero", "of="+path, "bs=1M", "count=1024", "status=none"); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		if err := f.r.run("", "mkswap", "--label", "swapfile", path); err != nil {
			return err
		}
		f.r.info("1 GiB /swapfile")
		return f.editFstab([]fstabEntry{{Source: "/swapfile", Mount: "none", FSType: "swap", Options: "defaults"}})
	default:
		f.r.info("no swap")
		return nil
	}
}

func (f *flasher) syncAll() error {
	return f.r.run("", "sync")
}

func (f *flasher) unmountLast() error {
	n := len(f.mounted)
	if n == 0 {
		return nil
	}
	mp := f.mounted[n-1]
	var err error
	for i := 0; i < 5; i++ {
		if err = f.r.run("", "umount", mp); err == nil {
			f.mounted = f.mounted[:n-1]
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}

func (f *flasher) cleanup() error {
	f.r.step("Unmounting")
	for len(f.mounted) > 0 {
		if err := f.unmountLast(); err != nil {
			return fmt.Errorf("%w (unmount %s by hand before removing the card)", err, f.mounted[len(f.mounted)-1])
		}
	}
	if f.work != "" {
		return os.RemoveAll(f.work)
	}
	return nil
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n      ")
}
