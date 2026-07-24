
	Perform a Prometheus range query to get metrics data.
	This tool can be used to query Prometheus for metrics data over a specified time range.
	It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
	for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
	are common. To get possible values of a label, the prometheus_label_values tool can be used.
	It returns a structured response with the following fields:
	- metric: A map of metric labels and their values.
	- value: A list of lists. Each item in the list has timestamp as the first element
		and the value as the second.
	Example:
	[ {
		"metric": {
			"__name__": "http_request_duration_seconds",
			"method": "GET",
			"status": "200"
		},
		"value": [
			[1700000000, "0.123"],
			[1700000060, "0.456"],
			...
		]
	}]
	The response will contain the metrics data for the specified query.
	Parameters:
	- query: (Required) The Prometheus query to execute.
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- datasource: (Optional) Name of the datasource to query. If omitted, uses the default configured datasource.

	**Time:** Prefer `lookback_minutes` for relative windows; `start_time_iso`+`end_time_iso` (RFC3339) override lookback. On lookback validation errors, retry with explicit ISO bounds instead of `lookback_minutes`.

	Full metrics usage guide: resource `last9://reference/metrics`