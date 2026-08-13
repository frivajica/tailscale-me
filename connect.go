// Connect step: bring the node up and advertise the subnet.

package main

import (
	"os/exec"
	"strings"
)

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
