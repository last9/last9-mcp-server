Lists configured Compass alert groups with metadata labels, team, tier, and rule counts.

This is configured inventory, not firing alerts. It includes groups with zero rules and
groups that are not currently firing. Do not infer changeboard grouping from get_alerts
group_labels — those exist only for alerts that have fired.

Does not return PromQL, expressions, or notification channels. For rule rows use
get_alert_config; for PromQL use get_entity_alert_rules with the group id; for live
firing use get_alerts.

Optional filters (all AND-combined):
- alert_group_name: Case-insensitive substring match on alert group name
- alert_group_type: Case-insensitive substring match on alert group type
- data_source_name: Case-insensitive substring match on data source name
- team: Exact case-insensitive match on configured team
- tier: Exact case-insensitive match on configured tier
- label_key + label_value: Must be set together. Exact case-insensitive match on one
  configured metadata.labels pair (for example domain=issuing)

Output is compact JSON: {"count":N,"groups":[...]}. Each group includes:
- id, name, type, entity_class
- team, tier (empty string when unset — that is the unlabeled audit signal)
- metadata.labels (object, empty when unset)
- rules_count, enabled_rules_count, disabled_rules_count

enabled_rules_count counts every rule whose state is not "disabled" (muted counts as
enabled). Groups with no rules have all three counts at 0.
