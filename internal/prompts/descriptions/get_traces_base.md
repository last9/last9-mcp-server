`tracejson_query`: JSON stages, **NOT SQL**. Each needs `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`. Use `get_service_traces` for exact traces.

**Filter shape:**
```json
[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]
```
`query`: `$and`/`$or` of `$eq`/`$neq`/`$contains`/`$regex`/`$gt` `[field,value]`; string values, `$and`-wrap. No filter_tags/tags.

**Pattern:** regex like `checkout.*` → `$regex`, not `$contains`.

**Existence:** `{"$neq":["attributes['key']",""]}`; never `$exists`/`$notnull`.

**Scope:** `service.name`→`ServiceName`; tenant→`resources['last9.tenant']`; env→`resources['deployment.environment']`.

**Time args:** `lookback_minutes` (default **60**); absolute RFC3339 uses `start_time_iso`+`end_time_iso`, not Timestamp filters.

**Fields:** OTel enum prefixes: `SPAN_KIND_SERVER`, `STATUS_CODE_ERROR`. Duration is nanoseconds (1000ms=`1000000000`). Attributes use `attributes['key']`/`resources['key']`, never `SpanAttributes.foo`.

**Aggregate:** use `aggregates`+`groupby`. Average/mean→`{"function":{"$avg":["Duration"]}}`, never `$quantile`. `$quantile` is the general/default percentile operator: `{"function":{"$quantile":[0.99,"Duration"]},"as":"p99"}`. Compute from raw spans; never average percentile samples. `Duration` is numeric already; for `attributes[...]` percentiles, `$regex`-gate numeric values first.

**window_aggregate:** `{"type":"window_aggregate","function":{"$quantile":[0.99,"Duration"]},"as":"p99","window":["24","hours"],"groupby":{"SpanName":"endpoint"}}`.

**Aggregate example:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`.

For calendar buckets, use explicit ISO bounds and time zone. P99 `Duration` output remains nanoseconds.

**Order:** filter first (match-all TraceId/SpanId before aggregate). Show/find → filter only; analysis → aggregate/window_aggregate.

Full manual: resource `last9://reference/tracejson`
