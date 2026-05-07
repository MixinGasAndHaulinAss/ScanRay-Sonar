#!/usr/bin/env bash
# GLCR pull-only deploy for dev — use after each merge to main once CI has published images.
#
#   cd /opt/scanraysonar
#   ./scripts/deploy-registry.sh
#
# Pulls fresh sonar-api + sonar-poller + sonar-collector layers from GLCR and recreates the
# containers so new digests always replace running tasks (compose alone may not restart when
# :latest moves). Every deploy rotates ALL three images at the same CalVer tag — no more
# "did you remember to docker pull the collector?" footguns.
#
# Optional env:
#   DEPLOY_REMOTE       git remote to pull (default: origin)
#   DEPLOY_BRANCH       branch name (default: main)
#   IMAGE_TAG           GLCR tag for api + poller + collector (default: latest). Set
#                       IMAGE_TAG=2026.5.6.20 (or whatever CalVer) to pin a specific release
#                       for rollback or production hand-off.
#   SKIP_DEV_COLLECTOR  set to 1 to skip the dev-test collector overlay (useful on hosts that
#                       are NOT supposed to run a co-resident collector against themselves).
#
# Default :latest is intentional. CI publishes :latest, :$VERSION, and :$CI_COMMIT_SHORT_SHA
# every green pipeline; :latest always points at the most recent successfully-packaged digest.
# Pin to :$VERSION (i.e. IMAGE_TAG="$(cat VERSION)") only when you want a deliberate, stable
# release tag — typically when handing an installer to an external customer or rolling back.
#
# /api/v1/version stays honest regardless: each binary embeds its build-time CalVer at compile,
# so the UI footer + central reporting reflect what is actually running, not what tag was pulled.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
  echo "ERROR: .env not found. Run scripts/dev-bootstrap.sh or copy .env.example." >&2
  exit 1
fi

REMOTE="${DEPLOY_REMOTE:-origin}"
BRANCH="${DEPLOY_BRANCH:-main}"

# Compose stack: base + registry overlay always, dev-collector overlay unless skipped.
# Keeping the dev test collector in compose means deploy-registry.sh pulls + recreates it
# every release, instead of leaving it pinned to whatever image it was started with by hand.
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.registry.yml)
if [[ "${SKIP_DEV_COLLECTOR:-0}" != "1" && -f docker-compose.dev-collector.yml ]]; then
  COMPOSE+=(-f docker-compose.dev-collector.yml)
  WITH_COLLECTOR=1
else
  WITH_COLLECTOR=0
fi

git fetch "$REMOTE" "$BRANCH"
git pull --ff-only "$REMOTE" "$BRANCH"

STACK_VER="$(tr -d '[:space:]' < VERSION)"
export SCANRAY_STACK_VERSION="$STACK_VER"
# Default to the moving :latest pointer so day-to-day deploys are just "merge → recreate"
# and don't require coordinating a VERSION bump. Override with IMAGE_TAG=$STACK_VER (or any
# other CalVer / SHA) when you want a pinned, reproducible roll.
export IMAGE_TAG="${IMAGE_TAG:-latest}"

echo "Deploy registry mode — tree $(git rev-parse --short HEAD) VERSION ${STACK_VER}  GLCR_IMAGE_TAG=${IMAGE_TAG}  dev-collector=${WITH_COLLECTOR}"

# Pull every service's tagged layers explicitly (gives a logged digest summary for each),
# then `up -d --pull always --force-recreate` so compose resolves :latest one more time and
# replaces every running container with a fresh process from the new image.
"${COMPOSE[@]}" pull
echo "Recreating containers (new processes from pulled images)…"
"${COMPOSE[@]}" up -d --pull always --force-recreate --remove-orphans
"${COMPOSE[@]}" ps

echo
echo "Tip: curl -fsS http://127.0.0.1:\${SONAR_API_PORT:-6969}/api/v1/version"
if [[ "$WITH_COLLECTOR" == "1" ]]; then
  echo "Tip: docker logs --tail=20 sonar-collector   # confirm collector heartbeat"
fi
