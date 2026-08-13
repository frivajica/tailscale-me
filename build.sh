#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

export CGO_ENABLED=0

go mod download 2>/dev/null || true

echo "==> Installing go-winres (embeds the UAC manifest into Windows exe) ..."
go install github.com/tc-hib/go-winres@v0.3.3
export PATH="$PATH:$(go env GOPATH)/bin"

echo "==> Generating Windows resources (installers: 386+amd64+arm64, launcher: 386) ..."
go-winres make --arch=386,amd64,arm64
( cd launcher/windows && go-winres make --arch=386 )

# Read the auth key from the gitignored .authkey file (one line) so the secret
# never lands in git. If it is missing, the build still succeeds with the
# placeholder and the binaries will not authenticate.
LDEXTRA=""
if [ -f .authkey ]; then
  KEY=$(tr -d '\r\n' < .authkey)
  if [ -n "$KEY" ]; then
    LDEXTRA="-X main.authKey=$KEY"
  fi
fi
if [ -z "$LDEXTRA" ]; then
  echo "WARNING: .authkey not found - building with placeholder key (will NOT authenticate)."
fi

# Optional SSH config, injected the same way. Both are gitignored; missing
# files just mean the matching feature is compiled in its default state.
if [ -f .sshkey ]; then
  SSHKEY=$(tr -d '\r\n' < .sshkey)
  if [ -n "$SSHKEY" ]; then
    # Public keys contain spaces; the single quotes survive the go ldflags
    # tokenizer so the whole key lands in main.sshKey.
    LDEXTRA="$LDEXTRA -X 'main.sshKey=$SSHKEY'"
  fi
fi
if [ -f .allow-cidr ]; then
  CIDR=$(tr -d '\r\n' < .allow-cidr)
  if [ -n "$CIDR" ]; then
    LDEXTRA="$LDEXTRA -X main.sshAllowCIDR=$CIDR"
  fi
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