# TailscaleMe — Single-Click Tailscale Auto-Installer & Subnet Router

TailscaleMe turns any Windows, macOS or Linux machine into a **remote gateway
for your private tailnet** — no technical effort on the receiving side.

It bundles a native installer for **Windows (7–11), macOS (Intel & Apple
Silicon) and Linux (amd64/arm64/arm/386)** that a non-technical person at a
remote location can run once to:

- install the official Tailscale client,
- join your tailnet,
- advertise its LAN subnet (printers, cameras, NAS, other PCs) as a route the
  rest of the tailnet can reach.

Each binary **detects the platform at runtime** and takes the correct path:
right package for the OS/architecture, right install method, right service
wait, right `tailscale up` flags.

## How it works

1. Detects the OS/architecture and picks the matching official package:
   - **Windows 7 / 8 / 8.1** → pinned **Tailscale v1.44.3** MSI (the final
     release supporting those OSes). On Windows 7 it also applies the
     **KB2921916** hotfix (a Microsoft update modern Go programs need on
     Windows 7) with a *clear warning + yes/no prompt* before any reboot.
   - **Windows 10 / 11** → latest stable MSI (amd64/arm64/x86).
   - **macOS** → headless standalone `tailscaled` client (no GUI app): via
     **Homebrew** if present, else the **Go toolchain**, else printed manual
     steps. Runs as a launchd daemon that boots before login.
   - **Linux** → latest static `.tgz` (amd64/arm64/arm/x86), installed as a
     **systemd** service.
2. Verifies every download with **SHA-256** against Tailscale's checksum
   endpoint (or a pinned hash for the legacy MSI/hotfix). Any mismatch aborts
   before anything is installed.
3. Installs the client, then waits for the daemon to come up (poll, not a
   blind sleep).
4. Connects and advertises the subnet, and enables **remote SSH access**:
   `tailscale up --auth-key=… [--ssh] [--unattended] --accept-dns --advertise-routes=192.168.1.0/24`
   (per-platform: `--ssh` for Tailscale SSH on Linux/macOS, `--unattended` on
   Windows, `--accept-dns` on macOS). On Windows, when a `.sshkey` was embedded
   at build time, it then installs OpenSSH Server and restricts inbound SSH to
   the Tailscale network.
5. Logs every step to `TailscaleMe.log` (in the temp folder), deletes the temp
   installer, and waits for Enter so the user can read the result.

The Windows binaries embed a UAC (User Account Control) manifest
(`requireAdministrator`) so Windows elevates them on launch; Linux/macOS builds
require `sudo`. Everything is built
with **Go 1.20** so the Windows binaries also run on Win7/8 (current Go
toolchains produce binaries that refuse to start there).

## Quick start

