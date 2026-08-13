//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	daemonSocket = "/var/run/tailscale/tailscaled.sock"
	plistPath    = "/Library/LaunchDaemons/com.tailscale.tailscaled.plist"
)

var cliCandidates = []string{
	"/opt/homebrew/bin/tailscale", // Apple Silicon Homebrew
	"/usr/local/bin/tailscale",    // Intel Homebrew / manual install
}

func bootstrap() {
	if os.Getenv("TERM") != "" {
		return
	}
	// Launched from Finder: no terminal is attached, so TERM is unset. Hand the
	// job to a real Terminal window running `sudo <binary>`, which keeps sudo
	// password entry interactive. TERM is set in Terminal.app and SSH sessions,
	// so this only triggers for the double-click case.
	if consoleUser() == "" {
		fmt.Println("No terminal is attached and no user is logged in at the screen,")
		fmt.Println("so no window can be opened. Re-run from a terminal with:")
		fmt.Println("    sudo " + mustExecutable())
		os.Exit(1)
	}
	exe := mustExecutable()
	quoted := "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
	script := "tell application \"Terminal\"\nactivate\ndo script \"sudo " + quoted + "\"\nend tell"
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		fmt.Printf("Could not open a Terminal window (%v): %s\n"+
			"Re-run from a terminal with:\n    sudo %s\n", err, out, exe)
		os.Exit(1)
	}
	os.Exit(0)
}

func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("Could not resolve this program's path: %v\n", err)
		os.Exit(1)
	}
	return exe
}

// consoleUser returns the logged-in GUI user, or "" at the login screen.
// /dev/console lists "root" when nobody is logged in.
func consoleUser() string {
	out, err := exec.Command("stat", "-f%Su", "/dev/console").Output()
	if err != nil {
		return ""
	}
	if u := strings.TrimSpace(string(out)); u != "root" {
		return u
	}
	return ""
}

func ensureElevated() error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("This tool must run as root. Please re-run with:\n" +
		"    sudo " + filepath.Base(os.Args[0]))
}

func isLegacyPlatform() bool { return false }

func platformDesc() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "macOS"
	}
	return fmt.Sprintf("macOS %s (%s)", strings.TrimSpace(string(out)), runtime.GOARCH)
}

func preInstall(legacy bool) {}

// installCLI brings up the headless tailscaled client without any GUI app.
// Order: already-installed CLI → Homebrew → Go toolchain → printed manual
// steps. It never installs a tool that isn't needed to reach the objective.
func installCLI(legacy bool) (string, error) {
	if cli := findCLI(); cli != "" {
		step("Tailscale is already installed at: %s (skipping install).", cli)
		return cli, nil
	}
	// The standalone daemon and the GUI app conflict (both want the TUN /
	// state directory), so refuse to add a second client on a Mac with the app.
	if app, err := os.Stat("/Applications/Tailscale.app"); err == nil && !app.IsDir() {
		return "", fmt.Errorf("Tailscale.app is already installed. The standalone " +
			"daemon this tool installs conflicts with the GUI app.\n\n" +
			"Quit the Tailscale menu bar app, move /Applications/Tailscale.app to " +
			"the Trash, then re-run this tool.\n" +
			"(Leave it installed and route through the GUI app instead if you do " +
			"not need headless boot-time startup.)\n    sudo " + filepath.Base(os.Args[0]))
	}
	if brew := findExecutable([]string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}, "brew"); brew != "" {
		return installViaBrew(brew)
	}
	if goCmd := findExecutable(nil, "go"); goCmd != "" {
		return installViaGo(goCmd)
	}
	return "", fmt.Errorf("Tailscale is not installed and neither Homebrew nor the Go " +
		"toolchain is available to install it automatically.\n\n" +
		"Option 1: install Homebrew from https://brew.sh, then re-run this tool.\n" +
		"Option 2: log in at the Mac's screen and re-run this tool from a Terminal " +
		"window with:\n    sudo " + filepath.Base(os.Args[0]))
}

