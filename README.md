# Last9 MCP Server

![last9 mcp demo](mcp-demo.gif)

Your AI agent doesn't know what's broken in production. This fixes that.

[Last9 MCP Server](https://last9.io/mcp/) connects Claude, Cursor, Windsurf, and any other MCP-capable AI assistant directly to your production observability data — logs, metrics, traces, exceptions, database queries, alerts, and deployments. The agent stops guessing and starts reading the actual signal.

- [Watch the demo](https://www.youtube.com/watch?v=AQH5xq6qzjI)
- [Announcement post](https://last9.io/blog/launching-last9-mcp-server/)

---

## Start in 30 seconds (Hosted)

No binary to install. No tokens to manage. One URL, OAuth in your browser, done.

Find your org slug in your Last9 URL: `app.last9.io/<org_slug>/...`

### Claude Code

```bash
claude mcp add --transport http last9 https://app.last9.io/api/v4/organizations/<org_slug>/mcp
```

Type `/mcp`, select last9, authenticate. That's it.

### Cursor

**Settings > MCP > Add New MCP Server:**

```json
{
  "mcpServers": {
    "last9": {
      "type": "http",
      "url": "https://app.last9.io/api/v4/organizations/<org_slug>/mcp"
    }
  }
}
```

Click **Connect**, complete OAuth.

### VS Code

Requires v1.99+. Open Command Palette → **MCP: Add Server**, paste the URL, authenticate.

Or directly in `settings.json`:

```json
{
  "mcp": {
    "servers": {
      "last9": {
        "type": "http",
        "url": "https://app.last9.io/api/v4/organizations/<org_slug>/mcp"
      }
    }
  }
}
```

### Windsurf

**Settings > Cascade > Open MCP Marketplace > gear icon (`mcp_config.json`):**

```json
{
  "mcpServers": {
    "last9": {
      "serverUrl": "https://app.last9.io/api/v4/organizations/<org_slug>/mcp"
    }
  }
}
```

### Claude Web/Desktop

**Settings > Connectors > Add custom connector.** Name it `last9`, paste the URL, authenticate.

> Requires admin access to your Claude organization.

---

## Self-Hosted (STDIO)

Use this when your MCP client doesn't support HTTP transport, or when you need the server running locally.

### Install

**Homebrew:**

```bash
brew install last9/tap/last9-mcp
```

**NPM:**

```bash
npm install -g @last9/mcp-server@latest
# or directly:
npx -y @last9/mcp-server@latest
```

**Binary releases** (Windows / manual):

Download from [GitHub Releases](https://github.com/last9/last9-mcp-server/releases/latest):

| Platform        | Archive                                 |
| --------------- | --------------------------------------- |
| Windows (x64)   | `last9-mcp-server_Windows_x86_64.zip`   |
| Windows (ARM64) | `last9-mcp-server_Windows_arm64.zip`    |
| Linux (x64)     | `last9-mcp-server_Linux_x86_64.tar.gz`  |
| Linux (ARM64)   | `last9-mcp-server_Linux_arm64.tar.gz`   |
| macOS (x64)     | `last9-mcp-server_Darwin_x86_64.tar.gz` |
| macOS (ARM64)   | `last9-mcp-server_Darwin_arm64.tar.gz`  |

### Get a Refresh Token

Only **admins** can create tokens.

1. Go to [API Access](https://app.last9.io/settings/api-access)
2. Click **Generate Token** with Write permissions
3. Copy it

### Client Configuration

**Homebrew:**

```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_REFRESH_TOKEN": "<your_refresh_token>"
      }
    }
  }
}
```

**NPM:**

```json
{
  "mcpServers": {
    "last9": {
      "command": "npx",
      "args": ["-y", "@last9/mcp-server@latest"],
      "env": {
        "LAST9_REFRESH_TOKEN": "<your_refresh_token>"
      }
    }
  }
}
```

**Where to paste this:**

| Client             | Location                                                                                                                                |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| Claude Web/Desktop | Settings > Developer > Edit Config (`claude_desktop_config.json`)                                                                       |
| Cursor             | Settings > Cursor Settings > MCP > Add New Global MCP Server                                                                            |
| Windsurf           | Settings > Cascade > MCP Marketplace > gear icon (`mcp_config.json`)                                                                    |
| VS Code            | Wrap in `{ "mcp": { "servers": { ... } } }` in `settings.json` — [details](https://code.visualstudio.com/docs/copilot/chat/mcp-servers) |

<details>
<summary>VS Code STDIO config</summary>

```json
{
  "mcp": {
    "servers": {
      "last9": {
        "type": "stdio",
        "command": "/opt/homebrew/bin/last9-mcp",
        "env": {
          "LAST9_REFRESH_TOKEN": "<your_refresh_token>"
        }
      }
    }
  }
}
```

For NPM: use `"command": "npx"` and add `"args": ["-y", "@last9/mcp-server@latest"]`.

</details>

<details>
<summary>Windows</summary>

After downloading from [GitHub Releases](https://github.com/last9/last9-mcp-server/releases/latest), extract and point to the full path:

```json
{
  "mcpServers": {
    "last9": {
      "command": "C:\\Users\\<user>\\AppData\\Local\\Programs\\last9-mcp-server.exe",
      "env": {
        "LAST9_REFRESH_TOKEN": "<your_refresh_token>"
      }
    }
  }
}
```

The NPM route is easier on Windows — no path management.

</details>

### Environment Variables

| Variable                     | Default              | Description |
| ---------------------------- | -------------------- | ----------- |
| `LAST9_REFRESH_TOKEN`        | *(required)*         | Refresh token from [API Access](https://app.last9.io/settings/api-access) |
| `LAST9_DATASOURCE`           | org default          | Datasource/cluster name — useful when you have multiple Levitate clusters |
| `LAST9_API_HOST`             | `app.last9.io`       | Override the API host |
| `LAST9_MAX_GET_LOGS_ENTRIES` | `5000`               | Max entries for chunked `get_logs` requests |
| `LAST9_DEBUG_CHUNKING`       | `false`              | Set `true` to log chunk-planning details for `get_logs`, `get_service_logs`, `get_traces` |
| `LAST9_DISABLE_TELEMETRY`    | `true`               | Set `false` to enable internal OTel tracing |
| `OTEL_SDK_DISABLED`          | —                    | Standard OTel env var. Overrides `LAST9_DISABLE_TELEMETRY` |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| —                    | OTLP collector endpoint (only when telemetry is enabled) |
| `OTEL_EXPORTER_OTLP_HEADERS` | —                    | OTLP auth headers (only when telemetry is enabled) |

---

## What It Can Do

### Service Health

- **`get_service_summary`** — Throughput, error rate, p95 response time across all services
- **`get_service_environments`** — Available environments for your services. Run this first — other APM tools need `env` from here
- **`get_service_performance_details`** — Full breakdown: throughput, error rate, p50/p90/p95/avg/max, apdex, availability
- **`get_service_operations_summary`** — Operations grouped by HTTP endpoints, DB calls, messaging, HTTP clients
- **`get_service_dependency_graph`** — Dependency map with throughput, latency, and error rates for upstream/downstream/infra
- **`get_exceptions`** — Server-side exceptions with service and span filters

### Database Observability

Four tools that go directly at your database performance, derived from OpenTelemetry trace spans. No extra instrumentation needed if you're already using OTel.

- **`get_databases`** — Discover all databases across your infrastructure: DB type, host, throughput (queries/min), p95 latency, error rate, number of dependent services
- **`get_database_slow_queries`** — The actual slowest query executions, ordered by duration, with trace IDs for drilling into full traces
- **`get_database_queries`** — Query patterns and aggregates: how often a query runs, average/p95 duration, error rate
- **`get_database_server_metrics`** — Server-side metrics from the DB host itself (CPU, connections, buffer hit rates — depends on your DB system)

Supports PostgreSQL, MySQL, MongoDB, Redis, Aerospike, and anything else OTel traces with a `db_system` attribute.

### Prometheus / PromQL

- **`prometheus_range_query`** — PromQL range queries over any metric
- **`prometheus_instant_query`** — Instant queries; use rollup functions like `avg_over_time`, `sum_over_time`
- **`prometheus_label_values`** — Label values for a given series
- **`prometheus_labels`** — All labels available for a series

Point these at a different datasource/cluster than the default by setting `LAST9_DATASOURCE`.

### Logs

- **`get_logs`** — Full JSON pipeline log queries (aggregations, filters, field extraction)
- **`get_service_logs`** — Raw log lines for a service, filterable by severity and body content
- **`get_log_attributes`** — Available attributes in the log schema for a time window
- **`get_drop_rules`** — Log drop rules from [Last9 Control Plane](https://last9.io/control-plane)
- **`add_drop_rule`** — Create a new drop rule to cut log volume at the source

### Traces

- **`get_traces`** — JSON pipeline trace queries for broad searches and aggregations
- **`get_service_traces`** — Traces by exact trace ID or service name. Use this when you have a trace ID — it's faster
- **`get_trace_attributes`** — Available attributes in the trace schema

### Change Events & Alerts

- **`get_change_events`** — Deployments, config changes, rollbacks. Correlate incidents with what changed
- **`get_alert_config`** — Alert rule configurations — searchable by name, severity, type, tags
- **`get_alerts`** — Currently firing alerts within a time window
- **`get_notification_channels`** — Configured notification channels (Slack, PagerDuty, email, etc.)

### Fuzzy Name Resolution

- **`did_you_mean`** — When the agent isn't sure about an entity name, this returns the closest matches from your catalog (services, environments, hosts, databases, K8s deployments/namespaces, jobs). Up to 3 suggestions with similarity scores. The server calls this automatically before most tools when a name lookup returns empty.

---

## How It Works

**Deep links on every response.** Every tool returns a `deep_link` field — a direct URL into the Last9 dashboard for that exact query and time range. The agent can hand you the link; you click it; you're there.

**Live attribute caching.** At startup, the server fetches the actual log and trace attribute names from your data and embeds them into tool descriptions. This means the AI assistant knows what fields exist in your schema, not just a generic list. The cache refreshes every 2 hours.

**Chunked large results.** `get_logs` and `get_traces` handle large result sets through chunking rather than truncating. The default limit is 5000 entries for logs; configurable via `LAST9_MAX_GET_LOGS_ENTRIES`.

---

## Development

<details>
<summary>HTTP mode, curl testing, building from source</summary>

### Run in HTTP Mode

```bash
export LAST9_REFRESH_TOKEN="your_refresh_token"
export LAST9_HTTP=true
export LAST9_PORT=8080
./last9-mcp-server
```

Server starts at `http://localhost:8080/mcp`.

### Test with curl

MCP Streamable HTTP requires an initialize handshake first. Don't set `Mcp-Session-Id` on the first request.

```bash
# Step 1: Initialize
SESSION_ID=$(curl -si -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -d '{
      "jsonrpc": "2.0",
      "id": 1,
      "method": "initialize",
      "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "curl-test", "version": "1.0"}
      }
    }' | grep -i "^Mcp-Session-Id:" | awk '{print $2}' | tr -d '\r')
echo "Session: $SESSION_ID"

# Step 2: Send initialized notification
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: $SESSION_ID" \
    -d '{"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}}'

# Step 3: List tools
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: $SESSION_ID" \
    -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}'

# Step 4: Call a tool
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: $SESSION_ID" \
    -d '{
      "jsonrpc": "2.0",
      "id": 3,
      "method": "tools/call",
      "params": {
        "name": "get_service_logs",
        "arguments": {
          "service": "your-service-name",
          "lookback_minutes": 30,
          "limit": 10
        }
      }
    }'
```

### Build from Source

```bash
git clone https://github.com/last9/last9-mcp-server.git
cd last9-mcp-server
go build -o last9-mcp-server
LAST9_HTTP=true ./last9-mcp-server
```

`LAST9_HTTP=true` is for local development. For actual usage, the [hosted HTTP endpoint](#start-in-30-seconds-hosted) is easier.

</details>

---

## Tool Reference

<details>
<summary>All parameters, time input standards, and details</summary>

### Time Input

- Absolute times (`start_time_iso`/`end_time_iso`, or `time_iso`) take precedence over `lookback_minutes`.
- For relative windows: use `lookback_minutes`.
- For absolute windows: use RFC3339/ISO8601 — `2026-02-09T15:04:05Z`.
- Legacy `YYYY-MM-DD HH:MM:SS` is accepted for compatibility only.

### get_exceptions

- `limit` (integer, optional): Max exceptions. Default: 20.
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional): Absolute time range.
- `service_name` (string, optional): Filter by service.
- `span_name` (string, optional): Filter by span name.
- `deployment_environment` (string, optional): Filter by environment.

### get_service_summary

- `start_time_iso` / `end_time_iso` (string, optional)
- `env` (string, optional): Defaults to `prod`.

### get_service_environments

- `start_time_iso` / `end_time_iso` (string, optional)

> All other APM tools require an `env` value. Use `""` if this returns empty.

### get_service_performance_details

- `service_name` (string, required)
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)
- `env` (string, optional): Defaults to `prod`.

### get_service_operations_summary

- `service_name` (string, required)
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)
- `env` (string, optional): Defaults to `prod`.

### get_service_dependency_graph

- `service_name` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)
- `env` (string, optional): Defaults to `prod`.

### get_databases

- `env` (string, optional): Filter by environment. Default: all.
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)

### get_database_slow_queries

- `db_system` (string, optional): e.g. `postgresql`, `mysql`, `mongodb`, `redis`.
- `host` (string, optional): Database host (`net_peer_name`).
- `service_name` (string, optional): Calling service name.
- `env` (string, optional)
- `min_duration_ms` (float, optional): Minimum query duration in ms.
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)
- `limit` (integer, optional): Default: 20.

### get_database_queries

- `db_system` (string, optional)
- `host` (string, optional)
- `service_name` (string, optional)
- `env` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)
- `limit` (integer, optional): Default: 20.

### get_database_server_metrics

- `db_system` (string, required): e.g. `postgresql`, `mysql`, `mongodb`, `redis`, `aerospike`.
- `host` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)

### prometheus_range_query

- `query` (string, required): The PromQL query.
- `start_time_iso` / `end_time_iso` (string, optional): Defaults to last 60 min.
- `lookback_minutes` (float, optional): Default: 60.

### prometheus_instant_query

- `query` (string, required)
- `time_iso` (string, optional): Defaults to now.
- `lookback_minutes` (float, optional)

### prometheus_label_values

- `match_query` (string, optional): PromQL filter.
- `label` (string, required): Label name.
- `start_time_iso` / `end_time_iso` (string, optional)

### prometheus_labels

- `match_query` (string, optional): PromQL filter.
- `start_time_iso` / `end_time_iso` (string, optional)

### get_logs

- `logjson_query` (array, required): JSON pipeline query.
- `lookback_minutes` (integer, optional): Default: 5.
- `start_time_iso` / `end_time_iso` (string, optional)
- `limit` (integer, optional): Server default: 5000.
- `index` (string, optional): `physical_index:<name>` or `rehydration_index:<block_name>`.

### get_service_logs

- `service` (string, required)
- `lookback_minutes` (integer, optional): Default: 60.
- `limit` (integer, optional): Default: 20.
- `env` (string, optional)
- `severity_filters` (array, optional): e.g. `["error", "warn"]`. OR logic.
- `body_filters` (array, optional): e.g. `["timeout", "failed"]`. OR logic.
- `start_time_iso` / `end_time_iso` (string, optional)
- `index` (string, optional)

Multiple filter types combine with AND. Each array uses OR internally.

### get_log_attributes

- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional)
- `region` (string, optional)
- `index` (string, optional)

### get_drop_rules

No parameters.

### add_drop_rule

- `name` (string, required)
- `filters` (array, required): Each filter: `key`, `value`, `operator` (`equals`/`not_equals`), `conjunction` (`and`).

### get_traces

Use for broad searches and aggregations. For exact trace ID lookup, use `get_service_traces`.

- `tracejson_query` (array, required)
- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `limit` (integer, optional): Default: 5000.

### get_service_traces

Exactly one of `trace_id` or `service_name` is required.

- `trace_id` (string, optional): Default lookback: 72 hours.
- `service_name` (string, optional): Default lookback: 60 min.
- `lookback_minutes` (integer, optional)
- `start_time_iso` / `end_time_iso` (string, optional)
- `limit` (integer, optional): Default: 10.
- `env` (string, optional)

### get_trace_attributes

- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional)
- `region` (string, optional)

