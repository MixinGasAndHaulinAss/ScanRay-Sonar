#!/usr/bin/env bash
# Push main to GitLab (CI canonical) then GitHub mirror — one command for local dev.
# Requires SSH/PAT auth already configured for each remote.
set -euo pipefail
cd "$(dirname "$0")/.."

push_if_remote() {
  local name="$1"
  if git remote get-url "$name" >/dev/null 2>&1; then
    echo "git push $name main"
    git push "$name" main
  else
    echo "skip: remote '$name' not configured"
  fi
}

push_if_remote gitlab
push_if_remote origin
