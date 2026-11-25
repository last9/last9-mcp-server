Discover all databases across your infrastructure with key performance metrics.

Returns databases found from two signals, merged into one list:
- OpenTelemetry client spans (`db_system` set) — these rows carry throughput, p95 latency,
  error rate, and how many services call the database.
- Infrastructure metrics such as CloudWatch — these rows appear even when nothing traces
  the database. They carry an `activity` value instead of trace metrics.

A row reports only the signals it actually has. A database seen only in metrics omits
`throughput_rpm`, `p95_latency_ms`, `error_rate_pct` and `service_count` entirely — treat a
missing key as "not measured", never as zero. `metrics_only: true` and `capabilities` say
what a row supports: do not call trace-backed tools (`get_database_queries`,
`get_database_slow_queries`) for a row whose capabilities do not list them.

Follow-up tools do not accept `id` or `database_id`. Pass `db_system` and `env` when present
to `get_database_slow_queries`. Omit the discovery row's `host`: it is not necessarily the
tool's `net_peer_name` filter value. Use `host` only when that value is already known; it
scopes traces, not logs.
`get_database_server_metrics` scopes instances only by `db_system` and may combine instances.
To compare the same window, pass matching `lookback_minutes`, `start_time_iso`, or `end_time_iso`.
When `resolved_labels` is present, use it in a Prometheus query to isolate one instance.

Rows include `id`, `env`, and `sources`. The `id` can repeat across environments. Metrics-only
rows add `activity_source`.

Rows are returned in the same order the Databases dashboard shows: busiest first.

Parameters:
- db_system: (Optional) Exact database system, e.g. "postgresql". Filters both trace and
  metrics-only rows. Omit to include all systems.
- env: (Optional) Filter by deployment environment. Accepts a regular expression, so
  "prod|staging" matches either. Default: all environments. For one exact environment
  use get_database_queries or get_service_performance_details, which take a literal name.
- lookback_minutes: (Optional) Time window in minutes (default: 60). The window may not
  exceed 7 days.
- start_time_iso: (Optional) Start time in RFC3339 format. Overrides lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339 format.