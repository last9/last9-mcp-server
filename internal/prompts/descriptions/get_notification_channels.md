
	Get notification channel configurations from Last9.
	Returns all notification channels configured in the organization as a TSV table.

	Columns: id, name, type, global, in_use, send_resolved, snoozed_until, severity, priority, services
	- send_resolved: true/false/null (null = not explicitly configured)
	- snoozed_until: UTC timestamp if snoozed, else "-"
	- services: comma-separated namespace/name pairs, "-" if global
