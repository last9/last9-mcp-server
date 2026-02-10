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
- `prometheus_range_query`: Execute PromQL range queries for metrics data.
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
- `get_log_attributes`: Get available log attributes (labels) for a specified time window.
**Traces Management:**
- `get_traces`: Retrieve traces using JSON pipeline queries for advanced filtering.
- `get_service_traces`: Retrieve traces by trace ID or service name with time range filtering.
- `get_trace_attributes`: Get available trace attributes (series) for a specified time window.
**Change Events:**
- `get_change_events`: Get change events from the last9_change_events prometheus metric over a given time range.
**Alert Management:**
- `get_alert_config`: Get alert configurations (alert rules) from Last9.
- `get_alerts`: Get currently active alerts from Last9 monitoring system.
## Tools Documentation
### Time Input Standard
- For relative windows, prefer `lookback_minutes`.
- For absolute windows, use `start_time_iso`, `end_time_iso`, or `time_iso` in RFC3339/ISO8601 (for example, `2026-02-09T15:04:05Z`).
- If both relative and absolute inputs are provided, absolute time inputs take precedence.
- Legacy `YYYY-MM-DD HH:MM:SS` is accepted only for compatibility.

### get_exceptions
Retrieves server-side exceptions over a specified time range.
Parameters:
- `limit` (integer, optional): Maximum number of exceptions to return.
  Default: 20.
- `lookback_minutes` (integer, recommended): Number of minutes to look back from
  now. Default: 60. Examples: 60, 30, 15.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `service_name` (string, optional): Filter exceptions by service name (e.g., api-service).
- `span_name` (string, optional): Name of the span to filter by.
- `deployment_environment` (string, optional): Filter exceptions by deployment environment from resource attributes (e.g., production, staging).
### get_service_summary
Get service summary over a given time range. Includes service name, environment, throughput, error rate, and response time. All values are p95 quantiles over the time range.
Parameters:
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to end_time_iso - 1 hour.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.
### get_service_environments
Get available environments for services. Returns an array of environments that can be used with other APM tools.
Parameters:
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to end_time_iso - 1 hour.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
Note: All other APM tools that retrieve service information (like `get_service_performance_details`, `get_service_dependency_graph`, `get_service_operations_summary`, `get_service_summary`) require an `env` parameter. This parameter must be one of the environments returned by this tool. If this tool returns an empty array, use an empty string `""` for the env parameter.
### get_service_performance_details
Get detailed performance metrics for a specific service over a given time range.
Parameters:
- `service_name` (string, required): Name of the service to get performance details for.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.
### get_service_operations_summary
Get a summary of operations inside a service over a given time range. Returns operations like HTTP endpoints, database queries, messaging producer and HTTP client calls.
Parameters:
- `service_name` (string, required): Name of the service to get operations summary for.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.
### get_service_dependency_graph
Get details of the throughput, response times and error rates of incoming, outgoing and infrastructure components of a service. Useful for analyzing cascading effects of errors and performance issues.
Parameters:
- `service_name` (string, optional): Name of the service to get the dependency graph for.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `env` (string, optional): Environment to filter by. Defaults to 'prod'.
### prometheus_range_query
Perform a Prometheus range query to get metrics data over a specified time range. Recommended to check available labels first using `prometheus_labels` tool.
Parameters:
- `query` (string, required): The range query to execute.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
### prometheus_instant_query
Perform a Prometheus instant query to get metrics data at a specific time. Typically should use rollup functions like sum_over_time, avg_over_time, quantile_over_time over a time window.
Parameters:
- `query` (string, required): The instant query to execute.
- `time_iso` (string, optional): Time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to current time.
### prometheus_label_values
Return the label values for a particular label and PromQL filter query. Similar to Prometheus /label_values call.
Parameters:
- `match_query` (string, required): A valid PromQL filter query.
- `label` (string, required): The label to get values for.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
### prometheus_labels
Return the labels for a given PromQL match query. Similar to Prometheus /labels call.
Parameters:
- `match_query` (string, required): A valid PromQL filter query.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - 60 minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
### get_logs
Gets logs filtered by service name and/or severity level within a specified time range. This tool now uses the advanced v2 logs API with physical index optimization for better performance.
**Note**: This tool now requires a `service_name` parameter and internally uses the same advanced infrastructure as `get_service_logs`.
Parameters:
- `service_name` (string, required): Name of the service to get logs for.
- `severity` (string, optional): Severity of the logs to get (automatically converted to severity_filters format).
- `lookback_minutes` (integer, recommended): Number of minutes to look back from now. Default: 60. Examples: 60, 30, 15.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
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
- `time_iso` (string, optional): Evaluation time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Preferred.
- `timestamp` (integer, optional): Unix timestamp for the query time. Deprecated alias.
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
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
Filtering behavior:
- Multiple filter types are combined with AND logic (service AND severity AND body)
- Each filter array uses OR logic (matches any pattern in the array)
Examples:
- service_name="api" + severity_filters=["error"] + body_filters=["timeout"] → finds error logs containing "timeout"
- service_name="web" + body_filters=["timeout", "failed", "error 500"] → finds logs containing any of these patterns
### get_log_attributes
Get available log attributes (labels) for a specified time window. This tool retrieves all attribute names that exist in logs during the specified time range, which can be used for filtering and querying logs.
Parameters:
- `lookback_minutes` (integer, optional): Number of minutes to look back from now for the time window. Default: 15. Examples: 15, 30, 60.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `region` (string, optional): AWS region to query. Leave empty to use default from configuration. Examples: ap-south-1, us-east-1, eu-west-1.
Returns:
- List of log attributes grouped into two categories:
  - Log Attributes: Standard log fields like service, severity, body, level, etc.
  - Resource Attributes: Resource-related fields prefixed with "resource_" like resource_k8s.pod.name, resource_service.name, etc.
