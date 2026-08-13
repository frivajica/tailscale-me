// Downloads and integrity: fetching installers, checksums and the package
// index into a private 0700 temp dir, all SHA-256 verified before install.

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"tailscale-me/internal/shasum"
	"time"
)

// stableIndex is a var (not const) so tests can point it at a httptest server.
var stableIndex = "https://pkgs.tailscale.com/stable/"

const (
	// v1.44.3 is the final Tailscale release supporting Windows 7/8. Keep this
	// pinned: upgrading it silently bricks legacy machines.
	legacyMSIX86URL      = "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-x86.msi"
	legacyMSIX86SHA256   = "6d718e2979846b3452992a565babdbd0736aa45fd073c68dfb932631402e8a5d"
	legacyMSIAMD64URL    = "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi"
	legacyMSIAMD64SHA256 = "d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"

	// Windows 7 requires the KB2921916 hotfix for Go binaries to run. The
	// mirror has no public checksum, so we pin the hashes of the official
	// x86/x64 files.
	kb2921916X86URL    = "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x86.msu"
	kb2921916X86SHA256 = "25bf6432519dd67c8c055cbecaa1139359024ec5dfac7c1ef6cec0ed06b327ea"
	kb2921916X64URL    = "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x64.msu"
	kb2921916X64SHA256 = "39d978285d01ee4c0dfe9e2462bc4c948260aaf041aaa04faef3275f6d46a773"

	downloadTimeout = 120 * time.Second

	// maxDownloadBytes bounds any fetched artifact so a bad/mittm'd response
	// cannot fill the disk before SHA-256 verification. No official Tailscale
	// installer comes anywhere near this.
	maxDownloadBytes = 1 << 29 // 512 MiB

	// retries for transient network failures (downloads + index fetches).
	downloadRetries = 3
	retrySleep      = 2 * time.Second
)

// legacyMSIForArch returns the pinned v1.44.3 MSI matching the running Windows
// architecture. The 386 binary also runs on 64-bit Windows via WOW64, so it
// must download the x86 installer rather than the amd64 default.
func legacyMSIForArch(arch string) (url, wantSHA string) {
	if arch == "386" {
		return legacyMSIX86URL, legacyMSIX86SHA256
	}
	return legacyMSIAMD64URL, legacyMSIAMD64SHA256
}

// kb2921916ForArch returns the KB2921916 hotfix matching the running Windows
// architecture; x64 is the default for any arch without a dedicated hotfix.
func kb2921916ForArch(arch string) (url, wantSHA string) {
	if arch == "386" {
		return kb2921916X86URL, kb2921916X86SHA256
	}
	return kb2921916X64URL, kb2921916X64SHA256
}

// ---- Downloads & integrity --------------------------------------------------

// privateTempDir holds every artifact this run writes (instaler, hotfix, …).
// A dedicated 0700 directory avoids firing writes at predictable paths in the
// world-writable shared temp folder.
var privateTempDir string

func ensurePrivateTempDir() (string, error) {
	if privateTempDir == "" {
		d, err := os.MkdirTemp("", "tailscale-me-*")
		if err != nil {
			return "", err
		}
		privateTempDir = d
	}
	return privateTempDir, nil
}

func cleanupPrivateTempDir() {
	if privateTempDir != "" {
		os.RemoveAll(privateTempDir)
		privateTempDir = ""
	}
}

// artifactPath returns privateTempDir/<name>, creating it on first use.
func artifactPath(name string) (string, error) {
	dir, err := ensurePrivateTempDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// httpGet fetches a small artifact, retrying transient failures and enforcing
// the size cap. Returns the raw bytes.
func httpGet(url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < downloadRetries; attempt++ {
		b, err := httpGetOnce(url)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if attempt+1 < downloadRetries {
			time.Sleep(retrySleep)
		}
	}
	return nil, lastErr
}

func httpGetOnce(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %s", resp.Status)
	}
	limited := io.LimitReader(resp.Body, maxDownloadBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxDownloadBytes {
		return nil, fmt.Errorf("artifact exceeded the %d MiB size cap", maxDownloadBytes>>20)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("download returned an empty body")
	}
	return b, nil
}

// downloadVerified fetches url into the private temp dir and verifies its
// SHA-256, returning the local path. Any mismatch removes the file.
func downloadVerified(url, wantSHA string) (string, error) {
	dest, err := artifactPath(filepath.Base(url))
	if err != nil {
		return "", err
	}
	b, err := httpGet(url)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, b, 0600); err != nil {
		return "", err
	}
	got := shasum.Hex(b)
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(dest)
		return "", fmt.Errorf("SHA-256 mismatch: expected %s, got %s", wantSHA, got)
	}
	return dest, nil
}

// fetchSHA256 retrieves the pinned checksum Tailscale publishes next to each
// versioned artifact.
func fetchSHA256(url string) (string, error) {
	b, err := httpGet(url)
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(b))
	if len(hash) != 64 {
		return "", fmt.Errorf("invalid checksum payload %q", hash)
	}
	return hash, nil
}

// ---- Latest version resolution ---------------------------------------------

// fetchLatestVersion parses the newest stable release number from the package
// index. The pattern matches every platform's naming scheme, so one function
// serves Windows, macOS and Linux.
func fetchLatestVersion() (string, error) {
	b, err := httpGet(stableIndex)
	if err != nil {
		return "", fmt.Errorf("could not reach package index: %w", err)
	}
	re := regexp.MustCompile(`(?i)tailscale[_-](\d+\.\d+\.\d+)`)
	matches := re.FindAllStringSubmatch(string(b), -1)
	best := ""
	for _, m := range matches {
		if compareVersions(best, m[1]) < 0 {
			best = m[1]
		}
	}
	if best == "" {
		return "", fmt.Errorf("no Tailscale version found on the package index")
	}
	return best, nil
}

// compareVersions returns <0 if a < b, 0 if equal, >0 if a > b.
func compareVersions(a, b string) int {
	if a == "" {
		if b == "" {
			return 0
		}
		return -1
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ia, _ := strconv.Atoi(pa[i])
		ib, _ := strconv.Atoi(pb[i])
		if ia != ib {
			return ia - ib
		}
	}
	return 0
}
