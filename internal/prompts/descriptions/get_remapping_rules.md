List remapping rules configured in the Last9 control plane.

Remapping rules transform telemetry at ingestion before storage. Use `rule_type` to select which rule set to return:

- `logs_extract` — extract fields from log bodies using JSON or regex pattern matching
- `logs_map` — map existing log attributes to standardized service, severity, or deployment environment fields
- `traces_map` — map trace attributes to standardized service names

Returns the API response as JSON. Use this before creating rules to check for existing names and avoid duplicates.