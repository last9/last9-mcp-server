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

Requires Node.js 22.23.4+, 24.22.1+ or 26.12.1+: earlier releases accept TLS certificates from the compromised eMudhra intermediates when the installer downloads the binary.

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
| `LAST9_TOOLSETS`             | all tools            | Comma-separated toolsets to expose (`logs`, `traces`, `metrics`, `alerts`, `dashboards`, `profiles`, `grafana`, `investigate`, `all`). Alias: `LAST9_MCP_TOOLSETS` |
| `LAST9_MCP_READ_ONLY`        | `false`              | Set `true` to serve only read-only tools; see [Read-only mode](#read-only-mode). A value that is not a boolean is a startup error. Alias: `LAST9_READ_ONLY` |
| `LAST9_MCP_ALLOW_WRITE_TOOLS`| —                    | Comma-separated write tools still served in read-only mode. Alias: `LAST9_ALLOW_WRITE_TOOLS` |
| `LAST9_MAX_GET_LOGS_ENTRIES` | `5000`               | Max entries for chunked `get_logs` requests |
| `LAST9_USE_LOG_SEARCH_API`   | `false`              | Set `true` to answer `get_logs` and `get_service_logs` with one server-side search call instead of client-side chunking. Each call gets 90 seconds, then returns advice to narrow the window; a 502 or 503 is retried once within it |
| `LAST9_DEBUG_CHUNKING`       | `false`              | Set `true` to log chunk-planning details for `get_logs`, `get_service_logs`, `get_traces` |
| `LAST9_DISABLE_TELEMETRY`    | `true`               | Set `false` to enable internal OTel tracing |
| `OTEL_SDK_DISABLED`          | —                    | Standard OTel env var. Overrides `LAST9_DISABLE_TELEMETRY` |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| —                    | OTLP collector endpoint (only when telemetry is enabled) |
| `OTEL_EXPORTER_OTLP_HEADERS` | —                    | OTLP auth headers (only when telemetry is enabled) |

### Read-only mode

`--read-only` (or `LAST9_MCP_READ_ONLY=true`) serves only tools annotated `readOnlyHint: true`, so no tool can create, change or delete alert rules, notification channels, dashboards or drop rules, acknowledge or reassign alerts, or mute groups. A tool without annotations counts as a write tool and is hidden.

To keep a few write tools, list them with `--allow-write-tools` (or `LAST9_MCP_ALLOW_WRITE_TOOLS`):

```bash
last9-mcp --read-only --allow-write-tools=acknowledge_alert,mute_alert_group
```

- The server refuses to start if an allowed name is not a write tool (a typo, or a read tool's name); the error lists the valid write tools.
- `--allow-write-tools` without `--read-only` is a startup error, because every write tool is already served.
- Read-only combines with `--toolsets`: a tool is served only when it passes both. `--toolsets=alerts --read-only --allow-write-tools=mute_alert_group` serves the alerts read tools plus `mute_alert_group`; an allowed write tool outside the chosen toolsets stays hidden.
- `--http` mode and `dump-tools` honor the same flags and env vars: `go run . dump-tools --read-only` prints the read-only surface.
- Read tools' descriptions, schemas and results, and the server instructions, never name a hidden write tool. An allowed write tool can still name its hidden siblings (`create_alert` creates a disabled rule that `patch_alert` enables), so allow such pairs together.

---

## What It Can Do

### Orientation

- **`get_workspace_overview`** — Recommended first call when a request names no service or tool (one that does goes straight to its tool): environments, which signals (logs, traces, metrics) were seen in the last hour, service and alert-group counts, firing alerts, and the current time in one cheap call. A signal not seen in that hour can still have older data, so it stays queryable. A failed sub-call leaves its field `null` and adds an `errors[]` entry. Served under every toolset
- **`get_current_time`** — Current time, plus absolute bounds for `last 2h`, `yesterday` or `since yesterday 9am IST`, in any IANA zone or common abbreviation. Served under every toolset

### Service Health

- **`get_service_summary`** — Ranked fleet `(service, env)` rows: interval request_count, throughput_rpm, HTTP 4xx/5xx counts, and gRPC error counts
- **`get_service_environments`** — Available environments for your services. Run this first — other APM tools need `env` from here
- **`get_service_performance_details`** — Full breakdown: throughput, error rate, p50/p90/p95/avg/max, apdex, availability
- **`get_service_operations_summary`** — Operations grouped by HTTP endpoints, DB calls, messaging, HTTP clients
- **`get_service_dependency_graph`** — Dependency map with throughput, latency, and error rates for upstream/downstream/infra
- **`get_apm_service_deviations`** — Compare a current window against an equal-duration baseline: regressions/improvements, Apdex reconciliation, and a terminal outcome (fleet or single service)
- **`get_exceptions`** — Server-side exceptions with service and span filters

### Database Observability

Four tools that go directly at your database performance, derived from OpenTelemetry trace spans and, where traces are absent, infrastructure metrics such as CloudWatch. No extra instrumentation needed if you're already using OTel.

- **`get_databases`** — Discover all databases across your infrastructure: DB type, host, throughput (queries/min), p95 latency, error rate, number of dependent services. Also discovers databases from infrastructure metrics such as CloudWatch, with no trace instrumentation needed — those rows carry an activity value instead of trace metrics
- **`get_database_slow_queries`** — The actual slowest query executions, ordered by duration, with trace IDs for drilling into full traces
- **`get_database_queries`** — Query patterns and aggregates: how often a query runs, average/p95 duration, error rate
- **`get_database_server_metrics`** — Server-side metrics from the DB host itself (CPU, connections, buffer hit rates — depends on your DB system)

Supports PostgreSQL, MySQL, MongoDB, Redis, Aerospike, and anything else OTel traces with a `db_system` attribute — plus databases discovered from infrastructure metrics such as CloudWatch, whose rows carry an activity value instead of trace metrics.

### Prometheus / PromQL

- **`prometheus_range_query`** — PromQL range queries over any metric
- **`prometheus_instant_query`** — Instant queries; use rollup functions like `avg_over_time`, `sum_over_time`
- **`prometheus_label_values`** — Label values for a given series
- **`prometheus_labels`** — All labels available for a series

Point these at a different datasource/cluster than the default by setting `LAST9_DATASOURCE`.

### Logs

- **`get_logs`** — Full JSON pipeline log queries (aggregations, filters, field extraction)
- **`get_service_logs`** — Raw log lines for a service, filterable by severity and body content
- **`get_log_patterns`** — A service's log lines in a window clustered into message templates, with count, first/last seen, severity mix and a sample
- **`get_emerging_log_patterns`** — Log templates that are new, increased, decreased or disappeared against a baseline window, with counts from both
- **`get_log_attributes`** — Global catalog of attributes in the log schema for a time window
- **`get_log_attributes_for_pipeline`** — Log fields actually present for an in-progress pipeline (scoped discovery), each with its exact `filter_field`
- **`get_drop_rules`** — Log drop rules from [Last9 Control Plane](https://last9.io/control-plane)
- **`add_drop_rule`** — Create a new drop rule to cut log volume at the source

### Traces

- **`get_traces`** — JSON pipeline trace queries for broad searches and aggregations
- **`get_service_traces`** — Traces by exact trace ID or service name. Use this when you have a trace ID — it's faster
- **`get_trace_attributes`** — Global catalog of attributes in the trace schema
- **`get_trace_attributes_for_pipeline`** — Attributes actually present for an in-progress pipeline (scoped discovery), each with its exact `filter_field`
- **`get_trace_attribute_values`** — Distinct values for a trace attribute, optionally scoped to a pipeline
- **`get_trace_attribute_deviations`** — Ranks attribute values that differ between two bounded span cohorts (slow vs fast, error vs non-error, or two time windows). Correlation, not cause
- **`get_trace_waterfall`** — One exact trace as a parent/child waterfall with interval-union self-time, slowest spans, and graph warnings

### Change Events & Alerts

- **`get_change_events`** — Deployments, config changes, rollbacks. Correlate incidents with what changed
- **`analyze_change_impact`** — Did a change break anything? Compares a service and its direct callers before and after a change: golden signals, APM and trace-attribute deviations, new firing alerts, and a verdict (`regression likely` / `no significant change` / `insufficient data`)
- **`get_alert_groups`** — Configured Compass alert groups with metadata labels, team, tier, and rule counts — including groups with zero rules and groups that are not firing
- **`get_alert_config`** — Alert rule configurations — searchable by name, severity, type, tags
- **`get_alerts`** — Currently firing alerts within a time window
- **`get_alert_rule_state`** — Historical firing state (1/0) per alert rule over a time range, grouped by `rule_id`. Filterable by alert group, rule name, label filters, and state.
- **`get_notification_channels`** — Configured notification channels (Slack, PagerDuty, email, etc.). `scope=channels` lists the channels themselves (ids for attaching); the default `attached` lists per-alert-group attachments
- **`recommend_alert_config`** — Read-only design guidance (lifecycle, validation gates, responder context) before creating an alert rule
- **`validate_alert_expression`** — Dry-run an alert expression against an alert group's KPIs without saving anything
- **`describe_alert_chart`** — Read-only: the alertable signals (key, unit) on a Discover chart (`discover-service`, `discover-exceptions`)
- **`create_alert`** — Create an alert rule (static, or adaptive via `adaptive_std_cmp`) in an existing alert group; always created disabled as a draft. Create once; refine with `update_alert` / `patch_alert`
- **`create_alert_from_chart`** — Create a static-threshold alert rule from a Discover chart in one call. Unlike `create_alert`, the rule is enabled immediately
- **`update_alert`** — Full replacement of a rule's evaluation config (may return a replacement rule ID)
- **`patch_alert`** — Partial change to a rule: enable/pause, `mute_until`, responder `properties`, weekly `recurring_mute_schedule` (or `clear_recurring_mute_schedule`)
- **`delete_alert`** — Permanently delete an alert rule
- **`mute_alert_group`** — Mute (or unmute) every rule in an alert group until a deadline
- **`acknowledge_alert`** — Take ownership of a firing alert, or release it
- **`reassign_alert`** — Hand an already-acknowledged alert to another user
- **`create_notification_channel`** — Create a Slack, PagerDuty, Opsgenie, email, or generic webhook channel
- **`attach_notification_channel`** — Route an existing channel to an alert group for `breach`, `threat`, or both
- **`detach_notification_channel`** — Remove one channel-to-alert-group attachment
- **`delete_notification_channel`** — Permanently delete a channel that has no attachments

The alert write tools (`create_alert`, `update_alert`, `patch_alert`, `delete_alert`, `create_alert_from_chart`, `mute_alert_group`, `acknowledge_alert`, `reassign_alert`, `create_notification_channel`, `attach_notification_channel`, `detach_notification_channel`, `delete_notification_channel`) need a token with write scope; `delete_alert`, `detach_notification_channel` and `delete_notification_channel` need delete scope instead. They are exposed only in the `alerts` toolset.

Rules, channels and attachments these tools create are marked `created_via: last9-mcp` together with the MCP client's name and version (on rules, the `_l9_created_via_` and `_l9_created_via_client_` annotations, which `patch_alert` and `update_alert` keep and callers cannot set). `get_entity_alert_rules` and `get_notification_channels` show the marker, and `delete_alert`, `detach_notification_channel` and `delete_notification_channel` tell agents to clean up only marked resources unless the user names one.

### Custom Dashboards

- **`list_dashboards`** — All custom dashboards in your org: IDs, names, and metadata
- **`get_dashboard`** — Full dashboard definition by ID, including panels and queries
- **`validate_dashboard`** — Read-only lint + execute + classify for a saved dashboard id or an inline `dashboard_definition` over a ≤24h window. Never creates or updates dashboards
- **`create_dashboard`** — Create a net-new custom dashboard once (panels, queries, metadata). After the id is returned, refine with `update_dashboard`.
- **`update_dashboard`** — Refine an existing dashboard by ID (full replacement; readonly system dashboards return an error)
- **`patch_dashboard`** — Targeted edits to an existing dashboard by ID (add/update/remove/move panel, set title, set variable); returns a compact diff, not the full dashboard
- **`delete_dashboard`** — Delete a custom dashboard by ID
- **`list_dashboard_snapshots`** — Frozen point-in-time snapshots for a dashboard (metadata only)
- **`get_dashboard_snapshot`** — Full frozen snapshot including panel data for RCA / shareable views
- **`delete_dashboard_snapshot`** — Delete a frozen snapshot by ID

### Continuous Profiling

Requires continuous profiling enabled for the org. Discover services first with `get_profile_services`, then pull a flamegraph or ranked functions.

- **`get_profile_services`** — Services that have profiling data in the window (index before querying)
- **`get_flamegraph`** — Nested flamegraph tree for one service (`cpu` default; also `alloc`, `wall`)
- **`get_top_functions`** — Self-sample ranking of hottest functions for one service
- **`get_profile_summary`** — Short natural-language triage of the profile for one service

### Grafana Dashboards

Read-only tools against the org's Grafana instance (via Last9's Grafana proxy). Credential fields are never returned to the model. Enable with `LAST9_TOOLSETS=grafana` (or leave toolsets unset for all tools).

- **`grafana_search_dashboards`** — Search dashboards by title substring (paginated; `truncated: true` when the cap is hit)
- **`grafana_get_dashboard`** — Dashboard summary by uid (panels, variables, PromQL targets); `full_json=true` for raw Grafana JSON
- **`grafana_list_folders`** — Folder tree
- **`grafana_list_folder_dashboards`** — Dashboards in one folder (paginated)
- **`grafana_list_datasources`** — Datasource inventory without credentials

### Fuzzy Name Resolution

- **`did_you_mean`** — When the agent isn't sure about an entity name, this returns the closest matches from your catalog (services, environments, hosts, databases, K8s deployments/namespaces, jobs). Up to 3 suggestions with similarity scores. The server calls this automatically before most tools when a name lookup returns empty.

### Service Profile

- **`get_service_profile`** — What a service's telemetry actually looks like, before you query it: which signals exist, language and runtime, deployment environments, the shape of its logs, and a recommended ingest fix where one applies. Lets the agent skip trace tools when a service has no traces, and parse severity from the log body when `SeverityText` is empty instead of filtering on it and finding nothing.

---

## How It Works

**Deep links on every response.** Every tool returns a `deep_link` field — a direct URL into the Last9 dashboard for that exact query and time range. The agent can hand you the link; you click it; you're there.

**Toolsets.** By default the server exposes every tool. Automation hosts that only need investigation (logs/traces/metrics/profiles) can set `LAST9_TOOLSETS=investigate` (or pass `--toolsets=investigate`) so `tools/list` stays small without client-side mass-disable. Named packs: `logs`, `traces`, `metrics`, `alerts`, `dashboards`, `profiles`, `grafana`, `investigate`, `all`. Unknown names fail fast. `get_current_time` and `get_workspace_overview` are served under every toolset. In HTTP mode a request can narrow this further with the `X-Last9-Toolsets` header (see [Per-request toolsets](#per-request-toolsets)). The `metrics` pack alone does **not** include `list_datasources` or `did_you_mean` — use `investigate` (or combine toolsets) when you need those discovery helpers.

**Tool reference resources.** Long logjson/tracejson/service-logs/metrics manuals are MCP resources (`last9://reference/logjson`, `last9://reference/tracejson`, `last9://reference/service_logs`, `last9://reference/metrics`, `last9://reference/investigation`), not always-on tool description text. Critical query rules stay on the tool description so agents that never call `resources/read` still get correct construction guidance. Discover org-specific fields with `get_log_attributes` / `get_log_attributes_for_pipeline` (and the trace equivalents)—they are not injected into descriptions.

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

The Streamable HTTP handler runs in **stateless** mode, so any request is served independently. An `initialize` handshake and an `Mcp-Session-Id` header are optional — clients that send them still work (the header is accepted and ignored), and clients can also skip straight to `tools/list` / `tools/call`. Every tool is an independent request/response query; the server issues no server→client notifications, so `GET /mcp` (the SSE stream) returns `405`.

```bash
# List tools — a session handshake is optional in stateless mode
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}'

# Call a tool
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -d '{
      "jsonrpc": "2.0",
      "id": 2,
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
```

### Per-request toolsets

In HTTP mode a request can narrow its tool surface with the `X-Last9-Toolsets` header, or the `toolsets` query parameter for clients that cannot set headers (`/mcp?toolsets=logs,traces`). A non-blank header wins when both are sent. The value takes the same comma-separated names as `--toolsets`. The request sees the intersection with the startup `--toolsets`: it can narrow, never widen, and `all` or an empty value keeps the startup set. An unknown name returns `400` naming it. `tools/list`, `tools/call` and the `initialize` instructions all follow the effective set; `get_current_time` and `get_workspace_overview` stay in every set.

```bash
curl -s -X POST http://localhost:8080/mcp \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -H "X-Last9-Toolsets: logs,alerts" \
    -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}'
```

Each distinct effective set is served by its own server, built on its first request (tens of milliseconds) and kept until shutdown; a request restating the startup set reuses the startup server. There is no eviction: only named toolsets are accepted, so the cache holds at most one server per combination of them (135 today, about 75 MB of live heap together; a full cache settled near 230 MB resident in testing).

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
- Resolve phrases like `since yesterday 9am IST` to RFC3339 bounds with `get_current_time`.

### get_workspace_overview

No parameters. Probes the last 60 minutes concurrently, each sub-call with a short timeout, and returns `current_time`, `lookback_minutes`, `environments`, `telemetry` (`logs`/`traces`/`metrics` booleans), `service_count` (distinct services `get_service_summary` ranks, every env), `alert_group_count`, `firing_alert_count` and `errors` (`[{field, error}]`). A field whose sub-call failed is `null` (unknown), never `0` or `false`. The probes are not narrowed by `--toolsets`: toolsets filter which tools are served, and the API token scopes what the server can read.

### get_current_time

- `range` (string, optional): `last 2h`, `past 30 minutes`, `last 7d` (s, m, h, d = 24h, w = 7d; at most 366d); `today`; `yesterday`; `since <point>`; `<point> to <point>`. A point is RFC3339, or `today` / `yesterday` / a weekday (`monday`, `mon`: its latest occurrence, today included) / `YYYY-MM-DD` with an optional `9am`, `9:30pm` or `14:30`. A trailing zone overrides `timezone` for both bounds; a zone anywhere else is an error. Omit for just `now`.
- `timezone` (string, optional): IANA name or abbreviation (IST = Asia/Kolkata, PST/PDT, EST/EDT, UTC, GMT). Default UTC. Unknown zones are an error, and so is the ambiguous `CST` (pass `America/Chicago` or `Asia/Shanghai`).

Returns `now` (and `start`/`end` with `range`) as `{iso, epoch_seconds, local}`; `local` appears only for a non-UTC zone.

### get_exceptions

- `limit` (integer, optional): Max exceptions. Default: 20.
- `lookback_minutes` (integer, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional): Absolute time range.
- `service_name` (string, optional): Filter by service.
- `span_name` (string, optional): Filter by span name.
- `env` (string, optional): Filter by environment.

### get_service_summary

- `start_time_iso` / `end_time_iso` (string, optional)
- `env` (string, optional): PromQL regex. Defaults to `.*`. Exact match needs anchors (e.g. `^prod$`).
- `sort_by` (string, optional): `request_count` (default), `throughput_rpm`, `http_4xx_count`, `http_5xx_count`, or `grpc_error_count`.
- `limit` (integer, optional): Max ranked rows. Omit or 0 means 10; values above 100 clamp to 100.

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

### get_apm_service_deviations

- `service_name` (string, optional): Omit for fleet scope; provide for one service and its operation correlations.
- `lookback_minutes` (integer, optional): Current window. Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional): Explicit current window.
- `baseline_start_time_iso` / `baseline_end_time_iso` (string, optional): Explicit baseline. Defaults to the immediately preceding equal-duration window.
- `datasource` (string, optional): Restrict the comparison to one datasource.
- `env` (string, optional): Defaults to `prod`.
- `max_services` / `max_operations` (integer, optional): Default 10, max 10 each.

### get_databases

- `env` (string, optional): Filter by environment. Accepts a regular expression. Default: all.
- `lookback_minutes` (integer, optional): Default: 60. Window may not exceed 7 days.
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
- `lookback_minutes` (float, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)

When no window is given and `match_query` names one metric (`metric{...}` or `{__name__="metric"}`), an empty 60-minute result is retried once over 4 days; a second text block says which window the result covers.

### prometheus_labels

- `match_query` (string, optional): PromQL filter.
- `lookback_minutes` (float, optional): Default: 60.
- `start_time_iso` / `end_time_iso` (string, optional)

Retries an empty single-metric lookup over 4 days, like `prometheus_label_values`.

### get_logs

- `logjson_query` (array, required): JSON pipeline query.
- `lookback_minutes` (integer, optional): Default: 5.
- `start_time_iso` / `end_time_iso` (string, optional)
- `limit` (integer, optional): Server default: 5000.
- `index` (string, optional): `physical_index:<name>` or `rehydration_index:<block_name>`.

For log-based service inventory, query `physical_index_service_count` first:

```promql
sum by (name, service_name, env) (physical_index_service_count{destination="logs"})
```

Use `service_name` as `ServiceName`, `env` as the environment when present, and `name` as the physical index name. If `name="default"`, omit `index`; for a non-default physical index selected by the user, pass `index: "physical_index:<name>"`. If the backend rejects explicit physical index filtering, retry without `index` and report that explicit physical index filtering is unavailable for that backend.

### get_service_logs

- `service_name` (string, required)
- `lookback_minutes` (integer, optional): Default: 60.
- `limit` (integer, optional): Default: 20.
- `env` (string, optional)
- `severity_filters` (array, optional): e.g. `["error", "warn"]`. OR logic.
- `body_filters` (array, optional): e.g. `["timeout", "failed"]`. OR logic.
- `start_time_iso` / `end_time_iso` (string, optional)
- `index` (string, optional)

Multiple filter types combine with AND. Each array uses OR internally.
Use `get_logs` for broad aggregate counts first; use `get_service_logs` only after narrowing to a service/env/index and a small sample set.

### get_log_patterns

- `service_name` (string, required)
- `env` (string, optional)
- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `severity_filters` / `body_filters` (array, optional): OR logic, as in `get_service_logs`. Set `severity_filters` only when `get_service_profile` reports `severity_set` `all`; with `none` or `partial` it drops lines whose severity is in the body.
- `index` (string, optional)
- `sample_size` (integer, optional): Lines sampled from the window, newest first. Default: 1000, maximum: 5000.
- `top_n` (integer, optional): Templates returned by count. Default: 20, maximum: 100. The rest are totalled in `other`.

Clustering runs in the MCP server over the sampled lines, so counts, `first_seen` and `last_seen` are within the sample; `window.sample_limit_reached` says when the sample may miss matching lines (it holds `sample_size` lines and the API does not report an exact `total_matching_lines` equal to that, or the search was `logs_truncated`), in which case it holds only the window's newest lines. Each `template` and `sample` is cut at 500 characters, ending in `…`, so a body that does not cluster (such as JSON without a `message` field) cannot fill the response. A search with failed chunks is a tool error; a truncated or capped search still returns patterns, flagged by `sample_limit_reached`. An empty `patterns` list means nothing matched only while `sample_limit_reached` is false; when it is true the search stopped early and may have missed matching lines.

### get_emerging_log_patterns

Takes every `get_log_patterns` parameter (the time range is the current window), plus:

- `baseline_start_time_iso` / `baseline_end_time_iso` (string, optional): Set both or neither. Default: the window of equal length immediately before the current one.
- `min_count` (integer, optional): Lines a changed pattern needs in the window it is larger in: current for `new`/`increased`, baseline for `decreased`/`disappeared`. Default: 3 (or `sample_size` if smaller), maximum: `sample_size`.
- `change_factor` (number, optional): Frequency ratio, greater than 1, that a pattern seen in both windows must reach (`increased`) or fall to the inverse of (`decreased`). Default: 2.

Both thresholds must hold, so a high-volume template whose frequency did not change by `change_factor` is never reported. Returns only changed templates (`new`, `increased`, `decreased`, `disappeared`) with `current` and `baseline` counts; `unchanged_patterns` counts templates in both windows that changed by less than `change_factor`, and `below_min_count_patterns` counts changes dropped for having fewer than `min_count` lines. Frequencies are compared per minute while both samples hold every matching line, per minute estimated from a capped sample's exact `total_matching_lines`, and otherwise as shares of the sample, where `increased`/`decreased` describe composition rather than rate (`comparison_basis`). An empty `patterns` list means no change above both thresholds, not unchanged logs: rarer changes are counted in `below_min_count_patterns`, and a window with `sample_limit_reached` compares samples, not whole windows.

### get_log_attributes

- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional)
- `region` (string, optional)
- `index` (string, optional)

### get_log_attributes_for_pipeline

- `pipeline` (array, required): Prior filter stages to scope discovery, e.g. `[{"type":"filter","query":{"$eq":["ServiceName","<service>"]}}]`.
- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional)
- `region` (string, optional)
- `index` (string, optional)

### get_drop_rules

No parameters. Lists drop rules via `GET /otel_settings/drop?region=...`.

### add_drop_rule

- `name` (string, required)
- `filters` (array, required): Each filter: `key`, `value`, `operator` (`equals`/`not_equals`), `conjunction` (`and`).
- Filter keys must use `attributes["key_name"]` or `resource.attributes["key_name"]` (required by the Last9 API).
- Creates the rule via `POST /otel_settings/drop?region=...&cluster_id=...`.

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

### get_trace_attributes_for_pipeline

- `pipeline` (array, required): Prior filter stages to scope discovery, e.g. `[{"type":"filter","query":{"$eq":["ServiceName","<service>"]}}]`.
- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional)
- `region` (string, optional)