### get_change_events

- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `service` (string, optional)
- `environment` (string, optional)
- `event_name` (string, optional): Call without this first to get `available_event_names`.

### get_alert_config

- `search_term` (string, optional): Free-text search across name, group, data source, tags.
- `rule_name` (string, optional)
- `severity` (string, optional)
- `rule_type` (string, optional): `static` or `anomaly`.
- `alert_group_name` / `alert_group_type` / `data_source_name` (string, optional)
- `tags` (array, optional): All must match (AND logic).

### get_alerts

- `time_iso` (string, optional): Evaluation time in RFC3339.
- `window` (integer, optional): Lookback in seconds. Default: 900. Range: 60–86400.
- `lookback_minutes` (integer, optional): Range: 1–1440.

### get_notification_channels

No parameters. Returns all configured notification channels (Slack, PagerDuty, email, webhooks, etc.).

### did_you_mean

- `query` (string, required): The name to search for — partial, misspelled, or abbreviated.
- `type` (string, optional): Restrict to entity type: `service`, `environment`, `host`, `database`, `k8s_deployment`, `k8s_namespace`, `job`.

Returns up to 3 closest matches with similarity scores. Use this before any tool call where the entity name is uncertain. If a previous call returned empty results, try this before retrying.

</details>

---

[![MseeP.ai Security Assessment Badge](https://mseep.net/pr/last9-last9-mcp-server-badge.png)](https://mseep.ai/app/last9-last9-mcp-server)
