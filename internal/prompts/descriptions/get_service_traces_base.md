Retrieve traces by exact `trace_id` or by `service_name`. Prefer over `get_traces` when you have an exact trace ID.

Parameters:
- `trace_id` (optional): Specific trace ID. Mutually exclusive with `service_name`.
- `service_name` (optional): Service to query. Mutually exclusive with `trace_id`.
- `lookback_minutes` (optional): Minutes to look back. Default 4320 for `trace_id`, 60 for `service_name`.
- `start_time_iso` / `end_time_iso` (optional): RFC3339/ISO8601 bounds.
- `limit` (optional): Max traces returned. Default 10.
- `env` (optional): Environment filter. Use `get_service_environments` to discover values.

Rules:
- Exactly one of `trace_id` or `service_name` required.
- When `trace_id` is known, pass **only** `trace_id` — omit `service_name`.
- Prefer `lookback_minutes` for relative windows; ISO bounds override lookback.
- Legacy `YYYY-MM-DD HH:MM:SS` accepted for compatibility.
- Empty `trace_id` result: retry with explicit `start_time_iso`/`end_time_iso` or larger `lookback_minutes`.

If unsure of `service_name` or `env` spelling, call `did_you_mean` first.
