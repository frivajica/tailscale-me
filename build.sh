#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Build-time config can come from the command line, an environment variable,
# a gitignored file, or main.go's default - checked in that order:
#
#   --auth-key <key>      -> $AUTHKEY   -> .authkey    -> placeholder (non-auth)
#   --ssh-key <pubkey>    -> $SSHKEY    -> .sshkey     -> "" (Windows SSH off)
#   --allow-cidr <cidr>   -> $ALLOWCIDR -> .allow-cidr -> 100.64.0.0/10
#   --ssh-password <pw>   -> $SSHPASSWORD -> .sshpassword -> "" (random per machine)
#   --ssh-password-auth <no|keep> -> $SSHPASSAUTH -> .sshpassauth -> "" (key-only)
#
# Values passed as args/env can appear in the shell history or process list,
# so prefer the gitignored files for repeatable builds.
usage() {
  cat <<'EOF'
Usage: build.sh [options]

Options:
  --auth-key <key>     Tailscale auth key (env AUTHKEY, file .authkey)
  --ssh-key <pubkey>   admin SSH public key for Windows OpenSSH (env SSHKEY, file .sshkey)
  --allow-cidr <cidr>  Windows SSH firewall scope (env ALLOWCIDR, file .allow-cidr)
  --ssh-password <pw>  optional Windows password to set when an admin account has none (env SSHPASSWORD, file .sshpassword; empty = random per machine)
  --ssh-password-auth <no|keep>
                       Windows SSH password auth: ""/no (default) = key-only on fresh installs; keep = leave password auth enabled (env SSHPASSAUTH, file .sshpassauth)
  --api-token <token>  Tailscale API token to verify the ACL covers SSH (env TS_API_TOKEN)
  --client-id <id>     OAuth client ID for ACL check (env TS_OAUTH_CLIENT_ID)
  --client-secret <s>  OAuth client secret for ACL check (env TS_OAUTH_CLIENT_SECRET)
  --tailnet <name>     tailnet for the ACL check (default: the token's tailnet)
  --tag <tag:managed>  managed tag the ACL check requires (env TS_TAG)
  --strict-acl         fail the build when an ACL rule is missing
  --help               show this help and exit

Each value is taken from the first source that provides one: command line,
then environment variable, then the matching gitignored file, then the
default compiled into main.go.
EOF
}

AUTHKEY_ARG="" SSHKEY_ARG="" ALLOWCIDR_ARG="" SSHPASS_ARG="" SSHPASSAUTH_ARG=""
APITOKEN_ARG="" CLIENTID_ARG="" CLIENTSECRET_ARG="" TAILNET_ARG="" TAG_ARG="" STRICT_ACL_ARG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --auth-key)
      [ $# -ge 2 ] || { echo "ERROR: --auth-key needs a value" >&2; exit 1; }
      AUTHKEY_ARG="$2"; shift 2 ;;
    --ssh-key)
      [ $# -ge 2 ] || { echo "ERROR: --ssh-key needs a value" >&2; exit 1; }
      SSHKEY_ARG="$2"; shift 2 ;;
    --allow-cidr)
      [ $# -ge 2 ] || { echo "ERROR: --allow-cidr needs a value" >&2; exit 1; }
      ALLOWCIDR_ARG="$2"; shift 2 ;;
    --ssh-password)
      [ $# -ge 2 ] || { echo "ERROR: --ssh-password needs a value" >&2; exit 1; }
      SSHPASS_ARG="$2"; shift 2 ;;
    --ssh-password-auth)
      [ $# -ge 2 ] || { echo "ERROR: --ssh-password-auth needs a value" >&2; exit 1; }
      SSHPASSAUTH_ARG="$2"; shift 2 ;;
    --api-token)
      [ $# -ge 2 ] || { echo "ERROR: --api-token needs a value" >&2; exit 1; }
      APITOKEN_ARG="$2"; shift 2 ;;
    --client-id)
      [ $# -ge 2 ] || { echo "ERROR: --client-id needs a value" >&2; exit 1; }
      CLIENTID_ARG="$2"; shift 2 ;;
    --client-secret)
      [ $# -ge 2 ] || { echo "ERROR: --client-secret needs a value" >&2; exit 1; }
      CLIENTSECRET_ARG="$2"; shift 2 ;;
    --tailnet)
      [ $# -ge 2 ] || { echo "ERROR: --tailnet needs a value" >&2; exit 1; }
      TAILNET_ARG="$2"; shift 2 ;;
    --tag)
      [ $# -ge 2 ] || { echo "ERROR: --tag needs a value" >&2; exit 1; }
      TAG_ARG="$2"; shift 2 ;;
    --strict-acl) STRICT_ACL_ARG=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

export CGO_ENABLED=0

go mod download 2>/dev/null || true

echo "==> Installing go-winres (embeds the UAC manifest into Windows exe) ..."
go install github.com/tc-hib/go-winres@v0.3.3
export PATH="$PATH:$(go env GOPATH)/bin"

