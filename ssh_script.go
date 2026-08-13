// Windows OpenSSH setup script builder. Kept build-tag-free so the generated
// PowerShell is unit-testable on any platform (the string is only executed on
// Windows). The script installs/configures OpenSSH Server, re-checking and
// repairing the admin key on every run (so a failed setup can be rescued by
// simply re-running the tool), scopes the firewall to the tailnet, then
// self-tests a key login and prints a full setup summary with the exact
// connect command - all of which also lands in TailscaleMe.log.

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
// on a fresh machine, or only repairs the admin key + Tailscale firewall scope
// when an existing OpenSSH install is detected. The admin's key is read from
// keyPath and the optional admin password from passPath (private temp files,
// never from the process command line). When the target Windows account has no
// password, the script sets one - the embedded value if present, else a random
// password it prints and logs - because OpenSSH refuses empty-password accounts
// even for key-only logins. Fresh installs are hardened to key-only SSH unless
// sshPasswordAuth is "keep".
func opensshScript(keyPath, passPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Continue'
$keyPath = %s
$passPath = %s
$authMode = %s
$v4Cidr = %s
$v6Cidr = %s
$who = $env:USERNAME
$ts = (Get-Command tailscale -ErrorAction SilentlyContinue).Source
if (-not $ts) { $ts = Join-Path $env:ProgramFiles 'Tailscale\tailscale.exe' }

# --- service presence / install ----------------------------------------------
$sshd = Get-Service sshd -ErrorAction SilentlyContinue
if ($sshd) {
  Write-Host "  Existing OpenSSH Server found - keeping its config; re-checking the admin key and firewall scope."
} else {
  Write-Host "  Installing OpenSSH Server (this can take a minute) ..."
  try {
    $cap = Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
  } catch {
    Write-Host "  WARNING: OpenSSH install threw: $_"
  }
  if ($cap) {
    if ($cap.State -eq 'Installed') { Write-Host "  OpenSSH Server installed." }
    elseif ($cap.State -eq 'RestartNeeded') { Write-Host "  WARNING: OpenSSH Server installed but a restart is required before SSH works." }
    else { Write-Host "  WARNING: OpenSSH Server install state: $($cap.State)" }
  }
  try {
    Start-Service sshd
    Set-Service -Name sshd -StartupType Automatic
    Write-Host "  sshd started and set to Automatic (boot), so SSH persists across reboots."
  } catch {
    Write-Host "  WARNING: could not start sshd: $_"
  }
}

# --- ensure the admin key is installed (re-running this tool repairs it) -----
$dir = Join-Path $env:ProgramData 'ssh'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$authKeys = Join-Path $dir 'administrators_authorized_keys'
$key = (Get-Content -Raw $keyPath -ErrorAction SilentlyContinue)
if ($key) {
  $key = $key.Trim()
  $existing = ''
  if (Test-Path $authKeys) { $existing = Get-Content -Raw $authKeys }
  if ($existing -and $existing.Trim() -eq $key) {
    Write-Host "  Admin SSH key already installed - nothing to change."
  } else {
    Set-Content -Path $authKeys -Value $key -Encoding ascii
    icacls $authKeys /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null
    Write-Host "  Admin SSH key installed for admin-group accounts."
  }
} else {
  Write-Host "  WARNING: the embedded SSH key could not be read from $keyPath - skipping key install."
}

# --- firewall: tailnet-only scope --------------------------------------------
$sshV4Rule = @{
  DisplayName   = 'OpenSSH-Server-In-TCP (Tailscale v4)'
  Direction     = 'Inbound'
  Protocol      = 'TCP'
  LocalPort     = 22
  Action        = 'Allow'
  RemoteAddress = $v4Cidr
}
$sshV6Rule = @{
  DisplayName   = 'OpenSSH-Server-In-TCP (Tailscale v6)'
  Direction     = 'Inbound'
  Protocol      = 'TCP'
  LocalPort     = 22
  Action        = 'Allow'
  RemoteAddress = $v6Cidr
}
Get-NetFirewallRule -DisplayName 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue |
  Set-NetFirewallRule -Enabled False
if (-not (Get-NetFirewallRule -DisplayName $sshV4Rule.DisplayName -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule @sshV4Rule | Out-Null
}
if (-not (Get-NetFirewallRule -DisplayName $sshV6Rule.DisplayName -ErrorAction SilentlyContinue)) {
  New-NetFirewallRule @sshV6Rule | Out-Null
}
Write-Host "  Inbound SSH on port 22 is limited to the Tailscale network."

# --- pick the SSH login account -----------------------------------------------
# The tool always runs elevated, so the current user ($who) is the account that
# will receive the SSH key. No fallback is needed.
$sshUser = $who

# --- ensure the account has a password (empty accounts reject even key logins) -
# The injected key is the credential; a password is only required so Windows
# OpenSSH will accept the key. An existing password is never overwritten.
$acct = Get-LocalUser -Name $sshUser -ErrorAction SilentlyContinue
$setPw = ''
if (-not $acct) {
  Write-Host "  WARNING: could not inspect account '$sshUser' - leaving its password untouched."
} elseif ($acct.PrincipalSource -eq 'MicrosoftAccount') {
  Write-Host "  NOTE: account '$sshUser' is linked to a Microsoft account; its password is managed there and is not changed here."
} elseif ($acct.PasswordRequired) {
  Write-Host "  Account '$sshUser' already has a password - leaving it untouched. The operator never needs it: the injected key is the credential."
} else {
  Write-Host "  Account '$sshUser' has no password. OpenSSH refuses to accept key logins for empty-password accounts, so setting one ..."
  if (Test-Path $passPath) { $setPw = (Get-Content -Raw $passPath -ErrorAction SilentlyContinue).Trim() }
  if (-not $setPw) {
    $rng = New-Object System.Security.Cryptography.RNGCryptoServiceProvider
    $bytes = New-Object byte[] 24
    $rng.GetBytes($bytes)
    $setPw = ([Convert]::ToBase64String($bytes)).Replace('/','_')
  }
  try {
    net user $sshUser $setPw | Out-Null
    if ($LASTEXITCODE -eq 0) {
      Write-Host "  Password set for '$sshUser'."
      if ($authMode -eq 'keep') {
        Write-Host "  It can be used to SSH in OR to log into the PC's screen:"
      } else {
        Write-Host "  It is only for logging into the PC's screen - SSH uses the injected key:"
      }
      Write-Host ("  password: " + $setPw)
      Write-Host "  Write this down before closing this window (it is also saved in this session's log)."
    } else {
      Write-Host "  WARNING: could not set the password (net user failed). Key login may still fail until one is set manually."
    }
  } catch {
    Write-Host "  WARNING: could not set the password: $_"
  }
}

# --- key-only hardening (fresh installs only) ---------------------------------
# Existing installs keep their own sshd_config untouched. A fresh install is
# switched to key-only unless the build opted out with sshPasswordAuth=keep.
$freshInstall = -not $sshd
if ($freshInstall) {
  $cfg = Join-Path $env:ProgramData 'ssh\sshd_config'
  if ($authMode -eq 'keep') {
    Write-Host "  Leaving password authentication enabled on the new OpenSSH install (per build flag --ssh-password-auth keep)."
  } elseif (Test-Path $cfg) {
    $hasLine = Select-String -Path $cfg -Pattern '^\s*PasswordAuthentication\s' -Quiet
    if (-not $hasLine) {
      Add-Content -Path $cfg -Value 'PasswordAuthentication no'
      Write-Host "  Restricted the new OpenSSH install to key-only SSH (PasswordAuthentication no)."
      Restart-Service sshd -Force -ErrorAction SilentlyContinue | Out-Null
    }
  }
}

# --- self-test: prove key login works end-to-end over the loopback -----------
$kg = (Get-Command ssh-keygen -ErrorAction SilentlyContinue).Source
$cc = (Get-Command ssh -ErrorAction SilentlyContinue).Source
if (-not $kg -or -not $cc) {
  Write-Host "  WARNING: ssh/ssh-keygen not found - could not self-test the key login."
} else {
  Write-Host "  Self-testing key login (loopback, no password needed) ..."
  $testDir = Join-Path $env:TEMP ("tsm-ssh-test-" + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $testDir | Out-Null
  $testPriv = Join-Path $testDir 'id'
  $testPubFile = Join-Path $testDir 'id.pub'
  $known = Join-Path $testDir 'known_hosts'
  & $kg -q -t ed25519 -N '""' -f $testPriv | Out-Null
  $testPub = (Get-Content -Raw $testPubFile).Trim()
  $savedKeys = ''
  if (Test-Path $authKeys) { $savedKeys = Get-Content -Raw $authKeys }
  if ($savedKeys) { Set-Content -Path $authKeys -Value ($savedKeys.Trim() + [char]10 + $testPub) -Encoding ascii }
  else { Set-Content -Path $authKeys -Value $testPub -Encoding ascii }
  icacls $authKeys /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null
  $out = & $cc -o BatchMode=yes -o 'StrictHostKeyChecking=no' -o ('UserKnownHostsFile=' + $known) -i $testPriv ("$sshUser@localhost") 'echo SSH_SELFTEST_OK' 2>&1
  $ok = ($LASTEXITCODE -eq 0) -and ($out -match 'SSH_SELFTEST_OK')
  if ($savedKeys) { Set-Content -Path $authKeys -Value $savedKeys.Trim() -Encoding ascii }
  else { Remove-Item -Force $authKeys -ErrorAction SilentlyContinue }
  if (Test-Path $authKeys) { icacls $authKeys /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' | Out-Null }
  Remove-Item -Recurse -Force $testDir -ErrorAction SilentlyContinue
  if ($ok) {
    Write-Host "  OK - SSH key login verified locally (account + key + service all work)."
  } else {
    Write-Host "  WARNING: local SSH key login FAILED for account '$sshUser'. It must be in the Administrators group"
    Write-Host "  and have a password set (SSH sets one automatically only when the account had none)."
    Write-Host "  Check whether a reboot is still pending, then re-run this tool; it repairs the SSH step."
  }
}

# --- summary (console + TailscaleMe.log) --------------------------------------
Write-Host ""
Write-Host "----- SSH SETUP SUMMARY -----"
Write-Host ("  OpenSSH service : " + (Get-Service sshd -ErrorAction SilentlyContinue).Status)
$ip = ''
$dns = ''
if (Test-Path $ts) {
  $ip = ((& $ts ip -4 2>$null) | Select-Object -First 1).Trim()
  $dns = (& $ts status --self --json 2>$null | ConvertFrom-Json).Self.DNSName
  $dns = $dns.TrimEnd('.')
}
Write-Host ("  Tailscale IPv4  : " + $ip)
Write-Host ("  MagicDNS name   : " + $dns)
if ($ip) { Write-Host ("  Connect         : ssh " + $sshUser + "@" + $ip) }
if ($dns) { Write-Host ("                  : ssh " + $sshUser + "@" + $dns) }
if ($setPw) {
  if ($authMode -eq 'keep') {
    Write-Host ("  Password        : " + $setPw + " (SSH key is preferred, but this password can also be used)")
  } else {
    Write-Host ("  Password        : " + $setPw + " (screen login only - SSH uses the injected key)")
  }
}
if ($authMode -eq 'keep') {
  Write-Host "  SSH uses the injected public key; the password above can also be used for SSH."
} else {
  Write-Host "  SSH uses the injected public key - the operator never needs a Windows password."
}
Write-Host ("  Session log     : " + $env:TEMP + "\TailscaleMe.log")
Write-Host "----- end SSH SETUP SUMMARY -----"
`, psQuote(keyPath), psQuote(passPath), psQuote(sshPasswordAuth), psQuote(sshAllowCIDR), psQuote(tailscaleIPv6Range))
}
