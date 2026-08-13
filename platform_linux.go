//go:build linux

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	bindir  = "/usr/bin"
	sbindir = "/usr/sbin"
	envFile = "/etc/default/tailscaled"
	sysdDir = "/etc/systemd/system"
	sock    = "/run/tailscale/tailscaled.sock"
)

func bootstrap() {}

func ensureElevated() error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("This tool must run as root. Please re-run with:\n" +
		"    sudo " + filepath.Base(os.Args[0]))
}

func isLegacyPlatform() bool { return false }

func platformDesc() string {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		if name := parseOSRelease(string(b)); name != "" {
			return fmt.Sprintf("%s (%s)", name, runtime.GOARCH)
		}
	}
	return fmt.Sprintf("Linux (%s)", runtime.GOARCH)
}

func preInstall(legacy bool) {}

func packageBase() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64", "arm", "386", "riscv64":
		return "tailscale_%s_" + runtime.GOARCH + ".tgz", nil
	}
	return "", fmt.Errorf("unsupported Linux architecture %q", runtime.GOARCH)
}

// installCLI fetches, verifies and extracts the static tarball, registers
// tailscaled with systemd so the node persists across reboots, and returns the
// CLI path. The package's own unit files expect tailscaled at /usr/sbin and an
// env file at /etc/default/tailscaled.
func installCLI(legacy bool) (string, error) {
	if cli := findCLI(); cli != "" {
		step("Tailscale is already installed at: %s (skipping install).", cli)
		return cli, nil
	}
	if !hasSystemd() {
		return "", fmt.Errorf("systemd was not detected on this machine. TailscaleMe " +
			"auto-installs only on systemd systems. Manually extract the tarball " +
			"and run: tailscaled --state=/var/lib/tailscale/tailscaled.state\n")
	}
	url, wantSHA, err := installerArtifact(legacy)
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

	dir, err := os.MkdirTemp("", "tailscale-me-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	step("Extracting Tailscale package ...")
	if err := extractTgz(dest, dir); err != nil {
		return "", err
	}
	src, err := pkgRoot(dir)
	if err != nil {
		return "", err
	}

	step("Installing binaries ...")
	if err := copyFile(filepath.Join(src, "tailscaled"), filepath.Join(sbindir, "tailscaled"), 0755); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(src, "tailscale"), filepath.Join(bindir, "tailscale"), 0755); err != nil {
		return "", err
	}

	step("Writing /etc/default/tailscaled ...")
	env := "PORT=\"41641\"\nFLAGS=\"\"\n"
	if err := os.WriteFile(envFile, []byte(env), 0644); err != nil {
		return "", err
	}

	step("Installing systemd service units ...")
	for _, u := range []string{"tailscaled.service", "tailscale-wait-online.service", "tailscale-online.target"} {
		if err := copyFileIfExists(filepath.Join(src, "systemd", u), filepath.Join(sysdDir, u)); err != nil {
			return "", err
		}
	}

	step("Enabling and starting the tailscaled service ...")
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return "", fmt.Errorf("systemctl daemon-reload: %w %s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", "tailscaled").CombinedOutput(); err != nil {
		return "", fmt.Errorf("systemctl enable --now tailscaled: %w %s", err, out)
	}
	return findCLI(), nil
}

func hasSystemd() bool {
	if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
		return true
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// pkgRoot locates the extracted top-level directory (the tarball wraps its
// files in a versioned folder).
func pkgRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "tailscaled")); err == nil {
				return filepath.Join(dir, e.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("tailscaled binary not found inside the package")
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyFileIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return copyFile(src, dst, 0644)
}

func findCLI() string {
	return findExecutable([]string{
		filepath.Join(sbindir, "tailscale"),
		filepath.Join(bindir, "tailscale"),
	}, "tailscale")
}

func startDaemon(cli string) {}

// waitDaemon polls until the local tailscaled UNIX socket appears, which the
// systemd unit exposes after the service reaches Running.
func waitDaemon(cli string) {
	step("Waiting for the tailscaled socket ...")
	if pollUntil(func() bool {
		if st, err := os.Stat(sock); err == nil && !st.IsDir() {
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

// upArgs on Linux keeps the node running as a service (no GUI), so the plain
// `up` with an auth key is all that is needed.
func upArgs() []string { return []string{"up"} }

func postConnect(cli string) {}
