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

// ---- Configuration (edit before building) ---------------------------------

// authKey is your Tailscale auth key. Generate one at admin console →
// Settings → Keys, tagged with tag:family-biz so the ACL autoApprovers rule
// (see ACL_Configuration.json) matches. Prefer a single-use key with a short
// expiry since it is baked into this executable.
const authKey = "tskey-auth-YOUR_AUTH_KEY_HERE"

// subnetRoute is the LAN CIDR advertised to the tailnet. It MUST match the
// "routes" key in your ACL autoApprovers block and should NOT overlap your own
// home LAN.
const subnetRoute = "192.168.1.0/24"

// ---------------------------------------------------------------------------

const (
	latestMSIURL = "https://pkgs.tailscale.com/stable/tailscale-setup-latest-amd64.msi"
	stableIndex  = "https://pkgs.tailscale.com/stable/"

	// v1.44.3 is the final Tailscale release supporting Windows 7/8. Keep this
	// pinned: upgrading it silently bricks legacy machines.
	legacyMSIURL    = "https://pkgs.tailscale.com/stable/tailscale-setup-1.44.3-amd64.msi"
	legacyMSISHA256 = "d119ec69c3f4a38872a43345c95f87effff0d2285ae4a1fef37ff8adf5d50e58"

	// Windows 7 requires the KB2921916 hotfix for Go binaries to run. The
	// mirror has no public checksum, so we pin the hash of the official file.
	kb2921916URL    = "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x64.msu"
	kb2921916SHA256 = "39d978285d01ee4c0dfe9e2462bc4c948260aaf041aaa04faef3275f6d46a773"

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

	if !isElevated() {
		fatal("This tool must run as Administrator. Right-click TailscaleMe.exe " +
			"and choose \"Run as administrator\", then click Yes on the UAC prompt.")
	}

	major, minor, err := windowsVersion()
	if err != nil {
		fatal("Could not detect the Windows version: %v", err)
	}
	legacy := major == 6 // Windows 7 (6.1), 8 (6.2), 8.1 (6.3)
	if legacy {
		step("Detected legacy Windows %d.%d - using Tailscale v1.44.3 (final release for this OS).", major, minor)
	} else {
		step("Detected Windows %d.%d - using the latest Tailscale release.", major, minor)
	}

	if major == 6 && minor == 1 {
		installKB2921916()
	}

	msiPath, err := downloadInstaller(legacy)
	if err != nil {
		fatal("Installation package could not be prepared: %v", err)
	}

	if exe := findTailscale(); exe != "" {
		step("Tailscale is already installed at: %s (skipping msiexec).", exe)
	} else {
		if err := runMSI(msiPath); err != nil {
			os.Remove(msiPath)
			fatal("Silent installation failed: %v", err)
		}
	}
	os.Remove(msiPath)
	step("Temporary installation files cleaned up.")
	exe := findTailscale()
	if exe == "" {
		fatal("tailscale.exe was not found after installation. Check %s for the last steps.", logPath())
	}

	waitForService()
	runTailscaleUp(exe)

	pauseExit(0)
}

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

// ---- Environment checks ----------------------------------------------------

func isElevated() bool {
	return exec.Command("net", "session").Run() == nil
}

func windowsVersion() (major, minor int, err error) {
	out, err := exec.Command("cmd", "/c", "ver").CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("cmd /c ver failed: %v", err)
	}
	re := regexp.MustCompile(`(\d+)\.(\d+)\.\d+`)
	m := re.FindStringSubmatch(string(out))
	if len(m) != 3 {
		return 0, 0, fmt.Errorf("could not parse version from %q", out)
	}
	major, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(m[2])
	return major, minor, err
}

// ---- Windows 7 hotfix ------------------------------------------------------

// installKB2921916 downloads, verifies and silently applies the hotfix. It
// never forces a reboot: it warns first and only reboots after the user
// accepts the prompt.
func installKB2921916() {
	path := filepath.Join(os.TempDir(), "Windows6.1-KB2921916-x64.msu")
	step("Downloading required Windows 7 update KB2921916 ...")
	if err := downloadFile(kb2921916URL, path); err != nil {
		step("WARNING: could not download KB2921916: %v", err)
		step("If Tailscale fails to start afterwards, apply the update manually and reboot.")
		return
	}
	got, err := sha256File(path)
	if err != nil || !strings.EqualFold(got, kb2921916SHA256) {
		step("WARNING: KB2921916 checksum mismatch (%s), discarding file.", got)
		os.Remove(path)
		return
	}
	step("Applying KB2921916 (this takes a few minutes) ...")
	out, err := exec.Command("wusa", path, "/quiet", "/norestart").CombinedOutput()
	if len(out) > 0 {
		step("wusa output: %s", strings.TrimSpace(string(out)))
	}
	if err == nil {
		step("KB2921916 is installed - no reboot required.")
		return
	}
	rebootRequired := false
	if ee, ok := err.(*exec.ExitError); ok {
		// 3010 = success + reboot required; 0x80240017 = already installed.
		if ee.ExitCode() == 3010 || ee.ExitCode() == 0x80240017 {
			rebootRequired = true
		}
	}
	if !rebootRequired {
		step("WARNING: KB2921916 step returned %v (it may already be installed).", err)
		step("Continuing as best-effort; if Tailscale fails, apply the update manually and reboot.")
		return
	}
	step("KB2921916 was installed but Windows needs ONE restart to finish it.")
	fmt.Print("A restart will close this tool. Run TailscaleMe.exe again after " +
		"Windows comes back (it will skip straight to connecting).\n")
	fmt.Print("Restart now? (Y/N): ")
	ans := bufio.NewReader(os.Stdin)
	line, _ := ans.ReadString('\n')
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "Y") {
		step("Rebooting in 15 seconds. Re-run TailscaleMe.exe after restart.")
		exec.Command("shutdown", "/r", "/t", "15",
			"/c", "TailscaleMe: finishing the KB2921916 update.").Run()
		if logFile != nil {
			logFile.Close()
		}
		os.Exit(0)
	}
	step("No restart now. Please restart before the Tailscale step, then re-run this tool.")
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

