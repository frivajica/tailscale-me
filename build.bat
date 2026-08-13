@echo off
setlocal
cd /d "%~dp0"

rem Build-time config can come from the command line, an environment variable,
rem a gitignored file, or main.go's default - checked in that order. Values
rem passed as args/env can appear in the process list, so prefer the files.
rem
rem   --auth-key <key>    ->  AUTHKEY env     ->  .authkey     -> placeholder
rem   --ssh-key <pubkey>  ->  SSHKEY env      ->  .sshkey      -> "" (skip)
rem   --allow-cidr <cidr> ->  ALLOWCIDR env   ->  .allow-cidr  -> 100.64.0.0/10

set AUTHKEY_ARG=
set SSHKEY_ARG=
set ALLOWCIDR_ARG=

:args
if "%~1"=="" goto args_done
if /i "%~1"=="--auth-key" (
  if "%~2"=="" (
    echo ERROR: --auth-key needs a value
    goto usage_err
  )
  set "AUTHKEY_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--ssh-key" (
  if "%~2"=="" (
    echo ERROR: --ssh-key needs a value
    goto usage_err
  )
  set "SSHKEY_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--allow-cidr" (
  if "%~2"=="" (
    echo ERROR: --allow-cidr needs a value
    goto usage_err
  )
  set "ALLOWCIDR_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--help" goto help
if /i "%~1"=="-h" goto help
echo ERROR: unknown option: %~1
goto usage_err

:args_done
goto prog

:help
call :usage
exit /b 0

:usage_err
call :usage
exit /b 1

:usage
echo Usage: build.bat [options]
echo.
echo Options:
echo   --auth-key ^<key^>     Tailscale auth key ^(env AUTHKEY, file .authkey^)
echo   --ssh-key ^<pubkey^>   admin SSH public key for Windows OpenSSH ^(env SSHKEY, file .sshkey^)
echo   --allow-cidr ^<cidr^>  Windows SSH firewall scope ^(env ALLOWCIDR, file .allow-cidr^)
echo   --help                show this help and exit
echo.
echo Each value is taken from the first source that provides one: command
echo line, then environment variable, then the matching gitignored file,
echo then the default compiled into main.go.
exit /b 0

:prog
set CGO_ENABLED=0

echo ==^> Installing go-winres (embeds the UAC manifest into Windows exe) ...
go install github.com/tc-hib/go-winres@v0.3.3
for /f "usebackq delims=" %%i in (`go env GOPATH`) do set "PATH=%%i\bin;%PATH%"

echo ==^> Generating Windows resources (installers: 386+amd64+arm64, launcher: 386) ...
go-winres make --arch=386,amd64,arm64
pushd launcher\windows
go-winres make --arch=386
popd

rem Resolve build-time config: flag -> env -> gitignored file -> main.go default.
rem EnableDelayedExpansion is required so variables set inside the
rem parenthesized blocks below (!AKEY! etc.) expand to the freshly-assigned
rem values rather than the pre-block empty value.
setlocal EnableDelayedExpansion
set LDEXTRA=

rem Auth key: if nothing is provided anywhere, the placeholder stays in and
rem the resulting binaries will not authenticate.
set "AKEY=%AUTHKEY_ARG%"
if not defined AKEY set "AKEY=%AUTHKEY%"
if not defined AKEY if exist .authkey set /p AKEY=<.authkey
if defined AKEY (
  set "LDEXTRA=-X main.authKey=!AKEY!"
)
if not defined AKEY echo WARNING: no auth key (--auth-key / AUTHKEY env / .authkey) - building with placeholder key (will NOT authenticate).

rem Admin SSH public key for Windows OpenSSH: left empty, the Windows SSH step
rem is skipped (see main.go). Linux/macOS Tailscale SSH does not need it.
set "SKEY=%SSHKEY_ARG%"
if not defined SKEY set "SKEY=%SSHKEY%"
if not defined SKEY if exist .sshkey set /p SKEY=<.sshkey
if defined SKEY (
  rem Public keys contain spaces; the single quotes survive the go ldflags
  rem tokenizer so the whole key lands in main.sshKey.
  set "LDEXTRA=!LDEXTRA! -X 'main.sshKey=!SKEY!'"
)

rem Windows SSH firewall scope: the default 100.64.0.0/10 (Tailscale only) is
rem already compiled into main.go.
set "CIDR=%ALLOWCIDR_ARG%"
if not defined CIDR set "CIDR=%ALLOWCIDR%"
if not defined CIDR if exist .allow-cidr set /p CIDR=<.allow-cidr
if defined CIDR set "LDEXTRA=!LDEXTRA! -X main.sshAllowCIDR=!CIDR!"

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