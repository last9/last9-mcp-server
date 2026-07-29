# Get Trace Waterfall

Retrieves one exact trace and returns a bounded parent-child waterfall with millisecond timing, interval-correct self-time, graph warnings, slowest spans, largest self-time contributors, and optional selected-span details.

Parameters:
- `trace_id` (required): exact trace ID.
- `environment`: optional exact deployment environment.
- `start_time_iso` / `end_time_iso`: optional RFC3339 bounds.
- `lookback_minutes`: default 4320 for exact lookup.
- `selected_span_id`: include attributes, events, and links for this span only.
- `max_spans`: default 500, maximum 1000.

Self-time subtracts the union of clipped direct-child intervals. The tool does not compute a critical path and does not prove root cause. Treat truncation, cycles, duplicates, missing parents, and clock-skew warnings as evidence limitations.
