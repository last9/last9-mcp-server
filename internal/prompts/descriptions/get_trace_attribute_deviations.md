Compare trace attribute cohorts to find values correlated with latency, errors, or a changed time window. `latency` and `errors` split spans within the same target window. Only `time` compares separate target and baseline windows. Correlation is not contribution, attribution, cause, or root cause.

Required: `comparison_mode` (`latency`, `errors`, or `time`), exact `service_name`, and exact `environment`. Optional `operation` is exact. `filters` are trace-JSON conditions; discover fields with `get_trace_attributes_for_pipeline`. `candidate_attributes` accepts at most 8 discovered field names; omit it for bounded safe discovery. Sensitive or identifier-like candidates are rejected.

For `latency`, pass positive `latency_threshold_ms`; omit it for other modes. Prefer `lookback_minutes` (default and maximum 15), or set both RFC3339 `start_time_iso` and `end_time_iso`. `time` mode also requires non-overlapping `baseline_start_time_iso` and `baseline_end_time_iso` of equal duration.

`minimum_cohort_size` defaults to 100, minimum 20. `minimum_value_support` defaults to 20, minimum 10. `limit` defaults to and cannot exceed 10. These bound ranking, not the underlying comparison. Preserve evidence quality, warnings, partial errors, scores and units; never manufacture percentages or merge environments. A null ratio means one share was zero; use the percentage-point delta.

Warnings: `candidate_limit_reached` means discovery filled its bounded attribute budget; name candidates to widen it. `minimum_cohort_size_not_met` means nothing is ranked. `attribute_value_limit_reached` drops an attribute whose exclusion is `distinct_value_limit_exceeded`. Policy exclusions use `sensitive_or_high_cardinality_policy`.

## When to use
Find trace dimensions whose values differ between current and baseline performance.

## When not to use
- Service/operation APM comparison: `get_apm_service_deviations`
- Raw trace search or exact ids: `get_service_traces`
- One-window fleet ranking: `get_service_summary`

## Pairs well with
- `get_trace_attributes`
- `get_service_traces`
- `get_change_events`

## Output shape
Sanitized comparison evidence with ranked attribute/value cohorts, windows, quality and warnings.

## Known traps
Read `last9://reference/tool_recipes` before interpreting cohorts or choosing follow-up queries.
