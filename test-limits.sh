#!/bin/bash
set -e

# Load environment variables
if [ -f .env ]; then
    source .env
fi

SERVER_PATH="./last9-mcp-server"

echo "=== Testing get_traces limit parameter ==="
echo

# Function to test traces with different limits
test_traces_limit() {
    local limit="$1"
    local test_name="$2"

    echo "Testing: $test_name"

    # Create JSON messages
    INIT_MSG='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'

    if [ -z "$limit" ]; then
        # No limit specified
        CALL_MSG='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_traces","arguments":{"tracejson_query":[{"type":"filter","query":{"$exists":["ServiceName"]}}],"lookback_minutes":1440}}}'
    else
        # With limit
        CALL_MSG='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_traces","arguments":{"tracejson_query":[{"type":"filter","query":{"$exists":["ServiceName"]}}],"lookback_minutes":1440,"limit":'$limit'}}}'
    fi

    # Send messages to server
    {
        echo "$INIT_MSG"
        echo "$CALL_MSG"
    } | LAST9_REFRESH_TOKEN="$LAST9_REFRESH_TOKEN" "$SERVER_PATH" 2>/dev/null | \
    grep -o '"result":.*' | tail -1 | \
    jq -r '.result.content[0].text' 2>/dev/null | \
    jq '.data | length' 2>/dev/null || echo "Error parsing response"

    echo "---"
    echo
}

# Test different scenarios
echo "Test 1: Default limit (no limit specified) - should return up to 20 traces"
test_traces_limit "" "Default limit"

echo "Test 2: Custom limit of 10 - should return up to 10 traces"
test_traces_limit "10" "Limit 10"

echo "Test 3: Custom limit of 50 - should return up to 50 traces"
test_traces_limit "50" "Limit 50"

echo "Test 4: Large limit of 150 - should be capped at 100 traces"
test_traces_limit "150" "Limit 150 (should cap at 100)"

echo "All tests completed!"