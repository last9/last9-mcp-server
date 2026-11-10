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

The response is an `investigation-evidence/v1` envelope: `request` echoes the scope and half-open windows; `evidence` carries `partial`, `truncated`, `warnings`, `provenance`, and typed `sanitization` metadata; `interpretation` carries `evidence_quality` and `claim_type`; and the waterfall itself is under `data`. Text and structured MCP results contain the same canonical JSON bytes. Dynamic waterfall strings and selected-span attributes, events, and links are sanitized with versioned redaction, string/item, input-body, and serialized-byte limits. A span list truncated to satisfy trace-evidence-sanitizer/v1 serialized-byte limit is trimmed deterministically and reported as partial with a `serialized_spans` action. This analysis is trace-scoped, so `request.scope` is the `trace_id`, plus `environment` only when that argument was passed. It reports no service because none was requested.

Values are redacted on the server under a per-org policy: credentials, query literals, URLs, emails, IPs and user identifiers read `[REDACTED]`, `[REDACTED_EMAIL]` or `[REDACTED_IP]`, and selected-span attributes may be dropped. `sanitizer` names the policy that ran; `evidence.sanitization` counts what it redacted or truncated. If something you need is redacted, ask an org admin to adjust the policy rather than working around it with other tools.

When `evidence.warnings` contains `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, a parent's `self_time_ms` may absorb the duration of children that were not returned — do not report it as time spent in that span. Sanitizer-only truncation is instead described by `evidence.sanitization` and does not imply missing spans. `evidence_quality` is `insufficient` when no spans were found. Unsupported capability, partial evidence, truncated evidence, insufficient evidence, sanitizer truncation, or a safe-serialization rejection is terminal for this evidence step: stop and report the limitation. Do not reconstruct the waterfall or selected-span evidence client-side with other MCP calls.