// downloadInstaller picks the correct MSI for the OS, downloads it, resolves
// the expected SHA-256 (latest = fresh from the package index; legacy = pinned)
// and aborts on any mismatch.
func downloadInstaller(legacy bool) (string, error) {
	var url, wantSHA string
	if legacy {
		url, wantSHA = legacyMSIURL, legacyMSISHA256
	} else {
		verRaw, err := fetchRemoteSHA256()
		if err != nil {
			return "", err
		}
		url = fmt.Sprintf("https://pkgs.tailscale.com/stable/tailscale-setup-%s-amd64.msi", verRaw)
		wantSHA, err = fetchRemoteSHA256For(verRaw)
		if err != nil {
			return "", err
		}
	}

	dest := filepath.Join(os.TempDir(), "tailscale-setup.msi")
	step("Downloading installer %s ...", url)
	if err := downloadFile(url, dest); err != nil {
		return "", fmt.Errorf("download failed: %v", err)
	}
	step("Verifying installer integrity (SHA-256) ...")
	got, err := sha256File(dest)
	if err != nil {
		os.Remove(dest)
		return "", err
	}
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(dest)
		return "", fmt.Errorf("SHA-256 mismatch: expected %s, got %s", wantSHA, got)
	}
	step("Installer verified.")
	return dest, nil
}

// fetchRemoteSHA256 resolves the newest stable version so the downloaded file
// can be checked against the versioned .sha256 endpoint (the "latest" alias
// does not expose one). This keeps the tool auto-updated yet tamper-proof.
func fetchRemoteSHA256() (string, error) {
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
	re := regexp.MustCompile(`tailscale-setup-(\d+\.\d+\.\d+)-amd64\.msi`)
	matches := re.FindAllStringSubmatch(string(body), -1)
	best := ""
	for _, m := range matches {
		if compareVersions(best, m[1]) < 0 {
			best = m[1]
		}
	}
	if best == "" {
		return "", fmt.Errorf("no amd64 installer version found on the package index")
	}
	return best, nil
}

func fetchRemoteSHA256For(version string) (string, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(fmt.Sprintf(
		"https://pkgs.tailscale.com/stable/tailscale-setup-%s-amd64.msi.sha256", version))
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

// ---- Installation ----------------------------------------------------------

func runMSI(msiPath string) error {
	step("Installing Tailscale silently (msiexec /quiet) ...")
	cmd := exec.Command("msiexec", "/i", msiPath, "/quiet", "/norestart")
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		step("msiexec output: %s", strings.TrimSpace(string(out)))
	}
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		switch code {
		case 0, 3010:
			step("Installation reported success (exit code %d).", code)
			return nil
		case 1602, 1603, 1618, 1622, 1638:
			return fmt.Errorf("msiexec failed with code %d (%s); see the log for details",
				code, msicode(code))
		default:
			return fmt.Errorf("msiexec failed with code %d", code)
		}
	}
	return nil
}

func msicode(c int) string {
	switch c {
	case 1602:
		return "user cancelled"
	case 1603:
		return "fatal error during installation"
	case 1618:
		return "another installation is already running"
	case 1622:
		return "error opening installation log file"
	case 1638:
		return "another version of this product is already installed"
	}
	return "unknown error"
}

func findTailscale() string {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Tailscale", "tailscale.exe"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("tailscale.exe"); err == nil {
		return p
	}
	return ""
}

// waitForService polls the Tailscale Windows service until it is RUNNING,
// replacing a blind sleep with a deterministic readiness check.
func waitForService() {
	step("Waiting for the Tailscale service to start ...")
	deadline := time.Now().Add(servicePollS)
	for time.Now().Before(deadline) {
		if serviceRunning() {
			step("Tailscale service is running.")
			time.Sleep(3 * time.Second) // let the daemon finish initialising
			return
		}
		time.Sleep(serviceTick)
	}
	step("Timed out waiting for the Tailscale service; attempting to continue anyway.")
	time.Sleep(5 * time.Second)
}

func serviceRunning() bool {
	out, err := exec.Command("sc", "query", "Tailscale").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "RUNNING")
}

// ---- Connect & advertise routes -------------------------------------------

func runTailscaleUp(exe string) {
	step("Connecting to your tailnet and advertising subnet %s ...", subnetRoute)
	args := []string{
		"up",
		"--auth-key=" + authKey,
		"--unattended",
		"--advertise-routes=" + subnetRoute,
	}
	cmd := exec.Command(exe, args...)
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
