Report whether a metric is live, how many series it has, how old the newest sample is, its inferred scrape/export cadence, and a suggested panel query window.

Use this before building dashboards or diagnosing "empty panel" false negatives. Slow emitters (daily CloudWatch/S3/RDS metrics) look dead on a 24h instant probe between samples; probe with a window that can cover multiple intervals (e.g. 5760 minutes for daily metrics).

Returns JSON:
- live: true when at least one series has a sample in the window
- series_count: number of series with a sample in the window (0 when absent)
- last_sample_age_seconds: seconds since the newest sample across series (0 when absent)
- inferred_interval_seconds: raw-sample interval in seconds from a scrape_interval rollup over the window, median across series; independent of the window width. null (never 0) when the cadence is unknown: the metric is absent, or no series has 2+ samples in the window
- suggested_window: "$__interval" for high-cadence metrics; otherwise a duration string such as "3d" sized to roughly 3× last_sample_age so last_over_time does not blank between slow emissions ("" when absent)
- note: present only when a field needs reading. Absent metric: "No series matched `<selector>` in the last <window>; check the metric name with `prometheus_label_values` (label `__name__`, `match_query` such as `{__name__=~".*<fragment>.*"}`), …" — live=false here means no match, not a metric that stopped. Unknown cadence: "Too few samples in the last <window> to infer cadence …; widen `window_minutes` …"

Parameters:
- metric: (Required) Metric name (e.g. amazonaws_com_AWS_S3_BucketSizeBytes) or a PromQL series selector (e.g. up{job="api"}).
- window_minutes: (Required) Lookback window in minutes (minimum: 1). Prefer a window wide enough to cover several emission intervals for the metric under test.
- datasource: (Optional) Datasource name from list_datasources. Default: the configured default datasource.