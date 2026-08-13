// Connect step: bring the node up and advertise the subnet.

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// lastUpHadSSH records whether the `tailscale up` that succeeded carried --ssh,
// so postConnect can verify (and warn about) Tailscale SSH on Linux/macOS.
var lastUpHadSSH bool

func runTailscaleUp(cli string) {
	step("Connecting to your tailnet and advertising subnet %s ...", subnetRoute)
	args := append(upArgs(), "--auth-key="+authKey, "--advertise-routes="+subnetRoute)
	out, err := exec.Command(cli, args...).CombinedOutput()
	if err != nil && hasFlag(args, "--ssh") {
		// Tailscale SSH needs port 22; when an sshd already owns it the flag
		// fails the whole `up`. Retry without it - the machine's own SSH server
		// remains reachable over the tailnet.
		step("Tailscale SSH could not be enabled (port 22 is likely in use). " +
			"Retrying without it.")
		args = withoutFlag(args, "--ssh")
		out, err = exec.Command(cli, args...).CombinedOutput()
	}
	lastUpHadSSH = hasFlag(args, "--ssh")
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

// reportSSHStatus checks whether Tailscale's SSH server actually started when
// --ssh was requested, and points the operator at the ACL `ssh` rule if not.
// Runs from Linux/macOS postConnect; the Windows OpenSSH summary covers that
// platform.
func reportSSHStatus(cli string) {
	if !lastUpHadSSH {
		return
	}
	out, err := exec.Command(cli, "status", "--self", "--json").CombinedOutput()
	if err != nil {
		step("NOTE: could not query Tailscale SSH status (%v); the node may still be syncing.", err)
		return
	}
	var st struct {
		Self struct {
			RunningSSHServer bool   `json:"RunningSSHServer"`
			DNSName          string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		step("NOTE: could not read Tailscale SSH status; check manually with `tailscale status --self --json`.")
		return
	}
	if st.Self.RunningSSHServer {
		host := strings.TrimSuffix(st.Self.DNSName, ".")
		step("Tailscale SSH is active on this machine.")
		if host != "" {
			step("Connect from your machine with:  ssh you@%s", host)
			step("(That requires an ACL `ssh` rule on your side - see docs/ACL_AND_NETWORKING.md.)")
		}
		return
	}
	step("WARNING: Tailscale SSH is NOT running even though --ssh was requested.")
	step("If SSH connections are refused, your tailnet ACL is missing the `ssh` rule - " +
		"paste the ssh block from ACL_Configuration.json (see docs/ACL_AND_NETWORKING.md).")
}

// hasFlag reports whether args contains the given flag.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// withoutFlag returns args with the given flag removed. Tailscale's `--ssh`
// flag takes no separate value, so only the exact token is dropped.
func withoutFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == flag {
			continue
		}
		out = append(out, a)
	}
	return out
}
