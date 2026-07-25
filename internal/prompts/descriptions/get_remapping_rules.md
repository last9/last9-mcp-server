List remapping rules configured in the Last9 control plane.

Remapping transforms telemetry at ingestion before storage. Use `rule_type` to select which rule set to return:

- `logs_extract` — extract fields from log bodies (JSON or regex pattern)
- `logs_map` — map log attributes to standardized `service`, `severity`, or `resource_deployment.environment`
- `traces_map` — map trace attributes to standardized `service`

Returns the API response as JSON.

Call this before `add_remapping_rule` to check existing rule names and avoid duplicates. For map rules, note there is typically one rule per `target_attribute` (the UI shows them grouped, but the API stores them separately).
