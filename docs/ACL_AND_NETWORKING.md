# ACL and networking

TailscaleMe relies on Tailscale's **route auto-approval** so the remote LAN
comes up without a manual click in the admin console. This page covers the ACL
rule, route acceptance on your own devices, the MagicDNS behavior on macOS, and
the headless macOS mode.

## Configure ACLs

In the admin console open the **policy editor** (Settings → Access controls)
and add:

```json
"autoApprovers": {
  "routes": {
    "192.168.1.0/24": ["tag:managed"]
  }
}
```

- The `routes` CIDR must match the `subnetRoute` value in `main.go` exactly.
- The `tag:managed` must match the tag on the auth key you used to build the
  binaries. You can pick any tag you like, but the two must agree.
- A ready-to-paste copy with additional notes lives in `ACL_Configuration.json`
  at the repo root.

Auto-approval means an advertised route becomes active without clicking
**Approve** in the admin console.

### Optional SSH rules

If you customize the default all-allow ACL, add `grants` (lets you reach the
managed nodes — required for Windows OpenSSH on port 22) and `ssh` (gates
Tailscale SSH on Linux/macOS):

```json
"grants": [
  {
    "src": ["email@example.com"],
    "dst": ["tag:managed"],
    "ip": ["*"]
  }
],
"ssh": [
  {
    "action": "accept",
    "src": ["email@example.com"],
    "dst": ["tag:managed"],
    "users": ["root", "autogroup:nonroot"]
  }
]
```

Replace `email@example.com` with your own account. On the default (all-allow)
tailnet, SSH already works without these.

