#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
BIN_DIR="$REPO_DIR/bin"

# Load .env if present (sets LAST9_REFRESH_TOKEN etc.)
if [[ -f "$REPO_DIR/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$REPO_DIR/.env"
  set +a
fi

if [[ -x "$BIN_DIR/last9-mcp" ]]; then
  exec "$BIN_DIR/last9-mcp" "$@"
fi

if [[ -x "$REPO_DIR/last9-mcp" ]]; then
  exec "$REPO_DIR/last9-mcp" "$@"
fi

echo "last9-mcp binary not found; run: go build -o bin/last9-mcp ." >&2
exit 1