### get_trace_attribute_values

- `tag_name` (string, required): Attribute name from `get_trace_attributes` (e.g. `resource_department` or `attributes['http.method']`).
- `pipeline` (array, optional): Prior filter stages to scope the values; omit for global values.
- `lookback_minutes` (integer, optional): Default: 15.
- `start_time_iso` / `end_time_iso` (string, optional): Historical RFC3339 bounds; take precedence over `lookback_minutes`.
- `region` (string, optional)

### get_trace_attribute_deviations

- `comparison_mode` (string, required): `latency`, `errors`, or `time`.
- `service_name` (string, required)
- `environment` (string, required): Exact `deployment.environment` value.
- `operation` (string, optional)
- `filters` (array, optional): Trace JSON filter conditions.
- `candidate_attributes` (array, optional): Maximum 8; omit for bounded discovery.
- `latency_threshold_ms` (number, optional): Required for `latency` mode; rejected for other modes.
- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 15. Maximum: 15.
- `baseline_start_time_iso` / `baseline_end_time_iso` (string, optional): Required for `time` mode; non-overlapping and equal in duration to the target window.
- `minimum_cohort_size` (integer, optional): Default: 100. Minimum: 20.
- `minimum_value_support` (integer, optional): Default: 20. Minimum: 10.
- `limit` (integer, optional): Default: 10. Maximum: 10.

