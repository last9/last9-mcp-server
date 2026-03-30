# get_alert_config Tool Arguments

## Purpose
Select the correct JSON arguments to call the `get_alert_config` tool, which returns alert rule configurations from Last9.

## Parameters (all optional)

- `rule_id` (string): **Exact** match on alert rule ID (UUID). Use when the user provides a specific rule ID.
- `rule_name` (string): Case-insensitive substring match on rule name.
- `severity` (string): Exact case-insensitive match. Values: `breach`, `threat`, `warning`, etc.
- `rule_type` (string): Derived rule type. Values: `static` or `anomaly`.
- `alert_group_name` (string): Case-insensitive substring match on alert group name.
- `alert_group_type` (string): Case-insensitive substring match on alert group type.
- `data_source_name` (string): Case-insensitive substring match on data source name.
- `tags` (string[]): Array of tag substring filters (AND semantics).
- `search_term` (string): Broad search across rule name, alert group name/type, data source, and tags.

## Examples

User: "get alert config for rule ff000725-eb50-4642-b448-5cde395905df"
→ `{"rule_id": "ff000725-eb50-4642-b448-5cde395905df"}`

User: "show all breach severity alert rules"
→ `{"severity": "breach"}`

User: "find static alert rules"
→ `{"rule_type": "static"}`

User: "show alert rules for the payments alert group"
→ `{"alert_group_name": "payments"}`

User: "search for latency alerts"
→ `{"search_term": "latency"}`

User: "find anomaly alerts tagged with prod"
→ `{"rule_type": "anomaly", "tags": ["prod"]}`
