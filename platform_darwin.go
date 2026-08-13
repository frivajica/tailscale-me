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

var cliCandidates = []string{
	"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	"/usr/local/bin/tailscale",
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

// packageBase is the universal pkg: it bundles both amd64 and arm64, so no
// architecture-specific file is needed.
func packageBase() (string, error) { return "Tailscale-%s-macos.pkg", nil }

func packageLocalName() string { return "Tailscale.pkg" }

func installPackage(path string) error {
	step("Installing Tailscale.pkg (macOS installer) ...")
	out, err := exec.Command("installer", "-pkg", path, "-target", "/").CombinedOutput()
	if len(out) > 0 {
		step("installer output: %s", strings.TrimSpace(string(out)))
	}
	if err != nil {
		return fmt.Errorf("installer failed: %v", err)
	}
	return nil
}

func findCLI() string {
	for _, c := range cliCandidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	return ""
}

// startDaemon launches the Tailscale app so its network extension daemon comes
// up. This is best-effort: on the first run macOS shows a system dialog
// asking the user to allow Tailscale's VPN configuration.
func startDaemon(cli string) {
	step("Launching Tailscale.app so its background service starts ...")
	exec.Command("open", "-a", "Tailscale").Run()
}

// waitDaemon polls `tailscale status`. The macOS daemon lives in the logged-in
// GUI session, so on SSH-only machines it will time out and print guidance.
func waitDaemon(cli string) {
	step("Waiting for the Tailscale service ...")
	deadline := time.Now().Add(servicePollS)
	for time.Now().Before(deadline) {
		if cliReachable(cli) {
			step("Tailscale service is reachable.")
			time.Sleep(2 * time.Second)
			return
		}
		time.Sleep(serviceTick)
	}
	step("Timed out waiting for Tailscale. Make sure a user is logged into the " +
		"Mac (not just SSH) and that the Tailscale menu bar app is running, then " +
		"re-run this tool.")
}

func cliReachable(cli string) bool {
	out, err := exec.Command(cli, "status").CombinedOutput()
	if err != nil {
		text := string(out)
		// The daemon answers once it is up, even when not yet connected.
		return strings.Contains(text, "Logged") || strings.Contains(text, "NeedsLogin")
	}
	return true
}

// upArgs on macOS has no --unattended flag; the app keeps the node connected.
func upArgs() []string { return []string{"up"} }
