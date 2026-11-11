# Investigation contracts

These artifacts are the repository-neutral compatibility fixtures for ENG-1488.

- `investigation-evidence-v1.schema.json` defines the shared evidence envelope.
- `investigation-workflow-v1.schema.json` defines supervisor state and events.
- `feature-flags-v1.json` records rollout gates that exist and names the surface enforcing each. Nothing here reads it; it does not gate registration.
- `fixtures/` contains deterministic examples consumed by compatibility tests.
  - `evidence-trace-waterfall.json` is a verbatim copy of the trace service's golden waterfall response, sanitizer block included. `get_trace_waterfall` relays that response unchanged, and its tests use this file as the upstream body. Refresh it when the service's output changes, then refresh the `content_sha256` values in `workflow-cases-v1.json`.
  - `evidence-attribute-deviations-endpoint.json` is a verbatim copy of the deviations endpoint's example response. It is the shared artifact that keeps `get_trace_attribute_deviations`'s description honest; refresh it when the endpoint's response changes.
  - `evidence-trace-deviations.json` is the workflow copy of the exact U1 endpoint golden response in `evidence-attribute-deviations-endpoint.json`; contract tests require byte identity before refreshing its workflow hash.

Compatibility rules:

1. Producers emit exact major versions `investigation-evidence/v1` and `investigation-workflow/v1`.
2. Consumers reject unknown major versions and ignore unknown optional fields within v1.
3. Times use RFC3339, windows are half-open `[start,end)`, and public durations use milliseconds.
4. `request.scope` takes one of two forms: `service_name` + `environment` for cohort analyses, or `trace_id` for trace-scoped analyses. A producer emits the form it was actually asked for and never invents the fields of the other.
5. Evidence is immutable. Workflow references use an evidence ID and SHA-256 content hash.
6. Events are append-only and strictly increase `sequence` within an investigation.
7. Partial, truncated, warning, limitation, and provenance metadata may not be discarded by adapters.
8. `get_trace_attribute_deviations` preserves the validated endpoint bytes exactly in text content and exposes the same bytes as `json.RawMessage` structured content. Unknown additive v1 fields survive unchanged; unsupported versions and responses above five deviations fail closed.
9. `get_trace_waterfall` accepts only service-produced evidence attested by `trace-evidence-sanitizer/v1`, then relays those exact bytes as text and `json.RawMessage` structured content. The service's organization-specific redaction and truncation policy is authoritative. The MCP read is capped at 524288 bytes; an oversized response fails closed with guidance to lower `max_spans` or omit `selected_span_id`.

These schemas are additive foundations, not a production endpoint or supervisor implementation.
