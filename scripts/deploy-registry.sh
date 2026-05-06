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

echo "Deploy registry mode — tree $(git rev-parse --short HEAD) VERSION $(cat VERSION)"

"${COMPOSE[@]}" pull sonar-api sonar-poller
"${COMPOSE[@]}" up -d --force-recreate --remove-orphans
"${COMPOSE[@]}" ps

echo
echo "Tip: curl -fsS http://127.0.0.1:\${SONAR_API_PORT:-6969}/api/v1/version"
