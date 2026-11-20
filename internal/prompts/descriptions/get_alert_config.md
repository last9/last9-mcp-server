List alert rules and their metadata. Use it to discover rule and entity ids; use `get_entity_alert_rules` for each indicator's PromQL and full properties.

Filters: `rule_id` exact; `search_term` searches rule/group names and types, datasource, tags, team, tier and labels; `rule_name`, `alert_group_name`, `alert_group_type`, and `data_source_name` are case-insensitive substrings; `severity` and `rule_type` (`static`, `anomaly`, `adaptive`) are case-insensitive exact matches; every `tags` entry must match.

Channel filters inspect per-entity bindings only. `only_without_notification_channel` is OR-combined with `notification_channel_types`. Names and severities (`breach` or `threat`) are AND constraints on the same binding row. Global org-wide channels do not satisfy these filters.

Each rule includes id, name, primary indicator, entity id, group metadata, severity, algorithm, lifecycle state, channel summary and timestamps. Recent evaluations contain 5 one-minute samples, newest first; the newest ends 1-2 minutes before the call. States are firing; ok (normal, resolved, pending or empty state); or unknown (missing row, failed lookup or unrecognized state, e.g. no_data, silenced). Failures noted at top. Disabled rules show none; history is omitted when more than 25 enabled rules match.
Rules with No data alerting enabled include `On No Data: alert`; absent means off. It fires after 2x `eval_window` minutes without samples and resolves about 11 minutes after data returns.

## When to use
Find alert rules by name, severity, group, metadata or notification channel.

## When not to use
- What is firing now: `get_alerts`
- Longer firing history: `get_alert_rule_state`
- Group inventory including zero-rule groups: `get_alert_groups`

## Pairs well with
- `get_entity_alert_rules`
- `get_notification_channels`

## Output shape
Matching rules with ids, group, severity, state, evaluations, channels and indicators.

## Known traps
Read `last9://reference/tool_recipes` when combining channel filters or interpreting evaluation history.
