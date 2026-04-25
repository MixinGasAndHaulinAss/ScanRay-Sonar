#!/usr/bin/env bash
# Cross-compile the Sonar Probe for every supported endpoint OS/arch.
# Output goes to ./dist/probe/.
#
# Usage: scripts/build-probe.sh [version]
#
# version defaults to the contents of the VERSION file. The git short
# SHA and ISO build time are baked in via -ldflags so /version on a
# running probe reports an accurate provenance.

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(cat VERSION 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w \
  -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${VERSION} \
  -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
  -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BUILD_TIME}"

mkdir -p dist/probe

# OS/arch matrix — keep in sync with docs/agent installation guide.
TARGETS=(
  "windows/amd64"
  "windows/arm64"
  "linux/amd64"
  "linux/arm64"
  "linux/armv7"
  "darwin/amd64"
  "darwin/arm64"
)

for t in "${TARGETS[@]}"; do
  os="${t%/*}"
  arch="${t#*/}"

  goarm=""
  if [[ "$arch" == "armv7" ]]; then
    arch="arm"
    goarm="7"
  fi

  ext=""
  [[ "$os" == "windows" ]] && ext=".exe"

  out="dist/probe/sonar-probe-${VERSION}-${os}-${t#*/}${ext}"
  echo "==> $out"
  GOOS="$os" GOARCH="$arch" GOARM="$goarm" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/sonar-probe
done

echo
echo "Built probe binaries for version $VERSION (commit $COMMIT) in dist/probe/"
ls -la dist/probe/
