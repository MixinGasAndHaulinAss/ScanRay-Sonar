#!/usr/bin/env bash
# Bake the host's trusted CA bundle into docker/local-ca.crt so the
# build images can validate TLS through a corporate proxy that
# inspects outbound traffic. Without this, `npm ci` and
# `go mod download` fail inside the build containers with
# "certificate verify failed".
#
# Run this once per host before `docker compose build`. The resulting
# docker/local-ca.crt is host-specific and must NOT be committed.

set -euo pipefail

cd "$(dirname "$0")/.."

OUT="docker/local-ca.crt"

candidates=(
  /etc/ssl/certs/ca-certificates.crt   # Debian/Ubuntu/Alpine
  /etc/pki/tls/certs/ca-bundle.crt     # RHEL/CentOS/Fedora
  /etc/ssl/cert.pem                    # macOS / BSD
)

src=""
for c in "${candidates[@]}"; do
  if [[ -s "$c" ]]; then
    src="$c"
    break
  fi
done

if [[ -z "$src" ]]; then
  echo "ERROR: no system CA bundle found in known locations." >&2
  echo "Set CA_BUNDLE=/path/to/cert.pem and re-run." >&2
  exit 1
fi

if [[ -n "${CA_BUNDLE:-}" ]]; then
  src="$CA_BUNDLE"
fi

if ! grep -q "BEGIN CERTIFICATE" "$src"; then
  echo "ERROR: $src does not look like a PEM bundle." >&2
  exit 1
fi

cp "$src" "$OUT"
count=$(grep -c "BEGIN CERTIFICATE" "$OUT" || true)

# Belt-and-braces: tell git to ignore worktree changes to this file so a
# populated corporate CA bundle can't be accidentally pushed upstream.
if git rev-parse --git-dir >/dev/null 2>&1; then
  git update-index --skip-worktree "$OUT" 2>/dev/null || true
fi

echo "Wrote ${count} CA certificate(s) from ${src} to ${OUT}."
echo "Build images now via: docker compose build"
echo
echo "Safety: ${OUT} is now marked --skip-worktree; further edits"
echo "will not appear in 'git status'. To undo, run:"
echo "  git update-index --no-skip-worktree ${OUT}"
