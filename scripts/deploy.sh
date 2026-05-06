#!/usr/bin/env bash
# Deploy on the `dev` host VM. Run this on dev (via the currituck-tendril
# root, or interactively over ssh):
#
#   cd /opt/scanraysonar
#   ./scripts/deploy.sh
#
# Phase 1 (current): build-on-host. `docker compose up -d --build`
# rebuilds sonar-api + sonar-poller from source on the dev host.
#
# Phase 2 (pull-only, after first green GitLab pipeline): replace the
# build with a pull from GLCR using docker-compose.registry.yml — see
# the "Phase 2: pull-only deploy on dev" section in README.md. Two-line
# diff once the operator has logged in to glcr.nclgisa.org:443 and
# repointed the origin remote at GitLab.
#
# The host's cloudflared service (NOT in this stack) routes:
#
#   sonar.<domain>  -> http://127.0.0.1:8080
#   ingest.<domain> -> http://127.0.0.1:8080  (path /agent/ws)

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
echo "Deploying ScanRay Sonar v${VERSION}"
export SONAR_VERSION="$VERSION"
export SCANRAY_STACK_VERSION="$VERSION"

docker compose pull --ignore-pull-failures
docker compose up -d --build --remove-orphans
docker compose ps

echo
echo "Health: curl -fsS http://127.0.0.1:8080/api/v1/healthz | jq ."
