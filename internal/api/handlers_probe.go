package api

import (
	"bytes"
	"io"
	"net/http"
	"path"
	"strings"
	"text/template"

	"github.com/go-chi/chi/v5"
)

// allowedProbePlatforms restricts the probe download endpoint to the
// OS/arch combinations we actually publish. Anything else returns 404
// rather than echoing a path back to the client. Keep in sync with the
// Dockerfile probebuild stage and probeArtifact below.
var allowedProbePlatforms = map[string]map[string]bool{
	"linux":   {"amd64": true, "arm64": true},
	"windows": {"amd64": true},
}

// probeArtifact returns the on-disk filename inside the embedded
// probebins FS for a given OS, plus the content-disposition filename
// the browser/curl will see. Windows binaries get the ".exe" suffix
// in both cases so PowerShell's Invoke-WebRequest (and operators
// inspecting the download) recognise them.
func probeArtifact(osName string) string {
	if osName == "windows" {
		return "sonar-probe.exe"
	}
	return "sonar-probe"
}

func (s *Server) handleProbeDownload(w http.ResponseWriter, r *http.Request) {
	osName := chi.URLParam(r, "os")
	arch := chi.URLParam(r, "arch")
	if !allowedProbePlatforms[osName][arch] {
		writeErr(w, http.StatusNotFound, "not_found", "no probe build for that os/arch")
		return
	}
	if s.probeFS == nil {
		writeErr(w, http.StatusNotFound, "not_found", "probe binaries not bundled with this build")
		return
	}
	fname := probeArtifact(osName)
	name := path.Join(osName, arch, fname)
	f, err := s.probeFS.Open(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", "probe binary missing from image")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "stat failed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	http.ServeContent(w, r, fname, info.ModTime(), readSeekerOrBuffer(f))
}

// readSeekerOrBuffer returns the underlying file as an io.ReadSeeker if
// it implements one, falling back to buffering the whole thing in memory.
// fs.File implementations from go:embed do not implement Seek, so the
// fallback is the common path.
func readSeekerOrBuffer(f io.Reader) io.ReadSeeker {
	if rs, ok := f.(io.ReadSeeker); ok {
		return rs
	}
	buf, _ := io.ReadAll(f)
	return bytes.NewReader(buf)
}

// installerBase returns the base URL that should be baked into the
// install scripts. Prefers the configured PublicURL (so prod always
// points at the canonical Cloudflare hostname) and falls back to the
// inbound request's host so a freshly-installed instance still
// produces a working one-liner before SONAR_PUBLIC_URL is set.
func (s *Server) installerBase(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// ---- Linux installer (bash) ----------------------------------------------

const installScriptTmplBash = `#!/usr/bin/env bash
# ScanRay Sonar — probe install one-liner.
# Run as root. Required env vars:
#   INSTALL_TOKEN   single-use enrollment token from the Sonar UI
#   SONAR_BASE      base URL of sonar-api (e.g. https://sonar.example.com)
#                   defaults to the host that served this script

set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "this installer must run as root" >&2; exit 1
fi
if [[ -z "${INSTALL_TOKEN:-}" ]]; then
  echo "INSTALL_TOKEN is required" >&2; exit 2
fi
SONAR_BASE="${SONAR_BASE:-{{.Base}}}"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 3 ;;
esac

INSTALL_DIR="/usr/local/bin"
ETC_DIR="/etc/sonar-probe"
BIN="$INSTALL_DIR/sonar-probe"

mkdir -p "$ETC_DIR"
chmod 700 "$ETC_DIR"

echo "==> downloading sonar-probe (linux/$ARCH) from $SONAR_BASE"
curl -fsSL --output "$BIN.tmp" "$SONAR_BASE/api/v1/probe/download/linux/$ARCH"
chmod +x "$BIN.tmp"
mv "$BIN.tmp" "$BIN"

echo "==> enrolling host"
"$BIN" enroll --token="$INSTALL_TOKEN" --base="$SONAR_BASE" --config="$ETC_DIR/agent.json"

cat >/etc/systemd/system/sonar-probe.service <<UNIT
[Unit]
Description=ScanRay Sonar Probe
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN run --config=$ETC_DIR/agent.json
Restart=always
RestartSec=5
User=root
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now sonar-probe.service

echo "==> sonar-probe installed and started"
systemctl --no-pager --lines=5 status sonar-probe.service || true
`

func (s *Server) handleProbeInstallScript(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("install").Parse(installScriptTmplBash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "template parse")
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_ = tmpl.Execute(w, map[string]string{"Base": s.installerBase(r)})
}

// ---- Windows installer (PowerShell) --------------------------------------

// installScriptTmplPS1 is the rendered Windows installer. The token and
// base URL are baked in at render time when the operator fetches the
// script via /probe/install.ps1?token=...&base=..., which is how the
// one-liner avoids the $env:INSTALL_TOKEN expansion trap when pasted
// into an already-running PowerShell prompt. Direct manual usage
// (`iwr ... | iex` without query params) still works via the env-var
// fallback below.
//
// Service registration uses New-Service rather than `sc.exe create`.
// The PowerShell call operator + sc.exe combination has well-known
// quoting bugs around binPath= values that contain both spaces and
// embedded double quotes (which ours always does); New-Service talks
// to the SCM API directly so the BinaryPathName is stored verbatim
// without command-line marshaling.
const installScriptTmplPS1 = `#requires -RunAsAdministrator
<#
  ScanRay Sonar — probe install one-liner (Windows).

  Inputs (any one):
    1. ?token=<TOKEN> query string when the script is fetched
       (this is what the UI's one-liner uses).
    2. INSTALL_TOKEN environment variable (manual usage).

  Optional:
    SONAR_BASE   Override the base URL baked in at render time.
#>

$ErrorActionPreference = 'Stop'

# The Token/Base placeholders are replaced server-side. An empty
# baked token means the script was fetched without ?token=, so fall
# back to the env var. We use sequential 'if' statements instead of
# the expression form because that needs backtick line-continuations,
# and this whole script lives inside a Go raw-string literal where
# backticks are the string terminator.
$BakedToken = '{{.Token}}'
$BakedBase  = '{{.Base}}'

$Token = $null
if ($BakedToken) { $Token = $BakedToken }
elseif ($env:INSTALL_TOKEN) { $Token = $env:INSTALL_TOKEN }
if (-not $Token) {
  Write-Error "Missing token. Fetch this script with ?token=<TOKEN> or set the INSTALL_TOKEN env var."
  exit 2
}

if ($env:SONAR_BASE) { $Base = $env:SONAR_BASE } else { $Base = $BakedBase }
$Base = $Base.TrimEnd('/')

switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { $Arch = 'amd64' }
  default {
    Write-Error "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
    exit 3
  }
}

$InstallDir  = Join-Path $env:ProgramFiles 'Sonar'
$DataDir     = Join-Path $env:ProgramData  'Sonar'
$Bin         = Join-Path $InstallDir 'sonar-probe.exe'
$Cfg         = Join-Path $DataDir    'agent.json'
$ServiceName = 'SonarProbe'

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir    | Out-Null

Write-Host "==> downloading sonar-probe (windows/$Arch) from $Base"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -UseBasicParsing -Uri "$Base/api/v1/probe/download/windows/$Arch" -OutFile "$Bin.tmp"
Move-Item -Force -Path "$Bin.tmp" -Destination $Bin

# Re-enroll path: stop + delete the existing service so the new
# binary isn't locked and New-Service below doesn't collide. Wait
# until the SCM has actually finished removing it before continuing
# (sc.exe delete is asynchronous; the entry can linger for a few
# seconds while existing handles drain).
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Write-Host "==> stopping existing $ServiceName service"
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  & sc.exe delete $ServiceName | Out-Null
  $deadline = (Get-Date).AddSeconds(15)
  while ((Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) -and ((Get-Date) -lt $deadline)) {
    Start-Sleep -Milliseconds 250
  }
  if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Write-Error "Existing $ServiceName service did not unregister within 15s; aborting."
    exit 4
  }
}

Write-Host "==> enrolling host"
& $Bin enroll --token=$Token --base=$Base --config=$Cfg
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "==> registering Windows service ($ServiceName)"
# New-Service stores BinaryPathName verbatim via the SCM API, so we
# just need a properly-quoted Win32 command line. Windows itself
# parses this string with CommandLineToArgvW when starting the
# service, so embedded quotes around paths-with-spaces are correct
# here (and would NOT be if we tried to round-trip through sc.exe
# via PowerShell's call operator).
$BinaryPathName = '"{0}" run --config="{1}"' -f $Bin, $Cfg
# Splatting via a hashtable so we don't need PowerShell line
# continuation backticks inside this Go raw-string literal.
$svcParams = @{
  Name           = $ServiceName
  BinaryPathName = $BinaryPathName
  DisplayName    = 'ScanRay Sonar Probe'
  Description    = 'Endpoint agent for ScanRay Sonar (phones home over WSS).'
  StartupType    = 'Automatic'
}
New-Service @svcParams | Out-Null

# Sanity check: confirm the SCM actually has the service before we
# try to start it. If New-Service somehow no-op'd (it shouldn't with
# -ErrorAction Stop, but defense in depth) we want a clear error
# instead of a confusing "Cannot find any service" later.
if (-not (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue)) {
  Write-Error "Service $ServiceName was not registered."
  exit 5
}

# Auto-restart on crash: 5s, 5s, 5s; reset failure counter after 60s
# of healthy uptime. sc.exe failure only takes the service name +
# scalar flags so the PowerShell call-operator quoting trap doesn't
# apply here.
& sc.exe failure $ServiceName reset= 60 actions= restart/5000/restart/5000/restart/5000 | Out-Null

Start-Service -Name $ServiceName

Write-Host "==> sonar-probe installed and started"
Get-Service -Name $ServiceName
`

func (s *Server) handleProbeInstallScriptPS1(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("install-ps1").Parse(installScriptTmplPS1)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "template parse")
		return
	}
	// text/plain so PowerShell doesn't try to handle the response as a
	// download offer when fetched via Invoke-WebRequest. The .ps1
	// extension is enough cue for humans.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	base := s.installerBase(r)
	if b := strings.TrimSpace(r.URL.Query().Get("base")); b != "" {
		base = strings.TrimRight(b, "/")
	}
	// Token comes from ?token= and is interpolated into the rendered
	// script as a literal PowerShell single-quoted string. Reject any
	// character that would break out of that string so the template
	// can never be coerced into executing additional code, even if a
	// hostile caller invents the URL by hand. Tokens issued by this
	// server are base64url so the allowed alphabet is more than
	// sufficient; we deliberately omit the equals sign because raw
	// base64url uses no padding.
	token := r.URL.Query().Get("token")
	if token != "" && !validInstallToken(token) {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid token characters")
		return
	}

	_ = tmpl.Execute(w, map[string]string{
		"Base":  base,
		"Token": token,
	})
}

// validInstallToken returns true iff s contains only base64url
// characters (the alphabet used by handleCreateEnrollmentToken).
// Anything else (including ' " $ ; backtick newline) would let a
// caller break out of the single-quoted PowerShell literal we splat
// the token into, so we refuse to render the script at all.
func validInstallToken(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_':
			// allowed
		default:
			return false
		}
	}
	return true
}