Requires the companion backend capability to be enabled.

### get_trace_waterfall

- `trace_id` (string, required)
- `environment` (string, optional)
- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 4320 (72 hours).
- `selected_span_id` (string, optional): Returns attributes, events, and links for that span only.
- `max_spans` (integer, optional): Default: 500. Maximum: 1000.

Returns an `investigation-evidence/v1` envelope; the waterfall is under `data`.

### get_change_events

- `start_time_iso` / `end_time_iso` (string, optional)
- `lookback_minutes` (integer, optional): Default: 60.
- `service_name` (string, optional)
- `env` (string, optional)
- `event_name` (string, optional): Call without this first to get `available_event_names`.

### analyze_change_impact

Pass exactly one of `change_event` or `service` + `timestamp`.

- `change_event` (object, optional): One element of `get_change_events` `change_events[]`, unchanged. Uses `metric.service`, `metric.deployment_environment` and the earliest `values[].timestamp`.
- `service` (string, optional): Required with `timestamp` when `change_event` is omitted.
- `timestamp` (string, optional): RFC3339 or unix seconds, rounded down to the minute.
- `env` (string, optional): Exact environment. Default: the event's, else all.
- `baseline_minutes` (integer, optional): Default: 60. Clamped to 30..120.
- `after_minutes` (integer, optional): Default: `baseline_minutes`. Clamped to 30..120 and capped at now.

