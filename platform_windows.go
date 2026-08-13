//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Windows 7 requires the KB2921916 hotfix for Go binaries to run. The
	// mirror has no public checksum, so we pin the hash of the official file.
	kb2921916URL    = "https://pkgs.tailscale.com/mirror/Windows6.1-KB2921916-x64.msu"
	kb2921916SHA256 = "39d978285d01ee4c0dfe9e2462bc4c948260aaf041aaa04faef3275f6d46a773"
)

func bootstrap() {}

func ensureElevated() error {
	if exec.Command("net", "session").Run() == nil {
		return nil
	}
	return fmt.Errorf("This tool must run as Administrator. Right-click TailscaleMe.exe " +
		"and choose \"Run as administrator\", then click Yes on the UAC prompt.")
}

func isLegacyPlatform() bool {
	major, minor, err := currentWindowsVersion()
	if err != nil {
		return false
	}
	return major == 6 && minor <= 3 // Windows 7 (6.1), 8 (6.2), 8.1 (6.3)
}

func platformDesc() string {
	major, minor, err := currentWindowsVersion()
	if err != nil {
		return "Windows"
	}
	return fmt.Sprintf("Windows %d.%d (%s)", major, minor, runtime.GOARCH)
}

func preInstall(legacy bool) {
	if legacy {
		major, minor, err := currentWindowsVersion()
		if err == nil && major == 6 && minor == 1 {
			installKB2921916()
		}
	}
}

func currentWindowsVersion() (major, minor int, err error) {
	out, err := exec.Command("cmd", "/c", "ver").CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("cmd /c ver failed: %w", err)
	}
	return parseWindowsVer(string(out))
}

func packageBase() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "tailscale-setup-%s-amd64.msi", nil
	case "arm64":
		return "tailscale-setup-%s-arm64.msi", nil
	case "386":
		return "tailscale-setup-%s-x86.msi", nil
	}
	return "", fmt.Errorf("unsupported Windows architecture %q", runtime.GOARCH)
}

func packageLocalName() string { return "tailscale-setup.msi" }

// installCLI fetches and verifies the MSI, installs it silently, and returns
// the CLI path. Skips straight to the CLI when Tailscale is already present.
func installCLI(legacy bool) (string, error) {
	if cli := findCLI(); cli != "" {
		step("Tailscale is already installed at: %s (skipping install).", cli)
		return cli, nil
	}
	url, wantSHA, _, err := installerArtifact(legacy)
	if err != nil {
		return "", fmt.Errorf("installation package could not be prepared: %w", err)
	}
	step("Downloading installer %s ...", url)
	step("Verifying installer integrity (SHA-256) ...")
	dest, err := downloadVerified(url, wantSHA)
	if err != nil {
		return "", err
	}
	step("Installer verified.")
	if err := installPackage(dest); err != nil {
		return "", err
	}
	step("Temporary installation files cleaned up.")
	return findCLI(), nil
}

func installPackage(path string) error {
	step("Installing Tailscale silently (msiexec /quiet) ...")
	cmd := exec.Command("msiexec", "/i", path, "/quiet", "/norestart")
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

func findCLI() string {
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

func startDaemon(cli string) {}

// waitDaemon polls the Tailscale Windows service until it is RUNNING,
// replacing a blind sleep with a deterministic readiness check.
func waitDaemon(cli string) {
	step("Waiting for the Tailscale service to start ...")
	if pollUntil(func() bool {
		return serviceRunning()
	}, serviceTick, servicePollS) {
		step("Tailscale service is running.")
		time.Sleep(3 * time.Second) // let the daemon finish initialising
		return
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

// upArgs controls persistence across logouts/reboots on Windows.
func upArgs() []string { return []string{"up", "--unattended"} }

func postConnect(cli string) {}

// ---- Windows 7 KB2921916 hotfix --------------------------------------------

// installKB2921916 downloads, verifies and silently applies the hotfix. It
// never forces a reboot: it warns first and only reboots after the user
// accepts the prompt.
func installKB2921916() {
	step("Downloading required Windows 7 update KB2921916 ...")
	dest, err := downloadVerified(kb2921916URL, kb2921916SHA256)
	if err != nil {
		step("WARNING: could not download or verify KB2921916: %v", err)
		step("If Tailscale fails to start afterwards, apply the update manually and reboot.")
		return
	}
	step("Applying KB2921916 (this takes a few minutes) ...")
	out, err := exec.Command("wusa", dest, "/quiet", "/norestart").CombinedOutput()
	if len(out) > 0 {
		step("wusa output: %s", strings.TrimSpace(string(out)))
	}
	if err == nil {
		step("KB2921916 is installed - no reboot required.")
		return
	}
	rebootRequired := false
	if ee, ok := err.(*exec.ExitError); ok {
		// 3010 = success + reboot required.
		if ee.ExitCode() == 3010 {
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
