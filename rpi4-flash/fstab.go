package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Why not `sed -i 's/mmcblk0/mmcblk1/g' root/etc/fstab` (the wiki step)?
//
// The SD card's name is not stable. The mainline bcm2711 device tree has no
// mmc0/mmc1 aliases, so mmcblk0 vs mmcblk1 depends on which SDHCI controller
// probes first; the downstream linux-rpi kernel names it mmcblk0; booted from
// USB it is sda. Any hard-coded name breaks on one of these setups (the wiki
// sed is exactly that: /boot fails to mount and systemd drops to emergency
// mode). Filesystem UUIDs do not depend on probe order, so fstab uses them.

// fstabEntry is one line we want in /etc/fstab, keyed by its mountpoint.
type fstabEntry struct {
	Source, Mount, FSType, Options string
	Dump, Pass                     int
}

func (e fstabEntry) String() string {
	return fmt.Sprintf("%-42s %-8s %-6s %-10s %d %d", e.Source, e.Mount, e.FSType, e.Options, e.Dump, e.Pass)
}

// rewriteFstab replaces the lines whose mountpoint (or, for swap, source)
// matches one of entries, and appends the entries that were not present.
// Comments and unrelated lines are kept as they are.
func rewriteFstab(orig string, entries []fstabEntry) string {
	done := make([]bool, len(entries))
	var out []string
	for _, line := range strings.Split(strings.TrimRight(orig, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || strings.HasPrefix(f[0], "#") {
			out = append(out, line)
			continue
		}
		replaced := false
		for i, e := range entries {
			if f[1] == e.Mount && (e.Mount != "none" || f[0] == e.Source) {
				if !done[i] {
					out = append(out, e.String())
					done[i] = true
				}
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, line)
		}
	}
	for i, e := range entries {
		if !done[i] {
			out = append(out, e.String())
		}
	}
	return strings.Join(out, "\n") + "\n"
}

var rootArg = regexp.MustCompile(`(^|\s)root=\S+`)

// rewriteCmdline points root= at a PARTUUID, for the same reason as above.
// Only relevant to firmware-direct boots (cmdline.txt); the aarch64 U-Boot
// script already resolves the root PARTUUID itself.
func rewriteCmdline(cmdline, partuuid string) string {
	cmdline = strings.TrimSpace(cmdline)
	if rootArg.MatchString(cmdline) {
		return rootArg.ReplaceAllString(cmdline, "${1}root=PARTUUID="+partuuid) + "\n"
	}
	return cmdline + " root=PARTUUID=" + partuuid + "\n"
}
