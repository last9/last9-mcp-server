Compare APM performance across a current window and an equal-duration baseline window. Use this tool for questions such as what regressed, improved, or changed; incident-versus-prior-period comparisons; recovery or post-mitigation verification; and fleet deviation discovery.

**Profile first:** Call `get_service_profile` for this service before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

Use `get_service_summary` for a one-window fleet ranking of interval request count, rpm, HTTP 4xx/5xx, and gRPC errors. Use `get_apm_service_deviations` when the question requires an equal-duration baseline comparison.

For a comparative question, call this tool first and by itself. Do not batch speculative corroboration or duplicate comparison calls. Inspect the returned outcome and evidence before deciding whether the user explicitly requested any deeper investigation.

## Scope and inputs

- Omit `service_name` for fleet scope. Provide `service_name` for one service and its operation correlations. Environments remain separate and are never merged; optionally use `env` to select one environment.
- V1 supports server-request workloads. A named non-server workload may return `unsupported_workload_shape`.
- The current window defaults to the last 60 minutes. Set `lookback_minutes`, or provide `start_time_iso` and `end_time_iso` for an explicit current window.
- Short lookbacks are unreliable: the resolver keeps only fully-completed 1-minute buckets, so integer `lookback_minutes` below 2 returns a "no completed buckets" error in production (the current time is essentially never minute-aligned), and a lookback below about 5 typically returns `insufficient_evidence` because deviation classification needs at least four aligned buckets.
- The baseline defaults to the immediately preceding equal-duration period. To compare another equal-duration period, provide both `baseline_start_time_iso` and `baseline_end_time_iso`.
- `datasource` optionally selects one datasource for the comparison. Do not combine data across datasources in one call.
- `max_services` and `max_operations` each default to 10 and cannot exceed 10.

## Interpreting results

- `regressions` and `improvements` are separate. Throughput movement is reported as a contextual shift, not inherently as good or bad.
- Telemetry changes identify identities present in only one window.
- Evidence quality is categorical and reflects data coverage. When reporting a material deviation, state the returned evidence quality or limitations. A stable result has empty deviation leaderboards; do not manufacture a change when none is returned.
- Treat `stable`, `no_data`, and `unsupported_workload_shape` as terminal comparison outcomes. Answer from that result and do not automatically call follow-up tools unless the user explicitly requested a deeper investigation. Terminal outcomes return an empty `recommended_followups`.
- `partial_errors` or warnings mean successful evidence remains usable, but explicitly qualify conclusions with the missing evidence. If all metric queries fail, the tool returns an error rather than a partial result.
- `operation_apdex_reconciliations` mathematically decomposes the service Apdex delta across comparable returned operations. State `current_request_coverage` and `baseline_request_coverage`; treat `observed_operation_delta` as explained only over that reported coverage. `unexplained_delta` is the residual from unreported operations and metric-population differences, not evidence of an unknown cause.
- Operation correlations and structured, non-executing follow-ups help narrow an investigation. Correlation is supporting evidence only and does not establish contribution, attribution, cause, or root cause. Corroborate conclusions with traces, logs, and change events.
