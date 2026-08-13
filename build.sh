#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

export CGO_ENABLED=0

go mod download 2>/dev/null || true

echo "==> Installing go-winres (embeds the UAC manifest into the exe) ..."
go install github.com/tc-hib/go-winres@latest
export PATH="$PATH:$(go env GOPATH)/bin"

echo "==> Generating Windows resources from main.exe.manifest ..."
go-winres make

# Build with Go 1.20 so TailscaleMe.exe itself also runs on Windows 7/8.
# (Binaries produced by Go >=1.21 refuse to start on Win7/8.)
export GOTOOLCHAIN=go1.20.14

echo "==> Cross-compiling TailscaleMe.exe (windows/amd64) ..."
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o TailscaleMe.exe .

echo ""
echo "Done: $(pwd)/TailscaleMe.exe"
echo "Sanity check: does the built exe embed the requireAdministrator manifest?"
if grep -aqi "requireAdministrator" TailscaleMe.exe; then
  echo "OK - UAC elevation manifest is embedded."
else
  echo "FAIL - manifest NOT embedded; do not ship this binary."
  exit 1
fi