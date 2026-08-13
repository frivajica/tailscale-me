# TailscaleMe — Single-Click Tailscale Auto-Installer & Subnet Router

Cross-compiles a native 64-bit Windows executable (`TailscaleMe.exe`) that a
non-technical user can run once to install the official Tailscale client on a
remote PC (Windows 7–11), join it to your tailnet, and advertise its LAN subnet
so other tailnet devices can reach local equipment (printers, cameras, NAS)
without installing anything on them.

## How it works

1. Detects the Windows version:
   - **Windows 7 / 8 / 8.1** → downloads pinned **Tailscale v1.44.3** (the final
     release supporting those OSes). On Windows 7 it also applies the
     **KB2921916** hotfix with a *clear warning + yes/no prompt* before any reboot.
   - **Windows 10 / 11** → resolves and downloads the **latest stable** MSI.
2. Verifies the downloaded installer with **SHA-256** against Tailscale's
   checksum endpoint (latest) or a pinned hash (legacy, hotfix). Any mismatch
   aborts before anything is installed.
3. Installs silently: `msiexec /i <msi> /quiet /norestart`.
4. Waits for the `Tailscale` service to report **RUNNING** (poll, not a blind
   sleep).
5. Connects and advertises the subnet:
   `tailscale up --auth-key=… --unattended --advertise-routes=192.168.1.0/24`.
6. Logs every step to `%TEMP%\TailscaleMe.log`, deletes the temp installer,
   and waits for Enter so the user can read the result.

The exe embeds a UAC manifest (`requireAdministrator`) so Windows elevates it on
launch. It is built with **Go 1.20** so the binary itself also runs on Win7/8
(current Go toolchains produce binaries that refuse to start there).

## Files

| File | Purpose |
| --- | --- |
| `main.go` | All logic (download, verify, install, connect, cleanup). |
| `main.exe.manifest` | UAC `requireAdministrator` manifest. |
| `winres/winres.json` | go-winres config that embeds the manifest. |
| `ACL_Configuration.json` | `autoApprovers` ACL snippet + setup guidance. |
| `build.sh` | macOS/Linux: installs go-winres, embeds manifest, cross-compiles. |
| `build.bat` | Windows equivalent of `build.sh`. |
| `go.mod` | `go 1.20` — needed for the legacy-Windows-safe toolchain pin. |

## Prerequisite: your auth key

1. Generate a key at Tailscale admin console → **Settings → Keys → Generate
   auth key**.
2. **Tag it `tag:family-biz`** (required so the ACL `autoApprovers` rule
   matches), **single-use**, short expiry, and allow it to *advertise routes*.
3. Replace the placeholder in `main.go`:

   ```go
   const authKey = "tskey-auth-YOUR_AUTH_KEY_HERE"
   ```

The key is baked into the exe — anyone holding the exe can join your tailnet,
which is why single-use + short expiry matters. Embedding a still-valid,
reusable key is a security hole.

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
1. Install `github.com/tc-hib/go-winres` and embed `main.exe.manifest`.
2. Cross-compile with `CGO_ENABLED=0 GOOS=windows GOARCH=amd64`, pinning
   `GOTOOLCHAIN=go1.20.14`.
3. Verify the built exe actually contains `requireAdministrator` and fail the
   build if not.

Output: `./TailscaleMe.exe` (~5 MB).

## Deploy

1. Send `TailscaleMe.exe` to the user via a trusted channel.
2. The user double-clicks it, answers **Yes** on the UAC prompt, and waits.
3. Windows SmartScreen will warn *"Windows protected your PC"* because the exe
   is unsigned — they must click **More info → Run anyway**. (Code-signing the
   binary removes this; it is the one optional cost.)
4. On Windows 7 only: if asked, they approve the restart, then **re-run the
   tool** — it skips straight to connecting.

The machine stays connected and the subnet remains advertised whenever the PC
is powered on (`--unattended`).

### What to send, and what to tell them

Send exactly **one file**: `TailscaleMe.exe`, via a private channel (e.g.
Signal/Dropbox) or USB. Nothing else needs to be copied — the installer, the
Windows 7 hotfix, and the network config are all fetched and verified
automatically by the tool.

Give them these instructions (also shown on screen):

1. Double-click **`TailscaleMe.exe`**.
2. Click **Yes** when Windows asks *"Do you want to allow this app to make
   changes to your device?"*
3. If you see *"Windows protected your PC"*, click **More info → Run anyway** —
   this is expected because the program isn't Microsoft-signed.
4. **Windows 7 only:** if it asks whether to restart, click **Yes / Restart**,
   and after Windows comes back, double-click `TailscaleMe.exe` **again**.
5. Wait 1–2 minutes; the window should end with **"Tailscale is connected and
   advertising routes."** Then you can close it.

Pre-flight before sending: set your real auth key in `main.go` and re-run
`bash build.sh`. Never ship one whose key is a placeholder.

## Troubleshooting

- **"msiexec failed with code 16xx"** → a previous install or pending Windows
  update is blocking it; reboot and re-run. Full error in
  `%TEMP%\TailscaleMe.log`.
- **"tailscale up failed"** with auth-related text → the embedded key expired or
  was already used; generate a fresh one and rebuild.
- **Windows 7** → confirm KB2921916 was applied (the tool does it) or Tailscale
  may not start; the log records the outcome.
- **Can't reach LAN devices** → verify `config.Tailscale` routes are accepted on
  the owner device (`--accept-routes`), and that the advertised CIDR does not
  overlap your home subnet.

## Safety notes

- Touches **no user data** — the only file it deletes is its own downloaded MSI
  in `%TEMP%`.
- All downloads are over HTTPS from the official `pkgs.tailscale.com` and are
  SHA-256 verified before running.
- The biggest risk is trusting an unsigned admin exe; keep it on a private
  channel and regenerate the auth key before each deployment.