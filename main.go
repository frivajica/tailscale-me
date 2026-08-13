package main

import (
	"fmt"
)

// ---- Build-time configuration ----------------------------------------------

// The placeholders below let the repo build cleanly with no secrets and are
// replaced at build time via -ldflags "-X main.<name>=…". Never ship a binary
// whose authKey is still the placeholder (see util.checkAuthKeyReady).
var (
	// Auth & version
	authKey      = "tskey-auth-YOUR_AUTH_KEY_HERE" // injected from gitignored .authkey
	buildVersion = "dev"                           // injected as the git sha/date by build.sh/build.bat

	// Network
	subnetRoute = "192.168.1.0/24" // advertised LAN CIDR; must match the ACL autoApprovers block

	// SSH access — sshPassword/sshPasswordAuth are referenced only from
	// opensshScript (called only by Windows code via postConnect), so the Go
	// linker dead-code-eliminates them from Linux/macOS binaries.
	//
	//   adSSH:          adds --ssh to tailscale up on Linux/macOS
	//   sshKey:         admin SSH public key for Windows OpenSSH ("" = skip)
	//   sshAllowCIDR:   Windows firewall scope for inbound SSH
	//   sshPassword:    password for Windows accounts with none ("" = random,
	//                   printed and logged at setup time; never overwrites)
	//   sshPasswordAuth: "keep" leaves password auth enabled on fresh installs;
	//                    ""/"no" (default) hardens fresh installs to key-only
	adSSH           = "true"          // enable Tailscale SSH on Linux/macOS
	sshKey          = ""              // admin SSH public key for Windows OpenSSH
	sshAllowCIDR    = "100.64.0.0/10" // Windows firewall scope for inbound SSH
	sshPassword     = ""              // Windows SSH fallback password ("" = random)
	sshPasswordAuth = ""              // Windows SSH password-auth mode (""/"no" = key-only)
)

// sshAdvertised reports whether `tailscale up` should enable Tailscale SSH.
func sshAdvertised() bool { return adSSH == "true" }

func main() {
	bootstrap()
	if err := initLog(); err != nil {
		fmt.Printf("[%s] WARNING: could not open log file: %v\n", ts(), err)
	}
	step("Welcome to the TailscaleMe setup tool (build %s).", buildVersion)
	step("A full log of this session is saved to: %s", logPath())

	if err := ensureElevated(); err != nil {
		fatal("%s", err)
	}
	if err := checkAuthKeyReady(); err != nil {
		fatal("%s", err)
	}

	legacy := isLegacyPlatform()
	if legacy {
		step("Detected %s - using Tailscale v1.44.3 (final release for this OS).", platformDesc())
	} else {
		step("Detected %s - using the latest Tailscale release.", platformDesc())
	}
	preInstall(legacy)

	cli, err := installCLI(legacy)
	if err != nil {
		fatal("Installation failed: %v", err)
	}
	if cli == "" {
		fatal("The tailscale CLI was not found after installation. Check %s for the last steps.",
			logPath())
	}

	startDaemon(cli)
	waitDaemon(cli)
	runTailscaleUp(cli)
	postConnect(cli)

	pauseExit(0)
}

// ---- Platform hooks (implemented per-OS in platform_*.go) ------------------
//
//	bootstrap()                  - first action; e.g. relaunch into a Terminal
//	ensureElevated() (error)     - verifies admin/root rights
//	isLegacyPlatform() bool      - legacy Windows 7/8 needs the pinned MSI
//	platformDesc() string        - human-readable OS description
//	preInstall(legacy bool)      - platform prep before the package step
//	packageBase() (string, error)- Sprintf template for the package URL
//	installPackage(path) error   - install the downloaded package
//	installCLI(legacy) (string,error) - install daemon+CLI, return CLI path
//	startDaemon(cli)             - best-effort daemon bring-up
//	waitDaemon(cli)              - wait until the daemon is reachable
//	upArgs() []string            - args for `tailscale up`
//	postConnect(cli)             - best-effort post-connect configuration
//
// Windows & Linux additionally implement findCLI()/installerArtifact();
// macOS implements installDaemon-style logic inside installCLI.
