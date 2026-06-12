
Fetches available trace attributes for a specified time window and returns each
one enriched with the exact filter_field string to use in a get_traces query.

Call this before building a tracejson filter whenever you need to filter by a
resource attribute or span attribute — never guess the filter_field syntax.

Returns a JSON array sorted by name. Each entry has:
  - name:          raw attribute name as returned by the API (e.g. "resource_department")
  - semantic_name: human-readable name with prefix stripped (e.g. "department")
  - type:          "toplevel" | "resource" | "event" | "span"
  - filter_field:  exact string to use in a tracejson $eq/$contains/etc. condition
                   (e.g. "resources['department']", "attributes['http.method']", "ServiceName")
  - hint:          ready-made example condition using filter_field

Use filter_field directly — do not transform it further.

Defaults to the last 15 minutes if no time window is provided.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
