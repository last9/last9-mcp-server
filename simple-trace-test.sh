#!/bin/bash
set -e

if [ -f .env ]; then
    source .env
fi

echo "=== Simple Trace Test with Limit ==="
echo

# Test with limit 10
echo "Testing get_traces with limit 10:"
echo

{
    echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'
    echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_traces","arguments":{"tracejson_query":[{"type":"filter","query":{"$exists":["ServiceName"]}}],"lookback_minutes":60,"limit":10}}}'
} | LAST9_REFRESH_TOKEN="$LAST9_REFRESH_TOKEN" ./last9-mcp-server 2>/dev/null | tail -5

echo
echo "=== Done ==="