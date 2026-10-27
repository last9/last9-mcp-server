Fetch log lines for one service (`service_name`, optional `severity_filters`, `body_filters`, `http_status_class`/`http_status_code`, `attribute_filters`, `env`, `index`, `limit`).

**Profile first:** Call `get_service_profile`. Route using `signal_shape` and `telemetry`; see `last9://reference/investigation`. If results contradict the profile, use discovery tools (profile TTL: 15min).

**Use this tool when:** filtering one known service by severity, HTTP status, named attributes, or plain-text `body_filters`—no pipeline needed.

**HTTP status:** Pass `http_status_class` (`5xx`) or `http_status_code` (`500`). This tool discovers the status field for the service/env/window. If discovery finds none or more than one, pass `http_status_field` (e.g. `attributes['http.status_code']`). Severity is not an HTTP-error proxy (5xx often INFO)—do not use `severity_filters` for status.

**Named attributes:** `attribute_filters` is `[{field, value}]` equality. For comparisons (`>`, `<`, ranges), grouping, or aggregation, use `get_logs` instead. `field` uses logjson syntax (`attributes['user_id']`). Invalid syntax is rejected; unknown org fields are allowed. Discover names with `get_log_attributes` / `get_log_attributes_for_pipeline` if unsure.

**gRPC status:** use `attributes['rpc.grpc.status_code']` (e.g. `14` for UNAVAILABLE).

**Prefer `get_logs` when:** you need parse/aggregate/`window_aggregate`, multiple HTTP statuses in one `$or`, numeric/range comparisons, or an ad-hoc pipeline.

**Lines:** `raw`=read/quote; `summary`=counts/patterns (default,>20 lines).

**Time/index:** Prefer `lookback_minutes` for relative windows; `start_time_iso`+`end_time_iso` (RFC3339) for absolute. Use a user-named `index` (`physical_index:<name>` / `rehydration_index:<block>`) or one from `get_service_profile`. Match env via `log_index_envs`; query matching indexes separately. Omit `index` for `default` or unknown.

Full service-logs reference: `last9://reference/service_logs`. Logjson: `last9://reference/logjson`

## When to use
Lines for one known service, filtered by severity, HTTP status, named attributes or body text.

## When not to use
- Aggregates, percentiles, several statuses at once, or numeric comparisons: `get_logs`
- Reading root-cause lines after `get_exceptions`: `get_logs` with a small limit

## Pairs well with
- `get_service_profile` (index and env)
- `get_log_attributes` (field names)
- `did_you_mean` (service spelling)

## Output shape
Line summary, or `raw` entries (timestamp, message, severity) up to `limit`, with truncation and total-matching counts.

## Known traps
A service whose logs live only in a named index returns nothing until `index` is set.
