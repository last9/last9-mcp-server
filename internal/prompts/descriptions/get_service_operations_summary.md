
	Get a summary of operations inside a service over a given time range.
	Returns a list of operations with their details.
	These include operations like HTTP endpoints, database queries, messaging producer and http client calls.
	Includes service name, environment, throughput, error rate, and response time for each operation.
	All values are p95 quantiles over the time range.
	Response times are in milliseconds. Throughput and error rates are in requests per minute (rpm).
	Each operation includes:
		- operation name
		- service name
		- environment
		- throughput in requests per minute (rpm)
		- error rate in requests per minute (rpm)
		- response time in milliseconds (p95, p90, p50 quantiles, avg, and max)
		- error percentage
	Database operations contain additional fields:
		- db_system: Database system (e.g., mysql, postgres, etc.)
		- net_peer_name: Database host or connection string
	Messaging operations contain additional fields:
		- messaging_system: Messaging system (e.g., kafka, rabbitmq, etc.)
		- net_peer_name: Messaging host or connection string
	HTTP client operations contain additional fields:
		- http_method: HTTP method (e.g., GET, POST, etc.)
		- net_peer_name: HTTP host or connection string
	
	Parameters:
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
	- service_name: (Required) Service name to filter by. Defaults to all services.
	- If unsure of the service_name or env spelling, call "did_you_mean" first.
