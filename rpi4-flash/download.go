package main

import (
	"context"
	"crypto/md5"
	_ "embed"
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

// The rootfs is only served over plain HTTP, and its .md5 comes from the same
// place: that catches corruption, not tampering. Unlike the wiki's procedure,
// rpi4-flash runs programs from the tarball as root on this machine (pacman
// in an arch-chroot), so the tarball's detached signature is checked against
// the Arch Linux ARM build system key, pinned here by fingerprint
// (https://archlinuxarm.org/about/package-signing).
const alarmBuilderFpr = "68B3537F39A313B3E574D06777193F152BDBE6A6"

//go:embed alarm-builder.gpg
var alarmBuilderKey []byte

func tarballName(arch string) string {
	return fmt.Sprintf("ArchLinuxARM-rpi-%s-latest.tar.gz", arch)
}

// fetchTarball makes sure an up-to-date, verified rootfs tarball is in the
// cache and returns its path. The .md5 is always re-fetched, so a stale
// "-latest" tarball from an earlier run is detected and replaced.
func fetchTarball(r *runner, cacheDir, arch string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	name := tarballName(arch)
	path := filepath.Join(cacheDir, name)

	sumFile, err := httpGet(r.ctx, alarmBase+name+".md5")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(sumFile))
	if len(fields) == 0 || len(fields[0]) != 32 {
		return "", fmt.Errorf("unexpected md5 file content: %q", sumFile)
	}
	want := fields[0]
	sig, err := httpGet(r.ctx, alarmBase+name+".sig")
	if err != nil {
		return "", err
	}
	sigPath := path + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		return "", err
	}

	if got, err := md5File(path); err == nil && got == want {
		r.info("cached %s is current (md5 %s)", name, want)
		return path, verifySignature(r, path, sigPath)
	}

	r.info("downloading %s", alarmBase+name)
	part := path + ".part"
	if err := download(r.ctx, alarmBase+name, part); err != nil {
		return "", err
	}
	got, err := md5File(part)
	if err != nil {
		return "", err
	}
	if got != want {
		os.Remove(part)
		return "", fmt.Errorf("md5 mismatch for %s: got %s, want %s", name, got, want)
	}
	r.info("md5 verified: %s", got)
	if err := verifySignature(r, part, sigPath); err != nil {
		os.Remove(part)
		return "", err
	}
	return path, os.Rename(part, path)
}

// verifySignature checks the detached signature of file with gpgv against
// the embedded build system key only (no user keyring, no trust database).
func verifySignature(r *runner, file, sigPath string) error {
	dir, err := os.MkdirTemp("", "rpi4-flash-key-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	keyring := filepath.Join(dir, "alarm-builder.gpg")
	if err := os.WriteFile(keyring, alarmBuilderKey, 0o600); err != nil {
		return err
	}
	out, err := r.capture("gpgv", "--keyring", keyring, "--status-fd", "1", sigPath, file)
	if err != nil {
		if err == errInterrupted {
			return err
		}
		return fmt.Errorf("signature check of %s FAILED: not signed by the Arch Linux ARM build system key (%s)",
			filepath.Base(file), alarmBuilderFpr)
	}
	if !validSig(out, alarmBuilderFpr) {
		return fmt.Errorf("signature check of %s FAILED: unexpected gpgv status:\n%s", filepath.Base(file), out)
	}
	r.info("signature verified: Arch Linux ARM Build System (%s)", alarmBuilderFpr)
	return nil
}

// validSig reports whether gpgv's status output holds a VALIDSIG made by the
// primary key fpr, and nothing that revokes or expires it.
func validSig(status, fpr string) bool {
	ok := false
	for _, line := range strings.Split(status, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "[GNUPG:]" {
			continue
		}
		switch f[1] {
		case "VALIDSIG":
			// last field: fingerprint of the primary key
			ok = f[len(f)-1] == fpr
		case "EXPSIG", "EXPKEYSIG", "REVKEYSIG", "BADSIG", "ERRSIG":
			return false
		}
	}
	return ok
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errInterrupted
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func download(ctx context.Context, url, dest string) error {
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	pw := &progressWriter{total: resp.ContentLength, last: time.Now()}
	_, err = io.Copy(f, io.TeeReader(resp.Body, pw))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		if ctx.Err() != nil {
			return errInterrupted
		}
		return err
	}
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
