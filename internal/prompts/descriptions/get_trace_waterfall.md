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

The `investigation-evidence/v1` response has scope and half-open windows in `request`, completeness and sanitization metadata in `evidence`, quality and claim type in `interpretation`, and the waterfall in `data`. The trace service applies organization-specific policy and emits the canonical evidence; text and structured results relay those exact JSON bytes. The MCP read is capped at 524288 bytes, with guidance to lower `max_spans` or omit `selected_span_id` if exceeded. Scope contains the `trace_id` and a passed `environment`; no service is invented.

Values are redacted on the server under a per-org policy: credentials, query literals, URLs, emails, IPs and user identifiers read `[REDACTED]`, `[REDACTED_EMAIL]` or `[REDACTED_IP]`, and selected-span attributes may be dropped. `sanitizer` names the policy that ran; `evidence.sanitization` counts what it redacted or truncated. If something you need is redacted, ask an org admin to adjust the policy rather than working around it with other tools.

When `evidence.warnings` contains `result reached max_spans; child spans may be missing and self_time_ms may be overstated`, a parent's `self_time_ms` may absorb the duration of children that were not returned — do not report it as time spent in that span. `span list truncated to satisfy trace-evidence-sanitizer/v1 serialized-byte limit` also means spans are missing; `evidence.sanitization` records the omitted count as `{"kind":"serialized_spans","count":N}`. Report that N spans were omitted and stop. `evidence_quality` is `insufficient` when no spans were found. Unsupported capability, partial evidence, truncated evidence, insufficient evidence, sanitizer truncation, or a safe-serialization rejection is terminal for this evidence step: stop and report the limitation. Do not reconstruct the waterfall or selected-span evidence client-side with other MCP calls.
