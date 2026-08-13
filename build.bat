@echo off
setlocal
cd /d "%~dp0"

set CGO_ENABLED=0

echo ==^> Installing go-winres (embeds the UAC manifest into Windows exe) ...
go install github.com/tc-hib/go-winres@v0.3.3
for /f "usebackq delims=" %%i in (`go env GOPATH`) do set "PATH=%%i\bin;%PATH%"

echo ==^> Generating Windows resources (installers: 386+amd64+arm64, launcher: 386) ...
go-winres make --arch=386,amd64,arm64
pushd launcher\windows
go-winres make --arch=386
popd

rem Read the auth key from the gitignored .authkey file (first line) so the
rem secret never lands in git. If it is missing, the build still succeeds with
rem the placeholder and the binaries will not authenticate.
rem EnableDelayedExpansion is required so the variables set inside the
rem parenthesized blocks below (%%KEY%% etc.) expand to the freshly-assigned
rem values rather than the pre-block empty value.
setlocal EnableDelayedExpansion
set LDEXTRA=
if exist .authkey (
  set /p KEY=<.authkey
  if defined KEY set "LDEXTRA=-X main.authKey=!KEY!"
)
if "%LDEXTRA%"=="" echo WARNING: .authkey not found - building with placeholder key (will NOT authenticate).

rem Optional SSH config, injected the same way. Both are gitignored; missing
rem files just mean the matching feature is compiled in its default state.
if exist .sshkey (
  rem Public keys contain spaces; the single quotes survive the go ldflags
  rem tokenizer so the whole key lands in main.sshKey.
  set /p SSHKEY=<.sshkey
  if defined SSHKEY set "LDEXTRA=!LDEXTRA! -X 'main.sshKey=!SSHKEY!'"
)
if exist .allow-cidr (
  set /p CIDR=<.allow-cidr
  if defined CIDR set "LDEXTRA=!LDEXTRA! -X main.sshAllowCIDR=!CIDR!"
)

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

rem The three Windows per-arch installers are now payload members inside the
rem universal launcher (below) and are removed from dist\ afterwards.
call :build windows amd64
call :build windows arm64
call :build windows 386

echo.
echo ==^> Packing universal Windows launcher (contains all 3 per-arch installers) ...
for /f "delims=" %%i in ('go run ./tools/pack shas dist\TailscaleMe-windows-386.exe dist\TailscaleMe-windows-amd64.exe dist\TailscaleMe-windows-arm64.exe') do set "SHAS=%%i"
echo ==^> Building launcher (windows/386) ...
set GOOS=windows
set GOARCH=386
go build -trimpath -ldflags="-s -w %SHAS%" -o dist\.launcher-tmp.exe ./launcher/windows
if errorlevel 1 exit /b 1
go run ./tools/pack append -out dist\TailscaleMe-windows.exe -launcher dist\.launcher-tmp.exe -386 dist\TailscaleMe-windows-386.exe -amd64 dist\TailscaleMe-windows-amd64.exe -arm64 dist\TailscaleMe-windows-arm64.exe
if errorlevel 1 exit /b 1
rem Ensure later builds (darwin/linux below) reset the env.
set GOOS=
set GOARCH=
del /q dist\.launcher-tmp.exe dist\TailscaleMe-windows-386.exe dist\TailscaleMe-windows-amd64.exe dist\TailscaleMe-windows-arm64.exe

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
echo Sanity check (universal Windows launcher): must embed the requireAdministrator manifest.
findstr /i "requireAdministrator" dist\TailscaleMe-windows.exe >nul
if %errorlevel%==0 (
  echo OK - UAC elevation manifest is embedded.
) else (
  echo FAIL - manifest NOT embedded; do not ship these binaries.
  exit /b 1
)