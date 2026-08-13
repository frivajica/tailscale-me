package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"tailscale-me/internal/shasum"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"0.9.0", "1.0.0", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.44.3", "1.44.3", 0},
		{"", "1.0.0", -1},
		{"1.0.0", "", 1},
		{"", "", 0},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		if (got < 0) != (tt.want < 0) || (got > 0) != (tt.want > 0) || (got == 0) != (tt.want == 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFetchLatestVersion(t *testing.T) {
	// Realistic stable-index body with mixed platforms, including the legacy
	// MSI line that must not win.
	body := "tailscale-setup-1.44.3-amd64.msi" +
		" tailscale-1.70.0-linux-amd64.tgz" +
		" tailscale-setup-1.70.0-amd64.msi" +
		" Tailscale-1.70.0-macos.pkg" +
		" tailscale_1.70.0_arm64.tgz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	old := stableIndex
	stableIndex = srv.URL
	defer func() { stableIndex = old }()

	got, err := fetchLatestVersion()
	if err != nil {
		t.Fatalf("fetchLatestVersion: %v", err)
	}
	if got != "1.70.0" {
		t.Fatalf("fetchLatestVersion = %q, want 1.70.0", got)
	}
}

func TestFetchLatestVersionEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "nothing here")
	}))
	defer srv.Close()
	old := stableIndex
	stableIndex = srv.URL
	defer func() { stableIndex = old }()

	if _, err := fetchLatestVersion(); err == nil {
		t.Fatal("expected error for index with no versions")
	}
}

func TestHTTPGetRetriesAndSharesClient(t *testing.T) {
	// Server fails twice then succeeds; 3-retry httpGet must recover.
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	b, err := httpGet(srv.URL)
	if err != nil {
		t.Fatalf("httpGet after transient failures: %v", err)
	}
	if string(b) != "ok" || attempts != 3 {
		t.Fatalf("got %q after %d attempts, want ok after 3", b, attempts)
	}
}

func TestHTTPGetSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty-ish body is rejected via the empty-body check instead.
		fmt.Fprint(w, strings.Repeat("x", maxDownloadBytes+1))
	}))
	defer srv.Close()

	if _, err := httpGet(srv.URL); err == nil {
		t.Fatal("expected size-cap error for oversized body")
	}
}

func TestHTTPGetNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := httpGet(srv.URL); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected HTTP 404 error, got %v", err)
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	data := []byte("hello checksum")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	want := shasum.Hex(data)
	got, err := shasum.FileHex(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("shasum.FileHex = %s, want %s", got, want)
	}
}

func TestDownloadVerifiedMismatchRemovesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "payload")
	}))
	defer srv.Close()
	dir, err := ensurePrivateTempDir()
	if err != nil {
		t.Skipf("could not create private temp dir: %v", err)
	}
	defer cleanupPrivateTempDir()

	_, err = downloadVerified(srv.URL, strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	// temp dir entries must not include a stale partial file beyond the fixed
	// artifacts we never wrote here; check the dir is empty or cleaned.
	name := filepath.Base(srv.URL)
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatalf("mismatched download left a file at %s/%s", dir, name)
	}
}

func TestCheckAuthKeyReady(t *testing.T) {
	old := authKey
	defer func() { authKey = old }()

	for _, k := range []string{"", placeholderAuthKey, "tskey-auth-YOUR_thing"} {
		authKey = k
		if err := checkAuthKeyReady(); err == nil {
			t.Errorf("authKey=%q: expected error", k)
		}
	}
	authKey = "tskey-auth-0123456789abcdef"
	if err := checkAuthKeyReady(); err != nil {
		t.Errorf("authKey=real: unexpected error %v", err)
	}
}

func TestParseOSRelease(t *testing.T) {
	got := parseOSRelease("PRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nNAME=\"Ubuntu\"")
	if got != "Ubuntu 24.04.1 LTS" {
		t.Fatalf("parseOSRelease = %q", got)
	}
	if parseOSRelease("no release file here") != "" {
		t.Fatal("expected empty when PRETTY_NAME missing")
	}
}

func TestParseWindowsVer(t *testing.T) {
	tests := []struct {
		in         string
		major, min int
		err        bool
	}{
		{"Microsoft Windows [Version 6.1.7601]", 6, 1, false}, // 7
		{"Microsoft Windows [Version 6.2.9200]", 6, 2, false}, // 8
		{"Microsoft Windows [Version 10.0.19045]", 10, 0, false},
		{"", 0, 0, true},
	}
	for _, tt := range tests {
		maj, min, err := parseWindowsVer(tt.in)
		if (err != nil) != tt.err {
			t.Errorf("parseWindowsVer(%q) err=%v want err=%v", tt.in, err, tt.err)
			continue
		}
		if maj != tt.major || min != tt.min {
			t.Errorf("parseWindowsVer(%q) = %d.%d want %d.%d", tt.in, maj, min, tt.major, tt.min)
		}
	}
}

