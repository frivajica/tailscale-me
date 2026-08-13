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
	oldAuth := sshPasswordAuth
	defer func() { sshAllowCIDR = oldCIDR; sshPasswordAuth = oldAuth }()
	sshAllowCIDR = "100.64.0.0/10"
	sshPasswordAuth = ""

	s := opensshScript(`C:\Temp\dir with spaces\authorized_key`, `C:\Temp\dir with spaces\ssh_password`)

	for _, want := range []string{
		"$keyPath = 'C:\\Temp\\dir with spaces\\authorized_key'",
		"$passPath = 'C:\\Temp\\dir with spaces\\ssh_password'",
		"$authMode = ''",
		"$v4Cidr = '100.64.0.0/10'",
		"$v6Cidr = '" + tailscaleIPv6Range + "'",
		"Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0",
		"administrators_authorized_keys",
		"OpenSSH-Server-In-TCP (Tailscale v4)",
		"OpenSSH-Server-In-TCP (Tailscale v6)",
		"re-running this tool repairs it",
		"Self-testing key login",
		"SSH_SELFTEST_OK",
		"Get-LocalUser",
		"PasswordRequired",
		"RNGCryptoServiceProvider",
		"net user $sshUser",
		"PasswordAuthentication no",
		"Restart-Service sshd",
		"the operator never needs a Windows password",
		"----- SSH SETUP SUMMARY -----",
		"tailscale",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("opensshScript missing %q", want)
		}
	}

	// The public key is read from the temp file - it must never be embedded.
	if strings.Contains(s, "ssh-ed25519") {
		t.Error("opensshScript must not contain key material")
	}

	// The script reads the password from the temp file, never from the Go var.
	// Even when sshPassword is set, the generated PowerShell must not contain
	// the literal value — only the $passPath reference.
	oldPass := sshPassword
	sshPassword = "hunter2"
	defer func() { sshPassword = oldPass }()
	s2 := opensshScript(`C:\k`, `C:\p`)
	if strings.Contains(s2, "hunter2") {
		t.Error("opensshScript must not embed the password value")
	}
}

func TestOpensshScriptKeepPasswordAuth(t *testing.T) {
	oldAuth := sshPasswordAuth
	defer func() { sshPasswordAuth = oldAuth }()
	sshPasswordAuth = "keep"

	s := opensshScript(`C:\k`, `C:\p`)
	if !strings.Contains(s, "$authMode = 'keep'") {
		t.Errorf("keep mode missing auth mode marker")
	}
	if !strings.Contains(s, "Leaving password authentication enabled") {
		t.Error("keep mode must announce leaving password auth enabled")
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
