package api

import (
	"bytes"
	"io"
	"net/http"
	"path"
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

const installScriptTmplPS1 = `#requires -RunAsAdministrator
<#
  ScanRay Sonar — probe install one-liner (Windows).

  Required environment variables:
    INSTALL_TOKEN   Single-use enrollment token from the Sonar UI.
    SONAR_BASE      Base URL of sonar-api (defaults to the host that
                    served this script).
#>

$ErrorActionPreference = 'Stop'

if (-not $env:INSTALL_TOKEN) {
  Write-Error "INSTALL_TOKEN environment variable is required"
  exit 2
}
$Token = $env:INSTALL_TOKEN
$Base  = if ($env:SONAR_BASE) { $env:SONAR_BASE } else { '{{.Base}}' }
$Base  = $Base.TrimEnd('/')

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

# If the service is already installed (re-enroll), stop + delete it
# first so 'sc.exe create' below succeeds and the new binary isn't
# locked by a running process.
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
  Write-Host "==> stopping existing $ServiceName service"
  Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
  & sc.exe delete $ServiceName | Out-Null
  Start-Sleep -Seconds 2
}

Write-Host "==> enrolling host"
& $Bin enroll --token=$Token --base=$Base --config=$Cfg
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "==> registering Windows service"
$BinPath = '"' + $Bin + '" run --config="' + $Cfg + '"'
& sc.exe create $ServiceName binPath= $BinPath start= auto DisplayName= "ScanRay Sonar Probe" | Out-Null
& sc.exe description $ServiceName "Endpoint agent for ScanRay Sonar (phones home over WSS)." | Out-Null
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
	_ = tmpl.Execute(w, map[string]string{"Base": s.installerBase(r)})
}
