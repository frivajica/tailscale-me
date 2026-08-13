# TailscaleMe — Single-Click Tailscale Auto-Installer & Subnet Router

Bundles a native installer for **Windows (7–11), macOS (Intel & Apple Silicon)
and Linux (amd64/arm64/arm/386)** that a non-technical user can run once to
install the official Tailscale client, join your tailnet, and advertise its LAN
subnet so other tailnet devices can reach local equipment (printers, cameras,
NAS) without installing anything on them.

Each binary **detects the platform at runtime** and takes the correct path:
right package for the OS/architecture, right install method, right service
wait, right `tailscale up` flags.

## How it works

1. Detects the OS/architecture and picks the matching official package:
   - **Windows 7 / 8 / 8.1** → pinned **Tailscale v1.44.3** MSI (the final
     release supporting those OSes). On Windows 7 it also applies the
     **KB2921916** hotfix with a *clear warning + yes/no prompt* before any
     reboot.
   - **Windows 10 / 11** → latest stable MSI (amd64/arm64/x86).
   - **macOS** → latest stable universal `.pkg` (Intel + Apple Silicon).
   - **Linux** → latest static `.tgz` (amd64/arm64/arm/386), installed as a
     **systemd** service.
2. Verifies the download with **SHA-256** against Tailscale's checksum endpoint
   (or a pinned hash for the legacy MSI/hotfix). Any mismatch aborts before
   anything is installed.
3. Installs silently (Windows `msiexec`, macOS `installer -pkg`, Linux extract +
   `systemctl enable --now tailscaled`).
4. Waits for the Tailscale daemon/service to come up (poll, not a blind sleep).
5. Connects and advertises the subnet:
   `tailscale up --auth-key=… [--unattended] --advertise-routes=192.168.1.0/24`.
6. Logs every step to `TailscaleMe.log` (in the temp folder), deletes the temp
   installer, and waits for Enter so the user can read the result.

The Windows binaries embed a UAC manifest (`requireAdministrator`) so Windows
elevates them on launch; Linux/macOS builds require `sudo`. Everything is built
with **Go 1.20** so the Windows binaries also run on Win7/8 (current Go
toolchains produce binaries that refuse to start there).

## Files

| File | Purpose |
| --- | --- |
| `main.go` | Shared flow: detect, download, verify, connect, cleanup. |
| `platform_windows.go` | Windows logic (msiexec, `sc query`, KB2921916, UAC). |
| `platform_darwin.go` | macOS logic (`installer`, Tailscale.app daemon). |
| `platform_linux.go` | Linux logic (tgz extract, systemd registration). |
| `main.exe.manifest` | UAC `requireAdministrator` manifest (Windows only). |
| `winres/winres.json` | go-winres config that embeds the manifest. |
| `ACL_Configuration.json` | `autoApprovers` ACL snippet + setup guidance. |
| `build.sh` | macOS/Linux: cross-compiles the full matrix into `dist/`. |
| `build.bat` | Windows equivalent of `build.sh`. |
| `go.mod` | `go 1.20` — needed for the legacy-Windows-safe toolchain pin. |
| `.authkey.example` | Sample auth-key file; create your own gitignored `.authkey`. |

## Prerequisite: your auth key

Follow the **step-by-step guide** below to generate a key, then store it in a
gitignored `.authkey` file. The build scripts inject it into the exe at
compile time, so **no secret ever lands in git**.

### Step-by-step: generate the key in the Tailscale console

1. **Log in** to https://login.tailscale.com → **Settings → Keys** (left sidebar).
2. Click **Generate auth key**.
3. Configure it:
   - **Reusable**: turn **ON** (covers the Windows 7 reboot re-run and any
     retry; you revoke it later).
   - **Expiration**: **7 days**.
   - **Tags**: type `tag:family-biz`. If it's a brand-new tag, Tailscale asks to
     add it to your tailnet's ACLs as a tag owner — accept. (This is the tag the
     `autoApprovers` rule matches, so it must be exactly `family-biz`.)
   - Leave everything else default. Click **Generate key**.
