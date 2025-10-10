#!/bin/bash
set -e

DOCKER_IMAGE="last9/mcp-server:latest"

# Load environment variables
if [ -f .env ]; then
    source .env
fi

# Function to run MCP command
run_mcp() {
    local message="$1"
    echo "Testing: $message"
    echo "$message" | docker run --rm -i --env-file .env "$DOCKER_IMAGE"
    echo -e "\n---\n"
}

echo "=== Last9 MCP Server Docker Tests ==="

# Test 1: Tools list
run_mcp '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

# Test 2: Initialize
run_mcp '{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"test-client","version":"1.0.0"}}}'

# Test 3: Get environments
run_mcp '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_service_environments","arguments":{}}}'

# Test 4: Get service summary
run_mcp '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_service_summary","arguments":{"env":""}}}'

echo "All tests completed!"