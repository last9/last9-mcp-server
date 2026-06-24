
	Get service performance metrics over a given time range.
	Returns the following information
		- service name
		- environment
		- throughput in rpm
		- error rate in rpm for 4xx and 5xx errors
		- error percentage
		- p50, p90, p95, avg, and max response times in seconds
		- apdex score
		- availability in percentage
		- top 10 web operations by response time
		- top 10 operations by error rate
		- top 10 errors or exceptions by count for the service
	The details for the operations in the "top 10 web operations by response time" and "top 10 operations by error rate" can be fetched using the "get_service_operation_details" tool.
	This tool can be used to get all perforamnce and debugging details for a service over a time range.
	It can also be used to get a summary for performance bottlenecks and errors / exceptions in a service.
	Some fields are in the promql resonse format. Sample response:
	[{"metric":{"service_name":"svc1","env":"prod"},"values":[[1700000000,"0.5"]]},{"metric":{"service_name":"svc2","env":"prod"},"values":[[1700000001,"0.1"]]}]
	where the "metric" key is a dict of metadata, the first value in "values" is the timestamp in seconds and the second value is the value of the metric.
	The fields in the response are:
	- service_name: Name of the service.
	- env: Environment of the service.
	- throughput: Throughput in requests per minute (rpm) by status code. The format of this is in promql response format.
	- error_rate: Error rate in requests per minute (rpm) by status code. The format of this is in promql response format.
	- error_percentage: Error percentage in requests by status code. The format of this is in promql response format.
	- response_times: Response times in seconds by quantile (p50, p90, p95, avg, max). The format of this is in promql response format.
	- apdex_score: Apdex score over the time range. The format of this is in promql response format.
	- availability: Availability in percentage over the time range. The format of this is in promql response format.
	- top_operations: Top operations by response time and error rate. The format of this is a dict of operations and their throuputs
	- top_errors: Top errors or exceptions by count. The format of this is a dict of errors and their counts.
	- top_operations.by_response_time: Top 10 operations by response time. The format of this is a list of dicts with operation name and response time.
	- top_operations.by_error_rate: Top 10 operations by error rate. The format of this is a list of dicts with operation name and error count.
	- top_errors: Top 10 errors or exceptions by count. The format of this is a list of dicts with exception type (or http error code) and count. 
	Parameters:
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
	- If unsure of the service_name or env spelling, call "did_you_mean" first.
