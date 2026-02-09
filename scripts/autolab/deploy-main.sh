#!/usr/bin/env bash
set -euo pipefail

PROD_REPO_PATH="${PROD_REPO_PATH:-/home/maoyu/apps/myclaw}"
SERVICE_NAME="${SERVICE_NAME:-myclaw}"
BINARY_PATH="${BINARY_PATH:-${PROD_REPO_PATH}/bin/myclaw}"

if [[ ! -d "${PROD_REPO_PATH}/.git" ]]; then
  echo "Error: PROD_REPO_PATH is not a git repository: ${PROD_REPO_PATH}" >&2
  exit 1
fi

if command -v go >/dev/null 2>&1; then
  GO_BIN="$(command -v go)"
elif [[ -x /usr/local/go/bin/go ]]; then
  GO_BIN="/usr/local/go/bin/go"
else
  echo "Error: go binary not found in PATH or /usr/local/go/bin/go" >&2
  exit 1
fi

cd "${PROD_REPO_PATH}"

git fetch origin
git pull --ff-only origin main

"${GO_BIN}" build -o "${BINARY_PATH}" ./cmd/myclaw

sudo systemctl restart "${SERVICE_NAME}"
sleep 2
sudo systemctl is-active "${SERVICE_NAME}" >/dev/null

DEPLOYED_SHA="$(git rev-parse --short HEAD)"
echo "deploy=ok service=${SERVICE_NAME} sha=${DEPLOYED_SHA}"