4. **Copy the key** (it starts with `tskey-auth-…`). It is shown **only once** —
   store it somewhere private.
5. **Store it for the build** (see below), then `bash build.sh`.
6. **Set the ACL first** so routes auto-approve when the machine joins — paste
   the `autoApprovers` block from the [Configure ACLs](#configure-acls) section
   into **Settings → Access controls**.
7. After the family member runs the tool, check **Machines** — the PC appears
   with the `family-biz` tag and an active `192.168.1.0/24` route (no manual
   Approve click needed).
8. **Revoke the key** once the node is confirmed connected: Settings → Keys →
   **⋯ → Revoke**. Existing nodes stay online; only future joins are blocked.

### Store the key for the build

Create a file named `.authkey` (gitignored) in this directory containing the
key on a single line:

```
tskey-auth-xxxxxxxxxxxxxxxx
```

Then build:

```bash
bash build.sh
```

- With `.authkey` present, the key is baked into `TailscaleMe.exe`.
- Without it, the build warns and produces an exe with a placeholder that will
  **not** authenticate — so the repo is always safe to clone/build.
- See `.authkey.example` for the expected format; never commit a real `.authkey`.

The key is baked into the exe — anyone holding the exe can join your tailnet.
This is why the recommended flow is **reusable + 7-day expiry + revoke after
setup** rather than a long-lived key left lying around.

## Customize the subnet

The advertised route defaults to `192.168.1.0/24` in `main.go`:

```go
const subnetRoute = "192.168.1.0/24"
```

**Must not overlap your own home LAN** (address conflicts = broken access), and
must match the `routes` key in `ACL_Configuration.json` exactly for
auto-approval.

## Configure ACLs

In the admin console policy editor add:

```json
"autoApprovers": {
  "routes": {
    "192.168.1.0/24": ["tag:family-biz"]
  }
}
```

On the **owner's** devices enable route acceptance so the remote LAN is
reachable:

```
tailscale up --accept-routes
```

See `ACL_Configuration.json` for the full guidance.

## Build

### macOS / Linux

```bash
bash build.sh
```

### Windows

```
build.bat
```

Both scripts:
1. Install `github.com/tc-hib/go-winres` and embed `main.exe.manifest`
   (Windows targets only).
2. Inject the key from `.authkey` via `-ldflags -X main.authKey=…` (warns if
   missing).
3. Cross-compile the full matrix with `CGO_ENABLED=0`, pinning
   `GOTOOLCHAIN=go1.20.14`.
4. Verify the Windows amd64 build contains `requireAdministrator` and fail the
   build if not.

Output — **one file per platform** in `dist/` (~5–7 MB each):

| Target machine | File to send | Run as |
| --- | --- | --- |
| Windows 10/11 (Intel 64-bit) | `dist/TailscaleMe-windows-amd64.exe` | double-click (UAC prompt) |
| Windows ARM64 | `dist/TailscaleMe-windows-arm64.exe` | double-click (UAC prompt) |
| Windows 32-bit (x86) | `dist/TailscaleMe-windows-386.exe` | double-click (UAC prompt) |
| Windows 7 / 8 | `dist/TailscaleMe-windows-amd64.exe` | double-click (UAC prompt) |
| macOS (Intel) | `dist/TailscaleMe-darwin-amd64` | terminal, `sudo ./…` |
| macOS (Apple Silicon) | `dist/TailscaleMe-darwin-arm64` | terminal, `sudo ./…` |
| Linux x86_64 | `dist/TailscaleMe-linux-amd64` | terminal, `sudo ./…` |
| Linux ARM64 | `dist/TailscaleMe-linux-arm64` | terminal, `sudo ./…` |
| Linux 32-bit ARM (Raspberry Pi) | `dist/TailscaleMe-linux-arm` | terminal, `sudo ./…` |
| Linux 32-bit x86 | `dist/TailscaleMe-linux-386` | terminal, `sudo ./…` |

## Deploy

Send the matching file from the table above via a trusted channel (e.g.
Signal/Dropbox) or USB. Nothing else needs to be copied — installers, the
Windows 7 hotfix, and the network config are fetched and verified
automatically by the tool.

**Windows** — double-click the `.exe`, answer **Yes** on the UAC prompt, and
wait. SmartScreen will warn *"Windows protected your PC"* because the binary is
unsigned — click **More info → Run anyway** (code-signing removes this; it is
the one optional cost). On Windows 7 only: if asked, approve the restart, then
**re-run the tool** — it skips straight to connecting.

**macOS** — run in Terminal:
```bash
sudo ./TailscaleMe-darwin-arm64
```
On first launch macOS shows a system dialog asking to allow **Tailscale**'s VPN
configuration — the user must click **Allow**. The Tailscale menu bar app must
be running (it opens automatically); the machine must have a user logged into
the GUI session (pure SSH access cannot activate the macOS app daemon).

**Linux** — run in a terminal with sudo:
```bash
sudo ./TailscaleMe-linux-amd64
```
Requires **systemd** (most modern distros). Non-systemd systems print manual
instructions instead.

After setup the machine stays connected and the subnet remains advertised
whenever it is powered on.

### What to send, and what to tell them

Send exactly **one file** (the right row from the build table above) via a
private channel or USB. Nothing else needs to be copied — the installer and the
network config are fetched and verified automatically by the tool.

Give them these instructions (also shown on screen):

**Windows**
1. Double-click **`TailscaleMe-windows-….exe`**.
2. Click **Yes** when Windows asks *"Do you want to allow this app to make
   changes to your device?"*
3. If you see *"Windows protected your PC"*, click **More info → Run anyway** —
   this is expected because the program isn't Microsoft-signed.
4. **Windows 7 only:** if it asks whether to restart, click **Yes / Restart**,
   and after Windows comes back, double-click the file **again**.

**macOS / Linux**
1. Open a Terminal window in the folder holding the file.
2. Run `sudo ./TailscaleMe-<darwin|linux>-<arch>` and type the password if asked.
3. **macOS only:** click **Allow** if macOS asks about Tailscale's VPN
   configuration.

**All platforms** — wait 1–2 minutes; the window should end with
**"Tailscale is connected and advertising routes."** Then they can close it.

Pre-flight before sending: create `.authkey` with a fresh key and re-run
`bash build.sh`. Never ship binaries whose key is the placeholder.

## Troubleshooting

The tool prints the log path at startup (`TailscaleMe.log` in the temp folder).
- **Windows "msiexec failed with code 16xx"** → a previous install or pending
  Windows update is blocking it; reboot and re-run.
- **"tailscale up failed"** with auth-related text → the embedded key expired or
  was already used; generate a fresh one and rebuild.
- **Windows 7** → confirm KB2921916 was applied (the tool does it) or Tailscale
  may not start; the log records the outcome.
- **Linux "systemd was not detected"** → the machine uses an init system other
  than systemd; the tool prints manual steps to run `tailscaled` instead.
- **macOS "Timed out waiting for Tailscale"** → a user must be logged into the
  Mac's GUI session and the Tailscale menu bar app running (SSH-only never
  activates the macOS daemon); re-run once that's satisfied.
- **Can't reach LAN devices** → verify routes are accepted on the owner device
  (`tailscale up --accept-routes`), and that the advertised CIDR does not
  overlap your home subnet.

## Safety notes

- Touches **no user data** — the only file it deletes is its own downloaded
  package in the temp folder.
- All downloads are over HTTPS from the official `pkgs.tailscale.com` and are
  SHA-256 verified before running.
- The biggest risk is trusting an unsigned installer running with admin rights;
  keep it on a private channel and regenerate the auth key before each
  deployment.