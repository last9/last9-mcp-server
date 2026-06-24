Get raw log entries for a specific service over a time range.

This tool retrieves actual log entries for a specified service, including log messages, timestamps, severity levels, and other metadata.
It's useful for debugging issues, monitoring service behavior, and analyzing specific log patterns.

Filtering behavior:
- severity_filters: Array of severity patterns (e.g., ["error", "warn"]) - uses OR logic (matches any pattern)
- body_filters: Array of message content patterns (e.g., ["timeout", "failed"]) - uses OR logic (matches any pattern)
- Multiple filter types are combined with AND logic (service AND severity AND body)

Examples:
1. service="api" + severity_filters=["error"] + body_filters=["timeout"]
   → finds error logs containing "timeout" for the "api" service
2. service="web" + body_filters=["timeout", "failed", "error 500"]
   → finds logs containing "timeout" OR "failed" OR "error 500" for the "web" service
3. service="db" + severity_filters=["error", "critical"] + body_filters=["connection", "deadlock"]
   → finds error/critical logs containing "connection" OR "deadlock" for the "db" service

Note: This tool returns raw log entries.

Parameters:
- service: (Required) Name of the service to get logs for
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes
- limit: (Optional) Maximum number of log entries to return. Default: 20
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.
- severity_filters: (Optional) Array of severity patterns to filter logs
- body_filters: (Optional) Array of message content patterns to filter logs
- index: (Optional) Explicit log index to query. Accepted values are physical_index:<name> and rehydration_index:<block_name>. Omit it when the user did not specify an index.
- If the user says "rehydration index X", use rehydration_index:X.
- If the user says "physical index X" or just "index X", use physical_index:X.
- For log-based service inventory, use prometheus_instant_query with physical_index_service_count before querying logs broadly.
- Inventory query pattern: sum by (name, service_name, env) (physical_index_service_count{destination="logs"}). The "name" label is the physical index name.
- If the inventory result has name="default", omit the index parameter. For a non-default physical index selected by the user, pass index as physical_index:<name>.
- If the backend rejects physical index filtering, retry without index and mention that explicit physical index filtering is unavailable for that backend.
- Avoid broad multi-service body searches with this raw-log tool. Pick one service/env/index first; use get_logs for aggregate counts, then use this tool for a few samples.

Returns a list of log entries with full details including message content, timestamps, severity, and attributes.

- If unsure of the service or env name, call "did_you_mean" first to find the correct spelling.