@echo off
setlocal
cd /d "%~dp0"

set CGO_ENABLED=0

echo ==^> Installing go-winres (embeds the UAC manifest into Windows exe) ...
go install github.com/tc-hib/go-winres@v0.3.3
for /f "usebackq delims=" %%i in (`go env GOPATH`) do set "PATH=%%i\bin;%PATH%"

echo ==^> Generating Windows resources from main.exe.manifest ...
go-winres make

rem Read the auth key from the gitignored .authkey file (first line) so the
rem secret never lands in git. If it is missing, the build still succeeds with
rem the placeholder and the binaries will not authenticate.
set LDEXTRA=
if exist .authkey (
  set /p KEY=<.authkey
  if defined KEY set "LDEXTRA=-X main.authKey=%KEY%"
)
if "%LDEXTRA%"=="" echo WARNING: .authkey not found - building with placeholder key (will NOT authenticate).

rem Stamp the build version into the binaries for diagnostics (git short sha,
rem or the date outside a git checkout).
set VERSION=dev
for /f "delims=" %%i in ('git rev-parse --short HEAD 2^>nul') do set VERSION=%%i
if "%VERSION%"=="dev" (
  for /f "delims=" %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMddHHmm"') do set VERSION=%%i
)
set "LDEXTRA=%LDEXTRA% -X main.buildVersion=%VERSION%"

rem Build with Go 1.20 so the Windows binaries also run on Windows 7/8.
rem (Binaries produced by Go ^>=1.21 refuse to start on Win7/8.)
set GOTOOLCHAIN=go1.20.14

if not exist dist mkdir dist

call :build windows amd64
call :build windows arm64
call :build windows 386
call :build darwin amd64
call :build darwin arm64
call :build linux amd64
call :build linux arm64
call :build linux arm 6
call :build linux 386
goto :done

:build
setlocal
set "os=%~1"
set "arch=%~2"
set "goarm=%~3"
set "name=dist\TailscaleMe-%os%-%arch%"
if "%os%"=="windows" set "name=%name%.exe"
echo ==^> %name%
set GOOS=%os%
set GOARCH=%arch%
set GOARM=%goarm%
go build -trimpath -ldflags="-s -w %LDEXTRA%" -o "%name%" .
endlocal & exit /b %errorlevel%

:done
echo.
echo Done. Binaries in dist\:
dir /b dist
echo.
echo Sanity check (windows amd64): must embed the requireAdministrator manifest.
findstr /i "requireAdministrator" dist\TailscaleMe-windows-amd64.exe >nul
if %errorlevel%==0 (
  echo OK - UAC elevation manifest is embedded.
) else (
  echo FAIL - manifest NOT embedded; do not ship these binaries.
  exit /b 1
)