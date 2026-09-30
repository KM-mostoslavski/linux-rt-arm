// rpi4-flash writes Arch Linux ARM to an SD card / USB disk for a Raspberry Pi 4.
//
// It follows https://archlinuxarm.org/platforms/armv8/broadcom/raspberry-pi-4
// with that page's known flaws fixed (see README.md): only four questions
// are asked, everything else is automated.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
)

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func main() {
	cfg := config{Arch: "aarch64", Kernel: kernelRT, Swap: swapNone}
	var yes, verbose, includeLoop bool
	flag.StringVar(&cfg.Arch, "arch", "", "aarch64 | armv7 (asked when empty)")
	flag.StringVar(&cfg.Device, "device", "", "target disk, e.g. /dev/sdb (asked when empty)")
	flag.StringVar(&cfg.Kernel, "kernel", "", kernelRT+" | "+kernelLatest+" (asked when empty)")
	flag.StringVar(&cfg.Swap, "swap", "", "none | file | partition (asked when empty)")
	flag.StringVar(&cfg.CacheDir, "cache-dir", "/var/cache/rpi4-flash", "where rootfs tarballs are kept between runs")
	flag.StringVar(&cfg.RTPkg, "rt-pkg", "", "linux-rt-arm package file to install (default: searched next to the binary and in the working directory)")
	flag.StringVar(&cfg.LogPath, "log", "", "log file (default: <cache-dir>/rpi4-flash.log)")
	flag.BoolVar(&yes, "yes", false, "do not ask for the final erase confirmation")
	flag.BoolVar(&verbose, "verbose", false, "stream command output to the terminal")
	flag.BoolVar(&includeLoop, "include-loop", false, "also offer loop devices (for testing with disk images)")
	flag.Parse()

	if err := run(&cfg, yes, verbose, includeLoop); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Fprintln(os.Stderr, "aborted, nothing was written")
			os.Exit(130)
		}
		if errors.Is(err, errInterrupted) {
			fmt.Fprintln(os.Stderr, errStyle.Render("INTERRUPTED: ")+cfg.Device+
				" was unmounted but may be partially written; run rpi4-flash again before using it")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, errStyle.Render("ERROR: ")+err.Error())
		if cfg.LogPath != "" {
			fmt.Fprintln(os.Stderr, "full log: "+cfg.LogPath)
		}
		os.Exit(1)
	}
}

func run(cfg *config, yes, verbose, includeLoop bool) error {
	if os.Geteuid() != 0 {
		return errors.New("must run as root: sudo " + os.Args[0])
	}
	if err := ask(cfg, includeLoop); err != nil {
		return err
	}
	if !yes {
		confirm := false
		err := huh.NewConfirm().
			Title(fmt.Sprintf("Erase ALL data on %s and install Arch Linux ARM %s?", cfg.Device, cfg.Arch)).
			Description(fmt.Sprintf("kernel: %s, swap: %s, boot: 1 GiB", cfg.Kernel, cfg.Swap)).
			Affirmative("Erase and flash").Negative("Abort").
			Value(&confirm).Run()
		if err != nil {
			return err
		}
		if !confirm {
			return huh.ErrUserAborted
		}
	}

	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return err
	}
	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(cfg.CacheDir, "rpi4-flash.log")
	}
	// From here on Ctrl+C / SIGTERM stop the current command and the run
	// ends through the normal unmount path instead of dying with the card
	// still mounted.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	r, err := newRunner(ctx, cfg.LogPath, verbose)
	if err != nil {
		return err
	}
	start := time.Now()
	f := &flasher{cfg: *cfg, r: r}
	if err := f.flash(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, okStyle.Render(fmt.Sprintf("\nDone in %s. %s can be removed and put in the Pi 4.",
		time.Since(start).Round(time.Second), cfg.Device)))
	fmt.Fprintln(os.Stderr, "Log in as alarm/alarm or root/root (change both passwords).")
	return nil
}

// ask fills whatever was not given on the command line with huh forms.
func ask(cfg *config, includeLoop bool) error {
	var groups []*huh.Group
	if cfg.Arch == "" {
		cfg.Arch = "aarch64"
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title("Which build?").
			Options(
				huh.NewOption("aarch64 (64-bit, mainline kernel + U-Boot)", "aarch64"),
				huh.NewOption("armv7h (32-bit, Raspberry Pi kernel)", "armv7"),
			).Value(&cfg.Arch)))
	}
	if cfg.Device == "" {
		disks, err := candidateDisks(includeLoop)
		if err != nil {
			return err
		}
		if len(disks) == 0 {
			return errors.New("no candidate disk found (disks holding the running system are never offered)")
		}
		var opts []huh.Option[string]
		for _, d := range disks {
			opts = append(opts, huh.NewOption(d.label(), d.Path))
		}
		cfg.Device = disks[0].Path
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title("Which device? (it will be ERASED)").
			Description("Disks holding the running system are not listed.").
			Options(opts...).Value(&cfg.Device)))
	}
	if cfg.Kernel == "" {
		cfg.Kernel = kernelRT
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title("Which kernel?").
			Options(
				huh.NewOption("7.2.7 (the version the linux-rt-arm PKGBUILD targets)", kernelRT),
				huh.NewOption("latest", kernelLatest),
			).Value(&cfg.Kernel)))
	}
	if cfg.Swap == "" {
		cfg.Swap = swapNone
		groups = append(groups, huh.NewGroup(huh.NewSelect[string]().
			Title("Swap?").
			Options(
				huh.NewOption("no swap", swapNone),
				huh.NewOption("1 GiB swap file (/swapfile)", swapFile),
				huh.NewOption("1 GiB swap partition", swapPartition),
			).Value(&cfg.Swap)))
	}
	if len(groups) > 0 {
		if err := huh.NewForm(groups...).Run(); err != nil {
			return err
		}
	}
	return validate(cfg)
}

func validate(cfg *config) error {
	switch cfg.Arch {
	case "aarch64", "armv7":
	case "armv7h":
		cfg.Arch = "armv7"
	default:
		return fmt.Errorf("unknown arch %q", cfg.Arch)
	}
	switch cfg.Kernel {
	case kernelRT, kernelLatest:
	default:
		return fmt.Errorf("unknown kernel %q", cfg.Kernel)
	}
	switch cfg.Swap {
	case swapNone, swapFile, swapPartition:
	default:
		return fmt.Errorf("unknown swap mode %q", cfg.Swap)
	}
	_, err := validateTarget(cfg.Device)
	return err
}
