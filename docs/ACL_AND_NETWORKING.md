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

## macOS headless mode

macOS never installs the GUI app. It uses the standalone `tailscaled` client
(the wiki-supported "Tailscaled on macOS" route), which:

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