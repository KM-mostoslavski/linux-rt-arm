package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// DECISION (branch decision/kernel-rt-pkg-vs-alarm-latest):
//
//   "7.2.7"  -> the linux-rt-arm PREEMPT_RT package built from the PKGBUILD
//               (pkgver 7.2.7). It is not in any repo, so it is taken from a
//               local .pkg.tar.* file; the closest version is used, with a
//               warning, when 7.2.7 itself is not there.
//   "latest" -> the newest kernel of the Arch Linux ARM repos, through a full
//               `pacman -Syu` (linux-aarch64 / linux-rpi; no partial upgrade).
//
// Both run pacman inside the rootfs with arch-chroot, which on an x86_64
// host needs qemu-user-static binfmt.

const (
	kernelRT     = "7.2.7"
	kernelLatest = "latest"
	rtPkgName    = "linux-rt-arm"
)

// kernelPlan is what installKernel will do, resolved during preflight so
// that nothing is erased when the requested kernel cannot be provided.
type kernelPlan struct {
	rtPkg string // package file to pacman -U; empty means pacman -Syu
}

var rtPkgRe = regexp.MustCompile(`^` + rtPkgName + `-(\d+(?:\.\d+)*)-(\d+)-aarch64\.pkg\.tar\.(?:xz|zst|gz)$`)

func planKernel(r *runner, cfg config) (*kernelPlan, error) {
	if err := checkChroot(cfg.Arch); err != nil {
		return nil, err
	}
	if cfg.Kernel == kernelLatest {
		r.info("kernel: latest from the Arch Linux ARM repos (pacman -Syu)")
		return &kernelPlan{}, nil
	}
	if cfg.Arch != "aarch64" {
		r.warn("%s is aarch64-only; armv7 gets the latest repo kernel instead", rtPkgName)
		return &kernelPlan{}, nil
	}
	pkg := cfg.RTPkg
	if pkg == "" {
		var err error
		if pkg, err = findRTPkg(searchDirs(cfg)); err != nil {
			return nil, err
		}
	}
	m := rtPkgRe.FindStringSubmatch(filepath.Base(pkg))
	if m == nil {
		return nil, fmt.Errorf("%s does not look like a %s aarch64 package", pkg, rtPkgName)
	}
	if _, err := os.Stat(pkg); err != nil {
		return nil, err
	}
	if m[1] != kernelRT {
		r.warn("no %s %s package found, using %s-%s", rtPkgName, kernelRT, m[1], m[2])
	}
	r.info("kernel: %s", pkg)
	return &kernelPlan{rtPkg: pkg}, nil
}

func searchDirs(cfg config) []string {
	dirs := []string{}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Dir(filepath.Dir(exe)))
	}
	return append(dirs, cfg.CacheDir)
}

// findRTPkg picks linux-rt-arm-7.2.7-* if present, else the newest version.
func findRTPkg(dirs []string) (string, error) {
	type cand struct{ path, ver, rel string }
	var cands []cand
	seen := map[string]bool{}
	for _, d := range dirs {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			m := rtPkgRe.FindStringSubmatch(e.Name())
			p := filepath.Join(d, e.Name())
			if m == nil || seen[p] {
				continue
			}
			seen[p] = true
			cands = append(cands, cand{p, m[1], m[2]})
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("no %s-*-aarch64.pkg.tar.* found in %s; build it from the PKGBUILD or pass --rt-pkg",
			rtPkgName, strings.Join(dirs, ", "))
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if (a.ver == kernelRT) != (b.ver == kernelRT) {
			return a.ver == kernelRT
		}
		if c := compareVersions(a.ver, b.ver); c != 0 {
			return c > 0
		}
		return compareVersions(a.rel, b.rel) > 0
	})
	return cands[0].path, nil
}

