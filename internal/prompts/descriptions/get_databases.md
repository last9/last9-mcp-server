Discover all databases across your infrastructure with key performance metrics.

Returns a list of databases detected from trace data, including database type, host,
throughput (queries/min), p95 latency, error rate, and how many services use each database.

This tool uses OpenTelemetry trace metrics (trace_client_count, trace_client_duration) to identify
databases from spans with db_system set.

Parameters:
- env: (Optional) Filter by deployment environment (e.g. "production"). Default: all environments.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format. Overrides lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339 format.