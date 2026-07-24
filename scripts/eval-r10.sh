#!/usr/bin/env bash
# R10 accuracy gate: critical log/trace construction against served descriptions.
# Requires: ANTHROPIC_API_KEY, LAST9_REFRESH_TOKEN (via .env), sibling last9-mcp-evals.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EVALS="${LAST9_MCP_EVALS_PATH:-$ROOT/../last9-mcp-evals}"

if [[ ! -d "$EVALS" ]]; then
  echo "last9-mcp-evals not found at $EVALS (set LAST9_MCP_EVALS_PATH)" >&2
  exit 1
fi

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "ANTHROPIC_API_KEY is required to run the R10 eval gate" >&2
  exit 1
fi

if [[ ! -f "$ROOT/.env" ]]; then
  echo "Missing $ROOT/.env with LAST9_REFRESH_TOKEN (needed by scripts/start-local.sh)" >&2
  exit 1
fi

cd "$ROOT"
mkdir -p bin
go build -o bin/last9-mcp .

export LAST9_MCP_SERVER_PATH="$ROOT"
cd "$EVALS"
# --use-server launches bin/start-local.sh so suites see served short descriptions.
npm run eval:log -- --use-server
npm run eval:trace -- --use-server
npm run eval:log_tool_selection -- --use-server
