`tracejson_query`: JSON stage array, **NOT SQL**. Stages use `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`.

**Filter:** `[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]`
`query`: `$and`/`$or` of `$eq`/`$neq`/`$contains`/`$regex`/`$gt` `[field,value]`; string values, `$and`-wrap. No filter_tags/tags.

**Pattern:** wildcards like `checkout.*` → `$regex`. Trace ID substring → `$contains` on `TraceId`; exact ID → `get_service_traces`.

**Existence:** `{"$neq":["attributes['key']",""]}`; never `$exists`/`$notnull`.

**Scope:** `service.name`→`ServiceName`; tenant→`resources['last9.tenant']`; env→`resources['deployment.environment']`.

**Time:** `lookback_minutes` (default **60**); RFC3339 uses `start_time_iso`+`end_time_iso`, not Timestamp filters.

**Fields:** OTel enums: `SPAN_KIND_SERVER`, `STATUS_CODE_ERROR`; Duration is ns. Use `attributes['key']`/`resources['key']`, not `SpanAttributes.foo`.

**Aggregate:** use `aggregates`+`groupby`. Average/mean→`{"function":{"$avg":["Duration"]}}`, not `$quantile`. By span: `{"type":"aggregate","aggregates":[{"function":{"$avg":["Duration"]},"as":"avg"}],"groupby":{"SpanName":"span"}}`. `$quantile` is the general/default percentile operator. Compute from raw spans; never average percentile samples. `Duration` is numeric already; for `attributes[...]` percentiles, `$regex`-gate numeric values first.

**window_aggregate:** `{"type":"window_aggregate","function":{"$quantile":[0.99,"Duration"]},"as":"p99","window":["24","hours"],"groupby":{"SpanName":"endpoint"}}`.

For calendar buckets, use explicit ISO bounds and time zone. P99 `Duration` output remains nanoseconds.

**Order:** filter first (match-all TraceId/SpanId before aggregate). Show/find → filter only; analysis → aggregate/window_aggregate.

**Aggregate example:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`.

Manual: `last9://reference/tracejson`

## When to use
Span filters, counts, averages or percentiles across services with a tracejson pipeline.

## When not to use
- Exact trace ID: `get_service_traces`
- One trace's span tree: `get_trace_waterfall`
- Ranked exceptions: `get_exceptions`
- Change against a baseline: `get_apm_service_deviations`

## Pairs well with
- `get_trace_attributes` and `get_trace_attribute_values` (field names and values)
- `get_current_time` (absolute bounds)

## Output shape
Span rows for filter-only pipelines; aggregate rows when the pipeline aggregates.

## Known traps
Field values in filters are strings, enums included (`STATUS_CODE_ERROR`).
