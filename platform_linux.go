//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	bindir  = "/usr/bin"
	sbindir = "/usr/sbin"
	envFile = "/etc/default/tailscaled"
	sysdDir = "/etc/systemd/system"
	sock    = "/run/tailscale/tailscaled.sock"
)

func ensureElevated() error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("This tool must run as root. Please re-run with:\n" +
		"    sudo " + filepath.Base(os.Args[0]))
}

func isLegacyPlatform() bool { return false }

func platformDesc() string {
	name := osReleaseName()
	if name == "" {
		name = "Linux"
	}
	return fmt.Sprintf("%s (%s)", name, runtime.GOARCH)
}

func osReleaseName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return ""
}

func preInstall(legacy bool) {}

func packageBase() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64", "arm", "386", "riscv64":
		return "tailscale_%s_" + runtime.GOARCH + ".tgz", nil
	}
	return "", fmt.Errorf("unsupported Linux architecture %q", runtime.GOARCH)
}

func packageLocalName() string { return "tailscale.tgz" }

// installPackage extracts the static tarball and registers tailscaled with
// systemd so the node persists across reboots. The package's own unit files
// expect tailscaled at /usr/sbin and an env file at /etc/default/tailscaled.
func installPackage(path string) error {
	if !hasSystemd() {
		return fmt.Errorf("systemd was not detected on this machine. TailscaleMe " +
			"auto-installs only on systemd systems. Manually extract the tarball " +
			"and run: tailscaled --state=/var/lib/tailscale/tailscaled.state\n")
	}

	dir, err := os.MkdirTemp("", "tailscale-me-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	step("Extracting Tailscale package ...")
	if err := extractTgz(path, dir); err != nil {
		return err
	}
	src, err := pkgRoot(dir)
	if err != nil {
		return err
	}

	step("Installing binaries ...")
	if err := copyFile(filepath.Join(src, "tailscaled"), filepath.Join(sbindir, "tailscaled"), 0755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(src, "tailscale"), filepath.Join(bindir, "tailscale"), 0755); err != nil {
		return err
	}

	step("Writing /etc/default/tailscaled ...")
	env := "PORT=\"41641\"\nFLAGS=\"\"\n"
	if err := os.WriteFile(envFile, []byte(env), 0644); err != nil {
		return err
	}

	step("Installing systemd service units ...")
	for _, u := range []string{"tailscaled.service", "tailscale-wait-online.service", "tailscale-online.target"} {
		if err := copyFileIfExists(filepath.Join(src, "systemd", u), filepath.Join(sysdDir, u)); err != nil {
			return err
		}
	}

	step("Enabling and starting the tailscaled service ...")
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %v %s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", "tailscaled").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable --now tailscaled: %v %s", err, out)
	}
	return nil
}

func hasSystemd() bool {
	if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
		return true
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// extractTgz unpacks a tarball into destDir with a path-traversal guard.
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
	for _, c := range []string{filepath.Join(sbindir, "tailscale"), filepath.Join(bindir, "tailscale")} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	return ""
}

func startDaemon(cli string) {}

// waitDaemon polls until the local tailscaled UNIX socket appears, which the
// systemd unit exposes after the service reaches Running.
func waitDaemon(cli string) {
	step("Waiting for the tailscaled socket ...")
	deadline := time.Now().Add(servicePollS)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(sock); err == nil && !st.IsDir() {
			step("tailscaled is running.")
			time.Sleep(2 * time.Second)
			return
		}
		time.Sleep(serviceTick)
	}
	step("Timed out waiting for tailscaled; attempting to continue anyway.")
}

// upArgs on Linux keeps the node running as a service (no GUI), so the plain
// `up` with an auth key is all that is needed.
func upArgs() []string { return []string{"up"} }
