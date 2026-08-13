# Deploying

Once you have built the binaries (see [Getting started](GETTING_STARTED.md)),
sending each person a single file is all it takes. The recipient runs it once;
the tool installs the official Tailscale client, joins your tailnet and
advertises its LAN subnet automatically.

## What to send

Send exactly **one file** — the matching row from the
[output table](GETTING_STARTED.md#output) — via a private channel (Signal,
Dropbox, email) or USB. Nothing else needs to be copied: installers, the
Windows 7 hotfix and the network config are fetched and verified automatically
by the tool.

Pre-flight before sending: create `.authkey` with a fresh key and re-run the
build. Never ship binaries whose key is still the placeholder
(`tskey-auth-YOUR_AUTH_KEY_HERE`).

## Per-platform instructions

### Windows

1. Double-click `TailscaleMe-windows.exe` (one file for every Windows machine,
   x86, x64 or ARM64).
2. Click **Yes** when Windows asks *"Do you want to allow this app to make
   changes to your device?"*.
3. If you see *"Windows protected your PC"*, click **More info → Run anyway** —
   this is expected because the program isn't Microsoft-signed.
4. **Windows 7 only:** if it asks whether to restart, click **Yes / Restart**,
   and after Windows comes back, double-click the file again — it skips
   straight to connecting.

### macOS

Double-click the file, or run in a terminal:

```bash
sudo ./TailscaleMe-darwin-arm64   # use -amd64 on Intel
```

Double-clicking opens a Terminal window that runs the tool under `sudo` (type
the password there). The first time, macOS may ask *"…wants to control
Terminal"* — click **OK**. No VPN-configuration dialog is shown, and no user
needs to stay logged in afterwards: the headless daemon runs from boot.

Needs either **Homebrew** or the **Go toolchain** on the machine; if neither is
present the tool prints exact manual steps (see
[macOS headless mode](ACL_AND_NETWORKING.md#macos-headless-mode)).

### Linux

Run in a terminal with sudo:

```bash
sudo ./TailscaleMe-linux-amd64
```

Requires **systemd** (most modern distros). Non-systemd systems print manual
instructions instead.

**ARM devices** (Raspberry Pi and similar):

- **64-bit OS** (Pi 3/4/5, most SBCs): `sudo ./TailscaleMe-linux-arm64`
- **32-bit OS** (Pi 1/2/3): the `linux-arm` build is compiled with `GOARM=6`,
  so it runs on every ARMv6-or-newer board, including the Pi 1 and Pi Zero.
- **ARMv6-only caveat:** the newest official Tailscale `arm` binaries target
  ARMv7, so on a Pi 1/Pi Zero running a very new Tailscale version the daemon
  may crash on start. If that happens, either move to a **64-bit OS** (then use
  `linux-arm64`) or install an older Tailscale. The huge majority of 32-bit
  boards (Pi 2/3 plus) are ARMv7+ and unaffected.

## What the recipient sees

**All platforms** — wait 1–2 minutes; the window should end with
**"Tailscale is connected and advertising routes."** Then it can be closed.

After setup the machine stays connected and the subnet remains advertised
whenever it is powered on.

## What you should do afterwards

1. Check **Machines** in the admin console — the device appears with the
   `tag:managed` tag and an active route (e.g. `192.168.1.0/24`), with no
   manual Approve click needed.
2. **Revoke the auth key** once the node is confirmed connected: Settings →
   Keys → **⋯ → Revoke**. Existing nodes stay online; only future joins are
   blocked.
3. On your own devices, enable route acceptance so the remote LAN is reachable:
   `tailscale up --accept-routes` (or enable *accept routes* in the GUI). See
   [ACL and networking](ACL_AND_NETWORKING.md).
4. **Remote SSH is ready on the machines** — log in over the tailnet with
   `ssh user@<hostname>`. Linux/macOS use Tailscale SSH (on by default);
   Windows uses OpenSSH Server, enabled when you built with `.sshkey`, and
   needs the recipient's account to have a password and admin rights.
   Everything is already firewall-scoped to the tailnet. See
   [Remote SSH access](ACL_AND_NETWORKING.md#remote-ssh-access).