The build can check these rules for you before you ship binaries — pass a
Tailscale API token (`TS_API_TOKEN` or `--api-token`) and `tools/aclcheck`
verifies your ACL and reports anything missing (add `--strict-acl` to fail the
build). See [Getting started — Verify your ACL at build time](GETTING_STARTED.md#verify-your-acl-at-build-time-optional).

## Enable route acceptance on your own devices

For the remote LAN to be reachable from a device in your tailnet, that device
must accept routes:

```
tailscale up --accept-routes
```

or enable **accept routes** in the Tailscale GUI.

## Don't overlap subnets

Do **not** advertise a subnet that overlaps a LAN you are already on (for
example, if your own home LAN is `192.168.1.0/24`, pick a different remote
subnet such as `192.168.10.0/24`). Overlapping routes are ignored and can cause
conflicts. Change the default in `main.go`:

```go
var subnetRoute = "192.168.1.0/24"
```

and rebuild (see [Getting started](GETTING_STARTED.md#customizing-the-subnet)).

## Remote SSH access

SSH into the deployed machines is scoped to the tailnet only:

- **Linux / macOS** — the node runs Tailscale's built-in **SSH server**
  (`tailscale up --ssh`), enabled by **default**. It answers **only on the
  machine's Tailscale IP**, so nothing on the LAN or internet can reach it —
  the ACL `ssh` rule is the only gate. Log in with `ssh user@<hostname>`
  (MagicDNS). After connecting, the tool checks `tailscale status` and confirms
  the SSH server is actually running, or warns you that your ACL is missing the
  `ssh` rule. If the machine already runs its own `sshd` on port 22, the tool
  logs a warning and connects without `--ssh`; that existing server stays
  reachable over the tailnet.
- **Windows** — SSH is set up only when a `.sshkey` was embedded at build time.
  The tool installs/uses **OpenSSH Server** and **scopes the Windows firewall
  for port 22 to the Tailscale address range** (`100.64.0.0/10`, plus
  Tailscale's IPv6 `fd7a:115c:a1e0::/48`). LAN and internet callers are
  dropped, so SSH works only from the tailnet. Afterwards it **self-tests a key
  login** and prints an **SSH setup summary** with the exact connect command
  (`ssh <windows-username>@<tailnet-ip>` and `ssh <windows-username>@<hostname>`).
  Notes:
  - the tool resolves the target account automatically: if the current user is
    an administrator it uses them, otherwise the first enabled local
    (non-Microsoft) admin account found;
  - if that account has no password, the tool sets one — OpenSSH refuses key
    logins for empty-password accounts. It uses the value from
    `--ssh-password` / `.sshpassword` if provided, otherwise generates a strong
    random password per machine. The password is printed to the console and
    saved in `TailscaleMe.log`; it is only needed for console/RDP login, not
    SSH. An existing password is never overwritten;
  - on **fresh** OpenSSH installs the tool also sets `PasswordAuthentication
    no` in `sshd_config` (key-only SSH). Pass `--ssh-password-auth keep` to
    leave password authentication enabled instead. Existing installs are never
    changed;
  - if OpenSSH was **already installed**, its configuration is untouched — the
    tool re-checks/repairs the admin key and adds the firewall scope;
  - a `RestartNeeded` message means a reboot is required before SSH works;
  - if the SSH step ever fails, **re-running the tool repairs it**.

### Verify SSH in 30 seconds

Once the first machine is deployed, confirm the whole path from your machine:

1. `tailscale ping <hostname>` — the node must respond (ACL reachability).
2. `ssh you@<hostname>` (Linux/macOS node) or `ssh <user>@<hostname>`
   (Windows node).
3. Expected outcomes:
   - connection **refused/closed** on Linux/macOS → your ACL lacks the `ssh`
     rule — paste the `ssh` block from `ACL_Configuration.json`;
   - connection **refused/closed** on Windows → your ACL lacks the `grants`
     rule, or the account has no password (the tool sets one automatically,
     but a reboot may be pending);
   - **"no such host"** → MagicDNS isn't enabled on your side — use the
     `ssh user@<tailnet-ip>` form instead;
   - any other refusal → re-run the tool on the node (it repairs the SSH step)
     and check the `TailscaleMe.log` summary block.

To skip the Windows SSH step, build without `.sshkey`. To disable Tailscale SSH
on Linux/macOS as well, build with `-ldflags "-X main.adSSH=false"`. The Windows
firewall scope (and `.allow-cidr` override) only applies when the Windows SSH
step runs.

## macOS headless mode

macOS never installs the GUI app. It uses the standalone `tailscaled` client
(the officially documented "Tailscaled on macOS" route), which:

- runs as a LaunchDaemon
  (`/Library/LaunchDaemons/com.tailscale.tailscaled.plist`) and starts
  **before any user logs in** — so a headless/SSH-managed Mac stays connected
  across reboots;
- needs **no "Allow VPN configuration" dialog** and no logged-in GUI session;
- is installed only if a `tailscale` CLI is not already present:
  1. **Homebrew** (`/opt/homebrew/bin/brew` or `/usr/local/bin/brew`) →
     `brew install --formula tailscale` then `tailscaled install-system-daemon`;
  2. else **Go toolchain** → `go install tailscale.com/cmd/tailscale{,d}@latest`
     then `tailscaled install-system-daemon`;
  3. else the tool prints exact manual steps and stops (nothing gets installed
     that isn't needed to reach the objective).
- enables **MagicDNS** best-effort by prepending `100.100.100.100` to the
  primary network service's DNS servers (`networksetup`).

**Double-click support:** if the binary is double-clicked in Finder (not run in
a Terminal), the tool opens a Terminal window and re-runs itself under `sudo`
so the password can be entered. The first time, macOS may ask once whether to
let the tool control Terminal — click **OK**. If no one is logged in at the
screen, it prints instructions instead of looping.

## MagicDNS specifics

Standalone `tailscaled` on macOS does not configure system DNS itself, so the
tool prepends Tailscale's MagicDNS resolver (`100.100.100.100`) to the primary
network service's DNS servers. This is a persistent OS-level change to that
service's DNS list. Subnet routing works regardless of whether this step
succeeds; a failure only logs a warning.

If MagicDNS names stop resolving later (e.g. after a manual network reset),
re-add the resolver in System Settings → Network → Details → DNS, or simply
re-run the tool.