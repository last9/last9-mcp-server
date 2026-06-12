Find slow database queries from traces and logs.

Retrieves the slowest database operations by searching trace spans where db_system is set,
ordered by duration (descending). These are actual observed query executions captured by
OpenTelemetry instrumentation.

For each slow query, returns: trace ID (for drill-down), service name, operation/query pattern,
duration, database system, status, and timestamp.

Parameters:
- db_system: (Optional) Filter by database system (e.g. "postgresql", "mysql", "mongodb", "redis").
- host: (Optional) Filter by database host (net_peer_name from traces).
- service_name: (Optional) Filter by calling service name.
- env: (Optional) Filter by deployment environment.
- min_duration_ms: (Optional) Minimum query duration in milliseconds to include (default: 0, returns slowest first).
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.
- limit: (Optional) Maximum number of slow queries to return (default: 20).
- If unsure of the db_system, host, or service_name spelling, call "did_you_mean" first.