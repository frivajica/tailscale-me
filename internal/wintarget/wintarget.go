// Package wintarget is the single source of truth for the Windows architectures
// the universal launcher supports and for the payload member name that holds
// each per-arch installer. Sharing it between the packer and the launcher keeps
// the two sides of the appended image from drifting apart.
package wintarget

// ArchOrder is the canonical ordering used by pack (ldflags, payload
// construction and verification) and by the build scripts.
var ArchOrder = []string{"386", "amd64", "arm64"}

// MemberName returns the payload member that holds the installer for arch.
func MemberName(arch string) string {
	return "TailscaleMe-windows-" + arch + ".exe"
}
