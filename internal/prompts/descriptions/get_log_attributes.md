
Fetches available log attributes (labels) for a specified time window.
This tool queries the Last9 logs API to retrieve all available attribute names
that can be used for filtering and querying logs within the specified time range.

The attributes returned are field names that exist in the logs during the specified
time window, which can then be used in log queries and filters.

Defaults to the last 15 minutes if no time window is provided.

Returns a list of attribute names like "service", "severity", "body", "level", etc.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- start_time_iso/end_time_iso accept RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.

Index rules:
- Pass index only when the user explicitly names a log index.
- Accepted values are physical_index:<name> and rehydration_index:<block_name>.
- If the user says "rehydration index X", use rehydration_index:X.
- If the user says "physical index X" or just "index X", use physical_index:X.
- Omit index when the user did not specify one.
