Use this tool to fetch raw log entries for a single service using simple filters.

## When to use `get_logs` instead

**CRITICAL:** If the query involves a structured attribute — HTTP status codes (401, 500, etc.), gRPC status codes, `user_id`, latency, or any field discoverable via `get_log_attributes` — **use `get_logs`** with a `logjson_query` attribute filter instead of this tool.

**Why:** `body_filters` performs plain-text substring search on the log message body only. It will miss all logs where the value is stored as a structured attribute (e.g. `attributes['http_status_code']`). Structured attribute queries are also **faster** because they are indexed.

**Decision rule:**
- Query involves a known or discoverable structured attribute → **use `get_logs`**
- Query is simple severity filtering or keyword/phrase match against log text → use `get_service_logs`

## Check log attributes first

Before using `body_filters`, call `get_log_attributes` to discover what structured attributes are available for the service. If the value you are filtering on is stored as a structured attribute, **prefer an attribute filter via `get_logs`** over a body text search.

**Do not assume an attribute's key name** — which fields exist and how they're keyed depend on the scope you've filtered to (for example, HTTP status is keyed `status_code` on some sources and `http.status_code` on others; neither is a safe default). To get the field that is actually present for your scope, build a pipeline scoped to this service (e.g. `{"$eq":["ServiceName","<service>"]}`, plus any other filters such as namespace or environment), call `get_log_attributes_for_pipeline`, then use the `filter_field` it returns. Entries marked `source: "body"` live inside the log `Body` as JSON and require the parse stage from their `hint` — that path needs `get_logs`, not this tool.

**Severity is not an HTTP-error proxy** — access logs commonly carry INFO severity even for 5xx responses, and severity can be empty. `severity_filters: ["error"]` returns 0 for such services. For HTTP error questions, use `get_logs` with the discovered status field instead.

Structured attribute queries are:
- **Faster**: indexed, not a full-text scan
- **More precise**: exact value match, not partial text
- **More reliable**: work even when the value is not embedded in the log message body

`body_filters` is a **last resort** — use it only when no structured attribute captures the information you need.

## Available structured attributes for this environment

{{labels}}

## Parameters

- `service` (required): Service name to query.
- `start_time_iso` / `end_time_iso` (optional): Absolute time range in RFC3339 / ISO8601 format. Use these when the user gives explicit timestamps or dates.
- `lookback_minutes` (optional): Relative time range only when the user did not give explicit timestamps.
- `limit` (optional): Maximum number of log entries to return.
- `severity_filters` (optional): Array of severity strings such as `["error", "fatal", "critical"]`.
- `body_filters` (optional): Array of substrings that should appear in the log body. **Last resort only — prefer `get_logs` with attribute filters for structured values.**
- `env` (optional): Deployment environment string.
- `index` (optional): Explicit log index in the form `physical_index:<name>` or `rehydration_index:<block_name>`.

## Log service inventory and index selection

- When the user has not named an exact service, do not use this raw-log tool for broad discovery.
- Use `prometheus_instant_query` first with `sum by (name, service_name, env) (physical_index_service_count{destination="logs"})`.
- Use `service_name` as the service argument, `env` as the environment when present, and `name` as the physical index name.
- If `name="default"`, omit the `index` parameter. For a non-default physical index selected by the user, use `index: "physical_index:<name>"`.
- If the backend rejects explicit physical index filtering, retry without `index` and tell the user that explicit physical index filtering is unavailable for that backend.
- Prefer `get_logs` for aggregate counts. Use this tool after the service/env/index and pattern are already narrowed, and request a small `limit` for samples.

## Rules

- Output a JSON object of tool arguments, not a query pipeline.
- Prefer `start_time_iso` and `end_time_iso` over `lookback_minutes` when the user provides absolute times.
- Keep `severity_filters` and `body_filters` as arrays of strings.
- Do not invent `index` or `env` unless the user explicitly asked for them or supplied that context.
- **NEVER use `body_filters` for values stored as structured attributes.** Call `get_log_attributes` to check first, then use `get_logs` if an attribute exists.

## Examples

### ❌ WRONG — HTTP 401 via body_filters (misses structured attributes)

```json
{
  "service": "auth-sanic",
  "env": "production",
  "lookback_minutes": 60,
  "body_filters": ["401", "unauthorized", "authentication failed"]
}
```

This misses all logs where `401` is stored as `attributes['http_status_code']` and the body doesn't contain the literal string.

### ✅ CORRECT — HTTP 401 via `get_logs` attribute filter

Use `get_logs` instead:

```json
{
  "logjson_query": [{"type": "filter", "query": {"$eq": ["attributes['http_status_code']", "401"]}}],
  "service": "auth-sanic",
  "lookback_minutes": 5
}
```

### ✅ CORRECT — Simple severity filter (valid use of this tool)

```json
{
  "service": "my-service",
  "start_time_iso": "2026-03-31T07:16:38.000Z",
  "end_time_iso": "2026-04-01T07:16:38.907Z",
  "limit": 100,
  "severity_filters": ["error", "fatal", "critical"]
}
```

### ✅ CORRECT — Plain keyword search (valid use of this tool)

```json
{
  "service": "db-proxy",
  "lookback_minutes": 10,
  "body_filters": ["connection reset by peer"]
}
```
