# Last9 MCP Server

![last9 mcp demo](mcp-demo.gif)

A [Model Context Protocol](https://modelcontextprotocol.io/) server implementation for [Last9](https://last9.io/mcp/) that enables AI agents to seamlessly bring real-time production context — logs, metrics, and traces — into your local environment to auto-fix code faster.

- [View demo](https://www.youtube.com/watch?v=AQH5xq6qzjI)
- Read our [announcement blog post](https://last9.io/blog/launching-last9-mcp-server/)

## Status

Works with Claude desktop app, or Cursor, Windsurf, and VSCode (Github Copilot) IDEs. Implements the following MCP [tools](https://modelcontextprotocol.io/docs/concepts/tools):

- `get_exceptions`: Get list of exceptions
- `get_service_graph`: Get service graph for an endpoint from the exception
- `get_logs`: Get logs filtered by service name and/or severity level
- `get_drop_rules`: Get drop rules for logs that determine what logs get filtered out

## Tools Documentation

### get_exceptions

Retrieves server-side exceptions over a specified time range.

Parameters:

- `limit` (integer, optional): Maximum number of exceptions to return. Default: 20.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS).
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS).
- `span_name` (string, optional): Name of the span to filter by.

### get_service_graph

Gets the upstream and downstream services for a given span name, along with the throughput for each service.

Parameters:

- `span_name` (string, required): Name of the span to get dependencies for.
- `lookback_minutes` (integer, optional): Number of minutes to look back. Default: 60.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS).

### get_logs

Gets logs filtered by optional service name and/or severity level within a specified time range.

Parameters:

- `service` (string, optional): Name of the service to get logs for.
- `severity` (string, optional): Severity of the logs to get.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS).
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS).
- `limit` (integer, optional): Maximum number of logs to return. Default: 20.

### get_drop_rules

Gets drop rules for logs, which determine what logs get filtered out from reaching Last9.

### add_drop_rule

Adds a new drop rule to filter out specific logs from reaching Last9.

Parameters:

- `name` (string, required): Name of the drop rule.
- `filters` (array, required): List of filter conditions to apply. Each filter has:
  - `key` (string, required): The key to filter on. For resource attributes, use format: resource.attribute[key_name]. Double quotes in key names must be escaped.
  - `value` (string, required): The value to filter against.
  - `operator` (string, required): The operator used for filtering. Valid values:
    - "equals"
    - "not_equals"
  - `conjunction` (string, required): The logical conjunction between filters. Valid values:
    - "and"

## Installation

You can install the Last9 Observability MCP server using either:

### Homebrew

```
# Add the Last9 tap
brew tap last9/tap

# Install the Last9 MCP CLI
brew install last9-mcp
```

### NPM

```bash
# Install globally
npm install -g @last9/mcp-server

# Or run directly with npx
npx @last9/mcp-server
```

## Configuration

### Environment Variables

The service requires the following environment variables:

- `LAST9_AUTH_TOKEN`: Authentication token for Last9 MCP server (required)
- `LAST9_BASE_URL`: Last9 API URL (required)

Signup at [Last9](https://app.last9.io/) and get your env variable keys [here](https://app.last9.io/integrations?integration=OpenTelemetry).

## Usage with Claude Desktop

Configure the Claude app to use the MCP server:

1. Open the Claude Desktop app
2. Go to Settings, then Developer, click Edit Config
3. Open the `claude_desktop_config.json` file
4. Copy and paste the server config to your existing file, then save
5. Restart Claude

```bash
code ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_AUTH_TOKEN": "your_auth_token",
        "LAST9_BASE_URL": "https://otlp.last9.io"
      }
    }
  }
}
```

## Usage with Cursor

Configure Cursor to use the MCP server:

1. Navigate to Settings, then Cursor Settings
2. Select MCP on the left
3. Click Add new global MCP server at the top right
4. Copy and paste the server config to your existing file, then save
5. Restart Cursor

```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_AUTH_TOKEN": "your_auth_token",
        "LAST9_BASE_URL": "https://otlp.last9.io"
      }
    }
  }
}
```

## Usage with Windsurf

Configure Cursor to use the MCP server:

1. Open Windsurf
2. Go to Settings, then Developer
3. Click Edit Config
4. Open the `windsurf_config.json` file
5. Copy and paste the server config to your existing file, then save
6. Restart Windsurf

```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_AUTH_TOKEN": "your_auth_token",
        "LAST9_BASE_URL": "https://otlp.last9.io"
      }
    }
  }
}
```
