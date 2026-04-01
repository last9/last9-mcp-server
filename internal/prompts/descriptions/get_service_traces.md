Use this tool to retrieve traces from Last9 either by exact `trace_id` or by `service_name`.


Parameters:
- `trace_id` (optional): Specific trace ID to retrieve. Cannot be used with `service_name`.
- `service_name` (optional): Name of the service to query. Cannot be used with `trace_id`.
- `lookback_minutes` (optional): Minutes to look back from now. Default is 4320 for `trace_id` lookups and 60 for `service_name` lookups.
- `start_time_iso` (optional): Start time in RFC3339 / ISO8601 format, for example `2026-02-09T15:04:05Z`.
- `end_time_iso` (optional): End time in RFC3339 / ISO8601 format, for example `2026-02-09T16:04:05Z`.
- `limit` (optional): Maximum number of traces to return. Default is 10.
- `env` (optional): Environment filter. Use `get_service_environments` if you need to discover valid environment values first.

Rules:
- Exactly one of `trace_id` or `service_name` must be provided.
- Prefer `lookback_minutes` for relative windows.
- Use `start_time_iso` and `end_time_iso` for absolute windows.
- The legacy timestamp format `YYYY-MM-DD HH:MM:SS` is accepted only for compatibility.
- If both `lookback_minutes` and absolute time bounds are provided, absolute time bounds take precedence.

Examples:
1. `{"trace_id":"abc123def456"}`
2. `{"service_name":"payment-service","lookback_minutes":30}`
3. `{"service_name":"checkout","env":"prod","lookback_minutes":60}`

If an exact `trace_id` lookup returns no data, ask for a narrower or explicit time window and retry with `start_time_iso` / `end_time_iso` or a larger explicit `lookback_minutes`.
