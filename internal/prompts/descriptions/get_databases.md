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

`resolved_labels` carries the metrics-native label names and values for the row. Use them
when building follow-up metric queries instead of guessing label names from `host`.

Each row also carries `id` (a stable identifier for the row), `env` (its deployment
environment), `sources` (which signals produced it), and, for a metrics-only row,
`activity_source` (which infrastructure signal the `activity` value came from).

Rows are returned in the same order the Databases dashboard shows: busiest first.

Parameters:
- env: (Optional) Filter by deployment environment. Accepts a regular expression, so
  "prod|staging" matches either. Default: all environments.
- lookback_minutes: (Optional) Time window in minutes (default: 60). The window may not
  exceed 7 days.
- start_time_iso: (Optional) Start time in RFC3339 format. Overrides lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339 format.