echo "==> Generating Windows resources (installers: 386+amd64+arm64, launcher: 386) ..."
go-winres make --arch=386,amd64,arm64
( cd launcher/windows && go-winres make --arch=386 )

# Resolve build-time config: flag -> env -> gitignored file -> main.go default.
LDEXTRA=""

# Auth key: if nothing is provided anywhere, the placeholder stays in and the
# resulting binaries will not authenticate.
AKEY="${AUTHKEY_ARG:-${AUTHKEY:-}}"
if [ -z "$AKEY" ] && [ -f .authkey ]; then
  AKEY=$(tr -d '\r\n' < .authkey)
fi
if [ -n "$AKEY" ]; then
  LDEXTRA="-X main.authKey=$AKEY"
else
  echo "WARNING: no auth key (--auth-key / \$AUTHKEY / .authkey) - building with placeholder key (will NOT authenticate)."
fi

# Admin SSH public key for Windows OpenSSH: left empty, the Windows SSH step
# is skipped (see main.go). Linux/macOS Tailscale SSH does not need it.
SKEY="${SSHKEY_ARG:-${SSHKEY:-}}"
if [ -z "$SKEY" ] && [ -f .sshkey ]; then
  SKEY=$(tr -d '\r\n' < .sshkey)
fi
if [ -n "$SKEY" ]; then
  # Public keys contain spaces; the single quotes survive the go ldflags
  # tokenizer so the whole key lands in main.sshKey.
  LDEXTRA="$LDEXTRA -X 'main.sshKey=$SKEY'"

  # Foolproof the key before shipping: a private key or mangled paste would
  # silently produce an authorized_keys that refuses every login.
  case "$SKEY" in
    ssh-ed25519*|ssh-rsa*|ecdsa-sha2-*|ssh-dss*|sk-ssh-ed25519*|sk-ecdsa-sha2-*)
      ;;
    *)
      echo "ERROR: --ssh-key / \$SSHKEY / .sshkey is not a valid OpenSSH public key (expected it to start with e.g. 'ssh-ed25519 ')." >&2
      exit 1
      ;;
  esac
  if command -v ssh-keygen >/dev/null 2>&1; then
    tmp=$(mktemp)
    printf '%s\n' "$SKEY" > "$tmp"
    if ssh-keygen -l -f "$tmp" -E sha256 >/dev/null 2>&1; then
      rm -f "$tmp"
    else
      rm -f "$tmp"
      echo "ERROR: --ssh-key / \$SSHKEY / .sshkey does not parse as a public key." >&2
      echo "       Expected e.g.: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... you@host" >&2
      exit 1
    fi
  else
    echo "WARNING: ssh-keygen not found - skipping SSH key parse validation."
  fi
fi

# Windows SSH firewall scope: the default 100.64.0.0/10 (Tailscale only) is
# already compiled into main.go.
CIDR="${ALLOWCIDR_ARG:-${ALLOWCIDR:-}}"
if [ -z "$CIDR" ] && [ -f .allow-cidr ]; then
  CIDR=$(tr -d '\r\n' < .allow-cidr)
fi
if [ -n "$CIDR" ]; then
  LDEXTRA="$LDEXTRA -X main.sshAllowCIDR=$CIDR"
fi

# Optional Windows SSH account password: used on Windows only when an admin
# account has NO password (OpenSSH refuses empty-password accounts even for key
# logins). Left empty, a strong random one is generated per machine at setup.
# Single quotes would break the ldflags tokenizer, so they are rejected here.
PW="${SSHPASS_ARG:-${SSHPASSWORD:-}}"
if [ -z "$PW" ] && [ -f .sshpassword ]; then
  PW=$(tr -d '\r\n' < .sshpassword)
fi
if [ -n "$PW" ]; then
  case "$PW" in
    *"'"*|*'"'*|*'!'*) echo "ERROR: --ssh-password / \$SSHPASSWORD / .sshpassword must not contain ' \" or ! (breaks the build)." >&2; exit 1 ;;
  esac
  LDEXTRA="$LDEXTRA -X 'main.sshPassword=$PW'"
fi

# Windows SSH password authentication: ""/no (default) = fresh OpenSSH installs
# are hardened to key-only; keep = password auth stays enabled. Existing OpenSSH
# installs are never touched either way.
PWAuth="${SSHPASSAUTH_ARG:-${SSHPASSAUTH:-}}"
if [ -z "$PWAuth" ] && [ -f .sshpassauth ]; then
  PWAuth=$(tr -d '\r\n' < .sshpassauth)
fi
if [ -n "$PWAuth" ]; then
  case "$PWAuth" in
    no|keep) ;;
    *) echo "ERROR: --ssh-password-auth / \$SSHPASSAUTH / .sshpassauth must be \"no\" or \"keep\" (got \"$PWAuth\")." >&2; exit 1 ;;
  esac
  LDEXTRA="$LDEXTRA -X main.sshPasswordAuth=$PWAuth"
