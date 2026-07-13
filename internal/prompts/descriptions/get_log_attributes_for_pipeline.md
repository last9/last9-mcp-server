
Returns the log fields that actually exist AFTER applying the given pipeline,
each enriched with the exact filter_field string to use in a get_logs condition.

Unlike get_log_attributes (which returns the global label catalog), this tool is
scoped to your in-progress pipeline, so it only reports fields that are really
present for that slice of logs.

Use it before filtering on a structured attribute (HTTP status, etc.):
1. First narrow with a filter stage, e.g. {"type":"filter","query":{"$eq":["ServiceName","<service>"]}}.
2. Call this tool with that pipeline.
3. Build your get_logs filter using only the filter_field values it returns.

Do not assume any field's key name. Which fields exist and how they are keyed
depend on the scope you have filtered to; the global get_log_attributes catalog
can list keys that are empty for a given scope. Confirm the real field with this
tool instead of guessing. (Example: HTTP status is keyed 'status_code' on some
sources and 'http.status_code' on others — neither is a safe default.)

Returns a JSON array sorted by name. Each entry has:
  - name:         raw field name as present in the logs (e.g. "status_code")
  - filter_field: exact string to use in a get_logs $eq/$gte/etc. condition
                  (e.g. "attributes['status_code']", "ServiceName", "resources['k8s.namespace.name']")
  - hint:         ready-made example condition using filter_field
  - source:       "body" when the field exists only INSIDE the log Body
                  (absent for indexed attributes). Body encoding may be JSON,
                  logfmt, or a plaintext-inline pattern — the hint's parse stage
                  names the correct parser (json / logfmt / regexp). A
                  body-derived field is usable ONLY after that parse stage —
                  copy it into your pipeline before any filter or groupby that
                  references the field; without it the filter/groupby silently
                  sees empty values. Prefer an indexed severity/SeverityText/
                  level field when present; do not add a body-derived level
                  parse when discovery already returned an indexed severity.
  - sample_coverage: for body-derived fields, in how many sampled rows the key
                  appeared (e.g. "5/5"). Prefer full-coverage keys; a sparse key
                  (e.g. "1/5") is absent on most rows of this scope.

Use filter_field directly — do not transform it further.

Defaults to the last 15 minutes if no time window is provided.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.

Index rules:
- Pass index only when the user explicitly names a log index.
- Accepted values are physical_index:<name> and rehydration_index:<block_name>.
- Omit index when the user did not specify one.
- For log-based service inventory, physical_index_service_count exposes the physical index name in the metric label named "name"; use sum by (name, service_name, env) (physical_index_service_count{destination="logs"}).
- If the inventory result has name="default", omit index. For a non-default physical index selected by the user, use physical_index:<name>.
- If the backend rejects physical index filtering, retry without index and mention that explicit physical index filtering is unavailable for that backend.
