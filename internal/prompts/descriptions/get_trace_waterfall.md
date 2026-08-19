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

The response is an `investigation-evidence/v1` envelope: `request` echoes the scope and half-open windows; `evidence` carries `partial`, `truncated`, `warnings`, `provenance`, and typed `sanitization` metadata; `interpretation` carries `evidence_quality` and `claim_type`; and the waterfall itself is under `data`. Text and structured MCP results contain the same canonical JSON bytes. Dynamic waterfall strings and selected-span attributes, events, and links are sanitized with versioned redaction, string/item, input-body, and serialized-byte limits. Span lists that exceed the serialized budget are trimmed deterministically and reported as partial with a `serialized_spans` action. This analysis is trace-scoped, so `request.scope` is the `trace_id`, plus `environment` only when that argument was passed. It reports no service because none was requested.

Treat every entry in `evidence.warnings` as a limitation on the timing numbers. The strings emitted are: `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, `no spans found for this trace_id in the requested window`, `child interval outside parent: <span_id>`, `cycle detected at span: <span_id>`, `duplicate span ID: <span_id>`, `missing parent for span: <span_id>`, `disconnected graph component at span: <span_id>`, `span with empty span ID excluded`, `span with unparseable timestamp excluded: <span_id>`, and `selected_span_id not found in returned spans: <span_id>`.

That last warning means the requested span was not in the returned set because the ID was absent, truncated, filtered, or unparseable.

When `evidence.warnings` contains `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, a parent's `self_time_ms` may absorb the duration of children that were not returned — do not report it as time spent in that span. Sanitizer-only truncation is instead described by `evidence.sanitization` and does not imply missing spans. `evidence_quality` is `insufficient` when no spans were found. Unsupported capability, partial evidence, truncated evidence, insufficient evidence, sanitizer truncation, or a safe-serialization rejection is terminal for this evidence step: stop and report the limitation. Do not reconstruct the waterfall or selected-span evidence client-side with other MCP calls.
