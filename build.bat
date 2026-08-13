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
rem   --ssh-password <pw> ->  SSHPASSWORD env ->  .sshpassword -> "" (random)
rem   --ssh-password-auth <no|keep> -> SSHPASSAUTH env -> .sshpassauth -> "" (key-only)

set AUTHKEY_ARG=
set SSHKEY_ARG=
set ALLOWCIDR_ARG=
set SSHPASS_ARG=
set SSHPASSAUTH_ARG=
set APITOKEN_ARG=
set CLIENTID_ARG=
set CLIENTSECRET_ARG=
set TAILNET_ARG=
set TAG_ARG=
set STRICT_ACL_ARG=

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
if /i "%~1"=="--ssh-password" (
  if "%~2"=="" (
    echo ERROR: --ssh-password needs a value
    goto usage_err
  )
  set "SSHPASS_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--ssh-password-auth" (
  if "%~2"=="" (
    echo ERROR: --ssh-password-auth needs a value
    goto usage_err
  )
  set "SSHPASSAUTH_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--api-token" (
  if "%~2"=="" (
    echo ERROR: --api-token needs a value
    goto usage_err
  )
  set "APITOKEN_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--client-id" (
  if "%~2"=="" (
    echo ERROR: --client-id needs a value
    goto usage_err
  )
  set "CLIENTID_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--client-secret" (
  if "%~2"=="" (
    echo ERROR: --client-secret needs a value
    goto usage_err
  )
  set "CLIENTSECRET_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--tailnet" (
  if "%~2"=="" (
    echo ERROR: --tailnet needs a value
    goto usage_err
  )
  set "TAILNET_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--tag" (
  if "%~2"=="" (
    echo ERROR: --tag needs a value
    goto usage_err
  )
  set "TAG_ARG=%~2"
  shift
  shift
  goto args
)
if /i "%~1"=="--strict-acl" (
  set "STRICT_ACL_ARG=1"
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

:badpw
echo ERROR: --ssh-password / SSHPASSWORD env / .sshpassword must not contain ' or ! ^(also avoid " ^- build.sh rejects all three^).
exit /b 1

:usage
echo Usage: build.bat [options]
echo.
echo Options:
echo   --auth-key ^<key^>     Tailscale auth key ^(env AUTHKEY, file .authkey^)
echo   --ssh-key ^<pubkey^>   admin SSH public key for Windows OpenSSH ^(env SSHKEY, file .sshkey^)
echo   --allow-cidr ^<cidr^>  Windows SSH firewall scope ^(env ALLOWCIDR, file .allow-cidr^)
echo   --ssh-password ^<pw^>   optional Windows password when an admin account has none ^(env SSHPASSWORD, file .sshpassword; empty = random per machine^)
echo   --ssh-password-auth ^<no^|keep^>
echo                        Windows SSH password auth: ""/no = key-only on fresh installs; keep = leave enabled ^(env SSHPASSAUTH, file .sshpassauth^)
echo   --api-token ^<token^>  Tailscale API token to verify the ACL covers SSH ^(env TS_API_TOKEN^)
echo   --client-id ^<id^>     OAuth client ID for ACL check ^(env TS_OAUTH_CLIENT_ID^)
echo   --client-secret ^<s^>  OAuth client secret for ACL check ^(env TS_OAUTH_CLIENT_SECRET^)
echo   --tailnet ^<name^>     tailnet for the ACL check ^(default: the token's tailnet^)
echo   --tag ^<tag:managed^>  managed tag the ACL check requires ^(env TS_TAG^)
echo   --strict-acl          fail the build when an ACL rule is missing
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

rem ---- Password validation (BEFORE EnableDelayedExpansion) ------------------
rem These checks MUST run before setlocal EnableDelayedExpansion so that !
rem stays a literal character and can be detected reliably. The resolved
rem values (PWV, PWAV) are then injected later inside the delayed block.
rem ---------------------------------------------------------------------------

set "PWV=%SSHPASS_ARG%"
if not defined PWV set "PWV=%SSHPASSWORD%"
if not defined PWV if exist .sshpassword set /p PWV=<.sshpassword

rem Reject single quotes and exclamation marks - they break the ldflags quoting
rem or the delayed expansion used below. Checked BEFORE EnableDelayedExpansion
rem so ! stays a literal. (A " in the password would also break the ldflags;
rem build.sh rejects all three - batch covers the two that are reliable here.)
if defined PWV if not "%PWV:'=%"=="%PWV%" goto badpw
if defined PWV if not "%PWV:!=%"=="%PWV%" goto badpw

rem Windows SSH password authentication: ""/no (default) = fresh OpenSSH
rem installs are hardened to key-only; keep = password auth stays enabled.
set "PWAV=%SSHPASSAUTH_ARG%"
if not defined PWAV set "PWAV=%SSHPASSAUTH%"
if not defined PWAV if exist .sshpassauth set /p PWAV=<.sshpassauth
if defined PWAV if /i not "%PWAV%"=="no" if /i not "%PWAV%"=="keep" (
  echo ERROR: --ssh-password-auth / SSHPASSAUTH env / .sshpassauth must be "no" or "keep".
  exit /b 1
)

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

rem Windows SSH account password: injected here (validated earlier, pre-delayed).
if defined PWV set "LDEXTRA=!LDEXTRA! -X 'main.sshPassword=!PWV!'"
rem Windows SSH password auth mode: ""/no = key-only on fresh installs; keep = leave enabled.
if defined PWAV set "LDEXTRA=!LDEXTRA! -X main.sshPasswordAuth=!PWAV!"

rem Admin SSH public key for Windows OpenSSH: left empty, the Windows SSH step
rem is skipped (see main.go). Linux/macOS Tailscale SSH does not need it.
set "SKEY=%SSHKEY_ARG%"
if not defined SKEY set "SKEY=%SSHKEY%"
if not defined SKEY if exist .sshkey set /p SKEY=<.sshkey
if defined SKEY (
  rem Public keys contain spaces; the single quotes survive the go ldflags
  rem tokenizer so the whole key lands in main.sshKey.
  set "LDEXTRA=!LDEXTRA! -X 'main.sshKey=!SKEY!'"

  rem Foolproof the key before shipping: a private key or mangled paste would
  rem silently produce an authorized_keys that refuses every login. findstr
  rem checks the public-key type prefix, ssh-keygen the full parse.
  where ssh-keygen >nul 2>nul
  set "HAS_KEYGEN="
  if not errorlevel 1 set "HAS_KEYGEN=1"
  set "KEYCHK=%TEMP%\tsm-keychk"
  >"%KEYCHK%" echo !SKEY!
  findstr /r /b /c:"ssh-ed25519 " /c:"ssh-rsa " /c:"ecdsa-sha2-" /c:"ssh-dss " /c:"sk-ssh-ed25519 " /c:"sk-ecdsa-sha2-" "%KEYCHK%" >nul 2>nul
  if errorlevel 1 (
    del /q "%KEYCHK%" >nul 2>nul
    echo ERROR: --ssh-key / SSHKEY env / .sshkey is not a valid OpenSSH public key ^(expected it to start with e.g. 'ssh-ed25519 '^).
    exit /b 1
  )
  if defined HAS_KEYGEN (
    ssh-keygen -l -f "%KEYCHK%" -E sha256 >nul 2>nul
    if errorlevel 1 (
      del /q "%KEYCHK%" >nul 2>nul
      echo ERROR: --ssh-key / SSHKEY env / .sshkey does not parse as a public key.
      echo        Expected e.g.: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... you@host
      exit /b 1
    )
  ) else (
    echo WARNING: ssh-keygen not found - skipping SSH key parse validation.
  )
  del /q "%KEYCHK%" >nul 2>nul
)

rem Windows SSH firewall scope: the default 100.64.0.0/10 (Tailscale only) is
rem already compiled into main.go.
set "CIDR=%ALLOWCIDR_ARG%"
if not defined CIDR set "CIDR=%ALLOWCIDR%"
if not defined CIDR if exist .allow-cidr set /p CIDR=<.allow-cidr
if defined CIDR set "LDEXTRA=!LDEXTRA! -X main.sshAllowCIDR=!CIDR!"

rem Optional preflight: verify the tailnet ACL covers the SSH rules. Only runs
rem when an API token or OAuth credentials are provided; --strict-acl turns a
rem missing rule into a hard build failure.
set "APITOKEN=%APITOKEN_ARG%"
if not defined APITOKEN set "APITOKEN=%TS_API_TOKEN%"
set "OAUTHID=%CLIENTID_ARG%"
if not defined OAUTHID set "OAUTHID=%TS_OAUTH_CLIENT_ID%"
set "OAUTHSECRET=%CLIENTSECRET_ARG%"
if not defined OAUTHSECRET set "OAUTHSECRET=%TS_OAUTH_CLIENT_SECRET%"
if defined APITOKEN (
  set "TAGV=%TAG_ARG%"
  if not defined TAGV set "TAGV=%TS_TAG%"
  if not defined TAGV set "TAGV=tag:managed"
  set "TAILNETARG=%TAILNET_ARG%"
  if not defined TAILNETARG set "TAILNETARG=%TS_TAILNET%"
  echo ==^> Verifying your tailnet ACL covers the SSH rules (tools/aclcheck) ...
  set "ACLARGS=--token !APITOKEN! --tag !TAGV!"
  if defined TAILNETARG set "ACLARGS=!ACLARGS! --tailnet !TAILNETARG!"
  if defined STRICT_ACL_ARG set "ACLARGS=!ACLARGS! --strict"
  go run ./tools/aclcheck !ACLARGS!
  if errorlevel 1 exit /b 1
) else if defined OAUTHID (
  set "TAGV=%TAG_ARG%"
  if not defined TAGV set "TAGV=%TS_TAG%"
  if not defined TAGV set "TAGV=tag:managed"
  set "TAILNETARG=%TAILNET_ARG%"
  if not defined TAILNETARG set "TAILNETARG=%TS_TAILNET%"
  echo ==^> Verifying your tailnet ACL covers the SSH rules (tools/aclcheck) ...
  set "ACLARGS=--client-id !OAUTHID! --client-secret !OAUTHSECRET! --tag !TAGV!"
  if defined TAILNETARG set "ACLARGS=!ACLARGS! --tailnet !TAILNETARG!"
  if defined STRICT_ACL_ARG set "ACLARGS=!ACLARGS! --strict"
  go run ./tools/aclcheck !ACLARGS!
  if errorlevel 1 exit /b 1
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
echo ==^> Zipping Windows and macOS binaries (avoids SmartScreen/zone blocks) ...
powershell -NoProfile -Command "Compress-Archive -Path 'dist\TailscaleMe-windows.exe' -DestinationPath 'dist\TailscaleMe-windows.zip' -Force"
powershell -NoProfile -Command "Compress-Archive -Path 'dist\TailscaleMe-darwin-amd64' -DestinationPath 'dist\TailscaleMe-darwin-amd64.zip' -Force"
powershell -NoProfile -Command "Compress-Archive -Path 'dist\TailscaleMe-darwin-arm64' -DestinationPath 'dist\TailscaleMe-darwin-arm64.zip' -Force"

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