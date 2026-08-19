# Get Trace Attribute Deviations

Compares attribute-value distributions between two bounded trace-span cohorts and ranks supported differences. Use it to find dimensions associated with slow spans, error spans, or a changed time window. Results describe correlation, not cause.

## Parameters

- `comparison_mode` (required): `latency`, `errors`, or `time`.
- `service_name` (required): exact service name.
- `environment` (required): exact `deployment.environment` value.
- `operation`: optional exact operation/span name.
- `population`: optional `service` or `operation`. Defaults to `service` when `operation` is absent and `operation` when it is present. Service population rejects an operation; operation population requires one.
- `filters`: optional trace JSON filter conditions. Discover valid fields first with `get_trace_attributes_for_pipeline`.
- `candidate_attributes`: up to 8 raw attribute names or returned `filter_field` values. Omit for bounded safe discovery, which auto-selects a small number of attributes within a server-side budget. Naming attributes explicitly is how you widen that, or analyze a specific dimension discovery did not pick. Sensitive and identifier-like attributes are rejected — a named one fails the call rather than being silently dropped.
- `latency_threshold_ms` / `latency_percentile`: latency mode requires exactly one. Threshold is positive milliseconds. Percentile must be greater than 0 and less than 100 and is computed from the same scoped population and normalized window used for cohorting. Both must be omitted for `errors` and `time` modes.
- `start_time_iso` / `end_time_iso`: explicit RFC3339 target window.
- `lookback_minutes`: alternative target lookback ending now; default 15, maximum 15.
- `baseline_start_time_iso` / `baseline_end_time_iso`: required for time mode; must be non-overlapping and exactly equal in duration to the target window.
- `minimum_cohort_size`: default 100, minimum 20.
- `minimum_value_support`: pooled support required to rank a value; default 20, minimum 10.
- `limit`: default 5. Legacy values through 10 are accepted, but the endpoint returns at most 5 deviations and reports `ranked_result_limit_reduced_to_five` when it reduces a larger request.

Omitted numeric bounds take the defaults above. A value outside a documented range is rejected with a message naming the bound, so a rejected call means the argument must be corrected — not retried unchanged.

Reading `evidence.warnings`. Discovery examines only a small server-side budget of attributes, so `candidate_limit_reached` means the budget was filled and dimensions were never examined. Also emitted: `minimum_cohort_size_not_met` (a cohort was below `minimum_cohort_size`, so nothing is ranked), `attribute_value_limit_reached` (an attribute exceeded the distinct-value budget and was excluded with `excluded_reason: "distinct_value_limit_exceeded"`), and `ranked_result_limit_reduced_to_five` (a legacy request above 5 was bounded to 5). Discovery also excludes attributes with `excluded_reason: "sensitive_or_high_cardinality_policy"`.

The tool delegates to a bounded atomic trace-analysis endpoint: population normalization, percentile thresholding, cohort execution, candidate discovery, cohort totals, value distributions, ranking, and representative trace IDs are resolved in one server-side query. It returns an `investigation-evidence/v1` envelope with explicit `target_cohort` / `control_cohort` windows and definitions; `normalized_population_count`; `candidate_coverage`; typed `threshold` source, effective value, population scope/window/count; `backend_duration_ms`; `target_missing` / `control_missing`; full-denominator shares (`target_share` / `control_share`); `percentage_point_delta`; nullable `ratio`; deterministic `rank`; and typed `partial`, `truncated`, `warnings`, and `evidence_quality` metadata. `ratio` is null whenever either share is zero — use `percentage_point_delta` in that case. `evidence_quality` is `medium` at best and `insufficient` when evidence is partial; this analysis never reports `high`.

Unsupported capability, partial evidence, truncated evidence, insufficient evidence, or a contract/version rejection is terminal for this evidence step: stop and report the returned limitation. Never reconstruct target/control cohorts, thresholds, rankings, or denominators client-side with other MCP calls.

Do not call a ranked value a root cause. Quote the returned counts and percentage-point delta, then say it is an association requiring representative-trace corroboration.
