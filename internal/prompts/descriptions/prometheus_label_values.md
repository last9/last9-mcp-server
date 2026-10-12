
	Return the label values for a particular label and promql filter query.
	This works similar to the prometheus /label_values call
	It returns an array of values for the label.
	Parameters:
	- match_query: (Required) A valid promql filter query
	- label: (Required) Name of the label to return values for
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60. For daily or sparse metrics (for example CloudWatch AWS/S3 BucketSizeBytes), use a wider window such as 5760 (4 days); the default 60-minute window omits them.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- datasource: (Optional) Name of the datasource to query. If omitted, uses the default configured datasource.

	match_query should be a well formed, valid promql query
	Prefer an explicit lookback_minutes (or start_time_iso/end_time_iso) sized to the metric's emission cadence.
	Do not treat an empty result under the default 60-minute window as proof a daily/sparse metric is absent.
