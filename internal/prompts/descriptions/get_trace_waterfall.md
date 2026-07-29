# Get Trace Waterfall

Bounded parent-child waterfall for one trace: millisecond timing, self-time, graph warnings, slowest spans, top self-time contributors, optional selected-span details.

- `trace_id` (required): exact trace ID.
- `environment`: optional deployment environment.
- `start_time_iso` / `end_time_iso`: optional RFC3339 bounds.
- `lookback_minutes`: default 4320.
- `selected_span_id`: attributes/events/links for this span only.
- `max_spans`: default 500, max 1000.

Not a critical path. Treat truncation, cycles, duplicates, missing parents, and clock-skew warnings as evidence limits.
