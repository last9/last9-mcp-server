Service performance metrics over a time range.

**Profile first:** Call `get_service_profile` for this service before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

Returns: service name, env, throughput (rpm), error rate (rpm, 4xx/5xx), error percentage, response times in seconds (p50, p90, p95, avg, max), apdex score, availability (%), top operations by response time and error rate, top errors/exceptions by count (10 of each by default, see `top_n`).

Use `get_service_operations_summary` for per-operation drill-down. Use for performance summaries, bottlenecks, and error overview.

Many metric fields use PromQL timeseries format: `[{"metric":{...},"values":[[timestamp_seconds,"value"]]}]`. Top operations/errors are dicts/lists of name→value.

Windows over 35 days are split into 35-day-or-narrower chunks queried and merged separately (the backend hard-caps a single query's range at 35 days); the requested window is hard-capped at 366 days total. On a wider (chunked) window, any sub-query failure is recorded in `partial_errors` and the rest of the data is still returned. On a plain (<=35 day) window, a non-2xx upstream response is likewise recorded in `partial_errors`; a read/parse failure on the response fails the whole call.

Counter-style fields (throughput, error rate) carry explicit zero values for intervals with no traffic, so a low-traffic service shows zeros rather than missing points. Each series is capped at about 200 points per chunk, so a wide window returns wide buckets, not a dense per-minute grid — expect most buckets to carry data. Quantile-style fields (response times) only cover intervals that actually had samples, so they can span far less than the requested window.

Parameters:
- `service_name`: (Required) Service to query.
- `env`: (Required) Environment. Use `get_service_environments` to list values.
- `lookback_minutes`: (Optional) Minutes to look back. Default 60.
- `start_time_iso` / `end_time_iso`: (Optional) RFC3339/ISO8601 bounds (e.g. `2026-02-09T15:04:05Z`). Override lookback when set; end defaults to now.
- `top_n`: (Optional) Max entries for `top_operations_by_response_time`, `top_operations_by_error_rate`, and `top_errors`. Default 10. Values above 100 clamp to 100.

If unsure of `service_name` or `env` spelling, call `did_you_mean` first.
