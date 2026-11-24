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
`get_database_slow_queries`) for a row whose capabilities do not list them. For slow queries on a
specific host, pass its `host` and the row's `db_system` and `env` when present. Only trace results are
filtered by host. Log results may include other hosts.

`resolved_labels` carries the metrics-native label names and values for the row. Use these as PromQL
label matchers for single-instance metric queries. `get_database_server_metrics` can filter
by database type only, so its results can cover multiple instances.

The stable `id` identifies the discovery row. Omit `database_id` from follow-up calls.
Each row also carries `env` (its deployment environment), `sources` (which signals produced it), and, for a metrics-only row,
`activity_source` (which infrastructure signal the `activity` value came from).

Rows are returned in the same order the Databases dashboard shows: busiest first.

Parameters:
- env: (Optional) Filter by deployment environment. Accepts a regular expression, so
  "prod|staging" matches either. Default: all environments. For one exact environment
  use get_database_queries or get_service_performance_details, which take a literal name.
- lookback_minutes: (Optional) Time window in minutes (default: 60). The window may not
  exceed 7 days.
- start_time_iso: (Optional) Start time in RFC3339 format. Overrides lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339 format.