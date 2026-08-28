Use this tool to fetch raw log entries for a single service using structured filters. Do not write a `logjson_query`.

## When to use this tool vs `get_logs`

**Use `get_service_logs` when:** the question is about one known service and you can express it with `severity_filters`, `http_status_class` / `http_status_code`, `attribute_filters`, or `body_filters`.

**Use `get_logs` when:** you need parse/aggregate/`window_aggregate`, or an ad-hoc pipeline the structured args cannot express.

**Do not** send HTTP status or named-attribute questions to `get_logs` by default. This tool compiles those filters server-side.

## HTTP status

Pass `http_status_class` (`2xx`/`3xx`/`4xx`/`5xx`) or `http_status_code` (`500`, `401`). The server discovers the status field for this service/env/window via the same path as `get_log_attributes_for_pipeline`.

If discovery finds no status-like field, or more than one, the tool errors and asks you to pass `http_status_field` with the exact logjson field (e.g. `attributes['http.status_code']`). Do not guess a field name.

`http_status_code` takes precedence over `http_status_class` when both are set.

**Severity is not an HTTP-error proxy** — access logs commonly carry INFO severity even for 5xx responses, and severity can be empty. `severity_filters: ["error"]` returns 0 for such services. Use `http_status_class` / `http_status_code` instead.

## Named attributes

`attribute_filters` is an array of `{field, value}` equality matches compiled into logjson `$eq`. `field` must use logjson syntax: `attributes['user_id']`, `resources['k8s.namespace.name']`, or a simple name that this tool wraps as `attributes['name']`.

Invalid syntax (double quotes, `resource_` prefixes) is a local error. An unknown org field is not rejected — it is forwarded and may match nothing.

Do not invent org-specific attribute names. Discover fields with `get_log_attributes` / `get_log_attributes_for_pipeline` if you are unsure of the key. Entries marked `source: "body"` live inside the log `Body` and need a parse stage; prefer `get_logs` with the hint pipeline for those.

`body_filters` is a last resort for plaintext that is not stored as a structured attribute.

## Parameters

- `service_name` (required): Service name to query.
- `start_time_iso` / `end_time_iso` (optional): Absolute time range in RFC3339 / ISO8601 format. Use these when the user gives explicit timestamps or dates.
- `lookback_minutes` (optional): Relative time range only when the user did not give explicit timestamps.
- `limit` (optional): Maximum number of log entries to return.
- `severity_filters` (optional): Array of severity strings such as `["error", "fatal", "critical"]`.
- `http_status_class` (optional): `2xx`, `3xx`, `4xx`, or `5xx`.
- `http_status_code` (optional): Exact 3-digit HTTP status such as `500` or `401`.
- `http_status_field` (optional): Explicit logjson field when discovery is ambiguous or empty.
- `attribute_filters` (optional): Array of `{field, value}` equality filters.
- `body_filters` (optional): Array of substrings that should appear in the log body. Last resort only.
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
- **NEVER use `body_filters` for HTTP status codes or values stored as structured attributes.** Use `http_status_*` or `attribute_filters`.

## Examples

### ❌ WRONG — HTTP 401 via body_filters (misses structured attributes)

```json
{
  "service_name": "auth-sanic",
  "env": "production",
  "lookback_minutes": 60,
  "body_filters": ["401", "unauthorized", "authentication failed"]
}
```

### ✅ CORRECT — HTTP 401 via this tool

```json
{
  "service_name": "auth-sanic",
  "env": "production",
  "lookback_minutes": 60,
  "http_status_code": "401"
}
```

### ✅ CORRECT — HTTP 5xx class

```json
{
  "service_name": "checkout",
  "env": "production",
  "lookback_minutes": 15,
  "http_status_class": "5xx"
}
```

### ✅ CORRECT — Named attribute equality

```json
{
  "service_name": "checkout",
  "lookback_minutes": 15,
  "attribute_filters": [{"field": "attributes['user_id']", "value": "abc"}]
}
```

### ✅ CORRECT — Simple severity filter

```json
{
  "service_name": "my-service",
  "start_time_iso": "2026-03-31T07:16:38.000Z",
  "end_time_iso": "2026-04-01T07:16:38.907Z",
  "limit": 100,
  "severity_filters": ["error", "fatal", "critical"]
}
```

### ✅ CORRECT — Plain keyword search

```json
{
  "service_name": "db-proxy",
  "lookback_minutes": 10,
  "body_filters": ["connection reset by peer"]
}
```
