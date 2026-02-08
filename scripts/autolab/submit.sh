#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "Error: gh CLI is required." >&2
  exit 1
fi

BRANCH="$(git branch --show-current)"
if [[ -z "$BRANCH" || "$BRANCH" == "main" ]]; then
  echo "Error: submit must run on a non-main branch." >&2
  exit 1
fi

if [[ "${SKIP_VERIFY:-0}" != "1" ]]; then
  "$(dirname "$0")/verify.sh"
fi

TITLE="${1:-$BRANCH}"

if gh pr view "$BRANCH" --json url >/dev/null 2>&1; then
  gh pr view "$BRANCH" --json url --jq '.url'
  exit 0
fi

git push -u origin "$BRANCH"

PR_URL="$(gh pr create --base main --head "$BRANCH" --title "$TITLE" --body "## Summary
- Automated branch-based change from $BRANCH
- Passed strict local verification before PR
- Awaiting CI checks and user approval for squash merge")"

echo "$PR_URL"
