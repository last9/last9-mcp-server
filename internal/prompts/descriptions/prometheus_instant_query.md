
	Perform a Prometheus instant query to get metrics data.
	Typically, the query should have rollup functions like sum_over_time, avg_over_time, quantile_over_time, etc
	over a time window. For example: avg_over_time(trace_endpoint_count{env="prod"}[1h])
	Percentiles are not composable: never average precomputed percentile series. With suitable Prometheus histogram buckets, use histogram_quantile(..., rate(...)) or histogram_quantile(..., increase(...)). Otherwise use get_logs/get_traces to aggregate raw values. Use a precomputed percentile metric only when its recorded window exactly matches the requested window.
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
	- datasource: (Optional) Datasource name; omitted uses the default, a typo its closest match; looser or ambiguous names list candidates, no query. An additional block names it unless the name was exact.

	## When to use
	One value per series at one time, usually a rollup over a window.

	## When not to use
	- A trend over time: `prometheus_range_query`
	- Fleet service ranking: `get_service_summary`

	## Pairs well with
	- `prometheus_labels` and `prometheus_label_values` (discover names first)
	- `list_datasources` (pick a datasource)

	## Output shape
	Series `[{metric, value}]`; value is `[timestamp, "string"]`.

	## Known traps
	Without a range rollup the query reads only the latest sample.
