For raw lines from one service use `get_service_logs`.

Pass JSON stages in `logjson_query`, never `pipeline`; this is NOT SQL.

**Order:** filter→parse→filter→aggregate. Stage types: only filter/parse/aggregate/window_aggregate; parsed labels: attributes['key']. Scope before parsing. Example: `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`. Use `$neq` for “is not”. A window stage uses `{"type":"window_aggregate","function":{"$count":[]},"as":"name","window":["5","minutes"]}`.

There is no `service_name` argument: filter `ServiceName`. Tenant and environment are `resources['last9.tenant']` and `resources['deployment.environment']`. For Body-derived fields, call `get_log_attributes_for_pipeline` before querying; never use the global catalog or guess.

`lookback_minutes` defaults to 5 and has minimum 1. For an absolute window set top-level RFC3339 `start_time_iso` and `end_time_iso`, never a Timestamp predicate. Body contains-family searches require explicit `lookback_minutes` or `start_time_iso`. Omit `response_mode`; use `raw` only for exact text.

A count is `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`. For other syntax read `last9://reference/logjson`; recipes are in `last9://reference/tool_recipes`. Preserve `l9_result.partial` and `l9_sanity` warnings.

Day-wise percentiles require one `get_logs` call over the full range with one `window_aggregate`; parse Body JSON before filtering or aggregating parsed fields. Use `[{"type":"parse","parser":"json","field":"Body","labels":{"latency":"latency"}},{"type":"filter","query":{"$regex":["attributes['latency']","^[0-9]+(?:\\.[0-9]+)?$"]}},{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency']"]},"as":"p99","window":["24","hours"]}]`. Never query each day separately or reconstruct percentiles client-side. If no percentile rows return, report unavailable and stop; never substitute counts, samples, traces, or metrics.

## When to use
Counts, aggregates, percentiles, or ad-hoc filters over logs.

## When not to use
- Raw lines for one service: `get_service_logs`
- Ranked exceptions: `get_exceptions`
- APM fleet ranking: `get_service_summary`

## Output shape
Line summary, raw rows, or aggregate rows with result and sanity metadata.
