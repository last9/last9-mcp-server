
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