func installViaBrew(brew string) (string, error) {
	step("Installing the Tailscale command line client via Homebrew ...")
	if out, err := exec.Command(brew, "install", "--formula", "tailscale").CombinedOutput(); err != nil {
		return "", fmt.Errorf("brew install failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if err := registerSystemDaemon(findTailscaled()); err != nil {
		return "", err
	}
	if cli := findCLI(); cli != "" {
		return cli, nil
	}
	// Older formula builds have no install-system-daemon fallback path here;
	// brew services is the pre-built fallback the Homebrew formula documents.
	step("Registering the tailscaled daemon with brew services ...")
	if out, err := exec.Command(brew, "services", "start", "tailscale").CombinedOutput(); err != nil {
		return "", fmt.Errorf("brew services start failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return findCLI(), nil
}

func installViaGo(goCmd string) (string, error) {
	// Tailscale requires the most recent Go toolchain; building with an old
	// one fails cryptically, so gate it up front.
	out, err := exec.Command(goCmd, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go version failed: %w", err)
	}
	major, minor, ok := parseGoVersion(string(out))
	if !ok || major < 1 || (major == 1 && minor < 21) {
		return "", fmt.Errorf("the installed Go toolchain (%s) is too old. "+
			"Building the latest Tailscale needs Go 1.21+.\n"+
			"Install a newer Go (or Homebrew) and re-run this tool.", strings.TrimSpace(string(out)))
	}
	step("Building the Tailscale command line client with Go (a few minutes) ...")
	for _, pkg := range []string{"tailscale.com/cmd/tailscale", "tailscale.com/cmd/tailscaled"} {
		cmd := exec.Command(goCmd, "install", pkg+"@latest")
		cmd.Env = append(os.Environ(), "GOBIN=/usr/local/bin")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("go install %s failed: %w\n%s", pkg, err, strings.TrimSpace(string(out)))
		}
	}
	if err := registerSystemDaemon("/usr/local/bin/tailscaled"); err != nil {
		return "", err
	}
	if cli := findCLI(); cli != "" {
		return cli, nil
	}
	return "", fmt.Errorf("the Go-built tailscale CLI was not found after installation")
}

// registerSystemDaemon installs the official LaunchDaemon so tailscaled boots
// before anyone logs in, keeping the machine connected after restarts.
func registerSystemDaemon(tsd string) error {
	if tsd == "" {
		return fmt.Errorf("tailscaled binary not found after installation")
	}
	step("Registering tailscaled as a system launch daemon ...")
	if out, err := exec.Command(tsd, "install-system-daemon").CombinedOutput(); err != nil {
		return fmt.Errorf("tailscaled install-system-daemon failed: %w\n%s",
			err, strings.TrimSpace(string(out)))
	}
	return nil
}

func findTailscaled() string {
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		p := filepath.Join(d, "tailscaled")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func findCLI() string {
	return findExecutable(cliCandidates, "tailscale")
}

// startDaemon ensures the LaunchDaemon is loaded. install-system-daemon and
// brew services usually leave it running already, so this is best-effort.
func startDaemon(cli string) {
	if st, err := os.Stat(daemonSocket); err == nil && !st.IsDir() {
		return
	}
	exec.Command("launchctl", "load", plistPath).Run()
}

// waitDaemon polls the standalone tailscaled socket until it appears, which
// the LaunchDaemon exposes after the service reaches a running state. Unlike
// the GUI app, no logged-in session is required.
func waitDaemon(cli string) {
	step("Waiting for the tailscaled socket ...")
	if pollUntil(func() bool {
		if st, err := os.Stat(daemonSocket); err == nil && !st.IsDir() {
			return true
		}
		return false
	}, serviceTick, servicePollS) {
		step("tailscaled is running.")
		time.Sleep(2 * time.Second)
		return
	}
	step("Timed out waiting for tailscaled; attempting to continue anyway.")
}

func upArgs() []string { return []string{"up", "--accept-dns"} }

// postConnect points the primary network service at Tailscale's MagicDNS
// resolver (100.100.100.100) so tailnet names resolve. Standalone tailscaled on
// macOS does not configure system DNS itself. Best-effort: subnet routing works
// regardless, so failures only log a warning. NOTE: this replaces the primary
// service's DNS servers in the OS network config (a persistent change).
func postConnect(cli string) {
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		step("WARNING: could not enable MagicDNS (%v).", err)
		return
	}
	iface := parseRouteInterface(string(out))
	if iface == "" {
		step("WARNING: could not enable MagicDNS (no default interface found).")
		return
	}
	order, err := exec.Command("networksetup", "-listnetworkserviceorder").CombinedOutput()
	if err != nil {
		step("WARNING: could not enable MagicDNS (%v).", err)
		return
	}
	service := parseServiceForInterface(string(order), iface)
	if service == "" {
		step("WARNING: could not enable MagicDNS (no network service for %s).", iface)
		return
	}
	cur, _ := exec.Command("networksetup", "-getdnsservers", service).CombinedOutput()
	current := parseDNSServers(string(cur))
	for _, s := range current {
		if s == "100.100.100.100" {
			return
		}
	}
	args := append([]string{"-setdnsservers", service, "100.100.100.100"}, current...)
	step("Enabling MagicDNS: adding 100.100.100.100 to %q.", service)
	if out, err := exec.Command("networksetup", args...).CombinedOutput(); err != nil {
		step("WARNING: could not update DNS for %q: %v %s", service, err, out)
	}
}
