package main

import (
	"strings"
	"testing"
)

func TestSSHAdvertised(t *testing.T) {
	old := adSSH
	defer func() { adSSH = old }()

	adSSH = "true"
	if !sshAdvertised() {
		t.Fatal("sshAdvertised() = false for adSSH=true")
	}
	adSSH = "false"
	if sshAdvertised() {
		t.Fatal("sshAdvertised() = true for adSSH=false")
	}
	adSSH = ""
	if sshAdvertised() {
		t.Fatal("sshAdvertised() = true for empty adSSH")
	}
}

func TestHasFlag(t *testing.T) {
	if !hasFlag([]string{"up", "--ssh", "--unattended"}, "--ssh") {
		t.Fatal("expected --ssh to be present")
	}
	if hasFlag([]string{"up", "--accept-dns"}, "--ssh") {
		t.Fatal("did not expect --ssh")
	}
}

func TestWithoutFlag(t *testing.T) {
	got := withoutFlag([]string{"up", "--ssh", "--unattended"}, "--ssh")
	want := []string{"up", "--unattended"}
	if len(got) != len(want) {
		t.Fatalf("withoutFlag = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("withoutFlag = %v, want %v", got, want)
		}
	}
	if got := withoutFlag([]string{"up"}, "--ssh"); len(got) != 1 || got[0] != "up" {
		t.Fatalf("withoutFlag no-op case = %v", got)
	}
}

func TestOpensshScript(t *testing.T) {
	oldCIDR := sshAllowCIDR
	defer func() { sshAllowCIDR = oldCIDR }()
	sshAllowCIDR = "100.64.0.0/10"

	s := opensshScript(`C:\Temp\dir with spaces\authorized_key`)

	for _, want := range []string{
		"$keyPath = 'C:\\Temp\\dir with spaces\\authorized_key'",
		"$v4Cidr = '100.64.0.0/10'",
		"$v6Cidr = '" + tailscaleIPv6Range + "'",
		"Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0",
		"administrators_authorized_keys",
		"OpenSSH-Server-In-TCP (Tailscale v4)",
		"OpenSSH-Server-In-TCP (Tailscale v6)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("opensshScript missing %q", want)
		}
	}

	// The public key is read from the temp file - it must never be embedded.
	if strings.Contains(s, "ssh-ed25519") {
		t.Error("opensshScript must not contain key material")
	}
}

func TestPsQuote(t *testing.T) {
	if got := psQuote(`C:\x\y`); got != "'C:\\x\\y'" {
		t.Fatalf("psQuote plain = %s", got)
	}
	// Embedded apostrophes must be doubled so the value survives the script.
	if got := psQuote(`C:\tmp\o'brien`); got != "'C:\\tmp\\o''brien'" {
		t.Fatalf("psQuote apostrophe = %s", got)
	}
}
