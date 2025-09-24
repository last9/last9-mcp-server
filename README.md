# Last9 MCP Server

![last9 mcp demo](mcp-demo.gif)

A [Model Context Protocol](https://modelcontextprotocol.io/) server
implementation for [Last9](https://last9.io/mcp/) that enables AI agents to
seamlessly bring real-time production context — logs, metrics, and traces — into
your local environment to auto-fix code faster.

- [View demo](https://www.youtube.com/watch?v=AQH5xq6qzjI)
- Read our
  [announcement blog post](https://last9.io/blog/launching-last9-mcp-server/)

## Status

Works with Claude desktop app, or Cursor, Windsurf, and VSCode (Github Copilot)
IDEs. Implements the following MCP
[tools](https://modelcontextprotocol.io/docs/concepts/tools):

**Observability & APM Tools:**

- `get_exceptions`: Get the list of exceptions.
- `get_service_summary`: Get service summary with throughput, error rate, and response time.
- `get_service_environments`: Get available environments for services.
- `get_service_performance_details`: Get detailed performance metrics for a service.
- `get_service_operations_summary`: Get operations summary for a service.
- `get_service_dependency_graph`: Get service dependency graph showing incoming/outgoing dependencies.

**Prometheus/PromQL Tools:**

- `promptheus_range_query`: Execute PromQL range queries for metrics data.
- `prometheus_instant_query`: Execute PromQL instant queries for metrics data.
- `prometheus_label_values`: Get label values for PromQL queries.
- `prometheus_labels`: Get available labels for PromQL queries.

**Logs Management:**

- `get_logs`: Get logs filtered by service name and/or severity level.
- `get_drop_rules`: Get drop rules for logs that determine what logs get
  filtered out at [Last9 Control Plane](https://last9.io/control-plane)
- `add_drop_rule`: Create a drop rule for logs at
  [Last9 Control Plane](https://last9.io/control-plane)
- `get_service_logs`: Get raw log entries for a specific service over a time range. Can apply filters on severity and body.

**Traces Management:**

- `get_service_traces`: Query traces for a specific service with filtering options for span kinds, status codes, and other trace attributes.

**Alert Management:**

- `get_alert_config`: Get alert configurations (alert rules) from Last9.
- `get_alerts`: Get currently active alerts from Last9 monitoring system.

## Tools Documentation

### get_exceptions

Retrieves server-side exceptions over a specified time range.

Parameters:

- `limit` (integer, optional): Maximum number of exceptions to return.
  Default: 20.
- `lookback_minutes` (integer, recommended): Number of minutes to look back from
  now. Default: 60. Examples: 60, 30, 15.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD
  HH:MM:SS). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD
  HH:MM:SS). Leave empty to default to current time.
- `span_name` (string, optional): Name of the span to filter by.

### get_service_summary

Get service summary over a given time range. Includes service name, environment, throughput, error rate, and response time. All values are p95 quantiles over the time range.

Parameters:

- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to end_time_iso - 1 hour.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.

### get_service_environments

Get available environments for services. Returns an array of environments that can be used with other APM tools.

Parameters:

- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to end_time_iso - 1 hour.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

Note: All other APM tools that retrieve service information (like `get_service_performance_details`, `get_service_dependency_graph`, `get_service_operations_summary`, `get_service_summary`) require an `env` parameter. This parameter must be one of the environments returned by this tool. If this tool returns an empty array, use an empty string `""` for the env parameter.

### get_service_performance_details

Get detailed performance metrics for a specific service over a given time range.

Parameters:

- `service_name` (string, required): Name of the service to get performance details for.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.

### get_service_operations_summary

Get a summary of operations inside a service over a given time range. Returns operations like HTTP endpoints, database queries, messaging producer and HTTP client calls.

Parameters:

- `service_name` (string, required): Name of the service to get operations summary for.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.

### get_service_dependency_graph

Get details of the throughput, response times and error rates of incoming, outgoing and infrastructure components of a service. Useful for analyzing cascading effects of errors and performance issues.

Parameters:

- `service_name` (string, optional): Name of the service to get the dependency graph for.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.

### promptheus_range_query

Perform a Prometheus range query to get metrics data over a specified time range. Recommended to check available labels first using `prometheus_labels` tool.

Parameters:

- `query` (string, required): The range query to execute.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

### prometheus_instant_query

Perform a Prometheus instant query to get metrics data at a specific point in time. Typically should use rollup functions like sum_over_time, avg_over_time, quantile_over_time over a time window.

Parameters:

- `query` (string, required): The instant query to execute.
- `time_iso` (string, optional): Time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

### prometheus_label_values

Return the label values for a particular label and PromQL filter query. Similar to Prometheus /label_values call.

Parameters:

- `match_query` (string, required): A valid PromQL filter query.
- `label` (string, required): The label to get values for.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

### prometheus_labels

Return the labels for a given PromQL match query. Similar to Prometheus /labels call.

Parameters:

- `match_query` (string, required): A valid PromQL filter query.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

### get_logs

Gets logs filtered by service name and/or severity level within a specified time range. This tool now uses the advanced v2 logs API with physical index optimization for better performance.

**Note**: This tool now requires a `service_name` parameter and internally uses the same advanced infrastructure as `get_service_logs`.

Parameters:

- `service_name` (string, required): Name of the service to get logs for.
- `severity` (string, optional): Severity of the logs to get (automatically converted to severity_filters format).
- `lookback_minutes` (integer, recommended): Number of minutes to look back from now. Default: 60. Examples: 60, 30, 15.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
- `limit` (integer, optional): Maximum number of logs to return. Default: 20.
- `env` (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.

### get_drop_rules

Gets drop rules for logs, which determine what logs get filtered out from
reaching Last9.

### add_drop_rule

Adds a new drop rule to filter out specific logs at
[Last9 Control Plane](https://last9.io/control-plane)

Parameters:

- `name` (string, required): Name of the drop rule.
- `filters` (array, required): List of filter conditions to apply. Each filter
  has:
  - `key` (string, required): The key to filter on. Only attributes and
    resource.attributes keys are supported. For resource attributes, use format:
    resource.attributes[key_name] and for log attributes, use format:
    attributes[key_name] Double quotes in key names must be escaped.
  - `value` (string, required): The value to filter against.
  - `operator` (string, required): The operator used for filtering. Valid
    values:
    - "equals"
    - "not_equals"
  - `conjunction` (string, required): The logical conjunction between filters.
    Valid values:
    - "and"

### get_alert_config

Get alert configurations (alert rules) from Last9. Returns all configured alert rules including their conditions, labels, and annotations.

Parameters:

None - This tool retrieves all available alert configurations.

Returns information about:

- Alert rule ID and name
- Primary indicator being monitored
- Current state and severity
- Algorithm used for alerting
- Entity ID and organization details
- Properties and configuration
- Creation and update timestamps
- Group timeseries notification settings

### get_alerts

Get currently active alerts from Last9 monitoring system. Returns all alerts that are currently firing or have fired recently within the specified time window.

Parameters:

- `timestamp` (integer, optional): Unix timestamp for the query time. Leave empty to default to current time.
- `window` (integer, optional): Time window in seconds to look back for alerts. Defaults to 900 seconds (15 minutes). Range: 60-86400 seconds.

Returns information about:

- Alert rule details (ID, name, group, type)
- Current state and severity
- Last fired timestamp and duration
- Rule properties and configuration
- Alert instances with current values
- Metric degradation information
- Group labels and annotations for each instance

### get_service_logs

Get raw log entries for a specific service over a time range. This tool retrieves actual log entries including log messages, timestamps, severity levels, and other metadata. Useful for debugging issues, monitoring service behavior, and analyzing specific log patterns.

Parameters:

- `service_name` (string, required): Name of the service to get logs for.
- `lookback_minutes` (integer, optional): Number of minutes to look back from now. Default: 60 minutes. Examples: 60, 30, 15.
- `limit` (integer, optional): Maximum number of log entries to return. Default: 20.
- `env` (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.
- `severity_filters` (array, optional): Array of severity patterns to filter logs (e.g., ["error", "warn"]). Uses OR logic.
- `body_filters` (array, optional): Array of message content patterns to filter logs (e.g., ["timeout", "failed"]). Uses OR logic.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

Filtering behavior:
- Multiple filter types are combined with AND logic (service AND severity AND body)
- Each filter array uses OR logic (matches any pattern in the array)

Examples:
- service_name="api" + severity_filters=["error"] + body_filters=["timeout"] → finds error logs containing "timeout"
- service_name="web" + body_filters=["timeout", "failed", "error 500"] → finds logs containing any of these patterns

### get_service_traces

Query traces for a specific service with filtering options for span kinds, status codes, and other trace attributes. This tool retrieves distributed tracing data for debugging performance issues, understanding request flows, and analyzing service interactions.

Parameters:

- `service_name` (string, required): Name of the service to get traces for.
- `lookback_minutes` (integer, optional): Number of minutes to look back from now. Default: 60 minutes. Examples: 60, 30, 15.
- `limit` (integer, optional): Maximum number of traces to return. Default: 10.
- `env` (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.
- `span_kind` (array, optional): Filter by span types (server, client, internal, consumer, producer).
- `span_name` (string, optional): Filter by specific span name.
- `status_code` (array, optional): Filter by trace status (ok, error, unset, success).
- `order` (string, optional): Field to order traces by. Default: "Duration". Options: Duration, Timestamp.
- `direction` (string, optional): Sort direction. Default: "backward". Options: forward, backward.
- `start_time_iso` (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.
- `end_time_iso` (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

Filtering options:
- Combine multiple filters to narrow down specific traces of interest
- Use time range filters with lookback_minutes or explicit start/end times

Examples:
- service_name="api" + span_kind=["server"] + status_code=["error"] → finds failed server-side traces
- service_name="payment" + span_name="process_payment" + lookback_minutes=30 → finds payment processing traces from last 30 minutes

## Installation

You can install and run the Last9 Observability MCP server in several ways:

### Remote Deployment (Cloudflare Workers) - NEW! 🚀

Deploy the MCP server remotely on Cloudflare Workers for internet-accessible MCP tools:

```bash
# Navigate to the Cloudflare directory
cd cloudflare

# Install dependencies
npm install

# Deploy to Cloudflare Workers
npm run deploy
```

**Benefits of Remote Deployment:**
- ✅ Internet-accessible from any MCP client
- ✅ No local installation required
- ✅ Automatic scaling and high availability
- ✅ OAuth authentication support
- ✅ Global edge deployment

See [Cloudflare Deployment Guide](./cloudflare/DEPLOYMENT.md) for detailed instructions.

### Local Installation

For local development and traditional STDIO usage:

#### Homebrew

```bash
# Add the Last9 tap
brew tap last9/tap

# Install the Last9 MCP CLI
brew install last9-mcp
```

#### NPM

```bash
# Install globally
npm install -g @last9/mcp-server

# Or run directly with npx
npx @last9/mcp-server
```

## Configuration

### Environment Variables

The Last9 MCP server requires the following environment variables:

- `LAST9_BASE_URL`: (required) Last9 API URL from
  [OTel integration](https://app.last9.io/integrations?integration=OpenTelemetry)
- `LAST9_AUTH_TOKEN`: (required) Authentication token for Last9 MCP server from
  [OTel integration](https://app.last9.io/integrations?integration=OpenTelemetry)
- `LAST9_REFRESH_TOKEN`: (required) Refresh Token with Write permissions, needed
  for accessing control plane APIs from
  [API Access](https://app.last9.io/settings/api-access)

## Usage

### Using Remote MCP Server (Cloudflare Workers)

After deploying to Cloudflare Workers, connect from any MCP client:

#### Claude Desktop (Remote)
```json
{
  "mcpServers": {
    "last9-remote": {
      "command": "npx",
      "args": ["@modelcontextprotocol/client-remote", "https://your-worker-url.workers.dev/mcp"]
    }
  }
}
```

#### Cursor (Remote)
```json
{
  "mcpServers": {
    "last9-remote": {
      "command": "npx",
      "args": ["@modelcontextprotocol/client-remote", "https://your-worker-url.workers.dev/mcp"]
    }
  }
}
```

### Using Local MCP Server

## Usage with Claude Desktop

Configure the Claude app to use the MCP server:

1. Open the Claude Desktop app, go to Settings, then Developer
2. Click Edit Config
3. Open the `claude_desktop_config.json` file
4. Copy and paste the server config to your existing file, then save
5. Restart Claude

### If installed via Homebrew:
```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

### If installed via NPM:
```json
{
  "mcpServers": {
    "last9": {
      "command": "npx",
      "args": ["-y", "@last9/mcp-server"],
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

## Usage with Cursor

Configure Cursor to use the MCP server:

1. Open Cursor, go to Settings, then Cursor Settings
2. Select MCP on the left
3. Click Add "New Global MCP Server" at the top right
4. Copy and paste the server config to your existing file, then save
5. Restart Cursor

### If installed via Homebrew:
```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

### If installed via NPM:
```json
{
  "mcpServers": {
    "last9": {
      "command": "npx",
      "args": ["-y", "@last9/mcp-server"],
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

## Usage with Windsurf

Configure Windsurf to use the MCP server:

1. Open Windsurf, go to Settings, then Developer
2. Click Edit Config
3. Open the `windsurf_config.json` file
4. Copy and paste the server config to your existing file, then save
5. Restart Windsurf

### If installed via Homebrew:
```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

### If installed via NPM:
```json
{
  "mcpServers": {
    "last9": {
      "command": "npx",
      "args": ["-y", "@last9/mcp-server"],
      "env": {
        "LAST9_BASE_URL": "<last9_otlp_host>",
        "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
        "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
      }
    }
  }
}
```

## Usage with VS Code

> Note: MCP support in VS Code is available starting v1.99 and is currently in
> preview. For advanced configuration options and alternative setup methods,
> [view the VS Code MCP documentation](https://code.visualstudio.com/docs/copilot/chat/mcp-servers).

1. Open VS Code, go to Settings, select the User tab, then Features, then Chat
2. Click "Edit settings.json"
3. Copy and paste the server config to your existing file, then save
4. Restart VS Code

### If installed via Homebrew:
```json
{
  "mcp": {
    "servers": {
      "last9": {
        "type": "stdio",
        "command": "/opt/homebrew/bin/last9-mcp",
        "env": {
          "LAST9_BASE_URL": "<last9_otlp_host>",
          "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
          "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
        }
      }
    }
  }
}
```

### If installed via NPM:
```json
{
  "mcp": {
    "servers": {
      "last9": {
        "type": "stdio",
        "command": "npx",
        "args": ["-y", "@last9/mcp-server"],
        "env": {
          "LAST9_BASE_URL": "<last9_otlp_host>",
          "LAST9_AUTH_TOKEN": "<last9_otlp_auth_token>",
          "LAST9_REFRESH_TOKEN": "<last9_write_refresh_token>"
        }
      }
    }
  }
}
```

## Development

For local development and testing, you can run the MCP server in HTTP mode which makes it easier to debug requests and responses.

### Running in HTTP Mode

Set the `HTTP_MODE` environment variable to enable HTTP server mode:

```bash
# Export required environment variables
export LAST9_API_TOKEN="your_api_token"
export LAST9_BASE_URL="https://your-last9-endpoint"  # Your Last9 endpoint
export HTTP_MODE=true
export HTTP_PORT=8080  # Optional, defaults to 8080

# Run the server
./last9-mcp-server
```

The server will start on `http://localhost:8080/mcp` and you can test it with curl:

### Testing with curl

```bash
# Test get_service_logs
curl -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: session_$(date +%s)000000000" \
    -d '{
      "jsonrpc": "2.0",
      "id": 1,
      "method": "tools/call",
      "params": {
        "name": "get_service_logs",
        "arguments": {
          "service_name": "your-service-name",
          "lookback_minutes": 30,
          "limit": 10
        }
      }
    }'

# Test get_service_traces
curl -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: session_$(date +%s)000000000" \
    -d '{
      "jsonrpc": "2.0",
      "id": 2,
      "method": "tools/call",
      "params": {
        "name": "get_service_traces",
        "arguments": {
          "service_name": "your-service-name",
          "lookback_minutes": 60,
          "limit": 5
        }
      }
    }'

# List available tools
curl -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: session_$(date +%s)000000000" \
    -d '{
      "jsonrpc": "2.0",
      "id": 3,
      "method": "tools/list",
      "params": {}
    }'
```

### Building from Source

```bash
# Clone the repository
git clone https://github.com/last9/last9-mcp-server.git
cd last9-mcp-server

# Build the binary
go build -o last9-mcp-server

# Run in development mode
HTTP_MODE=true ./last9-mcp-server
```

**Note**: HTTP mode is for development and testing only. When integrating with Claude Desktop or other MCP clients, use the default STDIO mode (without `HTTP_MODE=true`).

## Badges

[![MseeP.ai Security Assessment Badge](https://mseep.net/pr/last9-last9-mcp-server-badge.png)](https://mseep.ai/app/last9-last9-mcp-server)
