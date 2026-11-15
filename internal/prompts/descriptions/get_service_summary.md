Rank APM server-span services over one window. Each row is one `(service, env)` with interval `request_count`, `throughput_rpm`, `http_4xx_count`, `http_5xx_count`, and `grpc_error_count`. Cite returned rows only.

`sort_by` defaults to `request_count`; use `http_5xx_count`, `http_4xx_count`, `grpc_error_count`, or `throughput_rpm` for those explicit questions. Unknown keys are rejected. `limit` defaults to 20, maximum 100. `env` is a PromQL regex (default `.*`); anchor an exact value as `^prod$`.

Prefer `lookback_minutes` (default 60). Both RFC3339 `start_time_iso` and `end_time_iso` define an absolute interval; one bound fills the other using lookback. Counts are interval totals; rpm is count/window minutes. Never add HTTP and gRPC classes. Missing classes are zero; log-only services are absent. Multiple source series are summed.

## When to use
Fleet ranking by volume, rpm, HTTP 4xx/5xx, or gRPC errors.

## When not to use
- Exceptions: `get_exceptions`
- Log-only services: `get_logs`
- Baseline comparison: `get_apm_service_deviations`
- One service's charts: `get_service_performance_details`

## Pairs well with
- `get_service_environments`
- `get_service_performance_details`

## Output shape
Ranked `rows`, bounds, active sort/unit, queried window, scope, coverage, fingerprint and empty-result hint.

## Known traps
Read `last9://reference/tool_recipes` for language-to-sort routing and truncation handling.
