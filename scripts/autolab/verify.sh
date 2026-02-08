#!/usr/bin/env bash
set -euo pipefail

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Error: run this inside a git repository." >&2
  exit 1
fi

BRANCH="$(git branch --show-current)"
if [[ -z "$BRANCH" ]]; then
  echo "Error: detached HEAD is not allowed for verification." >&2
  exit 1
fi
if [[ "$BRANCH" == "main" ]]; then
  echo "Error: strict policy blocks verification directly on main." >&2
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

if command -v gofmt >/dev/null 2>&1; then
  GOFMT_BIN="$(command -v gofmt)"
elif [[ -x /usr/local/go/bin/gofmt ]]; then
  GOFMT_BIN="/usr/local/go/bin/gofmt"
else
  echo "Error: gofmt binary not found in PATH or /usr/local/go/bin/gofmt" >&2
  exit 1
fi

BASE_REF="${BASE_REF:-origin/main}"
git fetch origin >/dev/null 2>&1 || true

echo "[1/6] lint: gofmt (changed files only)"
mapfile -t GO_FILES < <(git diff --name-only --diff-filter=ACMRT "$BASE_REF"...HEAD -- '*.go')
if [[ ${#GO_FILES[@]} -eq 0 ]]; then
  echo "No changed Go files."
else
  UNFORMATTED="$("$GOFMT_BIN" -l "${GO_FILES[@]}")"
  if [[ -n "$UNFORMATTED" ]]; then
    echo "gofmt failed. Unformatted changed files:" >&2
    echo "$UNFORMATTED" >&2
    exit 1
  fi
fi

echo "[2/6] lint: go vet"
"$GO_BIN" vet ./...

echo "[3/6] test"
"$GO_BIN" test ./... -count=1

echo "[4/6] race"
"$GO_BIN" test -race ./... -count=1

echo "[5/6] build"
"$GO_BIN" build ./...

echo "[6/6] smoke"
TMP_HOME="$(mktemp -d)"
BIN="$(mktemp /tmp/myclaw-smoke-XXXXXX)"
trap 'rm -f "$BIN"; rm -rf "$TMP_HOME"' EXIT

"$GO_BIN" build -o "$BIN" ./cmd/myclaw
HOME="$TMP_HOME" "$BIN" onboard >/dev/null
HOME="$TMP_HOME" "$BIN" status >/dev/null

echo "verify=passed"
