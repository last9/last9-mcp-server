
	Get currently active alerts from Last9 monitoring system.
	Returns alerts that are currently firing or have fired recently within the specified time window.
	Parameters:
	- limit: Optional positive integer. Show the first N matching instances across rules in their returned order. Omit for all. Counts show displayed and total matches.
	- state: Filter alert instances by `firing` or `normal` before grouping by rule. For all states, omit the state key entirely (e.g. `{"window":900}`). Never send null or "all". A normal instance fired in the window but is no longer firing.
	- time_iso: RFC3339 snapshot time, at or before now. Preferred over timestamp; omit both for now.
	- timestamp: Unix timestamp for the query time (deprecated alias, defaults to current time)
	- window: Time window in seconds to look back for alerts (defaults to 900 seconds = 15 minutes, range: 1-3600). Max is 3600 seconds (1 hour). If the user asks for a longer period (e.g. 90 minutes, 2 hours, a day), cap window at 3600 — do not pass the raw computed value (such as 5400 or 7200), as the server rejects anything above 3600.
	- lookback_minutes: Relative time window in minutes (range: 1-60). Used only when window is not provided.
	
	Uses the datasource configured in the server config (or default if not specified).
	
	Each rule includes its ID, name, state, severity and alert instances. Each instance includes
	its state, label hash, current value, metric degradation, group labels and annotations.
	
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
