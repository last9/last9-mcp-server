Service performance metrics over a time range.

**Profile first:** Call `get_service_profile` for this service before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

Returns: service name, env, throughput (rpm), error rate (rpm, 4xx/5xx), error percentage, response times in seconds (p50, p90, p95, avg, max), apdex score, availability (%), top operations by response time and error rate, top errors/exceptions by count (see `top_n`).

By default each metric is summarized per series: `points`, `missing_points` (no-traffic availability buckets, excluded), `min`, `p50`, `p90`, `p99` (nearest-rank), `max`, `mean`, `last`, `baseline_mean` (first half of valid points), `delta_pct` (second half vs baseline; absent if 0) and up to 3 `change_points` (`timestamp`, `before_mean`, `after_mean`). Top 5 series per metric by mean; `series_omitted` counts the rest.

Use `get_service_operations_summary` for per-operation drill-down. Use for performance summaries, bottlenecks, and error overview.

`include_series=true` returns timeseries `[{"metric":{...},"values":[{"timestamp":seconds,"value":n}]}]`. Top operations/errors are name→value lists.

Windows over 35 days are queried in 35-day chunks and merged; the window is capped at 366 days. Sub-query failures land in `partial_errors` and the rest is still returned, except a read/parse failure on a <=35 day window, which fails the call.

Counter-style fields (throughput, error rate) carry explicit zeros for idle intervals; response times only cover intervals with samples. Each series is capped at about 200 points per chunk.

Parameters:
- `service_name`: (Required) Service to query.
- `env`: (Optional) Exact environment name, e.g. production. Use `get_service_environments` to list values. Default all environments (empty or .*). Anchored ^production$ is accepted and matched exactly; alternation (a|b) and wildcards (prod.*) are rejected. For regex matching use get_service_summary. The response echoes the matched `env`, plus `env_requested` when it differed.
- `lookback_minutes`: (Optional) Minutes to look back. Default 60.
- `start_time_iso` / `end_time_iso`: (Optional) RFC3339/ISO8601 bounds (e.g. `2026-02-09T15:04:05Z`). Override lookback when set; end defaults to now.
- `top_n`: (Optional) Max entries for `top_operations_by_response_time`, `top_operations_by_error_rate`, and `top_errors`. Default 5, or 10 with `include_series=true`. Values above 100 clamp to 100.
- `include_series`: (Optional) Return every point of every series instead of the summary. Default false (hundreds of KB).

If unsure of `service_name` or `env` spelling, call `did_you_mean` first.
