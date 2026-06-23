
Returns the trace attributes that actually exist AFTER applying the given pipeline,
each enriched with the exact filter_field string to use in a get_traces query.

Unlike get_trace_attributes (which returns the global tag catalog), this tool is
scoped to your in-progress pipeline, so it only reports attributes that are really
present for that slice of spans.

Use it before filtering on an attribute:
1. First narrow with a filter stage, e.g. {"type":"filter","query":{"$eq":["ServiceName","<service>"]}}.
2. Call this tool with that pipeline.
3. Build your get_traces filter using only the filter_field values it returns.

Do not assume any attribute's key name or syntax. Confirm the real field with this
tool instead of guessing.

Returns a JSON array. Each entry has:
  - name:          raw attribute name (e.g. "resource_department")
  - semantic_name: human-readable name with prefix stripped (e.g. "department")
  - type:          "toplevel" | "resource" | "event" | "span"
  - filter_field:  exact string to use in a tracejson $eq/$contains/etc. condition
  - hint:          ready-made example condition using filter_field

Use filter_field directly — do not transform it further.

Defaults to the last 15 minutes if no time window is provided.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
