
	Get a summary of operations inside a service over a given time range.

	**Profile first:** Call `get_service_profile` for this service before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

	Returns HTTP, database, and messaging operations in `operations`. For a top N, omit `limit`
	and rank the returned rows by the requested metric (e.g. `response_time.p95` for p95
	latency or `throughput` for traffic). Report the first N after sorting highest first.
	Response times are in milliseconds.
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
	Client operations may also include:
		- rpc_system: RPC protocol
		- net_peer_name: HTTP host or connection string
	
	Parameters:
	- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60.
	- start_time_iso: (Optional) Start time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Overrides lookback when provided.
	- end_time_iso: (Optional) End time of the time range in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to current time.
	- env: (Optional) Exact environment name, e.g. production. Use "get_service_environments" tool to get available environments. Default all environments (empty or .*). Anchored ^production$ is accepted and matched exactly; alternation (a|b) and wildcards (prod.*) are rejected. For regex matching use get_service_summary. The response echoes the matched `env`, plus `env_requested` when it differed.
	- service_name: (Required) One service name.
	- If unsure of the service_name or env spelling, call "did_you_mean" first.