`regression likely` when any holds: error rate up at least 1 percentage point, p95 up at least 40%, or throughput (rpm) down at least 50% for the service or one of its top 10 callers, ranked by throughput over both windows together (only with at least 100 requests before the change and, but for throughput, after it; p95 counts only `latency_env`'s requests); an alert for them starts firing after the change and is still firing at the end of the after window (matched case-insensitively by alert group name or a `service`, `service_name` or `service.name` label, else annotation (never free text such as a summary); with `env`, alerts labelled with another environment, ignoring case, are dropped and unlabelled ones are marked `env_match: unlabelled`); a regression from `get_apm_service_deviations`. `insufficient data` when the after window is under 5 minutes (including a change in the future), the service has too few requests, or golden signals, the direct-caller lookup, alerts or `get_apm_service_deviations` failed, wholly or in any query listed in `deviations.apm.unavailable`: missing evidence never yields `no significant change`, and a regression found anyway names what failed. `no significant change` is a valid answer, not a failure. Trace-attribute deviations run only when `env` is set, over at most 15 minutes each side; they explain a verdict but never set one, so skipping them or failing to fetch them leaves it unchanged. Each list keeps 10 items and reports how many it dropped; a failed upstream call leaves an `error` on its section only. Golden-signal latency comes from one environment in both windows, named in `latency_env`: with no `env`, the busiest. Golden-signal error rate counts 5xx and gRPC errors; `deviations.apm` also counts error-status spans and 429s. A caller's signals are its own server spans, so client-only callers show 0 rpm.

`regressions` lists one entry per trigger behind `regression likely`, in `reason`'s order, and is `[]` for the other verdicts. Each has `service`, `role` (`changed` or `dependent`), `env`, `metric` (exactly one of `error_rate_pct`, `latency_p95_ms`, `throughput_rpm`, `alert`, `apm_deviation`) and `baseline`/`after` in the metric's unit (%, ms, rpm, or the APM signal's own value; null for `alert`). An `alert` item adds `rule_id` and `state`, the instance's state in the newest alerts-monitor slice of the after window that lists it, always `firing`: an alert that started after the change but resolved by the end of the after window is not a regression, so it files no ticket; it stays in `alerts.started` with its `state`, and `reason` counts it. `env` is the environment the metric was measured in: the requested `env`, else error rate and throughput sum every environment (`env: ""`) while p95 uses the busiest, `latency_env`; an alert uses its own env label and an APM deviation its own env. Every regression is service-wide, so none names an endpoint.

### get_alert_groups

Configured Compass alert-group inventory for changeboard / label-coverage audits. Includes groups with zero rules and groups that are not firing. Does not return PromQL.

- `alert_group_name` / `alert_group_type` / `data_source_name` (string, optional): Case-insensitive substring match.
- `team` / `tier` (string, optional): Exact case-insensitive match on configured metadata.
- `label_key` + `label_value` (string, optional): Must be set together. Exact case-insensitive match on one `metadata.labels` pair — both key and value.

Returns compact JSON `{"count":N,"groups":[...]}` with `id`, `name`, `type`, `entity_class`, `team`, `tier`, `metadata.labels`, and rule counts. Empty `team` / `labels` means unset.

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

### get_alert_rule_state

- `start_time` (integer, required): Unix epoch start of the range (inclusive).
- `end_time` (integer, required): Unix epoch end of the range (inclusive).
- `step` (integer, required): Resolution in seconds between samples. The number of samples `((end_time - start_time) / step + 1)` is capped at 100.
- `alert_group_id` (string, optional): Filter by alert group ID.
- `rule_name` (string, optional): Regex filter on rule name.
- `alert_group_name` (string, optional): Regex filter on alert group name.
- `label_filters` (string, optional): Comma-separated `key=value` label filters.
- `state` (string, optional): Filter by state (e.g. `firing`).

Returns a JSON map of `rule_id -> [{timestamp, is_firing}]`. A timestamp at which a rule is absent from the upstream response is reported as `is_firing=0` — this means "not observed as firing", not a confirmed normal state.

### get_notification_channels

- `scope` (string, optional): `attached` (default) returns one row per alert-group attachment; the `id` is an attachment id, as taken by `detach_notification_channel`. `channels` returns the org's channels (`id`, `name`, `type`, `send_resolved`, `created_via`); the `id` is a channel id, as taken by `attach_notification_channel`. Destinations, headers, and certificates are never returned.

Returns configured notification channels (Slack, PagerDuty, email, webhooks, etc.) as a TSV table. Attachment rows (`service_fqid` set) show `global` false and `services` `-`; `services` lists only legacy service bindings. On attachment rows `send_resolved` is the row's stored default, not what delivery uses; resolve notifications follow the channel's own `send_resolved` (`scope=channels`). Both scopes include `created_via` (`last9-mcp` for rows an MCP agent created, else `-`).

### recommend_alert_config

Read-only. Returns lifecycle guidance, validation gates, and responder-context requirements; it is not evidence by itself, so ground thresholds with telemetry tools.

- `signal` (string, required): Metric or user-impact signal, including unit and dimensions.
- `objective` (string, required): Symptom or user impact to detect.
- `rule_type` (string, optional): `static` (default) or `anomaly`.
- `severity` (string, optional): `breach` (default) or `threat`.
- `environment` (string, optional): Environment and traffic profile.

### validate_alert_expression

Dry-runs an expression without saving. Use before `create_alert` / `update_alert`.

- `entity_id` (string, required): Alert group/entity UUID.
- `expression` (string, required): Arithmetic over indicators named by their KPI's exact name, e.g. `errors / requests`. At most 3 indicators. Write names with spaces, dots or dashes as-is (`Test Alert / errors`), exactly as `create_alert` saves them.
- `indicators_used` (object, required, non-empty): Indicator name to KPI UUID owned by `entity_id`. Use each KPI's exact name: this dry-run accepts any name, but `create_alert` and `update_alert` reject `expression_args` keys that are not the bound KPI's name.

Only the arithmetic expression is checked (pass the inner metric expression, not an `adaptive_std_cmp(...)` wrapper); `condition` / `alert_condition` are not. The at-least-one-timeseries check runs only when more than one indicator is used.

Returns `{"valid":true}` or `{"valid":false,"reason":"..."}`; an invalid expression is a normal result, not a tool error.

### describe_alert_chart

Read-only. Lists the alertable signals (`key`, `unit`, `eval_query`, `query_kind`) on a covered Discover chart. Viewer-role tokens are rejected.

- `surface` (string, required): `discover-service` or `discover-exceptions`; any other surface, including custom dashboards, is rejected before calling the API (use `create_alert` with a KPI instead).
- `chart_key` (string, required): e.g. `apdex`, `error_rate`, `response_time`, `exception_count`.
- `service_name` (string, required)
- `env` (string, optional)
- `attributes` (object, optional): Extra identity dimensions, using the key names this tool reports.

### create_alert

Creates a rule in an existing alert group, always disabled (a draft, even if `is_disabled=false` is passed). Call `recommend_alert_config` first, then promote with `patch_alert` `is_disabled=false` only after the user confirms. Create once and keep the returned rule ID. A `rule_name` the alert group already uses (case-insensitive) is refused without creating anything, naming the existing rule's id.

- `entity_id` (string, required): Alert group/entity UUID.
- `alert_rule` (object, required): Complete typed rule body, including `primary_indicator` and `expression_args` bound to real KPIs of the entity. Each `expression_args` key must be the exact name of the KPI it binds (not a placeholder such as `x`), and `primary_indicator` and `expression` use those names. Static rules require `condition`, `alert_condition`, and `eval_window` (0–60 minutes). Adaptive rules use `expression` = `adaptive_std_cmp(<primary_indicator>, sensitivity, window)`; the three fields are then optional (defaults `expr > 0`, `count_true(result) >= 1`, `1`). `algorithm` cannot be set. Responder context goes in `properties` (description, runbook, annotations, `query_mode`).

Notification routing is configured on the alert group, not the rule.

### create_alert_from_chart

Creates a static-threshold rule from a chart described by `describe_alert_chart`. The rule is created enabled and fires as soon as its condition is met.

- `surface` / `chart_key` / `service_name` / `signal_key` / `name` (string, required)
- `env` (string, optional)
- `attributes` (object, optional)
- `threshold` (number, optional): In the signal's unit; must be 0 or more. Score-unit signals such as apdex need an explicit value.
- `threshold_operator` (string, optional): `>` (default), `<`, `>=`, `<=`, `==`, `!=`.
- `eval_window` (integer, optional): Minutes, 1–60. Default: 5.
- `bad_minutes` (integer, optional): 1–`eval_window`. Default: 3.
- `severity` (string, optional): `breach` or `threat`.
- `description` (string, optional)
- `group_id` (string, optional): Normally omit.

### update_alert

Full replacement of a rule's evaluation config. Retrieve the rule first with `get_alert_config` or `get_entity_alert_rules`. Refuses without changing anything when the rule does not exist or belongs to another alert group. May soft-delete and recreate the rule, so use the returned replacement ID afterwards.

- `entity_id` (string, required)
- `id` (string, required): Alert rule UUID.
- `alert_rule` (object, required): Complete configuration; same shape as `create_alert`.

### patch_alert

Partial change that preserves the rule ID. At least one change is required. Refuses without changing anything when the rule does not exist or belongs to another alert group.

- `entity_id` (string, required)
- `id` (string, required): Alert rule UUID.
- `is_disabled` (boolean, optional): `false` promotes/enables, `true` pauses.
- `mute_until` (integer, optional): Unix epoch deadline; `0` unmutes.
- `properties` (object, optional): `description`, `runbook`, `annotations`, `query_mode`. Each field sent replaces the stored one; omitted or blank fields keep their stored value. `annotations` replaces the whole map, so send every key to keep; `{}` clears it.
- `recurring_mute_schedule` (object, optional): Replaces the weekly schedule: `enabled` (default false), `timezone` (IANA name, required), and `windows` (1–20 of `weekdays` as `sun`..`sat`, `start_time` / `end_time` as 24-hour `HH:MM`). An end at or before the start ends the next day. Send every window to keep.
- `clear_recurring_mute_schedule` (boolean, optional): Removes the schedule entirely; cannot be combined with `recurring_mute_schedule`. Differs from `enabled=false`, which keeps the windows stored.

Viewer-role users may only change `mute_until`. Rule-level mute changes are rejected while the group or entity is muted. Read changes back with `get_entity_alert_rules`, which shows `mute_until`, `recurring_mute_schedule` and `properties`.

### delete_alert

Permanently deletes a rule. Verify with `get_alert_config` and get explicit user confirmation first. Refuses without deleting anything when the rule does not exist or belongs to another alert group.

- `entity_id` (string, required)
- `id` (string, required): Alert rule UUID.

### mute_alert_group

Mutes every current and future rule in a group; to mute one rule use `patch_alert` with `mute_until`. Requires editor access.

- `entity_id` (string, required): Alert group/entity UUID.
- `until` (integer, required): Unix epoch seconds when the mute ends. `0` unmutes; a negative value such as `-1` mutes indefinitely.

### acknowledge_alert

Takes ownership of a firing alert, or releases it. Does not resolve or mute the alert. Acknowledging an alert you already own is a safe no-op. `ack` refuses without recording anything unless `get_alerts` lists that exact alert (all three ids) within the last hour.

- `alert_group_id` / `rule_id` / `rule_type` (string, required): Copy from the alert in `get_alerts`.
- `action` (string, optional): `ack` (default) or `unack`. Only the owner or an org admin can unack.
- `mute_until` (integer, optional): `ack` only. Records a mute you already applied with `mute_alert_group` or `patch_alert`; does not mute anything.

### reassign_alert

Hands an alert that already has an owner to another user; acknowledge it first if it has none. When nothing is reassigned because the alert has no owner, it says whether `get_alerts` lists that alert in the last hour at all. Last9 does not notify the new owner.

- `alert_group_id` / `rule_id` / `rule_type` (string, required): Copy from the alert in `get_alerts`.
- `owner_email` (string, optional): Preferred; matched case-insensitively against active org users.
- `owner_user_id` (string, optional): Provide exactly one of `owner_email` or `owner_user_id`.

### create_notification_channel

Creates a channel; it routes nothing until attached with `attach_notification_channel`. Create once and keep the id; there is no update tool. Returns only `id`, `name`, `type`, and `send_resolved`.

- `name` (string, required): Unique per type; no colon.
- `type` (string, required): `slack`, `pagerduty`, `opsgenie`, `email`, or `generic_webhook`.
- `destination` (string, required): Slack Incoming Webhook URL (or channel ID with `slack_app_mode`), PagerDuty routing key, Opsgenie API key, comma-separated work email addresses on any domain, not only the org's (the API rejects personal-provider addresses such as Gmail, and an org on a personal login accepts only its admin's address), or an HTTPS webhook URL.
- `slack_app_mode` (boolean, optional): Slack only. Default: false.
- `mention_handles` (string, optional): Comma-separated, e.g. `user|U123ABC|name`, `special|here`, `special|channel`.
- `webhook_headers` (object, optional): `generic_webhook` only.
- `webhook_ca_certificate` (string, optional): `generic_webhook` only; one PEM CA certificate up to 64 KiB.
- `send_resolved` (boolean, optional): Default: true.

### attach_notification_channel

Routes an existing channel to one alert group. Idempotent. Attachments copy the channel's delivery settings (headers, certificate, mentions) at attach time; if the channel's settings change later, reattach.

- `channel_id` (integer, required): A channel id (`get_notification_channels` with `scope=channels`), not an attachment id.
- `alert_group_id` (string, required): From `get_alert_groups`.
- `severity` (string, required): `breach`, `threat`, or `both`. `both` makes two attachments; if the second fails, the first stays attached and the error names the severity to retry.

### detach_notification_channel

Removes one attachment. Requires a delete-scope token; a write-scope token cannot detach.

- `attachment_id` (integer, required): The `id` from `get_notification_channels` with `scope=attached`. Channel ids are refused.
- `alert_group_id` (string, optional): Refuses if the attachment belongs to a different group.

### delete_notification_channel

Permanently deletes a channel that has no attachments; detach its attachments first. Requires a delete-scope token. Confirms the deletion by re-listing channels.

- `channel_id` (integer, required): The `id` from `get_notification_channels` with `scope=channels`. Attachment ids are refused.

### did_you_mean

- `query` (string, required): The name to search for — partial, misspelled, or abbreviated.
- `type` (string, optional): Restrict to entity type: `service`, `environment`, `host`, `database`, `k8s_deployment`, `k8s_namespace`, `job`.

Returns up to 3 closest matches with similarity scores. Use this before any tool call where the entity name is uncertain. If a previous call returned empty results, try this before retrying.

### get_service_profile

- `service_name` (string, required): Service to derive a telemetry profile for.
- `datasource` (string, optional): Datasource name. Omit for the default.

Returns a short investigation brief followed by the full profile as raw JSON: signal presence (`logs`/`traces`/`metrics` as `present`, `absent`, or `unknown`), language and runtime, deployment environments, log `signal_shape` (`log_format`, `severity_set`, `level_field`), and a recommended ingest fix where one applies. Derived upstream and cached with a ~15 minute TTL.

Call it before any service-scoped investigation so tool selection matches the service's actual telemetry — skip trace tools when `traces` is `absent`, and when `severity_set` is `none` or `partial` parse severity from `level_field` in the log body rather than using `severity_filters`. `metrics` is always `unknown` and `dependencies` is unpopulated in v1. When `logs` and `traces` are both `absent`, confirm the name with `did_you_mean` before concluding the service is unmonitored.

### list_dashboards

No parameters. Returns all custom dashboards in the org as a JSON array with `id`, `name`, and metadata.

### get_dashboard

- `id` (string, required): Dashboard UUID.
- `region` (string, optional): Region for panel query population. Defaults to configured datasource region.

### validate_dashboard

Read-only. Never creates or updates dashboards. Accept exactly one of `dashboard_id` or `dashboard_definition`.

- `dashboard_id` (string, optional): Saved dashboard UUID to validate.
- `dashboard_definition` (object, optional): Inline unsaved dashboard body (true dry run).
- `start_time_iso` / `end_time_iso` (string, optional): Validation window (RFC3339). Must be ≤ 24h.
- `region` (string, optional): Region for panel query execution.

Returns `dashboard_validation/v1`: per-panel lint + execute classification (`data` / `no_data` / `invalid` / `error`). Day-1 empty results classify as `valid_no_data` without diagnose probes.

### create_dashboard

Net-new only. After this call returns `dashboard.id`, refine with `update_dashboard` — do not create again to add, trim, or fix panels.

- `dashboard` (object, required): Dashboard definition with `name` and `panels[]`. Each panel requires `name`, `version`, `layout` (`x`, `y`, `w`, `h`), `visualization.type`, and `queries[]`.
- `metadata` (object, optional): Dashboard metadata — `_category` and `_type` fields (e.g. `{"_category":"custom","_type":"metrics"}`).

### update_dashboard

Prefer this after create. Full replacement by id (same body as create).

- `id` (string, required): Dashboard UUID to update.
- `dashboard` (object, required): Full replacement dashboard body (same shape as create).
- `metadata` (object, optional): Replacement metadata. Readonly system dashboards return a 403 error.

### patch_dashboard

Prefer this over `update_dashboard` for small refinements: unchanged panels are never re-sent. Reads the dashboard, applies `ops` in order and writes it back, so an edit made elsewhere between the read and the write is overwritten. Returns `{dashboard_id, changes[]}`; diff values over 200 characters are truncated.

- `id` (string, required): Dashboard UUID to patch.
- `ops` (array, required): Ops applied in order; if any is invalid nothing is written and the error names it as `ops[i]`.
  - `add_panel`: `panel` (full panel object; `version` defaults to 1), optional `index` (0-based insert position, default append).
  - `update_panel`: `panel_id`, `fields` (top-level panel keys to replace; `null` removes a key; `id` cannot change).
  - `remove_panel`: `panel_id`.
  - `move_panel`: `panel_id`, `index` (0-based position after the move).
  - `set_title`: `title` (the dashboard name).
  - `set_variable`: `variable`; replaces the variable with the same `target`, otherwise appends it.

### delete_dashboard

- `id` (string, required): Dashboard UUID to delete. Readonly system dashboards cannot be deleted.

### list_dashboard_snapshots

- `dashboard_id` (string, required): Dashboard UUID whose snapshots to list.

Returns metadata only (`id`, `name`, `expires_at`, etc.). Use `get_dashboard_snapshot` for frozen panel data.

### get_dashboard_snapshot

- `id` (string, required): Snapshot UUID.

Returns the full frozen snapshot including `dashboard_definition`, `panel_data`, `time_range`, and `variables`.

### delete_dashboard_snapshot

- `id` (string, required): Snapshot UUID to delete.

### get_profile_services

- `lookback_minutes` / `start_time_iso` / `end_time_iso` (optional): Window; prefer lookback or explicit ISO bounds (default 60m).
- `region` (string, optional): Region override.

Returns services that have profiling data in the window. Call this before `get_flamegraph` / `get_top_functions` / `get_profile_summary`.

### get_flamegraph

- `service` (string, required): Service name from `get_profile_services`.
- `profile_type` (string, optional): `cpu` (default), `alloc`, or `wall`. Pin a type when comparing windows.
- `env` / `cluster` / `namespace` / `runtime` (string, optional): Scope filters.
- `limit` (number, optional): Max aggregated stack rows (default 1000, max 10000).
- `lookback_minutes` / `start_time_iso` / `end_time_iso` / `region` (optional).

Returns a nested flamegraph tree (`name` / `value` / `self` / `children`). `truncated: true` means the API row limit was hit.

### get_top_functions

Same filters as `get_flamegraph`. Returns self-sample ranking of hottest functions. May be truncated; check `truncated`.

### get_profile_summary

Same filters as `get_flamegraph`. Returns a short natural-language triage of the profile for the service.

### grafana_search_dashboards

- `query` (string, optional): Title substring. Empty lists broadly (subject to the 5,000-row cap).

Returns `{"dashboards":[…], "truncated":bool}` with `uid`, `title`, `uri`, `url`, `type`, `tags`. Use `uid` with `grafana_get_dashboard`.

### grafana_get_dashboard

- `uid` (string, required): Grafana dashboard uid.
- `full_json` (boolean, optional): When true, return raw Grafana JSON instead of the filtered summary.

Default summary: version, tags, templating variables, and each panel's type/datasource/gridPos/promQL targets. Unknown plugin panel types appear in `unsupportedPanelTypes`.

### grafana_list_folders

No parameters. Returns the folder tree.

### grafana_list_folder_dashboards

- `folder_uid` (string, required): Grafana folder uid.

Returns `{"dashboards":[…], "truncated":bool}` for dashboards in that folder (paginated up to 5,000).

### grafana_list_datasources

No parameters. Returns a safe projection of datasources (no credential fields).

</details>

---

## Testing

See [TESTING.md](TESTING.md) for integration test setup and instructions.

---

[![MseeP.ai Security Assessment Badge](https://mseep.net/pr/last9-last9-mcp-server-badge.png)](https://mseep.ai/app/last9-last9-mcp-server)
