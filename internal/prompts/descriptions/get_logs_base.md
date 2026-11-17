Query logs with JSON stages in `logjson_query`, never `pipeline`; this is NOT SQL. Stages are `filter`, `parse`, `aggregate`, and `window_aggregate` only. Use it for counts, percentiles, or ad-hoc filtering across services. Use `get_service_logs` for raw lines from one known service.

**Order:** filter→parse→filter→aggregate. Stage types: only filter/parse/aggregate/window_aggregate; parsed labels: attributes['key']. Scope before parsing. A filter stage is `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`. One predicate may stand alone; combine several with `$and`/`$or`/`$not`. Use `$neq` for “is not”, not `$not`+`$eq`. Regex is RE2. Parse Body-derived fields before using them; parsed labels are `attributes['key']`.

There is no `service_name` argument: filter `ServiceName`. Tenant and environment are `resources['last9.tenant']` and `resources['deployment.environment']`. Discover uncertain fields with `get_log_attributes_for_pipeline`. Bare fields include `ServiceName`, `Body`, `SeverityText`, and `Timestamp`.

`lookback_minutes` defaults to 5 and must be at least 1. For an absolute window set top-level RFC3339 `start_time_iso` and `end_time_iso`, never a Timestamp predicate. Body contains-family searches require explicit `lookback_minutes` or `start_time_iso`. Omit `response_mode`; use `raw` only for exact text. Raw output is capped at 32,000 bytes.

A count aggregate is `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`. A window stage uses `type`, `function`, `as`, and `window`, for example `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]}`. More aggregate syntax and percentile recipes live in `last9://reference/tool_recipes`. Read `last9://reference/logjson` before writing a parse stage, percentile, or unfamiliar filter. Preserve `l9_result.partial` and `l9_sanity` warnings.

Day-wise percentiles require one `get_logs` call over the full range with one `window_aggregate`; parse Body JSON before filtering or aggregating parsed fields. Never query each day separately or reconstruct percentiles client-side.

## When to use
Counts, aggregates, percentiles, or ad-hoc filters over logs.

## When not to use
- Raw lines for one service: `get_service_logs`
- Ranked exceptions: `get_exceptions`
- APM fleet ranking: `get_service_summary`

## Output shape
Line summary, raw rows, or aggregate rows with result and sanity metadata.
