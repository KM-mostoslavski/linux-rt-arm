package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

const alarmBase = "http://os.archlinuxarm.org/os/"

func tarballName(arch string) string {
	return fmt.Sprintf("ArchLinuxARM-rpi-%s-latest.tar.gz", arch)
}

// fetchTarball makes sure an up-to-date, md5-verified rootfs tarball is in the
// cache and returns its path. The .md5 is always re-fetched, so a stale
// "-latest" tarball from an earlier run is detected and replaced.
func fetchTarball(r *runner, cacheDir, arch string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	name := tarballName(arch)
	path := filepath.Join(cacheDir, name)

	sumFile, err := httpGet(alarmBase + name + ".md5")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(sumFile))
	if len(fields) == 0 || len(fields[0]) != 32 {
		return "", fmt.Errorf("unexpected md5 file content: %q", sumFile)
	}
	want := fields[0]

	if got, err := md5File(path); err == nil && got == want {
		r.info("cached %s is current (md5 %s)", name, want)
		return path, nil
	}

	r.info("downloading %s", alarmBase+name)
	if err := download(alarmBase+name, path+".part"); err != nil {
		return "", err
	}
	got, err := md5File(path + ".part")
	if err != nil {
		return "", err
	}
	if got != want {
		os.Remove(path + ".part")
		return "", fmt.Errorf("md5 mismatch for %s: got %s, want %s", name, got, want)
	}
	r.info("md5 verified: %s", got)
	return path, os.Rename(path+".part", path)
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	pw := &progressWriter{total: resp.ContentLength, last: time.Now()}
	if _, err := io.Copy(f, io.TeeReader(resp.Body, pw)); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	return f.Sync()
}

type progressWriter struct {
	total, done int64
	last        time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if time.Since(p.last) > 500*time.Millisecond || p.done == p.total {
		p.last = time.Now()
		if p.total > 0 {
			fmt.Fprintf(os.Stderr, "\r    %s / %s (%d%%)   ", humanize.IBytes(uint64(p.done)),
				humanize.IBytes(uint64(p.total)), p.done*100/p.total)
		} else {
			fmt.Fprintf(os.Stderr, "\r    %s   ", humanize.IBytes(uint64(p.done)))
		}
	}
	return len(b), nil
}

func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
