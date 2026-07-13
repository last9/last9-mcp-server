Compare APM performance across a current window and an equal-duration baseline window. Use this tool for questions such as what regressed, improved, or changed; incident-versus-prior-period comparisons; recovery or post-mitigation verification; and fleet deviation discovery.

Use `get_service_summary` for a one-window snapshot of current performance. Use `get_apm_service_deviations` when the question requires an equal-duration baseline comparison.

For a comparative question, call this tool first and by itself. Do not batch speculative corroboration or duplicate comparison calls. Inspect the returned outcome and evidence before deciding whether the user explicitly requested any deeper investigation.

## Scope and inputs

- Omit `service_name` for fleet scope. Provide `service_name` for one service and its operation correlations. Environments remain separate and are never merged; optionally use `env` to select one environment.
- V1 supports server-request workloads. A named non-server workload may return `unsupported_workload_shape`.
- The current window defaults to the last 60 minutes. Set `lookback_minutes`, or provide `start_time_iso` and `end_time_iso` for an explicit current window.
- The baseline defaults to the immediately preceding equal-duration period. To compare another equal-duration period, provide both `baseline_start_time_iso` and `baseline_end_time_iso`.
- `datasource` optionally selects one datasource for the comparison. Do not combine data across datasources in one call.
- `max_services` and `max_operations` each default to 10 and cannot exceed 10.

## Interpreting results

- `regressions` and `improvements` are separate. Throughput movement is reported as a contextual shift, not inherently as good or bad.
- Telemetry changes identify identities present in only one window.
- Evidence quality is categorical and reflects data coverage. When reporting a material deviation, state the returned evidence quality or limitations. A stable result has empty deviation leaderboards; do not manufacture a change when none is returned.
- Treat `stable`, `no_data`, and `unsupported_workload_shape` as terminal comparison outcomes. Answer from that result and do not automatically call follow-up tools unless the user explicitly requested a deeper investigation.
- `partial_errors` or warnings mean successful evidence remains usable, but explicitly qualify conclusions with the missing evidence. If all metric queries fail, the tool returns an error rather than a partial result.
- Operation correlations and structured, non-executing follow-ups help narrow an investigation. Correlation is supporting evidence only and does not establish contribution, attribution, cause, or root cause. Corroborate conclusions with traces, logs, and change events.