// compareVersions compares dotted numeric versions.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			fmt.Sscan(as[i], &x)
		}
		if i < len(bs) {
			fmt.Sscan(bs[i], &y)
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// checkChroot makes sure arch-chroot can run the target's binaries.
func checkChroot(arch string) error {
	if _, err := lookPath("arch-chroot"); err != nil {
		return fmt.Errorf("arch-chroot not found (pacman -S arch-install-scripts)")
	}
	native := map[string]string{"aarch64": "arm64", "armv7": "arm"}[arch]
	if runtime.GOARCH == native || (arch == "armv7" && runtime.GOARCH == "arm64") {
		return nil
	}
	fmtName := map[string]string{"aarch64": "qemu-aarch64", "armv7": "qemu-arm"}[arch]
	b, err := os.ReadFile("/proc/sys/fs/binfmt_misc/" + fmtName)
	if err != nil || !strings.HasPrefix(string(b), "enabled") {
		return fmt.Errorf("binfmt %s not registered; running %s binaries needs it (pacman -S qemu-user-static qemu-user-static-binfmt)", fmtName, arch)
	}
	if !strings.Contains(string(b), "flags: ") || !strings.Contains(strings.SplitN(string(b), "flags: ", 2)[1], "F") {
		return fmt.Errorf("binfmt %s lacks the F (fix-binary) flag, it will not work inside a chroot", fmtName)
	}
	return nil
}

func (f *flasher) chroot(args ...string) error {
	return f.r.run("", "arch-chroot", append([]string{f.root}, args...)...)
}

func (f *flasher) installKernel() error {
	// A fresh image has no pacman keyring; the wiki does this on the Pi's
	// first boot, but pacman cannot verify packages without it.
	f.r.info("initialising the pacman keyring")
	if err := f.chroot("pacman-key", "--init"); err != nil {
		return err
	}
	if err := f.chroot("pacman-key", "--populate", "archlinuxarm"); err != nil {
		return err
	}

	if f.kernel.rtPkg == "" {
		f.r.info("pacman -Syu (full upgrade, pulls the latest kernel; slow under emulation)")
		if err := f.chroot("pacman", "-Syu", "--noconfirm"); err != nil {
			return err
		}
	} else {
		name := filepath.Base(f.kernel.rtPkg)
		dst := filepath.Join(f.root, "root", name)
		if err := f.r.run("", "cp", f.kernel.rtPkg, dst); err != nil {
			return err
		}
		defer os.Remove(dst)
		// linux-rt-arm conflicts with the stock kernel; --noconfirm would
		// answer "no" to the removal prompt, so drop the stock kernel first.
		for _, stock := range []string{"linux-aarch64", "linux-rpi"} {
			if f.chroot("pacman", "-Q", stock) == nil {
				f.r.info("removing stock kernel %s", stock)
				if err := f.chroot("pacman", "-Rdd", "--noconfirm", stock); err != nil {
					return err
				}
			}
		}
		f.r.info("pacman -U %s", name)
		if err := f.chroot("pacman", "-U", "--noconfirm", "/root/"+name); err != nil {
			return err
		}
	}

	out, err := f.r.capture("arch-chroot", f.root, "pacman", "-Q", "--search", "^linux-(aarch64|rpi|rt-arm|armv7)$")
	if err == nil {
		f.r.info("installed kernel: %s", strings.ReplaceAll(out, "\n", " "))
	}
	if err := f.rebuildInitramfs(); err != nil {
		return err
	}
	return f.checkBootFiles()
}

// rebuildInitramfs regenerates /boot/initramfs-linux.img without autodetect.
//
// arch-chroot bind-mounts the HOST's /sys, so the pacman hook's mkinitcpio
// "autodetects" the machine doing the flashing: with an NVIDIA GPU on the
// host, nouveau.ko and every NVIDIA firmware blob end up in the Pi's
// initramfs (133 MiB instead of ~25). Skipping autodetect (and kms, which
// pulls GPU drivers + firmware) gives a host-independent image; the first
// kernel update on the Pi regenerates it with real autodetection.
func (f *flasher) rebuildInitramfs() error {
	entries, err := os.ReadDir(filepath.Join(f.root, "usr", "lib", "modules"))
	if err != nil {
		return err
	}
	// modules.builtin is shipped by every kernel package (linux-rt-arm has no
	// pkgbase file); a removed kernel leaves only depmod's generated files.
	var kver string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(f.root, "usr", "lib", "modules", e.Name(), "modules.builtin")); err == nil {
			if kver != "" {
				return fmt.Errorf("several kernels installed (%s, %s)", kver, e.Name())
			}
			kver = e.Name()
		}
	}
	if kver == "" {
		return fmt.Errorf("no installed kernel found in /usr/lib/modules")
	}
	f.r.info("rebuilding initramfs for %s without host autodetection", kver)
	if err := f.chroot("mkinitcpio", "-k", kver, "-g", "/boot/initramfs-linux.img", "-S", "autodetect,kms"); err != nil {
		return err
	}
	if fi, err := os.Stat(filepath.Join(f.root, "boot", "initramfs-linux.img")); err == nil {
		f.r.info("initramfs-linux.img: %d MiB", fi.Size()>>20)
	}
	return nil
}

// checkBootFiles verifies that /boot has what the Pi's boot chain loads.
func (f *flasher) checkBootFiles() error {
	boot := filepath.Join(f.root, "boot")
	need := []string{"initramfs-linux.img", "config.txt", "start4.elf", "fixup4.dat"}
	if f.cfg.Arch == "aarch64" {
		// firmware -> kernel8.img (U-Boot) -> boot.scr -> Image + dtbs/<fdtfile>
		need = append(need, "kernel8.img", "boot.scr", "Image", "dtbs/broadcom/bcm2711-rpi-4-b.dtb")
	} else {
		need = append(need, "cmdline.txt", "bcm2711-rpi-4-b.dtb")
	}
	var missing []string
	if f.cfg.Arch == "armv7" {
		// the firmware tries kernel7l.img (LPAE) then kernel7.img on a Pi 4
		_, e1 := os.Stat(filepath.Join(boot, "kernel7l.img"))
		_, e2 := os.Stat(filepath.Join(boot, "kernel7.img"))
		if e1 != nil && e2 != nil {
			missing = append(missing, "kernel7l.img|kernel7.img")
		}
	}
	for _, n := range need {
		if _, err := os.Stat(filepath.Join(boot, n)); err != nil {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("/boot is missing %s; the Pi would not boot", strings.Join(missing, ", "))
	}
	return nil
}
