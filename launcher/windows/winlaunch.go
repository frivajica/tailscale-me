// Windows universal launcher: a single 386 exe that runs on every 64-bit
// Windows host via WOW64 (and on 32-bit Windows natively), unpacks the
// matching prebuilt installer from the payload appended to itself, verifies it
// and runs it. Written to be unit-testable anywhere, so this file has NO build
// tag: it compiles and tests on macOS/Linux CI too. The serialization format
// lives in internal/payload so the packer and the launcher can never drift.

package main

import (
	"strings"
)

// The sha256 of each per-arch installer member in the appended payload. The
// packer injects these with -ldflags "-X main.sha386=<hex>" etc. so a
// truncated, reordered or tampered payload is rejected before anything runs.
// Empty means "not pinned" and skips the check (pack always sets all three).
var (
	sha386   string
	shaAmd64 string
	shaArm64 string
)

// shaForArch returns the pinned sha256 for the installer of arch.
func shaForArch(arch string) string {
	switch arch {
	case "amd64":
		return shaAmd64
	case "arm64":
		return shaArm64
	case "386":
		return sha386
	default:
		return ""
	}
}

// hostArchName resolves the REAL CPU architecture of a Windows host when this
// binary is a 32-bit (386) process. Under WOW64 the OS exposes the actual
// 64-bit architecture via PROCESSOR_ARCHITEW6432 (PROCESSOR_ARCHITECTURE only
// reports the emulated 32-bit value). The payload must match the host, not the
// process, so a 386 installer is chosen only on genuinely 32-bit Windows.
func hostArchName(procArch, procArchW6432 string) string {
	arch := procArchW6432
	if arch == "" {
		arch = procArch
	}
	switch strings.ToLower(arch) {
	case "amd64", "x64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return "386"
	}
}
