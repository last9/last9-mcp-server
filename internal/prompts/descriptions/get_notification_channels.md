
	Get notification channel configurations from Last9.
	Returns all notification channels configured in the organization as a TSV table.

	Columns: id, name, type, global, in_use, send_resolved, snoozed_until, severity, priority, services, service_fqid, created_via
	- global: false on every alert-group attachment row (service_fqid set); true only for an unscoped row
	- send_resolved: true/false/null (null = not explicitly configured). On attachment rows (service_fqid set) this is the row's stored default, not what delivery uses: resolve notifications follow the channel's own send_resolved, shown by scope=channels.
	- snoozed_until: UTC timestamp if snoozed, else "-"
	- severity: breach or threat for this binding row (which alert severities route to this channel)
	- services: comma-separated namespace/name pairs of a legacy service binding, "-" if none (attachment rows are scoped by service_fqid)
	- service_fqid: alert-group entity id this channel is bound to (empty/"-" if global or unbound).
	  Rows include per-entity mapped channels (exact=true API), not only org-wide masters.
	- created_via: last9-mcp when an MCP agent created the row, else "-" (created elsewhere or not recorded).

	Optional scope (default attached):
	- attached: the table above, one row per alert-group attachment. The id column is an attachment id.
	- channels: the org's channels as a TSV table with columns id, name, type, send_resolved, created_via. The id is a channel id. Destinations, headers and certificates are never returned.

	## Pairs well with
	- `detach_notification_channel` (takes an attachment id from scope=attached)
	- `attach_notification_channel` (takes a channel id from scope=channels)
