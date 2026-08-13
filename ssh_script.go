// Windows OpenSSH setup script builder. Kept build-tag-free so the generated
// PowerShell is unit-testable on any platform (the string is only executed on
// Windows).

package main

import (
	"fmt"
	"strings"
)

// tailscaleIPv6Range is Tailscale's reserved ULA prefix, scoped alongside the
// v4 CGNAT range so IPv6 MagicDNS lookups can reach port 22 too.
const tailscaleIPv6Range = "fd7a:115c:a1e0::/48"

// psQuote wraps a value in PowerShell single quotes, doubling any embedded
// quote so the string survives passing through the script verbatim.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// opensshScript returns the PowerShell that installs/configures OpenSSH Server
// on a fresh machine, or only adds the Tailscale firewall scope when an
// existing OpenSSH install is detected. The admin's key is read from keyPath
// (a private temp file, never from the process command line).
func opensshScript(keyPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Continue'
$keyPath = %s
$v4Cidr = %s
$v6Cidr = %s

$sshd = Get-Service sshd -ErrorAction SilentlyContinue
if ($sshd) {
  Write-Host "  Existing OpenSSH Server found - keeping its configuration, adding Tailscale firewall scope only."
} else {
  Write-Host "  Installing OpenSSH Server (this can take a minute) ..."
  $cap = Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
  if ($cap.State -eq 'Installed') {
    Write-Host "  OpenSSH Server installed."
  } elseif ($cap.State -eq 'RestartNeeded') {
    Write-Host "  WARNING: OpenSSH Server installed but a restart is required before SSH works."
  } else {
    Write-Host "  WARNING: OpenSSH Server install state: $($cap.State)"
  }

  Start-Service sshd
  Set-Service -Name sshd -StartupType Automatic

  $dir = Join-Path $env:ProgramData 'ssh'
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $authKeys = Join-Path $dir 'administrators_authorized_keys'
  $key = (Get-Content -Raw $keyPath).Trim()
  Set-Content -Path $authKeys -Value $key -Encoding ascii
  icacls $authKeys /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null
  Write-Host "  Admin SSH key installed (admin-group accounts can log in)."
}

# The OpenSSH installer leaves a rule that allows port 22 from anywhere.
# Disable it and replace it with Tailscale-scoped v4/v6 rules so only tailnet
# traffic can reach SSH - the LAN and internet are dropped.
Get-NetFirewallRule -DisplayName 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue |
  Set-NetFirewallRule -Enabled False
$sshV4Rule = @{
  DisplayName   = 'OpenSSH-Server-In-TCP (Tailscale v4)'
  Direction     = 'Inbound'
  Protocol      = 'TCP'
  LocalPort     = 22
  Action        = 'Allow'
  RemoteAddress = $v4Cidr
}
if (-not (Get-NetFirewallRule -DisplayName $sshV4Rule.DisplayName -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule @sshV4Rule | Out-Null
}
$sshV6Rule = @{
  DisplayName   = 'OpenSSH-Server-In-TCP (Tailscale v6)'
  Direction     = 'Inbound'
  Protocol      = 'TCP'
  LocalPort     = 22
  Action        = 'Allow'
  RemoteAddress = $v6Cidr
}
if (-not (Get-NetFirewallRule -DisplayName $sshV6Rule.DisplayName -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule @sshV6Rule | Out-Null
}
Write-Host "  Inbound SSH on port 22 is limited to the Tailscale network (firewall scope)."
`, psQuote(keyPath), psQuote(sshAllowCIDR), psQuote(tailscaleIPv6Range))
}
