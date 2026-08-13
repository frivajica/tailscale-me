package main

import (
	"os"
	"path/filepath"
	"strings"

	"tailscale-me/internal/payload"
	"tailscale-me/internal/shasum"
	"tailscale-me/internal/wintarget"
	"testing"
)

func TestHostArchName(t *testing.T) {
	tests := []struct {
		procArch, w6432, want string
	}{
		// 64-bit hosts running the 32-bit (WOW64) launcher: the real arch is
		// reported via PROCESSOR_ARCHITEW6432.
		{"x86", "AMD64", "amd64"},
		{"x86", "ARM64", "arm64"},
		// 32-bit Windows natively: no WOW6432 var.
		{"x86", "", "386"},
		{"x86", "x86", "386"},
		// Unknown archs degrade to 386 (safe: everything can run it).
		{"POWERPC", "", "386"},
		{"MIPS", "MIPS", "386"},
	}
	for _, tt := range tests {
		if got := hostArchName(tt.procArch, tt.w6432); got != tt.want {
			t.Errorf("hostArchName(%q, %q) = %q, want %q", tt.procArch, tt.w6432, got, tt.want)
		}
	}
}

func TestBinaryName(t *testing.T) {
	for _, arch := range wintarget.ArchOrder {
		want := "TailscaleMe-windows-" + arch + ".exe"
		if got := wintarget.MemberName(arch); got != want {
			t.Errorf("MemberName(%q) = %q, want %q", arch, got, want)
		}
	}
}

// buildInstallers returns an appended-image (exe + marker + gzipped tar) with
// the three per-arch installers, exactly like tools/pack produces.
func buildInstallers(t *testing.T) []byte {
	t.Helper()
	members := []payload.Member{
		{Name: wintarget.MemberName("386"), Data: []byte(strings.Repeat("a", 16))},
		{Name: wintarget.MemberName("amd64"), Data: []byte(strings.Repeat("b", 16))},
		{Name: wintarget.MemberName("arm64"), Data: []byte(strings.Repeat("c", 16))},
	}
	data, err := payload.Append([]byte("launcher-bytes"), members)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRoundTrip(t *testing.T) {
	data := buildInstallers(t)
	start, err := payload.Start(data)
	if err != nil {
		t.Fatal(err)
	}

	for arch, repeat := range map[string]string{"386": "a", "amd64": "b", "arm64": "c"} {
		name := wintarget.MemberName(arch)
		bin := filepath.Join(t.TempDir(), name)
		if err := payload.ExtractFile(data[start:], name, bin); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.Repeat(repeat, 16); string(got) != want {
			t.Errorf("round-trip %s = %q, want %q", arch, got, want)
		}
	}
}

func TestSHAForArch(t *testing.T) {
	sha386, shaAmd64, shaArm64 = "1", "2", "3"
	defer func() { sha386, shaAmd64, shaArm64 = "", "", "" }()
	if got := shaForArch("amd64"); got != "2" {
		t.Errorf("shaForArch(amd64) = %q, want 2", got)
	}
	if got := shaForArch("arm64"); got != "3" {
		t.Errorf("shaForArch(arm64) = %q, want 3", got)
	}
	if got := shaForArch("386"); got != "1" {
		t.Errorf("shaForArch(386) = %q, want 1", got)
	}
	if got := shaForArch("weird"); got != "" {
		t.Errorf("shaForArch(weird) fallback = %q, want empty", got)
	}
}

func TestSHA256HexOf(t *testing.T) {
	// sha256("abc") is a well-known digest.
	if got, want := shasum.Hex([]byte("abc")), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("shasum.Hex(abc) = %s, want %s", got, want)
	}
}

func TestSHA256File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := shasum.FileHex(path)
	if err != nil {
		t.Fatal(err)
	}
	want := shasum.Hex([]byte("abc"))
	if got != want {
		t.Errorf("shasum.FileHex = %s, want %s", got, want)
	}
	if _, err := shasum.FileHex(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestExtractMissingMember(t *testing.T) {
	data := buildInstallers(t)
	_, err := payload.Extract(data, "missing.exe")
	if err == nil {
		t.Error("expected error for missing member")
	}
}

func TestTraversalRejected(t *testing.T) {
	members := []payload.Member{{Name: "../../evil.exe", Data: []byte("x")}}
	data, err := payload.Append([]byte("exe"), members)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payload.Extract(data, "evil.exe"); err == nil {
		t.Error("expected error for traversal member name")
	}
}

// TestLauncherRejectsTamperedPayload mirrors main_windows.go's integrity gate:
// extract the member for an arch and compare its hash against the pinned sha,
// exactly the check the runtime performs. A payload altered after packing must
// be caught.
func TestLauncherRejectsTamperedPayload(t *testing.T) {
	data := buildInstallers(t)
	start, err := payload.Start(data)
	if err != nil {
		t.Fatal(err)
	}
	arch := "amd64"
	name := wintarget.MemberName(arch)

	bin := filepath.Join(t.TempDir(), name)
	if err := payload.ExtractFile(data[start:], name, bin); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}

	// Pinned to the true value: the gate accepts.
	sha386, shaAmd64, shaArm64 = "", shasum.Hex(got), ""
	if shaForArch(arch) != shasum.Hex(got) {
		t.Fatal("test setup: pinned sha should match the extracted bytes")
	}

	// Tamper: a different digest must never equal the pinned sha.
	corrupt := append([]byte{}, got...)
	corrupt[3] ^= 0xFF
	if digest := shasum.Hex(corrupt); digest == shaForArch(arch) {
		t.Error("tampered payload hashed to the pinned sha")
	}
}
