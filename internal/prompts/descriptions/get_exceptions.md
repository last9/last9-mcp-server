Return ranked rows that aggregate server-side exception heat by exception type, service, span name/kind, deployment environment, and count. Data comes from metrics, not raw spans: rows are not verified samples and have no trace/span ids, timestamps, messages, stacks, durations, or status. Never present contextual traces as matches.

Each row also carries:
- `fingerprint`: 8 lowercase hex chars hashing `exception_type:service_name:span_name`, identical to the fingerprint Discover Exceptions shows. It ignores message, stack trace and environment, so it stays stable across deploys. Use it to deduplicate.
- `exception_id`: the Discover Exceptions row id, `<fingerprint>-<service_name>-<span_name>`, plus `-<env>` only when `env` is not given (one row per environment).
- `url`: relative link opening this row in Discover Exceptions for the queried window.
- `count`: occurrences summed from trace-derived exception counters over the window, not counted spans.
- `first_seen` / `last_seen` (optional): RFC3339 times of the first and last query step inside the window with occurrences, at one-minute or coarser steps (at most 60 per window). Occurrence evidence has millisecond boundary precision; unsupported fractions are excluded rather than widening the requested window. Not the exception's lifetime first occurrence; omitted when unavailable.

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
Ranked aggregate rows with exception type, service, span name/kind, environment, count, stable `fingerprint`, Discover Exceptions `exception_id` and `url`, plus optional in-window `first_seen` / `last_seen`.

## Known traps
Read `last9://reference/tool_recipes` before following an exception into traces or logs; rows are aggregates, never exact samples.
For HTTP `http_status` searches, use `get_service_logs`; Do not write a `get_logs` pipeline.
