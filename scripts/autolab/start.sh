#!/usr/bin/env bash
set -euo pipefail

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Error: run this inside a git repository." >&2
  exit 1
fi

TOPIC="${1:-task}"
BASE_REF="${BASE_REF:-origin/main}"
STAMP="$(date +%Y%m%d-%H%M%S)"

if [[ "$TOPIC" == autolab/* ]]; then
  BRANCH="$TOPIC"
else
  BRANCH="autolab/${STAMP}-${TOPIC}"
fi

if git show-ref --verify --quiet "refs/heads/${BRANCH}"; then
  echo "Error: branch already exists: ${BRANCH}" >&2
  exit 1
fi

git fetch origin

git switch -c "$BRANCH" "$BASE_REF"

echo "created=${BRANCH}"
echo "base=${BASE_REF}"
