#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

export CGO_ENABLED=0

go mod download 2>/dev/null || true

echo "==> Installing go-winres (embeds the UAC manifest into Windows exe) ..."
go install github.com/tc-hib/go-winres@latest
export PATH="$PATH:$(go env GOPATH)/bin"

echo "==> Generating Windows resources from main.exe.manifest ..."
go-winres make

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

build windows amd64
build windows arm64
build windows 386
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
echo "Sanity check (windows/amd64): must embed the requireAdministrator manifest."
if grep -aqi "requireAdministrator" dist/TailscaleMe-windows-amd64.exe; then
  echo "OK - UAC elevation manifest is embedded."
else
  echo "FAIL - manifest NOT embedded; do not ship these binaries."
  exit 1
fi