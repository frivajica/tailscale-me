#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Build-time config can come from the command line, an environment variable,
# a gitignored file, or main.go's default - checked in that order:
#
#   --auth-key <key>      -> $AUTHKEY   -> .authkey    -> placeholder (non-auth)
#   --ssh-key <pubkey>    -> $SSHKEY    -> .sshkey     -> "" (Windows SSH off)
#   --allow-cidr <cidr>   -> $ALLOWCIDR -> .allow-cidr -> 100.64.0.0/10
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
  --help               show this help and exit

Each value is taken from the first source that provides one: command line,
then environment variable, then the matching gitignored file, then the
default compiled into main.go.
EOF
}

AUTHKEY_ARG="" SSHKEY_ARG="" ALLOWCIDR_ARG=""
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