fi

# Optional preflight: verify the tailnet ACL covers the SSH rules. Only runs
# when an API token or OAuth credentials are provided; --strict-acl turns a
# missing rule into a hard build failure (set -e aborts the script).
APITOKEN="${APITOKEN_ARG:-${TS_API_TOKEN:-}}"
CLIENTID="${CLIENTID_ARG:-${TS_OAUTH_CLIENT_ID:-}}"
CLIENTSECRET="${CLIENTSECRET_ARG:-${TS_OAUTH_CLIENT_SECRET:-}}"
if [ -n "$APITOKEN" ] || [ -n "$CLIENTID" ]; then
  TAILNET_ARG="${TAILNET_ARG:-${TS_TAILNET:-}}"
  TAG="${TAG_ARG:-${TS_TAG:-tag:managed}}"
  echo "==> Verifying your tailnet ACL covers the SSH rules (tools/aclcheck) ..."
  ACLARGS=(--tag "$TAG")
  [ -n "$APITOKEN" ] && ACLARGS+=(--token "$APITOKEN")
  [ -n "$CLIENTID" ] && ACLARGS+=(--client-id "$CLIENTID")
  [ -n "$CLIENTSECRET" ] && ACLARGS+=(--client-secret "$CLIENTSECRET")
  [ -n "$TAILNET_ARG" ] && ACLARGS+=(--tailnet "$TAILNET_ARG")
  [ -n "$STRICT_ACL_ARG" ] && ACLARGS+=(--strict)
  go run ./tools/aclcheck "${ACLARGS[@]}"
fi

# Stamp the build version into the binaries for diagnostics (git short sha, or
# the date outside a git checkout).
VERSION=$(git rev-parse --short HEAD 2>/dev/null || date +%Y%m%d%H%M)
LDEXTRA="$LDEXTRA -X main.buildVersion=$VERSION"

# Build with Go 1.20 so the Windows binaries also run on Windows 7/8.
# (Binaries produced by Go >=1.21 refuse to start on Win7/8.)
export GOTOOLCHAIN=go1.20.14

mkdir -p dist

build() { # <os> <arch> [goarm]
  local os=$1 arch=$2 goarm=${3:-}
  local name="dist/TailscaleMe-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  echo "==> $name"
  GOOS="$os" GOARCH="$arch" GOARM="$goarm" \
    go build -trimpath -ldflags="-s -w $LDEXTRA" -o "$name" .
}

# The three Windows per-arch installers are now payload members inside the
# universal launcher (below) and are removed from dist/ afterwards.
build windows amd64
build windows arm64
build windows 386

echo ""
echo "==> Packing universal Windows launcher (contains all 3 per-arch installers) ..."
SHAS=$(go run ./tools/pack shas \
  dist/TailscaleMe-windows-386.exe \
  dist/TailscaleMe-windows-amd64.exe \
  dist/TailscaleMe-windows-arm64.exe)
echo "==> Building launcher (windows/386) ..."
GOOS=windows GOARCH=386 go build -trimpath \
  -ldflags="-s -w $SHAS" \
  -o dist/.launcher-tmp.exe ./launcher/windows
go run ./tools/pack append \
  -out dist/TailscaleMe-windows.exe \
  -launcher dist/.launcher-tmp.exe \
  -386   dist/TailscaleMe-windows-386.exe \
  -amd64 dist/TailscaleMe-windows-amd64.exe \
  -arm64 dist/TailscaleMe-windows-arm64.exe
rm -f dist/.launcher-tmp.exe \
      dist/TailscaleMe-windows-386.exe \
      dist/TailscaleMe-windows-amd64.exe \
      dist/TailscaleMe-windows-arm64.exe

build darwin amd64
build darwin arm64
build linux amd64
build linux arm64
build linux arm 6
build linux 386

echo ""
echo "==> Zipping Windows and macOS binaries (avoids SmartScreen/zone blocks) ..."
if command -v zip >/dev/null 2>&1; then
  (cd dist && zip TailscaleMe-windows.zip TailscaleMe-windows.exe)
  (cd dist && zip TailscaleMe-darwin-amd64.zip TailscaleMe-darwin-amd64)
  (cd dist && zip TailscaleMe-darwin-arm64.zip TailscaleMe-darwin-arm64)
else
  echo "WARNING: zip not found - skipping zip step (binaries may be blocked on Windows/macOS)."
fi

echo ""
echo "Done. Binaries in dist/:"
ls -1 dist/

echo ""
echo "Sanity check (universal Windows launcher): must embed the requireAdministrator manifest."
if grep -aqi "requireAdministrator" dist/TailscaleMe-windows.exe; then
  echo "OK - UAC elevation manifest is embedded."
else
  echo "FAIL - manifest NOT embedded; do not ship these binaries."
  exit 1
fi