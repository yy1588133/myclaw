#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "Error: gh CLI is required." >&2
  exit 1
fi

ORIGIN_URL="$(git remote get-url origin)"
REPO="${GITHUB_REPO:-$(printf '%s' "$ORIGIN_URL" | sed -E 's#^git@github.com:##; s#^https://github.com/##; s#\.git$##')}"

TARGET="${1:-}"
if [[ -z "$TARGET" ]]; then
  BRANCH="$(git branch --show-current)"
  if [[ -z "$BRANCH" || "$BRANCH" == "main" ]]; then
    echo "Error: provide PR number/url or run on a PR branch." >&2
    exit 1
  fi
  TARGET="$BRANCH"
fi

gh pr merge "$TARGET" --repo "$REPO" --squash --delete-branch

if [[ "${AUTO_DEPLOY:-1}" == "1" ]]; then
  "$(dirname "$0")/deploy-main.sh"
  echo "promote=merged_and_deployed"
else
  echo "promote=merged"
fi
