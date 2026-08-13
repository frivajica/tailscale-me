package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- Package data filled at build time -------------------------------------

// authKey is injected at build time from the gitignored .authkey file via
// -ldflags "-X main.authKey=...". The placeholder here keeps the repo usable
// and is never shipped with a real key.
var authKey = "tskey-auth-YOUR_AUTH_KEY_HERE"

// subnetRoute is the LAN CIDR advertised to the tailnet. It MUST match the
// "routes" key in your ACL autoApprovers block and should NOT overlap your own
// home LAN.
const subnetRoute = "192.168.1.0/24"

// ---- Shared constants -------------------------------------------------------

const (
	stableIndex = "https://pkgs.tailscale.com/stable/"

	// v1.44.3 is the final Tailscale release supporting Windows 7/8. Keep this
	// pinned: upgrading it silently bricks legacy machines.
	legacyMSIURL    = "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi"
	legacyMSISHA256 = "d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"

	downloadTimeout = 120 * time.Second
	servicePollS    = 90 * time.Second
	serviceTick     = 2 * time.Second
)

var logFile *os.File

func main() {
	if err := initLog(); err != nil {
		fmt.Printf("[%s] WARNING: could not open log file: %v\n", ts(), err)
	}
	step("Welcome to the Tailscale family-business setup tool.")
	step("A full log of this session is saved to: %s", logPath())

	if err := ensureElevated(); err != nil {
		fatal("%s", err)
	}

	legacy := isLegacyPlatform()
	if legacy {
		step("Detected %s - using Tailscale v1.44.3 (final release for this OS).", platformDesc())
	} else {
		step("Detected %s - using the latest Tailscale release.", platformDesc())
	}
	preInstall(legacy)

	url, wantSHA, localName, err := installerArtifact(legacy)
	if err != nil {
		fatal("Installation package could not be prepared: %v", err)
	}
	dest := filepath.Join(os.TempDir(), localName)
	step("Downloading installer %s ...", url)
	if err := downloadFile(url, dest); err != nil {
		os.Remove(dest)
		fatal("Download failed: %v", err)
	}
	step("Verifying installer integrity (SHA-256) ...")
	got, err := sha256File(dest)
	if err != nil {
		os.Remove(dest)
		fatal("Could not hash the downloaded installer: %v", err)
	}
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(dest)
		fatal("SHA-256 mismatch: expected %s, got %s", wantSHA, got)
	}
	step("Installer verified.")

	if cli := findCLI(); cli != "" {
		step("Tailscale is already installed at: %s (skipping install).", cli)
	} else {
		if err := installPackage(dest); err != nil {
			os.Remove(dest)
			fatal("Installation failed: %v", err)
		}
	}
	os.Remove(dest)
	step("Temporary installation files cleaned up.")

	cli := findCLI()
	if cli == "" {
		fatal("The tailscale CLI was not found after installation. Check %s for the last steps.",
			logPath())
	}

	startDaemon(cli)
	waitDaemon(cli)
	runTailscaleUp(cli)

	pauseExit(0)
}

// ---- Platform hooks (implemented per-OS in platform_*.go) ------------------
//
//	ensureElevated() (error, string)  - verifies admin/root rights
//	isLegacyPlatform() bool           - legacy Windows 7/8 needs the pinned MSI
//	platformDesc() string             - human-readable OS description
//	preInstall(legacy bool)           - platform prep before the package step
//	packageBase() (string, error)     - Sprintf template for the package URL
//	packageLocalName() string         - temp file name to save the package as
//	installPackage(path) error        - install the downloaded package
//	findCLI() string                  - locate the tailscale CLI
//	startDaemon(cli)                  - best-effort daemon bring-up
//	waitDaemon(cli)                   - wait until the daemon is reachable
//	upArgs() []string                 - args for `tailscale up`

// ---- Logging ---------------------------------------------------------------

func ts() string { return time.Now().Format("2006-01-02 15:04:05") }

func logPath() string {
	if logFile == nil {
		return "(unavailable)"
	}
	return filepath.Join(os.TempDir(), "TailscaleMe.log")
}

func initLog() error {
	f, err := os.OpenFile(filepath.Join(os.TempDir(), "TailscaleMe.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	logFile = f
	return nil
}

// step echoes to the console and appends to the log so a non-technical user
// can screenshot any failure for diagnosis.
func step(format string, a ...interface{}) {
	line := fmt.Sprintf(format, a...)
	fmt.Println(line)
	if logFile != nil {
		logFile.WriteString(ts() + " " + line + "\n")
		logFile.Sync()
	}
}

func fatal(format string, a ...interface{}) {
	step("\nERROR: "+format, a...)
	step("If you need help, send a screenshot (or the file %s) to the setup owner.",
		logPath())
	pauseExit(1)
}

func pauseExit(code int) {
	fmt.Print("\nPress Enter to exit...")
	r := bufio.NewReader(os.Stdin)
	r.ReadString('\n') // EOF-safe: read failure still exits below
	if logFile != nil {
		logFile.Close()
	}
	os.Exit(code)
}

// ---- Download & checksum verification -------------------------------------

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(dest)
		return err
	}
	if n == 0 {
		os.Remove(dest)
		return fmt.Errorf("download returned an empty file")
	}
	return nil
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

// installerArtifact decides which package to fetch. Legacy Windows uses the
// pinned v1.44.3 MSI; everything else resolves the newest stable version from
// the package index and verifies against its versioned .sha256 endpoint (the
// "latest" aliases do not expose one). This keeps the tool auto-updated yet
// tamper-proof on every platform.
func installerArtifact(legacy bool) (url, wantSHA, localName string, err error) {
	if legacy {
		return legacyMSIURL, legacyMSISHA256, "tailscale-setup.msi", nil
	}
	version, err := fetchLatestVersion()
	if err != nil {
		return "", "", "", err
	}
	base, err := packageBase()
	if err != nil {
		return "", "", "", err
	}
	url = "https://pkgs.tailscale.com/stable/" + fmt.Sprintf(base, version)
	wantSHA, err = fetchSHA256(url + ".sha256")
	if err != nil {
		return "", "", "", err
	}
	return url, wantSHA, packageLocalName(), nil
}

// fetchLatestVersion parses the newest stable release number from the package
// index. The pattern matches every platform's naming scheme, so one function
// serves Windows, macOS and Linux.
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(stableIndex)
	if err != nil {
		return "", fmt.Errorf("could not reach package index: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?i)tailscale[_-](\d+\.\d+\.\d+)`)
	matches := re.FindAllStringSubmatch(string(body), -1)
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

func fetchSHA256(url string) (string, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	hash := strings.TrimSpace(string(b))
	if len(hash) != 64 {
		return "", fmt.Errorf("invalid checksum payload %q", hash)
	}
	return hash, nil
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

// ---- Connect & advertise routes -------------------------------------------

func runTailscaleUp(cli string) {
	step("Connecting to your tailnet and advertising subnet %s ...", subnetRoute)
	args := append(upArgs(), "--auth-key="+authKey, "--advertise-routes="+subnetRoute)
	cmd := exec.Command(cli, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if text != "" {
		step(text)
	}
	if err != nil {
		fatal("tailscale up failed: %v\n\"%s\"\nThis usually means the auth key is "+
			"expired/used (generate a fresh one) or the advertised subnet conflicts "+
			"with the local network.", err, text)
	}
	step("Tailscale is connected and advertising routes.")
}
