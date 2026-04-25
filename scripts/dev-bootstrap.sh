#!/usr/bin/env bash
# One-time local dev bootstrap: generates a fresh master key, JWT
# secret, DB password, and a bootstrap admin password, writing them to
# .env. Won't overwrite an existing .env.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  echo "refusing to overwrite existing .env" >&2
  exit 1
fi

gen() { openssl rand -base64 "$1" | tr -d '\n'; }

cp .env.example .env

MASTER=$(gen 32)
JWT=$(gen 64)
DBPW=$(gen 24 | tr -d '/+=' | cut -c1-24)
ADMINPW=$(gen 18 | tr -d '/+=' | cut -c1-18)

# macOS sed needs `-i ''`; GNU sed needs `-i`. Detect.
if sed --version >/dev/null 2>&1; then
  SED_INPLACE=(-i)
else
  SED_INPLACE=(-i '')
fi

sed "${SED_INPLACE[@]}" "s|^SONAR_MASTER_KEY=.*$|SONAR_MASTER_KEY=${MASTER}|"     .env
sed "${SED_INPLACE[@]}" "s|^SONAR_JWT_SECRET=.*$|SONAR_JWT_SECRET=${JWT}|"        .env
sed "${SED_INPLACE[@]}" "s|^SONAR_DB_PASSWORD=.*$|SONAR_DB_PASSWORD=${DBPW}|"     .env
sed "${SED_INPLACE[@]}" "s|^SONAR_BOOTSTRAP_ADMIN_PASSWORD=.*$|SONAR_BOOTSTRAP_ADMIN_PASSWORD=${ADMINPW}|" .env

echo
echo "Wrote .env with freshly generated secrets."
echo
echo "Bootstrap admin will be created on first start:"
echo "  email:    $(grep ^SONAR_BOOTSTRAP_ADMIN_EMAIL .env | cut -d= -f2-)"
echo "  password: ${ADMINPW}"
echo
echo "Save the password somewhere safe — it is not stored anywhere else."
