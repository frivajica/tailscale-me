//go:build windows

// Universal launcher runtime: the Windows-only entry point that ties the
// pure, cross-platform helpers in winlaunch.go together. This is compiled as a
// single windows/386 exe (see build.sh / tools/pack) which runs on every
// 32-bit or 64-bit Windows host via WOW64 and on Windows ARM64 via emulation,
// then locates, extracts and executes the matching per-arch installer that the
// packer appended to the end of this same file.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"tailscale-me/internal/payload"
	"tailscale-me/internal/shasum"
	"tailscale-me/internal/wintarget"
)

func main() {
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		fatal(fmt.Errorf("cannot read self: %w", err))
	}
	start, err := payload.Start(data)
	if err != nil {
		fatal(err)
	}

	arch := hostArchName(os.Getenv("PROCESSOR_ARCHITECTURE"), os.Getenv("PROCESSOR_ARCHITEW6432"))
	name := wintarget.MemberName(arch)
	wantSHA := shaForArch(arch)

	tmp, err := os.MkdirTemp("", "tailscale-me-launch-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, name)
	if err := payload.ExtractFile(data[start:], name, bin); err != nil {
		fatal(err)
	}
	if wantSHA != "" {
		got, err := shasum.FileHex(bin)
		if err != nil {
			fatal(err)
		}
		if !strings.EqualFold(got, wantSHA) {
			fatal(fmt.Errorf("integrity check failed for %s: got sha256 %s, want %s", name, got, wantSHA))
		}
	}

	cmd := exec.Command(bin, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fatal(fmt.Errorf("launching %s: %w", name, err))
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "TailscaleMe launcher: %v\n", err)
	os.Exit(1)
}
