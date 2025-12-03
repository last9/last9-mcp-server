#!/bin/bash
set -e

# Load environment variables
if [ -f .env ]; then
    source .env
fi

echo "=== Getting Real Traces Data ==="
echo

# Create a temp file for the responses
TEMP_FILE=$(mktemp)

# Run MCP server and capture all JSON responses
{
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
    sleep 0.1
    echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_traces","arguments":{"tracejson_query":[{"type":"filter","query":{"$exists":["ServiceName"]}}],"lookback_minutes":60,"limit":3}}}'
    sleep 1
} | LAST9_REFRESH_TOKEN="$LAST9_REFRESH_TOKEN" timeout 10s ./last9-mcp-server 2>/dev/null > "$TEMP_FILE"

echo "Raw MCP responses:"
cat "$TEMP_FILE" | head -20
echo

echo "Extracting traces data..."
# Look for the tool call response and extract traces
cat "$TEMP_FILE" | grep '"method":"tools/call"' -A 10 || \
cat "$TEMP_FILE" | grep '"jsonrpc":"2.0".*result' | tail -1 | \
jq -r '.result.content[0].text' 2>/dev/null | \
jq '.data[0:3]' 2>/dev/null || echo "Could not parse traces data"

# Cleanup
rm -f "$TEMP_FILE"