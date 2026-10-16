
	List all available datasources configured for this organization.
	Use this tool to discover valid datasource names before passing them to
	prometheus_range_query, prometheus_instant_query, prometheus_label_values,
	or prometheus_labels via the datasource parameter.

	Returns an array of objects, each with:
	- name: the datasource name to use in the datasource parameter
	- is_default: true for the datasource that is used when no datasource is specified

	## When to use
	Pick a datasource name for the `datasource` parameter of the Prometheus tools.

	## When not to use
	Grafana datasources: `grafana_list_datasources`. Whether metrics exist at all: `get_workspace_overview`.

	## Pairs well with
	`prometheus_range_query`, `prometheus_instant_query`, `prometheus_label_values`, `prometheus_labels`.

	## Output shape
	Array of `{name, is_default}`.

	## Known traps
	The list is read at startup; a datasource added later appears after a restart.
