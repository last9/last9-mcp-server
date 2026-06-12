
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

	Each alert rule includes:
	- id: Unique identifier for the alert rule
	- name: Human-readable name of the alert
	- primary_indicator: Name of the primary KPI (metric) being monitored
	- entity_id: Use this with get_entity_alert_rules to fetch the full PromQL for this entity's rules
	- state: Current state of the alert rule (active, inactive, etc.)
	- severity: Alert severity level
	- algorithm: Detection algorithm (static_threshold, high_spike, inc_trend, etc.)
	- created_at: When the alert rule was created
	- updated_at: When the alert rule was last modified
