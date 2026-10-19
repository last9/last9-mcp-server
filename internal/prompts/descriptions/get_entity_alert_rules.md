Fetches the expression logic and resolved PromQL for all alert rules on a specific entity.

Companion to get_alert_config: call get_alert_config first to discover rules and find the
entity_id, then call this tool with that entity_id to get the full alert configuration
including the actual PromQL queries behind each indicator.

Input:
- entity_id (required): UUID of the entity / alert group (from the Entity ID field in get_alert_config output)
- severity (optional): filter rules by severity (e.g. "breach", "threat")

Output per rule (expression-focused; basic metadata such as state/severity/timestamps is in get_alert_config):
- id, rule_name
- created_via, printed as "Created Via:": last9-mcp (with the creating client) for rules an MCP agent created, else "-"
- expression, condition, alert_condition, eval_window
- indicators: each indicator name with resolved PromQL and unit
- mute_until, recurring_mute_schedule and properties (description, runbook, annotations), when set

## Pairs well with
- `patch_alert` (read its changes back here)