// Shared build-time config guards and executable lookup used by every platform.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ---- Build-time config guards ----------------------------------------------

// placeholderAuthKey marks a binary built without a real auth key; the build
// scripts leave it in place when .authkey is missing.
const placeholderAuthKey = "tskey-auth-YOUR_AUTH_KEY_HERE"

// checkAuthKeyReady aborts before any network or install work if the embedded
// key is the placeholder, so a mis-built binary fails loudly and fast instead
// of calling `tailscale up` with a garbage key.
func checkAuthKeyReady() error {
	if authKey == "" || authKey == placeholderAuthKey || strings.HasPrefix(authKey, "tskey-auth-YOUR_") {
		return fmt.Errorf("this binary was built without a Tailscale auth key.\n" +
			"Rebuild with build.sh/build.bat after creating a .authkey file (see README § Store the key).")
	}
	return nil
}

// findExecutable returns the first candidate path that is a regular file, then
// falls back to PATH lookup for command.
func findExecutable(candidates []string, command string) string {
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath(command); err == nil {
		return p
	}
	return ""
}
