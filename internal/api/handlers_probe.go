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
// rather than echoing a path back to the client.
var allowedProbePlatforms = map[string]map[string]bool{
	"linux": {"amd64": true, "arm64": true},
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
	name := path.Join(osName, arch, "sonar-probe")
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
	w.Header().Set("Content-Disposition", `attachment; filename="sonar-probe"`)
	http.ServeContent(w, r, "sonar-probe", info.ModTime(), readSeekerOrBuffer(f))
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

const installScriptTmpl = `#!/usr/bin/env bash
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
	tmpl, err := template.New("install").Parse(installScriptTmpl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "template parse")
		return
	}
	base := s.cfg.PublicURL
	if base == "" {
		// Fall back to the request host so curl-from-this-very-host works
		// during local smoke tests without any public URL configured.
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_ = tmpl.Execute(w, map[string]string{"Base": base})
}
