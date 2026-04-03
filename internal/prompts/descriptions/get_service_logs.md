Use this tool to fetch raw log entries for a single service.

Parameters:
- `service` (required): Service name to query.
- `start_time_iso` / `end_time_iso` (optional): Absolute time range in RFC3339 / ISO8601 format. Use these when the user gives explicit timestamps or dates.
- `lookback_minutes` (optional): Relative time range only when the user did not give explicit timestamps.
- `limit` (optional): Maximum number of log entries to return.
- `severity_filters` (optional): Array of severity strings such as `["error", "fatal", "critical"]`.
- `body_filters` (optional): Array of substrings that should appear in the log body.
- `env` (optional): Deployment environment string.
- `index` (optional): Explicit log index in the form `physical_index:<name>` or `rehydration_index:<block_name>`.

Rules:
- Output a JSON object of tool arguments, not a query pipeline.
- Prefer `start_time_iso` and `end_time_iso` over `lookback_minutes` when the user provides absolute times.
- Keep `severity_filters` and `body_filters` as arrays of strings.
- Do not invent `index` or `env` unless the user explicitly asked for them or supplied that context.

Examples:

User: "Get the last 100 error, fatal, or critical logs for service l9alert-pinelabs from 2026-03-31T07:16:38.000Z to 2026-04-01T07:16:38.907Z."

Output:
{
  "service": "l9alert-pinelabs",
  "start_time_iso": "2026-03-31T07:16:38.000Z",
  "end_time_iso": "2026-04-01T07:16:38.907Z",
  "limit": 100,
  "severity_filters": ["error", "fatal", "critical"]
}
