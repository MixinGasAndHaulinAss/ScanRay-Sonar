#!/usr/bin/env bash
# GLCR pull-only deploy for dev — use after each merge to main once CI has published images.
#
#   cd /opt/scanraysonar
#   ./scripts/deploy-registry.sh
#
# Pulls fresh sonar-api + sonar-poller layers from GLCR and recreates containers so new
# digests always replace running tasks (compose alone may not restart when :latest moves).
#
# Optional env:
#   DEPLOY_REMOTE  git remote to pull (default: origin)
#   DEPLOY_BRANCH  branch name (default: main)
#   IMAGE_TAG      GLCR tag for api + poller (default: CalVer read from VERSION after git pull)
#
# Defaulting IMAGE_TAG from VERSION keeps sonar-api and sonar-poller on the same registry tag.
# Override with IMAGE_TAG=latest if you intentionally want the moving :latest pointer.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  echo "ERROR: .env not found. Run scripts/dev-bootstrap.sh or copy .env.example." >&2
  exit 1
fi

REMOTE="${DEPLOY_REMOTE:-origin}"
BRANCH="${DEPLOY_BRANCH:-main}"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.registry.yml)

git fetch "$REMOTE" "$BRANCH"
git pull --ff-only "$REMOTE" "$BRANCH"

STACK_VER="$(tr -d '[:space:]' < VERSION)"
export SCANRAY_STACK_VERSION="$STACK_VER"
export IMAGE_TAG="${IMAGE_TAG:-$STACK_VER}"

echo "Deploy registry mode — tree $(git rev-parse --short HEAD) VERSION ${STACK_VER}  GLCR_IMAGE_TAG=${IMAGE_TAG}"

# Pull tagged layers explicitly (logged digest summary), then up again with --pull always so
# compose resolves :latest immediately before replacing containers — avoids stale local tags.
"${COMPOSE[@]}" pull sonar-api sonar-poller
echo "Recreating containers (new processes from pulled images)…"
"${COMPOSE[@]}" up -d --pull always --force-recreate --remove-orphans
"${COMPOSE[@]}" ps

echo
echo "Tip: curl -fsS http://127.0.0.1:\${SONAR_API_PORT:-6969}/api/v1/version"