1. **Configure your tailnet** — generate a `tag:managed` auth key in the
   [admin console](https://login.tailscale.com/admin/settings/keys) and paste
   the `autoApprovers` ACL rule from
   [ACL and networking](docs/ACL_AND_NETWORKING.md).
2. **Build** — save the key as a gitignored `.authkey` file (or pass it as
   `--auth-key` / the `AUTHKEY` env var; see
   [Getting started](docs/GETTING_STARTED.md#2-provide-the-build-time-values)).
   Optionally add your SSH public key as a gitignored `.sshkey` file to enable
   the Windows OpenSSH setup (Linux/macOS get Tailscale SSH automatically; see
   [Getting started](docs/GETTING_STARTED.md#ssh-access-optional)). Then run
   `bash build.sh` (or `build.bat` on Windows). One universal Windows launcher
   plus one binary per macOS/Linux platform land in `dist/`.
3. **Deploy** — send each machine the one-file binary matching it; the person
   runs it once and it connects itself.

Full step-by-step guidance lives in the docs:

| Doc | What it covers |
| --- | --- |
| [Getting started](docs/GETTING_STARTED.md) | Auth key, `.authkey`, building, output matrix, custom subnet |
| [Deploying](docs/DEPLOYING.md) | What to send, per-OS run instructions, what to tell the recipient |
| [ACL and networking](docs/ACL_AND_NETWORKING.md) | `autoApprovers`, `--accept-routes`, MagicDNS, macOS headless mode |
| [Troubleshooting](docs/TROUBLESHOOTING.md) | Common issues and safety notes |

## Files

| File | Purpose |
| --- | --- |
| `main.go` | Shared flow: detect, download, verify, connect, cleanup. |
| `platform_windows.go` | Windows logic (msiexec, `sc query`, KB2921916, UAC). |
| `platform_darwin.go` | macOS headless logic (brew/go install, launchd, MagicDNS). |
| `platform_linux.go` | Linux logic (tgz extract, systemd registration). |
| `connect.go`, `log.go`, `util.go` | Connect step, session logging, shared config guards. |
| `download.go` | Download + SHA-256 verification into a private temp dir. |
| `parsers.go`, `poll.go` | Pure text parsers for OS/tool output; wait-loop polling. |
| `archive.go` | Traversal-safe `.tgz` extraction (Linux installer). |
| `ssh_script.go`, `ssh_test.go` | Windows OpenSSH PowerShell script builder + tests. |
| `util_test.go` | Tests for parsers, downloads, extraction and version compare. |
| `platform_package.go` | Package selection + checksum resolution (Windows/Linux). |
| `main.exe.manifest` | UAC `requireAdministrator` manifest (Windows only). |
| `winres/winres.json` | go-winres config that embeds the manifest. |
| `launcher/windows/` | Universal Windows launcher (single 386 exe that unpacks the right per-arch installer). |
| `internal/payload/` | Shared launcher/packer serialization (marker, gzip-tar append, extract, verify). |
| `internal/shasum/` | Shared sha256 helpers (hex digest of bytes + files), used by all three binaries. |
| `internal/wintarget/` | Single source for the Windows arch triple + payload member names. |
| `tools/pack` | Packs the per-arch installers into `TailscaleMe-windows.exe` and verifies the payload. |
| `tools/aclcheck` | Verifies your tailnet ACL covers the SSH rules before building (build.sh/build.bat call it when `TS_API_TOKEN`/`--api-token` is set). |
| `ACL_Configuration.json` | `autoApprovers` ACL snippet + setup guidance. |
| `docs/` | Full guides: [started](docs/GETTING_STARTED.md), [deploy](docs/DEPLOYING.md), [ACL](docs/ACL_AND_NETWORKING.md), [troubleshoot](docs/TROUBLESHOOTING.md). |
| `build.sh` | macOS/Linux: cross-compiles the full matrix into `dist/`. |
| `build.bat` | Windows equivalent of `build.sh`. |
| `go.mod` | `go 1.20` — needed for the legacy-Windows-safe toolchain pin. |
| `.authkey.example` | Sample auth-key file; create your own gitignored `.authkey`. |
| `.sshkey.example` | Sample SSH public-key file; create your own gitignored `.sshkey` to enable the Windows OpenSSH setup. |
| `.allow-cidr.example` | Sample scope file; a gitignored `.allow-cidr` overrides the default Windows SSH firewall scope (`100.64.0.0/10`). |
| `.sshpassword.example` | Sample Windows SSH password file; a gitignored `.sshpassword` sets the password for admin accounts that have none (empty = random per machine). |
| `.sshpassauth.example` | Sample Windows SSH password-auth toggle; a gitignored `.sshpassauth` with `keep` leaves password authentication enabled on fresh OpenSSH installs (default = key-only). |

## Security

- The auth key is injected at build time via
  `-ldflags "-X main.authKey=…"` from the first available source — command
  line, `AUTHKEY` env var, or gitignored `.authkey` file — **no secret ever
  lands in git**, and the repo builds cleanly without one (the resulting
  binary just won't authenticate). Passing the key on a command line or env
  var can expose it in the process list / shell history, so prefer the file.
- Any download is SHA-256 verified against the official checksum before it is
  run.
- Because the key is embedded in the binary, anyone holding a binary can join
  your tailnet: use a reusable key with a short expiry and revoke it once the
  device is connected. See
  [Getting started](docs/GETTING_STARTED.md) and
  [Troubleshooting](docs/TROUBLESHOOTING.md#safety-notes).
- The optional Windows SSH password (`--ssh-password`) is embedded the same
  way as the auth key — it travels inside the binary. If omitted, a strong
  random password is generated per machine at setup time, printed and logged
  for the local user. Either way, the password is only needed for console/RDP
  login; SSH uses the injected key. See
  [ACL and networking](docs/ACL_AND_NETWORKING.md#remote-ssh-access).
- Remote SSH access is scoped to the tailnet only: Linux/macOS use Tailscale's
  built-in SSH server (which answers only on the Tailscale IP), and Windows
  OpenSSH is firewalled to Tailscale's address range (`100.64.0.0/10`). See
  [ACL and networking](docs/ACL_AND_NETWORKING.md#remote-ssh-access).
- SSH keys are validated at build time (`ssh-keygen`), the tailnet ACL can be
  verified at build time (`tools/aclcheck`), and the Windows node self-tests a
  key login and prints an SSH setup summary to `TailscaleMe.log` — so a broken
  key or missing ACL rule is caught before or at deploy time, and re-running
  the tool repairs a failed SSH step.

## License

[MIT](LICENSE)