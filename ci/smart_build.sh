#!/usr/bin/env bash
# =============================================================================
# ci/smart_build.sh — per-service smart docker build for ScanRay Sonar.
#
# Most pipelines on master only touch one of the two services (sonar-api
# or sonar-poller). Forcing a real `docker build` of the untouched
# service every time burns ~2-3 minutes of runner time and produces an
# image that is byte-identical to last pipeline's. This script
# short-circuits that:
#
#   1. Decide whether to do a real `docker build` or just retag the
#      previous SHA's image as the current SHA. The decision is driven
#      by a watched-paths diff between $CI_COMMIT_BEFORE_SHA and HEAD.
#   2. Always-rebuild gates override the diff check:
#        FORCE_REBUILD=true                  (operator-set CI variable)
#        $CI_COMMIT_TAG set                  (release pipeline)
#        $CI_COMMIT_BEFORE_SHA empty/zero    (first pipeline on a branch)
#        .gitlab-ci.yml or this script changed (CI itself moved)
#   3. On retag fallback chain we try `:$CI_COMMIT_BEFORE_SHA` first,
#      then `:latest`, and only finally fall back to a real build —
#      never silently publish nothing.
#
# Output: pushes `$IMAGE:$CI_COMMIT_SHORT_SHA` to GLCR. The package
# stage handles the additional `:latest`/`:$VERSION`/`:$CI_COMMIT_TAG`
# aliases.
#
# Usage (from a `.docker-job`-derived job):
#   ./ci/smart_build.sh <service> <dockerfile> "<watched_path1> <watched_path2> ..."
#
# Example:
#   ./ci/smart_build.sh api docker/api.Dockerfile \
#     "cmd/sonar-api/ internal/api/ web/ docker/api.Dockerfile go.mod go.sum"
# =============================================================================

set -euo pipefail

SERVICE="${1:?service name required (api|poller)}"
DOCKERFILE="${2:?dockerfile path required}"
WATCHED_PATHS="${3:?space-separated watched paths required}"

REGISTRY="${REGISTRY:?REGISTRY env var required (e.g. glcr.nclgisa.org:443)}"
IMAGE_PREFIX="${IMAGE_PREFIX:?IMAGE_PREFIX env var required (e.g. striketeam/scanray-sonar)}"
SHORT_SHA="${CI_COMMIT_SHORT_SHA:?CI_COMMIT_SHORT_SHA must be set by GitLab CI}"
BEFORE_SHA="${CI_COMMIT_BEFORE_SHA:-}"

IMG="${REGISTRY}/${IMAGE_PREFIX}/${SERVICE}"
NEW_TAG="${IMG}:${SHORT_SHA}"

log() { echo "[smart-build] ${SERVICE}: $*"; }

force_rebuild_reason=""
if [[ "${FORCE_REBUILD:-false}" == "true" ]]; then
  force_rebuild_reason="FORCE_REBUILD=true"
elif [[ -n "${CI_COMMIT_TAG:-}" ]]; then
  force_rebuild_reason="release pipeline (CI_COMMIT_TAG=${CI_COMMIT_TAG})"
elif [[ -z "$BEFORE_SHA" || "$BEFORE_SHA" =~ ^0+$ ]]; then
  force_rebuild_reason="no parent SHA (first pipeline on branch)"
elif git diff --name-only "$BEFORE_SHA" HEAD -- .gitlab-ci.yml ci/smart_build.sh 2>/dev/null | grep -q .; then
  force_rebuild_reason=".gitlab-ci.yml or ci/smart_build.sh changed"
fi

if [[ -z "$force_rebuild_reason" ]]; then
  # Diff-based decision. Convert the space-separated watched paths into
  # an array so paths with embedded spaces (none right now, but be safe)
  # don't get split mid-token.
  read -r -a paths <<< "$WATCHED_PATHS"
  changed="$(git diff --name-only "$BEFORE_SHA" HEAD -- "${paths[@]}" 2>/dev/null || true)"
  if [[ -z "$changed" ]]; then
    log "no watched paths changed between ${BEFORE_SHA} and ${SHORT_SHA}; attempting retag"
    # Retag chain: try previous SHA first (most accurate), then :latest.
    for src in "${IMG}:${BEFORE_SHA:0:8}" "${IMG}:latest"; do
      log "trying to pull ${src}"
      if docker pull "$src" >/dev/null 2>&1; then
        log "retagging ${src} -> ${NEW_TAG}"
        docker tag "$src" "$NEW_TAG"
        docker push "$NEW_TAG"
        log "retag complete"
        exit 0
      fi
    done
    log "retag sources unavailable; falling through to real build"
  else
    log "watched paths changed:"
    echo "$changed" | sed 's/^/[smart-build]   /'
  fi
else
  log "force-rebuild reason: ${force_rebuild_reason}"
fi

# Real build path. Cache from :latest so layer reuse is preserved across
# unrelated pipelines, and inline-cache so the next runner can warm
# from this image's layers.
log "docker build -f ${DOCKERFILE} -t ${NEW_TAG}"
docker pull "${IMG}:latest" >/dev/null 2>&1 || log "no :latest cache yet (first build)"
docker build \
  -f "$DOCKERFILE" \
  --cache-from "${IMG}:latest" \
  --build-arg BUILDKIT_INLINE_CACHE=1 \
  --build-arg "VERSION=${SONAR_VERSION:-$(cat VERSION 2>/dev/null | tr -d '[:space:]')}" \
  --build-arg "COMMIT=${CI_COMMIT_SHA:-unknown}" \
  --build-arg "BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t "$NEW_TAG" \
  .
docker push "$NEW_TAG"
log "build + push complete"