### get_traces
Execute advanced trace queries using JSON pipeline syntax for complex filtering and aggregation. This tool provides powerful querying capabilities for traces using a pipeline-based approach with filters, aggregations, and transformations.
Parameters:
- `tracejson_query` (array, required): JSON pipeline query for traces. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z).
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z).
- `lookback_minutes` (integer, optional): Number of minutes to look back from now. Default: 60 minutes.
- `limit` (integer, optional): Maximum number of traces to return. Default: 20. Range: 1-100.
This tool supports complex queries with multiple filter conditions, aggregations, and custom processing pipelines for advanced trace analysis.
### get_service_traces
Retrieve traces from Last9 by trace ID or service name. This tool allows you to get specific traces either by providing a trace ID for a single trace, or by providing a service name to get all traces for that service within a time range.
Parameters:
- `trace_id` (string, optional): Specific trace ID to retrieve. Cannot be used with service_name.
- `service_name` (string, optional): Name of service to get traces for. Cannot be used with trace_id.
- `lookback_minutes` (integer, optional): Number of minutes to look back from now. Default: 60 minutes. Examples: 60, 30, 15.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `limit` (integer, optional): Maximum number of traces to return. Default: 10. Range: 1-100.
- `env` (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.
Usage rules:
- Exactly one of `trace_id` or `service_name` must be provided (not both, not neither)
- Time range filtering only applies when using `service_name`
Examples:
- trace_id="abc123def456" - retrieves the specific trace
- service_name="payment-service" + lookback_minutes=30 - gets all payment service traces from last 30 minutes
Returns trace data including trace IDs, spans, duration, timestamps, and status information.
### get_trace_attributes
Get available trace attributes (series) for a specified time window. This tool retrieves all attribute names that exist in traces during the specified time range, which can be used for filtering and querying traces.
Parameters:
- `lookback_minutes` (integer, optional): Number of minutes to look back from now for the time window. Default: 15. Examples: 15, 30, 60.
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `region` (string, optional): AWS region to query. Leave empty to use default from configuration. Examples: ap-south-1, us-east-1, eu-west-1.
Returns:
- An alphabetically sorted list of all available trace attributes (e.g., http.method, http.status_code, db.name, resource_service.name, duration, etc.)
### get_change_events
Get change events from the last9_change_events prometheus metric over a given time range. Returns change events that occurred in the specified time window, including deployments, configuration changes, and other system modifications.
Parameters:
- `start_time_iso` (string, optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - lookback_minutes.
- `end_time_iso` (string, optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
- `lookback_minutes` (integer, optional): Number of minutes to look back from now. Default: 60 minutes. Examples: 60, 30, 15.
- `service` (string, optional): Name of the service to filter change events for.
- `environment` (string, optional): Environment to filter by.
- `event_name` (string, optional): Name of the change event to filter by (use available_event_names to see valid values).
Returns:
- `available_event_names`: List of all available event types that can be used for filtering
- `change_events`: Array of timeseries data with metric labels and timestamp-value pairs
- `count`: Total number of change events returned
- `time_range`: Start and end time of the query window
Each change event includes:
- `metric`: Map of metric labels (service_name, env, event_type, message, etc.)
- `values`: Array of timestamp-value pairs representing the timeseries data
Common event types include: deployment, config_change, rollback, scale_up/scale_down, restart, upgrade/downgrade, maintenance, backup/restore, health_check, certificate, database.
Best practices:
1. First call without event_name to get available_event_names
2. Use exact event name from available_event_names for the event_name parameter
3. Combine with other filters (service, environment, time) for precise results
## Installation
You can install and run the Last9 Observability MCP server in several ways:
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
- `LAST9_REFRESH_TOKEN`: (required) Refresh Token with Write permissions from
  [API Access](https://app.last9.io/settings/api-access). This token is used for
  all authentication and will automatically obtain access tokens as needed.
- `OTEL_EXPORTER_OTLP_ENDPOINT`: (required) OpenTelemetry collector endpoint URL
- `OTEL_EXPORTER_OTLP_HEADERS`: (required) Headers for OTLP exporter authentication

Optional environment variables:
- `LAST9_DATASOURCE`: Name of the datasource/cluster to use. If not specified, the default datasource configured in your Last9 organization will be used.
- `LAST9_API_HOST`: API host to connect to. Defaults to `app.last9.io`. Use this if you need to connect to a different Last9 endpoint (e.g., regional or self-hosted instances).
- `LAST9_DISABLE_TELEMETRY`: Set to `true` to disable OpenTelemetry tracing and metrics. Use this if you don't have an OTLP collector running or want to skip telemetry.

## Usage
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
        "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
        "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
        "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
          "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
          "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
          "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
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
          "LAST9_REFRESH_TOKEN": "<last9_refresh_token>",
          "OTEL_EXPORTER_OTLP_ENDPOINT": "<otel_endpoint_url>",
          "OTEL_EXPORTER_OTLP_HEADERS": "<otel_headers>"
        }
      }
    }
  }
}
```
## Development
For local development and testing, you can run the MCP server in HTTP mode which makes it easier to debug requests and responses.
### Running in HTTP Mode
Set the `LAST9_HTTP` environment variable to enable HTTP server mode:
```bash
# Export required environment variables
export LAST9_REFRESH_TOKEN="your_refresh_token"
export OTEL_EXPORTER_OTLP_ENDPOINT="<otel_endpoint_url>"
export OTEL_EXPORTER_OTLP_HEADERS="<otel_headers>"
export LAST9_HTTP=true
export LAST9_PORT=8080  # Optional, defaults to 8080
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
LAST9_HTTP=true ./last9-mcp-server
```
**Note**: HTTP mode is for development and testing only. When integrating with Claude Desktop or other MCP clients, use the default STDIO mode (without `LAST9_HTTP=true`).

## Testing

See [TESTING.md](TESTING.md) for detailed testing instructions.

## Badges
[![MseeP.ai Security Assessment Badge](https://mseep.net/pr/last9-last9-mcp-server-badge.png)](https://mseep.ai/app/last9-last9-mcp-server)
