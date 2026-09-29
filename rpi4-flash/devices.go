package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/dustin/go-humanize"
)

// blockDev mirrors the subset of `lsblk --json` output we care about.
type blockDev struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Size        uint64     `json:"size"`
	Type        string     `json:"type"`
	Tran        string     `json:"tran"`
	RM          bool       `json:"rm"`
	Model       string     `json:"model"`
	Vendor      string     `json:"vendor"`
	Mountpoints []string   `json:"mountpoints"`
	Children    []blockDev `json:"children"`
}

// systemMounts are mountpoints that mark a disk as the running system's disk.
var systemMounts = map[string]bool{
	"/": true, "/boot": true, "/efi": true, "/boot/efi": true,
	"/usr": true, "/var": true, "/home": true, "[SWAP]": true,
}

func lsblk(args ...string) ([]blockDev, error) {
	cmd := exec.Command("lsblk", append([]string{
		"--json", "--bytes",
		"--output", "NAME,PATH,SIZE,TYPE,TRAN,RM,MODEL,VENDOR,MOUNTPOINTS",
	}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk: %w", err)
	}
	var parsed struct {
		Blockdevices []blockDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parsing lsblk output: %w", err)
	}
	return parsed.Blockdevices, nil
}

// mounts returns every non-empty mountpoint in d and its children.
func (d blockDev) mounts() []string {
	var ms []string
	for _, m := range d.Mountpoints {
		if m != "" {
			ms = append(ms, m)
		}
	}
	for _, c := range d.Children {
		ms = append(ms, c.mounts()...)
	}
	return ms
}

func (d blockDev) isSystemDisk() bool {
	for _, m := range d.mounts() {
		if systemMounts[m] {
			return true
		}
	}
	return false
}

func (d blockDev) label() string {
	desc := strings.TrimSpace(strings.TrimSpace(d.Vendor) + " " + strings.TrimSpace(d.Model))
	if desc == "" {
		desc = "unknown model"
	}
	extra := []string{}
	if d.Tran != "" {
		extra = append(extra, d.Tran)
	}
	if d.RM {
		extra = append(extra, "removable")
	}
	if ms := d.mounts(); len(ms) > 0 {
		extra = append(extra, "mounted: "+strings.Join(ms, ","))
	}
	return fmt.Sprintf("%-14s %8s  %s  [%s]", d.Path, humanize.IBytes(d.Size), desc, strings.Join(extra, ", "))
}

// candidateDisks lists whole disks that can be flashed: the disk(s) holding
// the running system, zero-sized readers, zram and optical drives are left out.
func candidateDisks(includeLoop bool) ([]blockDev, error) {
	devs, err := lsblk("--nodeps")
	if err != nil {
		return nil, err
	}
	// --nodeps drops children, so query the tree to know what is mounted.
	tree, err := lsblk()
	if err != nil {
		return nil, err
	}
	byPath := map[string]blockDev{}
	for _, d := range tree {
		byPath[d.Path] = d
	}
	var out []blockDev
	for _, d := range devs {
		full := byPath[d.Path]
		switch {
		case d.Type == "loop" && !includeLoop,
			d.Type != "disk" && d.Type != "loop",
			d.Size == 0,
			strings.HasPrefix(d.Name, "zram"),
			full.isSystemDisk():
			continue
		}
		out = append(out, full)
	}
	return out, nil
}

// validateTarget re-checks a device path given on the command line.
func validateTarget(path string) (blockDev, error) {
	devs, err := lsblk(path)
	if err != nil {
		return blockDev{}, err
	}
	if len(devs) != 1 {
		return blockDev{}, fmt.Errorf("%s: not a single block device", path)
	}
	d := devs[0]
	if d.Type != "disk" && d.Type != "loop" {
		return blockDev{}, fmt.Errorf("%s is a %s, expected a whole disk", path, d.Type)
	}
	if d.isSystemDisk() {
		return blockDev{}, fmt.Errorf("%s holds the running system (%s), refusing", path, strings.Join(d.mounts(), ", "))
	}
	return d, nil
}

// partitions returns the partition device paths of disk, in table order.
func partitions(disk string) ([]string, error) {
	devs, err := lsblk(disk)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, d := range devs {
		for _, c := range d.Children {
			if c.Type == "part" {
				parts = append(parts, c.Path)
			}
		}
	}
	return parts, nil
}
