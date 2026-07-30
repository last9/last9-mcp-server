# Get Trace Waterfall

Retrieves one exact trace and returns a bounded parent-child waterfall with millisecond timing, interval-correct self-time, graph warnings, slowest spans, largest self-time contributors, and optional selected-span details.

Parameters:
- `trace_id` (required): exact trace ID.
- `environment`: optional exact deployment environment.
- `start_time_iso` / `end_time_iso`: optional RFC3339 bounds.
- `lookback_minutes`: default 4320 for exact lookup.
- `selected_span_id`: include attributes, events, and links for this span only.
- `max_spans`: default 500, maximum 1000.

Self-time subtracts the union of clipped direct-child intervals. The tool does not compute a critical path and does not prove root cause.

The response is an `investigation-evidence/v1` envelope: `request` echoes the scope and half-open windows, `evidence` carries `partial`, `truncated`, `warnings`, and `provenance`, `interpretation` carries `evidence_quality` and `claim_type`, and the waterfall itself is under `data`. This analysis is trace-scoped, so `request.scope` is the `trace_id`, plus `environment` only when that argument was passed. It reports no service, because none was requested — read the services involved from `data.spans` and `data.summary.service_count`.

Treat every entry in `evidence.warnings` as a limitation on the timing numbers. The strings emitted are: `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, `no spans found for this trace_id in the requested window`, `child interval outside parent: <span_id>`, `cycle detected at span: <span_id>`, `duplicate span ID: <span_id>`, `missing parent for span: <span_id>`, `disconnected graph component at span: <span_id>`, `span with empty span ID excluded`, and `span with unparseable timestamp excluded: <span_id>`.

When `evidence.truncated` is true, a parent's `self_time_ms` may absorb the duration of children that were not returned — do not report it as time spent in that span. `evidence_quality` is `insufficient` when no spans were found; an empty waterfall is not evidence that the trace does not exist, so widen the window or verify the trace ID before concluding anything.
