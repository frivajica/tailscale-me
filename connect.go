// Connect step: bring the node up and advertise the subnet.

package main

import (
	"os/exec"
	"strings"
)

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