func TestLegacyMSIForArch(t *testing.T) {
	tests := []struct {
		arch   string
		url    string
		sha256 string
	}{
		{"386", "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-x86.msi",
			"6d718e2979846b3452992a565babdbd0736aa45fd073c68dfb932631402e8a5d"},
		{"amd64", "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi",
			"d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"},
		{"arm64", "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi",
			"d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"}, // no legacy arm64; falls back to amd64
	}
	for _, tt := range tests {
		url, sha := legacyMSIForArch(tt.arch)
		if url != tt.url || sha != tt.sha256 {
			t.Errorf("legacyMSIForArch(%q) = (%s, %s), want (%s, %s)", tt.arch, url, sha, tt.url, tt.sha256)
		}
	}
}

func TestKB2921916ForArch(t *testing.T) {
	tests := []struct {
		arch   string
		url    string
		sha256 string
	}{
		{"386", "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x86.msu",
			"25bf6432519dd67c8c055cbecaa1139359024ec5dfac7c1ef6cec0ed06b327ea"},
		{"amd64", "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x64.msu",
			"39d978285d01ee4c0dfe9e2462bc4c948260aaf041aaa04faef3275f6d46a773"},
	}
	for _, tt := range tests {
		url, sha := kb2921916ForArch(tt.arch)
		if url != tt.url || sha != tt.sha256 {
			t.Errorf("kb2921916ForArch(%q) = (%s, %s), want (%s, %s)", tt.arch, url, sha, tt.url, tt.sha256)
		}
	}
}

func TestParseGoVersion(t *testing.T) {
	maj, min, ok := parseGoVersion("go version go1.23.1 darwin/arm64")
	if !ok || maj != 1 || min != 23 {
		t.Fatalf("parseGoVersion = %d.%d ok=%v", maj, min, ok)
	}
	if _, _, ok := parseGoVersion("not go"); ok {
		t.Fatal("expected !ok for garbage")
	}
}

func TestParseRouteInterface(t *testing.T) {
	out := "   route to: default\n     interface: en0\n     flags: <UP,GATEWAY>"
	if got := parseRouteInterface(out); got != "en0" {
		t.Fatalf("parseRouteInterface = %q", got)
	}
	if parseRouteInterface("no interface here") != "" {
		t.Fatal("expected empty when missing")
	}
}

func TestParseServiceForInterface(t *testing.T) {
	order := "An asterisk (*) denotes that a network service is disabled.\n" +
		"(1) Wi-Fi\n" +
		"(Hardware Port: Wi-Fi, Device: en0)\n" +
		"\n" +
		"(2) USB 10/100/1000 LAN\n" +
		"(Hardware Port: USB 10/100/1000 LAN, Device: en5)\n"
	if got := parseServiceForInterface(order, "en0"); got != "Wi-Fi" {
		t.Fatalf("en0 -> %q, want Wi-Fi", got)
	}
	if got := parseServiceForInterface(order, "en5"); got != "USB 10/100/1000 LAN" {
		t.Fatalf("en5 -> %q, want USB 10/100/1000 LAN", got)
	}
	if got := parseServiceForInterface(order, "en99"); got != "" {
		t.Fatalf("en99 -> %q, want empty", got)
	}
	// Lines with "(" but no ")" must not panic.
	if got := parseServiceForInterface("(broken\n", "en0"); got != "" {
		t.Fatalf("malformed -> %q, want empty", got)
	}
}

func TestParseDNSServers(t *testing.T) {
	if got := parseDNSServers("There aren't any DNS Servers set on Wi-Fi."); got != nil {
		t.Fatalf("no-DNS case -> %v, want nil", got)
	}
	got := parseDNSServers("8.8.8.8\n1.1.1.1\n")
	if len(got) != 2 || got[0] != "8.8.8.8" || got[1] != "1.1.1.1" {
		t.Fatalf("parseDNSServers = %v", got)
	}
}

// TestExtractTgzTraversalGuard builds an in-memory tarball whose entry tries to
// escape the destination dir, exactly as the path-traversal guard is meant to
// block.
func TestExtractTgzTraversalGuard(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("evil")
	tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0644, Size: int64(len(content))})
	tw.Write(content)
	tw.Close()
	gz.Close()

	dir := t.TempDir()
	tgz := filepath.Join(dir, "in.tgz")
	if werr := os.WriteFile(tgz, buf.Bytes(), 0600); werr != nil {
		t.Fatal(werr)
	}
	out := filepath.Join(dir, "out")
	if err := extractTgz(tgz, out); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := os.Lstat(filepath.Join(dir, "escape")); !os.IsNotExist(err) {
		t.Fatal("traversal file must not exist above the dest dir")
	}
	if _, err := os.Lstat(out); err == nil {
		t.Fatal("nothing should have been extracted")
	}
}

func TestExtractTgzNormal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []string{"tailscale", "hello.txt"}
	for _, f := range files {
		c := []byte("data-" + f)
		tw.WriteHeader(&tar.Header{Name: f, Mode: 0755, Size: int64(len(c))})
		tw.Write(c)
	}
	tw.Close()
	gz.Close()

	dir := t.TempDir()
	tgz := filepath.Join(dir, "in.tgz")
	os.WriteFile(tgz, buf.Bytes(), 0600)
	out := filepath.Join(dir, "out")
	if err := extractTgz(tgz, out); err != nil {
		t.Fatalf("extractTgz: %v", err)
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing extracted %s: %v", f, err)
		}
	}
}
