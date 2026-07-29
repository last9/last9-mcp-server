# Get Trace Attribute Deviations

Compares attribute-value distributions between two bounded trace-span cohorts. Use for slow/error spans or changed windows. Correlation, not cause.

- `comparison_mode` (required): `latency`, `errors`, or `time`.
- `service_name`, `environment` (required): exact service and `deployment.environment`.
- `operation`: optional exact operation/span name.
- `filters`: optional trace JSON filters; discover fields with `get_trace_attributes_for_pipeline`.
- `candidate_attributes`: up to 8 names or `filter_field` values; omit for bounded discovery.
- `latency_threshold_ms`: required for latency mode (ms → nanosecond `Duration`).
- `start_time_iso` / `end_time_iso` or `lookback_minutes` (default 15, max 15): target window.
- `baseline_start_time_iso` / `baseline_end_time_iso`: required for time mode; non-overlapping, equal duration.
- `minimum_cohort_size`: default 100, min 20. `minimum_value_support`: default 20, min 10. `limit`: default/max 10.

Returns shares, deltas, ratios, exclusions, ranks, truncation metadata. Never call ranked values root cause—corroborate with representative traces.
