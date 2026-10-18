
	Get currently active alerts from Last9 monitoring system.
	Returns all alerts that are currently firing or have fired recently within the specified time window.
	Parameters:
	- time_iso: Evaluation time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Preferred over timestamp.
	- timestamp: Unix timestamp for the query time (deprecated alias, defaults to current time)
	- window: Time window in seconds to look back for alerts (defaults to 900 seconds = 15 minutes, range: 1-3600). Max is 3600 seconds (1 hour). If the user asks for a longer period (e.g. 90 minutes, 2 hours, a day), cap window at 3600 — do not pass the raw computed value (such as 5400 or 7200), as the server rejects anything above 3600.
	- lookback_minutes: Relative time window in minutes (range: 1-60). Used only when window is not provided.
	
	Uses the datasource configured in the server config (or default if not specified).
	
	Each alert includes:
	- id: Unique identifier for this alert instance
	- rule_id: ID of the alert rule that triggered this alert
	- rule_name: Name of the alert rule
	- state: Current state (firing, resolved, pending)
	- severity: Alert severity level
	- starts_at: When this alert instance started firing
	- ends_at: When this alert instance was resolved (if resolved)
	- labels: Key-value pairs for alert identification and routing
	- annotations: Additional context and descriptions
	- generator_url: URL to the source of the alert
	- fingerprint: Unique fingerprint for this alert instance
	
	Each alert rule that has been acknowledged or has an owner also carries a claim block (omitted when the rule is unclaimed):
	- owner_user_id / owner_name / owner_email: the responder who owns the rule
	- claimed_by_user_id: the user who recorded the claim
	- claimed_at: when the claim was made (Unix seconds, shown as UTC)
	- muted_until: mute deadline chosen while acknowledging (Unix seconds, shown as UTC); omitted when the acknowledgement carried no mute

	## When to use
	Alerts firing now or recently: `window` is default 15 minutes, at most 1 hour.

	## When not to use
	- Firing history over a longer range: `get_alert_rule_state`
	- Rule definitions: `get_alert_config`
	- Only a count of firing alerts: `get_workspace_overview`

	## Pairs well with
	- `get_alert_config` (rule details)
	- `get_entity_alert_rules` (PromQL behind a rule)
	- `get_current_time` (evaluation time)

	## Output shape
	Text listing each alert rule with its group, state, severity and alert instances.

	## Known traps
	`window` above 3600 seconds is rejected; cap it.
