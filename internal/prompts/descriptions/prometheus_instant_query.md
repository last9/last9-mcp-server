
	Perform a Prometheus instant query to get metrics data.
	Typically, the query should have rollup functions like sum_over_time, avg_over_time, quantile_over_time, etc
	over a time window. For example: avg_over_time(trace_endpoint_count{env="prod"}[1h])
	This tool can be used to query Prometheus for metrics data at a specific point in time.
	It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
	for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
	are common. To get possible values of a label, the prometheus_label_values tool can be used.
	It returns a structured response with the following fields:
	- metric: A map of metric labels and their values.
	- value: A list of lists. Each item in the list has timestamp as the first element
		and the value as the second.
	Response Example:
	[ {
		"metric": {
			"__name__": "http_request_duration_seconds",
			"method": "GET",
			"status": "200"
		},
		"value": [1700000000, "0.123"]
	}]
	The response will contain the metrics data for the specified query.
	Parameters:
	- query: (Required) The Prometheus query to execute.
	- time_iso: (Optional) The point in time to query in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- lookback_minutes: (Optional) Number of minutes to look back from now when time_iso is omitted.
	- datasource: (Optional) Name of the datasource to query. If omitted, uses the default configured datasource.
