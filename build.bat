@echo off
setlocal
cd /d "%~dp0"

set CGO_ENABLED=0

echo ==^> Installing go-winres (embeds the UAC manifest into the exe) ...
go install github.com/tc-hib/go-winres@latest
for /f "usebackq delims=" %%i in (`go env GOPATH`) do set "PATH=%%i\bin;%PATH%"

echo ==^> Generating Windows resources from main.exe.manifest ...
go-winres make

rem Build with Go 1.20 so TailscaleMe.exe itself also runs on Windows 7/8.
rem (Binaries produced by Go ^>=1.21 refuse to start on Win7/8.)
set GOTOOLCHAIN=go1.20.14

echo ==^> Building TailscaleMe.exe (windows/amd64) ...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o TailscaleMe.exe .

echo.
echo Done: %CD%\TailscaleMe.exe
findstr /i "requireAdministrator" TailscaleMe.exe >nul
if %errorlevel%==0 (
  echo OK - UAC elevation manifest is embedded.
) else (
  echo FAIL - manifest NOT embedded; do not ship this binary.
  exit /b 1
)