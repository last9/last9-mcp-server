Compare trace attribute cohorts between a current window and equal-duration baseline to find values correlated with latency or errors. Correlation is not contribution, attribution, cause, or root cause.

`service_name` is required. `env` is optional and exact. Prefer `lookback_minutes` for the current window, or set RFC3339 `start_time_iso` and `end_time_iso`; set both `baseline_start_time_iso` and `baseline_end_time_iso` to override the preceding equal window. Windows must satisfy the handler's alignment and duration constraints.

Optional limits bound returned attributes and values, not the underlying comparison. Preserve returned evidence quality, warnings, partial errors, scores and units; never manufacture percentages or merge environments.

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
