
	Return the labels for a given  promql match query.
	This works similar to the prometheus /labels call
	It returns an array of labels.
	Parameters:
	- match_query: (Required) A valid promql filter query (also accepted as "match")
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- datasource: (Optional) Name of the datasource to query. If omitted, uses the default configured datasource.

	match_query should be a well formed, valid promql query
	It is encouraged to not use default
	values of start_time and end_time and use values that are appropriate for the
	use case
