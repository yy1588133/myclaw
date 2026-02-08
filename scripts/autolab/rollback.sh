#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "Error: gh CLI is required." >&2
  exit 1
fi

ORIGIN_URL="$(git remote get-url origin)"
REPO="${GITHUB_REPO:-$(printf '%s' "$ORIGIN_URL" | sed -E 's#^git@github.com:##; s#^https://github.com/##; s#\.git$##')}"

if [[ $# -eq 0 ]]; then
  echo "Select a rollback target and run:"
  echo "  scripts/autolab/rollback.sh <target-ref>"
  echo ""
  echo "Recent remote branches:"
  git fetch origin >/dev/null 2>&1 || true
  git for-each-ref --sort=-committerdate --count=15 --format="  %(refname:short)" refs/remotes/origin
  echo ""
  echo "Recent tags:"
  git tag --sort=-creatordate | head -n 10 | sed "s/^/  /"
  exit 0
fi

TARGET="$1"
STAMP="$(date +%Y%m%d-%H%M%S)"
RB_BRANCH="autolab/rollback-${STAMP}"

git fetch origin

git switch -c "$RB_BRANCH" origin/main

git merge --no-ff "$TARGET" -m "rollback: promote ${TARGET}"

git push -u origin "$RB_BRANCH"

PR_URL="$(gh pr create --repo "$REPO" --base main --head "$RB_BRANCH" --title "rollback: ${TARGET}" --body "## Rollback target
- ${TARGET}

## Notes
- This rollback PR was generated from user-selected target.
- Merge strategy remains squash.")"

echo "$PR_URL"
