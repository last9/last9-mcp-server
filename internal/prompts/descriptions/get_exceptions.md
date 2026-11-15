Return ranked server-side exception aggregates by exception type, service, span name/kind, deployment environment, and count. Data comes from metrics, not raw spans: it has no trace/span ids, timestamps, messages, stacks, durations, status, or exact samples. Never present contextual traces as matches.

For a known `service_name`, call `get_service_profile` first to decide whether span exceptions are likely the answer or a symptom. Use `did_you_mean` if service or environment spelling is uncertain.

`limit` defaults to 20. `lookback_minutes` defaults to 60 and is preferred for relative windows. For an absolute window set RFC3339 `start_time_iso` and `end_time_iso`; legacy `YYYY-MM-DD HH:MM:SS` is compatibility-only. Optional filters: `service_name`, `span_name`, and `env`.

## When to use
Find the hottest service, span, and exception type in a window.

## When not to use
- Exception text, stack traces, or root cause: `get_logs`
- HTTP status ranking: `get_service_summary`
- Exact or contextual traces: `get_service_traces`

## Pairs well with
- `get_service_profile`
- `get_service_traces`
- `get_logs`

## Output shape
Ranked aggregate rows with exception type, service, span name/kind, environment and count.

## Known traps
Read `last9://reference/tool_recipes` before following an exception into traces or logs; rows are aggregates, never exact samples.
For HTTP `http_status` searches, use `get_service_logs`; Do not write a `get_logs` pipeline.
