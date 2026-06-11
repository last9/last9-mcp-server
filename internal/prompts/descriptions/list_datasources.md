
	List all available datasources configured for this organization.
	Use this tool to discover valid datasource names before passing them to
	prometheus_range_query, prometheus_instant_query, prometheus_label_values,
	or prometheus_labels via the datasource parameter.

	Returns an array of objects, each with:
	- name: the datasource name to use in the datasource parameter
	- is_default: true for the datasource that is used when no datasource is specified
