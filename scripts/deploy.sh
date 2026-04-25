#!/usr/bin/env bash
# Deploy on the `dev` host VM. Run this on dev (via the currituck-tendril
# root, or interactively over ssh):
#
#   cd /opt/scanraysonar
#   ./scripts/deploy.sh
#
# This is a thin wrapper around docker compose pull + up. The host's
# cloudflared service (NOT in this stack) routes:
#
#   sonar.<domain>  -> http://127.0.0.1:8080
#   ingest.<domain> -> http://127.0.0.1:8080  (path /agent/ws)

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="$(cat VERSION)"
echo "Deploying ScanRay Sonar v${VERSION}"

if [[ ! -f .env ]]; then
  echo "ERROR: .env not found. Run scripts/dev-bootstrap.sh or copy .env.example." >&2
  exit 1
fi

git fetch --tags --quiet || true
git pull --ff-only

export SONAR_VERSION="$VERSION"

docker compose pull --ignore-pull-failures
docker compose up -d --build --remove-orphans
docker compose ps

echo
echo "Health: curl -fsS http://127.0.0.1:8080/api/v1/healthz | jq ."
