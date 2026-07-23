
	Get notification channel configurations from Last9.
	Returns all notification channels configured in the organization as a TSV table.

	Columns: id, name, type, global, in_use, send_resolved, snoozed_until, severity, priority, services, service_fqid
	- send_resolved: true/false/null (null = not explicitly configured)
	- snoozed_until: UTC timestamp if snoozed, else "-"
	- services: comma-separated namespace/name pairs, "-" if global
	- service_fqid: alert-group entity id this channel is bound to (empty/"-" if global or unbound).

	To find alert rules with no notification channel configured: call get_alert_config to list rules
	with their entity_id, then a rule is unconfigured if no channel here has service_fqid equal to
	that entity_id.
