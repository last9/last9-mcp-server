Report whether a metric is live, how many series it has, how old the newest sample is, its inferred scrape/export cadence, and a suggested panel query window.

Use this before building dashboards or diagnosing "empty panel" false negatives. Slow emitters (daily CloudWatch/S3/RDS metrics) look dead on a 24h instant probe between samples; probe with a window that can cover multiple intervals (e.g. 5760 minutes for daily metrics).

Returns JSON:
- live: true when at least one series has a sample in the window
- series_count: number of series with a sample in the window (0 when absent)
- last_sample_age_seconds: seconds since the newest sample across series (0 when absent)
- inferred_interval_seconds: median consecutive sample-timestamp delta in the window (0 when fewer than 2 samples)
- suggested_window: "$__interval" for high-cadence metrics; otherwise a duration string such as "3d" sized to roughly 3× last_sample_age so last_over_time does not blank between slow emissions

Parameters:
- metric: (Required) Metric name (e.g. amazonaws_com_AWS_S3_BucketSizeBytes) or a PromQL series selector (e.g. up{job="api"}).
- window_minutes: (Required) Lookback window in minutes (minimum: 1). Prefer a window wide enough to cover several emission intervals for the metric under test.
- datasource: (Optional) Datasource name from list_datasources. Default: the configured default datasource.