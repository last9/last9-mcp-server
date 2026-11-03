# Get Trace Waterfall

Retrieves one exact trace and returns a bounded parent-child waterfall with millisecond timing, interval-correct self-time, graph warnings, slowest spans, largest self-time contributors, and optional selected-span details.

Parameters:
- `trace_id` (required): exact 32-character hexadecimal OpenTelemetry trace ID.
- `environment`: optional exact deployment environment.
- `start_time_iso` / `end_time_iso`: optional RFC3339 bounds.
- `lookback_minutes`: default 4320 for exact lookup.
- `selected_span_id`: include attributes, events, and links for this span only.
- `max_spans`: default 500, maximum 1000; your org's policy may cap it lower.

Self-time subtracts the union of clipped direct-child intervals. The tool does not compute a critical path and does not prove root cause.

The response is an `investigation-evidence/v1` envelope: `request` echoes the scope and half-open windows, `evidence` carries `partial`, `truncated`, `warnings`, and `provenance`, `interpretation` carries `evidence_quality` and `claim_type`, and the waterfall itself is under `data`. `request.scope` is the `trace_id`, plus `environment` only when passed; read the services involved from `data.spans`.

Values are redacted on the server under a per-org policy: credentials, query literals, URLs, emails, IPs and user identifiers read `[REDACTED]`, `[REDACTED_EMAIL]` or `[REDACTED_IP]`, and selected-span attributes may be dropped. `sanitizer` names the policy that ran; `evidence.sanitization` counts what it redacted or truncated. If something you need is redacted, ask an org admin to adjust the policy rather than working around it with other tools.

Treat every entry in `evidence.warnings` as a limitation on the timing numbers. Notable ones:
- `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, or `span list truncated to satisfy trace-evidence-sanitizer/v1 serialized-byte limit`: spans are missing (`evidence.truncated`), so a parent's `self_time_ms` may absorb unreturned children; do not report it as time spent in that span.
- `selected_span_id not found in returned spans: <span_id>`: a wrong ID, or a span removed by truncation or the environment filter. Do not retry the same ID; read `data.spans` for the returned IDs.
- `no spans found for this trace_id in the requested window`: `evidence_quality` is `insufficient`. An empty waterfall is not evidence the trace does not exist; widen the window or verify the trace ID.
