Service performance metrics over a time range.

Returns: service name, env, throughput (rpm), error rate (rpm, 4xx/5xx), error percentage, response times in seconds (p50, p90, p95, avg, max), apdex score, availability (%), top 10 operations by response time and error rate, top 10 errors/exceptions by count.

Use `get_service_operation_details` for per-operation drill-down. Use for performance summaries, bottlenecks, and error overview.

Many metric fields use PromQL timeseries format: `[{"metric":{...},"values":[[timestamp_seconds,"value"]]}]`. Top operations/errors are dicts/lists of name→value.

Parameters:
- `service_name`: (Required) Service to query.
- `env`: (Required) Environment. Use `get_service_environments` to list values.
- `lookback_minutes`: (Optional) Minutes to look back. Default 60.
- `start_time_iso` / `end_time_iso`: (Optional) RFC3339/ISO8601 bounds (e.g. `2026-02-09T15:04:05Z`). Override lookback when set; end defaults to now.

If unsure of `service_name` or `env` spelling, call `did_you_mean` first.
