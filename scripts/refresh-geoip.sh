#!/usr/bin/env bash
# =============================================================================
# refresh-geoip.sh — download MaxMind GeoLite2-City + GeoLite2-ASN
# into the sonar-geoip docker volume.
#
# Run this:
#   * once on initial deploy
#   * weekly via cron (MaxMind updates the free databases on Tuesdays)
#   * after rotating the license key
#
# Requires:
#   * MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY in /opt/scanraysonar/.env
#     (or in the calling shell's env)
#   * docker (the script writes through a busybox helper container so we
#     don't depend on whatever happens to be installed on the host)
#
# Failure mode is deliberately conservative: if the download fails the
# existing files in the volume are left in place. The API still serves
# yesterday's lookups instead of crashing or showing "unknown" for
# every host because of one upstream blip.
# =============================================================================
set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/scanraysonar/.env}"
VOLUME_NAME="${VOLUME_NAME:-sonar-geoip}"

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  set -a; . "$ENV_FILE"; set +a
fi

if [[ -z "${MAXMIND_ACCOUNT_ID:-}" || -z "${MAXMIND_LICENSE_KEY:-}" ]]; then
  echo "refresh-geoip: MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY must be set in $ENV_FILE" >&2
  exit 2
fi

echo "refresh-geoip: ensuring volume $VOLUME_NAME exists"
docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1 || docker volume create "$VOLUME_NAME" >/dev/null

# Stage to a host-side tempdir, then atomically swap into the volume
# via tar+busybox so partial writes never leave the API reading half
# a database. MaxMind ships the .mmdb in a versioned subdirectory of
# the tarball; we strip that out so the file lands at the path the
# API expects.
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

download() {
  local edition="$1" outname="$2"
  local url="https://download.maxmind.com/app/geoip_download?edition_id=${edition}&license_key=${MAXMIND_LICENSE_KEY}&suffix=tar.gz"
  echo "refresh-geoip: downloading $edition"
  curl -sSfL --user "${MAXMIND_ACCOUNT_ID}:${MAXMIND_LICENSE_KEY}" \
    -o "$TMPDIR/${edition}.tgz" "$url"
  tar -xzf "$TMPDIR/${edition}.tgz" -C "$TMPDIR"
  # The archive contains <edition>_YYYYMMDD/<edition>.mmdb
  local extracted
  extracted="$(find "$TMPDIR" -maxdepth 2 -name "${edition}.mmdb" -print -quit)"
  if [[ -z "$extracted" ]]; then
    echo "refresh-geoip: $edition.mmdb not found in archive" >&2
    return 1
  fi
  cp -f "$extracted" "$TMPDIR/$outname"
}

download "GeoLite2-City" "GeoLite2-City.mmdb"
download "GeoLite2-ASN"  "GeoLite2-ASN.mmdb"

echo "refresh-geoip: writing into volume $VOLUME_NAME"
docker run --rm \
  -v "$VOLUME_NAME":/dst \
  -v "$TMPDIR":/src:ro \
  busybox:latest sh -c '
    set -e
    cp -f /src/GeoLite2-City.mmdb /dst/GeoLite2-City.mmdb
    cp -f /src/GeoLite2-ASN.mmdb  /dst/GeoLite2-ASN.mmdb
    chmod 644 /dst/GeoLite2-City.mmdb /dst/GeoLite2-ASN.mmdb
    ls -la /dst
  '

echo "refresh-geoip: done. Restart sonar-api to pick up the new files (or call SIGHUP-like reload once supported)."
