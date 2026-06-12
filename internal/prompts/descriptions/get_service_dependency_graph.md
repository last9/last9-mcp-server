
	Get details of the throughput, response times and error rates of
	incoming, outgoing and infrastructure components like messaging and databases
	of a service.
	This tool can be used to get a detailed dependency graph of a service and help
	in analysis of cascading effect of errors and performance issues.
	It returns a structured response with the following fields:
	- service name
	- environment
	- throughput in requests per minute (rpm)
	- error rate in requests per minute (rpm)
	- p95 response time in milliseconds
	- p90 response time in milliseconds
	- p50 response time in milliseconds
	- avg response time in milliseconds
	- max response time in milliseconds
	- error percentage
	The detailed metrics, error rates and operation details of incoming and outgoing dependencies
	can be obtained by using the get_service_details tool.
	Parameters:
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
	- service_name: (Required) Name of the service to get the dependency graph for.
	- If unsure of the service_name or env spelling, call "did_you_mean" first.
	