# Get Trace Attribute Deviations

Compares attribute-value distributions between two bounded trace-span cohorts and ranks differences. Use for slow/error spans or changed windows. Correlation, not cause.

## Parameters

- `comparison_mode` (required): `latency`, `errors`, or `time`.
- `service_name` (required): exact service name.
- `environment` (required): exact `deployment.environment`.
- `operation`: optional exact operation/span name.
- `filters`: optional trace JSON filters. Discover fields with `get_trace_attributes_for_pipeline`.
- `candidate_attributes`: up to 8 attribute or `filter_field` names. Omit for bounded discovery.
- `latency_threshold_ms`: required for latency mode (ms; converted to nanosecond `Duration`).
- `start_time_iso` / `end_time_iso`: RFC3339 target window.
- `lookback_minutes`: target lookback ending now; default 15, max 15.
- `baseline_start_time_iso` / `baseline_end_time_iso`: required for time mode; non-overlapping, equal duration to target.
- `minimum_cohort_size`: default 100, min 20.
- `minimum_value_support`: pooled support to rank; default 20, min 10.
- `limit`: default 10, max 10.

Returns shares, percentage-point deltas, ratios, exclusions, ranks, and truncation metadata. Never call a ranked value root cause—quote counts/deltas and corroborate with representative traces.
