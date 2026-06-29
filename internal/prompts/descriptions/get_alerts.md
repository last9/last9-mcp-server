
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
