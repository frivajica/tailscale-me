# Getting started

This guide covers everything you need to do **once, as the admin**: generate an
auth key, configure the tailnet, and build the per-platform binaries. Once the
binaries exist, handing one to a person at a remote location is all that
remains — see [Deploying](DEPLOYING.md).

## Prerequisites

- A recent Go toolchain to run the build scripts; they pin Go 1.20 (via
  `GOTOOLCHAIN`) so the Windows binaries stay compatible with Windows 7/8.
- A **Tailscale account** with admin rights on a tailnet (create one free at
  https://login.tailscale.com).
- You don't need any access to the remote machines — the recipient runs the
  binary.

## 1. Generate an auth key

The binary authenticates the remote machine to your tailnet using an **auth
key** baked in at build time. The key never lands in git — it is injected into
the executable by the build script.

1. Log in to https://login.tailscale.com → **Settings → Keys** (left sidebar).
2. Click **Generate auth key**.
3. Configure it:
   - **Reusable**: turn **ON** (covers the Windows 7 restart re-run — see
     [Deploying](DEPLOYING.md#windows) — and any retry; you revoke it later).
   - **Expiration**: **7 days**.
   - **Tags**: type `tag:managed`. If it's a brand-new tag, Tailscale asks to
     add it to your tailnet's ACLs as a tag owner — accept. (Use any tag you
     like, but it must match the `autoApprovers` rule — see
     [ACL and networking](ACL_AND_NETWORKING.md).)
   - Leave everything else default. Click **Generate key**.
4. **Copy the key** (it starts with `tskey-auth-…`). It is shown **only once** —
   store it somewhere private.
5. **Set the ACL first** so routes auto-approve when the machine joins — paste
   the `autoApprovers` block from [ACL and networking](ACL_AND_NETWORKING.md)
   into **Settings → Access controls**.
6. Store the key for the build (next step), then run the build.

## 2. Provide the build-time values

Four build-time values control the binaries. Each is resolved in the same
order — **command line → environment variable → gitignored file → the default
compiled into `main.go`** — so the file-only method below is just the
preferred, secret-safe option.

| Value | Flags | Env | File (gitignored) | Default |
| --- | --- | --- | --- | --- |
| Tailscale auth key | `--auth-key <key>` | `AUTHKEY` | `.authkey` | placeholder (won't authenticate) |
| Admin SSH public key (Windows) | `--ssh-key <pubkey>` | `SSHKEY` | `.sshkey` | empty (Windows SSH step skipped) |
| Windows SSH firewall scope | `--allow-cidr <cidr>` | `ALLOWCIDR` | `.allow-cidr` | `100.64.0.0/10` |

Create the files in the repo root (these are gitignored), each value on a
single line:

```
tskey-auth-xxxxxxxxxxxxxxxx      # .authkey
ssh-ed25519 AAAA…your-public-key… you@example.com   # .sshkey
```

Then build:

```bash
bash build.sh
```

- With a value present (any layer), it is baked into the binaries.
- Without an auth key anywhere, the build warns and produces binaries with a
  placeholder that will **not** authenticate — so the repo is always safe to
  clone and build.
- Passing values as flags or env variables works too but they can appear in
  your shell history / process list — prefer the files. Similarly, don't put a
  real key on a shared build machine's command line.
- See `.authkey.example` / `.sshkey.example` for the expected formats; never
  commit a real `.authkey`.
- Quick example (flag beats the files):

  ```bash
  bash build.sh --auth-key "tskey-auth-xxx" --ssh-key "ssh-ed25519 AAAA… me@host"
  ```
  ```bat
  build.bat --auth-key tskey-auth-xxx --ssh-key "ssh-ed25519 AAAA… me@host"
  ```

### SSH access (optional)

You can `ssh` into the deployed machines afterwards (e.g. to administer a
remote gateway). Enablement differs by platform:

- **Linux / macOS** — Tailscale's built-in SSH server (`tailscale up --ssh`)
  is enabled by **default**. It is reachable **only over the tailnet** (it
  answers just on the machine's Tailscale IP) and gated by an ACL `ssh` rule —
  see [ACL and networking](ACL_AND_NETWORKING.md#remote-ssh-access). No key
  file is needed. To disable it, build with `-ldflags "-X main.adSSH=false"`.
- **Windows** — OpenSSH Server is set up only when you provide your SSH
  **public** key — typically in a gitignored `.sshkey` file in the repo root
  (single line), or via `--ssh-key <pubkey>` / the `SSHKEY` env var:

  ```
  ssh-ed25519 AAAA…your-public-key… you@example.com
  ```

  It drops your key into the administrators' `authorized_keys` and **firewalls
  port 22 to the Tailscale network only** (`100.64.0.0/10`). Existing OpenSSH
  installs are left alone except for that firewall scope.

Optional: a gitignored `.allow-cidr` file (or `--allow-cidr` / the `ALLOWCIDR`
env var) overrides the Windows firewall scope (default `100.64.0.0/10`). See
`.sshkey.example` / `.allow-cidr.example` for the format. Without `.sshkey`,
only the Windows SSH step is skipped — Linux/macOS keep their built-in Tailscale
SSH.

## 3. Build the binaries

### macOS / Linux

```bash
bash build.sh
```

### Windows

```
build.bat
```

Both scripts:

1. Install `github.com/tc-hib/go-winres` and embed the
   `requireAdministrator` manifest into the Windows targets and the launcher.
2. Resolve each build-time value (auth key, `.sshkey`, `.allow-cidr`) from the
   first source that provides it — command line, env var, or gitignored file —
   and inject it via `-ldflags -X …` (warns if no auth key is found anywhere).
   See [§ 2](GETTING_STARTED.md#2-provide-the-build-time-values).
3. Build the three Windows per-arch installers, then pack them into the
   universal `TailscaleMe-windows.exe` launcher via `tools/pack` (which also
   embeds each installer's SHA-256 so a tampered payload is rejected at run
   time).
4. Cross-compile the macOS and Linux targets with `CGO_ENABLED=0`, pinning
   `GOTOOLCHAIN=go1.20.14` (so the Windows binaries still run on Windows 7/8).
5. Run a sanity check that the Windows build embeds `requireAdministrator` and
   fail the build if not.

## Output

One file per platform in `dist/` (~5–8 MB each):

| Target machine | File to send | Run as |
| --- | --- | --- |
| Any Windows (x86, x64, ARM64; 7–11) | `dist/TailscaleMe-windows.exe` | double-click (UAC prompt) |
| macOS (Intel) | `dist/TailscaleMe-darwin-amd64` | double-click or terminal, `sudo ./…` |
| macOS (Apple Silicon) | `dist/TailscaleMe-darwin-arm64` | double-click or terminal, `sudo ./…` |
| Linux x86_64 | `dist/TailscaleMe-linux-amd64` | terminal, `sudo ./…` |
| Linux ARM64 | `dist/TailscaleMe-linux-arm64` | terminal, `sudo ./…` |
| Linux 32-bit ARM (Raspberry Pi) | `dist/TailscaleMe-linux-arm` | terminal, `sudo ./…` |
| Linux 32-bit x86 | `dist/TailscaleMe-linux-386` | terminal, `sudo ./…` |

The single `TailscaleMe-windows.exe` is a universal launcher: it is a 32-bit
exe that runs on every Windows machine (natively on 32-bit, via WOW64 on
64-bit Intel, via emulation on Windows ARM64), then detects the real CPU and
runs the matching per-arch installer that it carries inside itself. The three
per-arch installers (`TailscaleMe-windows-386.exe`, `-amd64.exe`, `-arm64.exe`)
are built as payload members during the pack step and are no longer shipped
separately.

## Customizing the subnet

The advertised route defaults to `192.168.1.0/24` in `main.go`:

```go
var subnetRoute = "192.168.1.0/24"
```

**Must not overlap the recipient's LAN** (address conflicts = broken access),
and must match the `routes` key in your `autoApprovers` ACL rule exactly for
auto-approval. See [ACL and networking](ACL_AND_NETWORKING.md) for details.
