Retrieve traces by exact `trace_id` or by `service_name`. Prefer over `get_traces` when you have an exact trace ID.

**Profile first (service-scoped):** When querying by `service_name` (not `trace_id` alone), call `get_service_profile` before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

Parameters:
- `trace_id` (optional): Specific 32-character hexadecimal OpenTelemetry trace ID. Mutually exclusive with `service_name`.
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

## When to use
One exact trace by `trace_id`, or recent traces for one service.

## When not to use
- Pipeline filters or aggregates over spans: `get_traces`
- A span tree with timing for one trace: `get_trace_waterfall`

## Pairs well with
- `get_exceptions` (find the hot service)
- `get_service_environments` (env values)
- `did_you_mean` (service spelling)

## Output shape
Spans with trace_id, span_id, span_name, span_kind, service_name, duration, timestamp and status_code.

## Known traps
Passing `service_name` alongside a known `trace_id` narrows the lookup; pass the ID alone.
