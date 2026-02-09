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

TARGET_INPUT="$1"
git fetch origin --tags

if git rev-parse --verify -q "origin/${TARGET_INPUT}" >/dev/null; then
  TARGET_REF="origin/${TARGET_INPUT}"
elif git rev-parse --verify -q "${TARGET_INPUT}" >/dev/null; then
  TARGET_REF="${TARGET_INPUT}"
else
  echo "Error: target ref not found: ${TARGET_INPUT}" >&2
  exit 1
fi

TARGET_SHA="$(git rev-parse "${TARGET_REF}")"
STAMP="$(date +%Y%m%d-%H%M%S)"
RB_BRANCH="autolab/rollback-${STAMP}"

git switch -c "${RB_BRANCH}" origin/main
git restore --source "${TARGET_SHA}" -- .

if git diff --quiet --exit-code; then
  echo "Error: no file diff between origin/main and ${TARGET_REF}. Nothing to rollback." >&2
  exit 1
fi

git add -A
git commit -m "rollback: align tree to ${TARGET_INPUT} (${TARGET_SHA:0:12})"
git push -u origin "${RB_BRANCH}"

PR_URL="$(gh pr create --repo "${REPO}" --base main --head "${RB_BRANCH}" --title "rollback: ${TARGET_INPUT}" --body "## Rollback target
- input: ${TARGET_INPUT}
- resolved: ${TARGET_REF}
- commit: ${TARGET_SHA}

## Notes
- This rollback PR aligns the repository tree to the selected target.
- Merge strategy remains squash.")"

echo "${PR_URL}"
