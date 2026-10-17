
	Return the label values for a particular label and promql filter query.
	This works similar to the prometheus /label_values call
	It returns an array of values for the label.
	Parameters:
	- match_query: (Required) A valid promql filter query
	- label: (Required) Name of the label to return values for
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- datasource: (Optional) Name of the datasource to query. If omitted, uses the default configured datasource.

	match_query should be a well formed, valid promql query
	It is encouraged to not use default
	values of start_time and end_time and use values that are appropriate for the
	use case

	## When to use
	The valid values of one label (env, service) for a metric selector, before filtering on it.

	## When not to use
	- Label names: `prometheus_labels`
	- APM service environments: `get_service_environments`
	- Fuzzy name lookup: `did_you_mean`

	## Pairs well with
	- `prometheus_labels`
	- `prometheus_range_query`
	- `prometheus_instant_query`

	## Output shape
	A JSON array of strings.

	## Known traps
	An empty array usually means the window is too short or the selector matches nothing; widen or fix it before concluding the value does not exist.
