
	Get alert configurations (alert rules) from Last9.
	Returns configured alert rules with metadata and supports both typed filters and free-text search.
	Use this tool first to discover rules and entity IDs, then if required, use get_entity_alert_rules
	with an entity_id to get the PromQL for the indicator and other details of the alert group (entity) of the alert rule.

	Optional filters:
	- rule_id: Exact match on alert rule ID
	- search_term: Case-insensitive substring search across rule name, alert group name/type, data source name, and tags
	- rule_name: Case-insensitive substring match on rule name
	- severity: Exact case-insensitive match
	- rule_type: Exact case-insensitive match on derived rule type ("static" or "anomaly")
	- alert_group_name: Case-insensitive substring match on alert group name
	- alert_group_type: Case-insensitive substring match on alert group type
	- data_source_name: Case-insensitive substring match on alert group data source name
	- tags: Array of case-insensitive substring matches; all provided tags must match
	- only_without_notification_channel: Include rules whose alert group has no per-entity channel
	  binding (dashboard "Not configured"). OR-combined with notification_channel_types when both
	  are set. Global org-wide channels do not satisfy per-entity filters; listed when this filter is used.
	- notification_channel_types: Include rules whose alert group has a per-entity channel with any
	  listed type (case-insensitive, e.g. slack, email, pagerduty, generic_webhook). OR-combined with
	  only_without_notification_channel when both are set.
	- notification_channel_names: Include rules whose alert group has a per-entity channel with any
	  listed name (case-insensitive exact match). AND-combined with other notification_channel_* filters
	  on the same binding row.
	- notification_channel_severities: Include rules whose alert group has a per-entity channel with any
	  listed severity (breach or threat). AND-combined with notification_channel_types and
	  notification_channel_names on the same binding row. OR-combined with only_without_notification_channel
	  when both are set.

	Each alert rule includes:
	- id: Unique identifier for the alert rule
	- name: Human-readable name of the alert
	- primary_indicator: Name of the primary KPI (metric) being monitored
	- entity_id: Use this with get_entity_alert_rules to fetch the full PromQL for this entity's rules
	- alert_group: Human-readable name of the entity (alert group) this rule belongs to, when resolved
	- data_source: The alert group's data source name, when set
	- tags: The alert group's tags, comma-separated, when set
	- state: Current state of the alert rule (active, inactive, etc.)
	- severity: Alert severity level
	- algorithm: Detection algorithm (static_threshold, high_spike, inc_trend, etc.)
	- created_at: When the alert rule was created
	- updated_at: When the alert rule was last modified
