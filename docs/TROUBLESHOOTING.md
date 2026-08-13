# Troubleshooting

The tool prints the path of its session log at startup
(`TailscaleMe.log` in the OS temp folder). Ask whoever ran it to send a
screenshot of the window, or the log file — the log records every step and any
error for diagnosis.

## Common issues

- **Windows "msiexec failed with code 16xx"** → a previous install or pending
  Windows update is blocking it; reboot and re-run.
- **"tailscale up failed"** with auth-related text → the embedded key expired
  or was already used; generate a fresh one and rebuild (see
  [Getting started](GETTING_STARTED.md)).
- **Windows 7** → confirm the KB2921916 hotfix was applied (the tool does it)
  or Tailscale may not start; the log records the outcome.
- **Linux "systemd was not detected"** → the machine uses an init system other
  than systemd; the tool prints manual steps to run `tailscaled` instead.
- **macOS "Tailscale setup failed"** → the machine has neither Homebrew nor the
  Go toolchain. The tool prints exact manual steps (`brew install --formula
  tailscale`, `sudo brew services start tailscale`); install Homebrew and
  re-run, or follow the printed commands.
- **macOS MagicDNS names don't resolve** → the tool prepends `100.100.100.100`
  to the primary network service's DNS servers; if that changed (e.g. a manual
  network reset) set it again in System Settings → Network → Details → DNS.
  Subnet routing works regardless — see
  [ACL and networking](ACL_AND_NETWORKING.md#magicdns-specifics).
- **Can't reach LAN devices** → verify routes are accepted on the accepting
  device (`tailscale up --accept-routes`), and that the advertised CIDR does
  not overlap the local subnet (see
  [ACL and networking](ACL_AND_NETWORKING.md#dont-overlap-subnets)).

## Safety notes

- Touches **no user data** — the only file it deletes is its own downloaded
  package in the temp folder.
- All downloads are over HTTPS from the official `pkgs.tailscale.com` and are
  SHA-256 verified before running.
- The biggest risk is trusting an unsigned installer running with admin rights;
  keep binaries on a private channel and regenerate the auth key before each
  deployment. The auth key is embedded in the binary, so anyone holding it can
  join your tailnet — use a reusable key with a short expiry, and revoke it
  after the device is connected (see [Deploying](DEPLOYING.md#what-you-should-do-afterwards)).