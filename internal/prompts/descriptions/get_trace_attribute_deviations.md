# Get Trace Attribute Deviations

Compares attribute-value distributions between two bounded trace-span cohorts and ranks supported differences. Use it to find dimensions associated with slow spans, error spans, or a changed time window. Results describe correlation, not cause.

## Parameters

- `comparison_mode` (required): `latency`, `errors`, or `time`.
- `service_name` (required): exact service name.
- `environment` (required): exact `deployment.environment` value.
- `operation`: optional exact operation/span name.
- `filters`: optional trace JSON filter conditions. Discover valid fields first with `get_trace_attributes_for_pipeline`.
- `candidate_attributes`: up to 8 raw attribute names or returned `filter_field` values. Omit for bounded safe discovery, which evaluates at most 3 auto-selected attributes. Naming attributes explicitly is how you analyze more than 3, or a specific dimension discovery did not pick. Sensitive and identifier-like attributes are rejected — a named one fails the call rather than being silently dropped.
- `latency_threshold_ms`: required for latency mode. Pass milliseconds; the value is sent as milliseconds and the split is applied server-side. Must be positive, and must be omitted for `errors` and `time` modes.
- `start_time_iso` / `end_time_iso`: explicit RFC3339 target window.
- `lookback_minutes`: alternative target lookback ending now; default 15, maximum 15.
- `baseline_start_time_iso` / `baseline_end_time_iso`: required for time mode; must be non-overlapping and exactly equal in duration to the target window.
- `minimum_cohort_size`: default 100, minimum 20.
- `minimum_value_support`: pooled support required to rank a value; default 20, minimum 10.
- `limit`: default 10, maximum 10.

Omitted numeric bounds take the defaults above. A value outside a documented range is rejected with a message naming the bound, so a rejected call means the argument must be corrected — not retried unchanged.

Reading `evidence.warnings`: `candidate_limit_reached` on a discovery call means discovery filled its 3-attribute ceiling, not that the request was wrong — pass explicit `candidate_attributes` to examine dimensions beyond those 3. If the call fails with the capability being unavailable or disabled, this analysis is not enabled for the connected tenant; do not retry, and fall back to `get_traces` aggregations.

The tool delegates to a bounded atomic trace-analysis endpoint: candidate discovery, cohort totals, value distributions, ranking, and representative trace IDs are resolved in a single server-side query with server-enforced limits. It returns full-denominator shares, percentage-point deltas, finite ratios, missing-value counts, exclusions, deterministic ranks, truncation/partial metadata, and evidence quality under `investigation-evidence/v1`.

Do not call a ranked value a root cause. Quote the returned counts and percentage-point delta, then say it is an association requiring representative-trace corroboration.
