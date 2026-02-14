#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "Error: gh CLI is required." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

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

to_windows_path() {
  local input="$1"
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$input"
    return
  fi
  printf '%s' "$input"
}

deploy_main() {
  local uname_out
  uname_out="$(uname -s 2>/dev/null || printf unknown)"
  uname_out="$(printf '%s' "$uname_out" | tr '[:upper:]' '[:lower:]')"

  case "$uname_out" in
    mingw*|msys*|cygwin*)
      local ps_script
      ps_script="$(to_windows_path "${SCRIPT_DIR}/deploy-main.ps1")"
      if command -v pwsh >/dev/null 2>&1; then
        pwsh -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$ps_script"
      elif command -v powershell >/dev/null 2>&1; then
        powershell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$ps_script"
      else
        echo "Error: Windows deployment requires pwsh or powershell in PATH." >&2
        exit 1
      fi
      ;;
    *)
      "${SCRIPT_DIR}/deploy-main.sh"
      ;;
  esac
}

gh pr merge "$TARGET" --repo "$REPO" --squash --delete-branch

if [[ "${AUTO_DEPLOY:-1}" == "1" ]]; then
  deploy_main
  echo "promote=merged_and_deployed"
else
  echo "promote=merged"
fi
