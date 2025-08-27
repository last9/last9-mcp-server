#!/usr/bin/env python3

import json
import subprocess
import time
import sys
import os

def test_alert_tools():
    """Test if the alert tools are properly registered"""
    
    # Build the binary
    print("Building the project...")
    result = subprocess.run(["go", "build", "-o", "last9-mcp-alerts", "."], 
                          capture_output=True, text=True, env={**os.environ, "GO111MODULE": "on"})
    if result.returncode != 0:
        print(f"Build failed: {result.stderr}")
        return False
    
    print("Build successful!")
    
    # Start the server in HTTP mode
    print("Starting server...")
    env = {
        **os.environ,
        "LAST9_BASE_URL": os.environ.get("LAST9_BASE_URL", "https://otlp-aps1.last9.io:443"),
        "LAST9_AUTH_TOKEN": os.environ.get("LAST9_AUTH_TOKEN", "your_auth_token_here"),
        "LAST9_REFRESH_TOKEN": os.environ.get("LAST9_REFRESH_TOKEN", "your_refresh_token_here")
    }
    
    server = subprocess.Popen(
        ["./last9-mcp-alerts", "-http", "-port", "8084"],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )
    
    # Wait for server to start
    time.sleep(5)
    
    try:
        # Test if server is responsive
        result = subprocess.run(
            ["curl", "-s", "-H", "Content-Type: application/json", 
             "-d", '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}',
             "http://localhost:8084/mcp"],
            capture_output=True, text=True, timeout=10
        )
        
        if result.returncode != 0:
            print(f"Failed to connect to server: {result.stderr}")
            return False
        
        # Parse the response
        try:
            response = json.loads(result.stdout)
            tools = response.get("result", {}).get("tools", [])
            
            # Check if our alert tools are present
            tool_names = [tool["name"] for tool in tools]
            
            print(f"Found {len(tools)} tools:")
            for tool_name in tool_names:
                print(f"  - {tool_name}")
            
            # Check for alert tools
            alert_tools = [name for name in tool_names if "alert" in name]
            if "get_alert_config" in alert_tools and "get_alerts" in alert_tools:
                print("\n✅ Alert tools successfully registered!")
                print(f"Found alert tools: {alert_tools}")
                return True
            else:
                print(f"\n❌ Alert tools not found. Looking for get_alert_config and get_alerts")
                print(f"Found: {alert_tools}")
                return False
                
        except json.JSONDecodeError as e:
            print(f"Failed to parse response: {e}")
            print(f"Response was: {result.stdout}")
            return False
            
    except subprocess.TimeoutExpired:
        print("Request timed out")
        return False
    finally:
        # Clean up the server process
        server.terminate()
        server.wait()

if __name__ == "__main__":
    success = test_alert_tools()
    sys.exit(0 if success else 1)