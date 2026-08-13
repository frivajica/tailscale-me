// Shared network, checksum, version and validation helpers used by every
// platform. Everything here is OS-agnostic so it can be unit-tested anywhere.

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// stableIndex is a var (not const) so tests can point it at a httptest server.
var stableIndex = "https://pkgs.tailscale.com/stable/"

const (
	// v1.44.3 is the final Tailscale release supporting Windows 7/8. Keep this
	// pinned: upgrading it silently bricks legacy machines.
	legacyMSIURL    = "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi"
	legacyMSISHA256 = "d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"

	downloadTimeout = 120 * time.Second
	servicePollS    = 90 * time.Second
	serviceTick     = 2 * time.Second

	// maxDownloadBytes bounds any fetched artifact so a bad/mittm'd response
	// cannot fill the disk before SHA-256 verification. No official Tailscale
	// installer comes anywhere near this.
	maxDownloadBytes = 1 << 29 // 512 MiB

	// retries for transient network failures (downloads + index fetches).
	downloadRetries = 3
	retrySleep      = 2 * time.Second
)

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
	got := sha256Hex(b)
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(dest)
		return "", fmt.Errorf("SHA-256 mismatch: expected %s, got %s", wantSHA, got)
	}
	return dest, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

// ---- Polling ----------------------------------------------------------------

// pollUntil blocks until cond() returns true or the timeout elapses.
func pollUntil(cond func() bool, tick, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(tick)
	}
	return cond()
}

// ---- Build-time config guards ----------------------------------------------

// placeholderAuthKey marks a binary built without a real auth key; the build
// scripts leave it in place when .authkey is missing.
const placeholderAuthKey = "tskey-auth-YOUR_AUTH_KEY_HERE"

// checkAuthKeyReady aborts before any network or install work if the embedded
// key is the placeholder, so a mis-built binary fails loudly and fast instead
// of calling `tailscale up` with a garbage key.
func checkAuthKeyReady() error {
	if authKey == "" || authKey == placeholderAuthKey || strings.HasPrefix(authKey, "tskey-auth-YOUR_") {
		return fmt.Errorf("this binary was built without a Tailscale auth key.\n" +
			"Rebuild with build.sh/build.bat after creating a .authkey file (see README § Store the key).")
	}
	return nil
}

// ---- Pure parsers (unit-testable, no process I/O) ---------------------------

// parseOSRelease extracts PRETTY_NAME="…" from /etc/os-release.
func parseOSRelease(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return ""
}

var winVerRe = regexp.MustCompile(`(\d+)\.(\d+)\.\d+`)

// parseWindowsVer extracts (major, minor) from `cmd /c ver` output. Windows 11
// still reports itself as 10.0.x, which is fine: the legacy check only cares
// about 6.x.
func parseWindowsVer(text string) (major, minor int, err error) {
	m := winVerRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0, 0, fmt.Errorf("could not parse version from %q", text)
	}
	major, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(m[2])
	return major, minor, err
}

var goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)`)

// parseGoVersion extracts (major, minor) from `go version` output. ok is false
// when the string cannot be parsed.
func parseGoVersion(text string) (major, minor int, ok bool) {
	m := goVersionRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// parseRouteInterface extracts the `interface:` value from `route -n get
// default`.
func parseRouteInterface(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	return ""
}

var svcNameRe = regexp.MustCompile(`^\(\d+\)(.+)$`)

// parseServiceForInterface maps an interface (en0) to its network-service name
// using `networksetup -listnetworkserviceorder` output:
//
//	(1) Wi-Fi
//	(Hardware Port: Wi-Fi, Device: en0)
func parseServiceForInterface(orderText, iface string) string {
	lines := strings.Split(orderText, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		m := svcNameRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		for _, n := range lines[i+1:] {
			n = strings.TrimSpace(n)
			if n == "" {
				break
			}
			if strings.HasPrefix(n, "(Hardware Port") && strings.Contains(n, "Device: "+iface+")") {
				return name
			}
		}
	}
	return ""
}

// parseDNSServers parses `networksetup -getdnsservers` output. It returns nil
// for the "There aren't any DNS Servers…" case.
func parseDNSServers(text string) []string {
	if strings.Contains(text, "There aren't any DNS Servers") {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.EqualFold(line, "DNS Servers") {
			servers = append(servers, line)
		}
	}
	return servers
}

// extractTgz unpacks a verified .tgz into destDir, rejecting path-traversal
// entries (e.g. "../evil") and silently skipping non-regular entries. Used by
// the Linux installer so a compromised download cannot write outside the
// extraction directory.
func extractTgz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	base := filepath.Clean(destDir)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Join(base, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(name), base) {
			return fmt.Errorf("archive entry escapes target directory: %s", hdr.Name)
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(name, 0755); err != nil {
				return err
			}
			continue
		}
		if hdr.Typeflag == tar.TypeReg {
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}
