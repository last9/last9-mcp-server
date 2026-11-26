Compare APM server-request performance across a current window and an equal-duration baseline. Call this tool first and by itself for what regressed, improved, or changed; do not call `get_service_profile` first. Do not batch speculative corroboration. Use `get_service_summary` for a one-window fleet ranking.

Omit `service_name` for fleet scope or set it for one service and operation correlations. Environments remain separate; `env` is an exact literal, not a regex. V1 supports server-request workloads; others may return `unsupported_workload_shape`. `datasource` defaults to the configured source; near matches resolve, ambiguous names return candidates.

Current time defaults to 60 minutes. Prefer `lookback_minutes`; short lookbacks are unreliable because only complete minute buckets count and classification usually needs at least four. Alternatively set minute-aligned `start_time_iso` and `end_time_iso`. Baseline defaults to the preceding equal window; override it with both `baseline_start_time_iso` and `baseline_end_time_iso`, also equal-duration and minute-aligned.

`max_services` and `max_operations` each default to 10 and cannot exceed 10. Caps bound the response, not analysis; retained regressions precede improvements and stable/throughput rows.

Use returned percentages and deltas; never recompute them. State the returned evidence quality or limitations. `partial_errors` means successful evidence remains usable, but explicitly qualify conclusions; if all metric queries fail, the tool returns an error. Treat `stable`, `no_data`, and `unsupported_workload_shape` as terminal and do not automatically call follow-up tools.

`operation_apdex_reconciliations` reports current/baseline request coverage and `unexplained_delta` over reported coverage. It does not establish contribution, attribution, cause, or root cause.

## When to use
Equal-window APM comparison for a fleet or service.

## When not to use
- One-window fleet ranking: `get_service_summary`
- Trace cohorts: `get_trace_attribute_deviations`

## Pairs well with
- `get_change_events`
- `get_service_performance_details`

## Output shape
Outcome plus regressions, improvements, shifts, evidence, reconciliation, warnings and follow-ups.

## Known traps
Read `last9://reference/tool_recipes` before interpreting reconciliation or claiming cause.
