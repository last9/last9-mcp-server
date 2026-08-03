
	Get a fleet-wide summary of all services over a given time range. For a single service, use get_service_performance_details.
	Includes service name, environment, throughput, error rate, and response time.
	All values are p95 quantiles over the time range.
	Response times are in milliseconds. Throughput and error rates are in requests per minute (rpm).
	Each service includes:
	- service name
	- environment
	- throughput in requests per minute (rpm)
	- error rate in requests per minute (rpm)
	- p95 response time in milliseconds
	Parameters:
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- env: (Optional) Environment to filter by. If not provided, defaults to all environments.
