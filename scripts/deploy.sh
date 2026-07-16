#!/usr/bin/env bash
# Build-on-host deploy fallback. Prefer scripts/deploy-registry.sh on hosts
# that pull from GLCR (StrikeTeam lab / production pull-only).
#
#   cd /opt/scanraysonar
#   ./scripts/deploy.sh
#
# This path runs `docker compose up -d --build` (rebuilds sonar-api +
# sonar-poller from source). Use when GLCR is unavailable or you are
# iterating on a greenfield install. See docs/src/installation.md.
#
# The host's cloudflared service (NOT in this stack) routes:
#
#   sonar.<domain>  -> http://127.0.0.1:6969
#   ingest.<domain> -> http://127.0.0.1:6969

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  echo "ERROR: .env not found. Run scripts/dev-bootstrap.sh or copy .env.example." >&2
  exit 1
fi

git fetch --tags --quiet || true
git pull --ff-only

# Read VERSION *after* the pull so the image label matches the source
# tree we just fetched. Reading earlier means a release-bumping commit
# in the same pull would build with the previous image label.
VERSION="$(tr -d '[:space:]' < VERSION)"
echo "Deploying ScanRay Sonar v${VERSION} (build-on-host)"
export SONAR_VERSION="$VERSION"
export SCANRAY_STACK_VERSION="$VERSION"

docker compose pull --ignore-pull-failures
docker compose up -d --build --remove-orphans
docker compose ps

echo
echo "Health: curl -fsS http://127.0.0.1:\${SONAR_API_PORT:-6969}/api/v1/healthz"
echo "Version: curl -fsS http://127.0.0.1:\${SONAR_API_PORT:-6969}/api/v1/version"
echo "Prefer ./scripts/deploy-registry.sh when using GLCR pull-only."
