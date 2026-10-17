`tracejson_query`: JSON stage array, **NOT SQL**. Stages use `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`.

**Filter shape:**
```json
[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]
```
`query` uses `$and`/`$or` of operator:[field,value] pairs; values are strings. Always `$and`-wrap.

**Pattern:** wildcards like `checkout.*` → `$regex`. Trace ID substring → `$contains` on `TraceId`; exact ID → `get_service_traces`.

**Existence:** `{"$neq":["attributes['key']",""]}`; never `$exists`.

**Scope:** `service.name`→`ServiceName`; tenant→`resources['last9.tenant']`; env→`resources['deployment.environment']`.

**Time args:** `lookback_minutes` (default **60**); RFC3339 uses `start_time_iso`+`end_time_iso`, never pipeline Timestamp.

**Fields:** OTel enums: `SPAN_KIND_SERVER`, `STATUS_CODE_ERROR`; Duration is ns. Use `attributes['key']`/`resources['key']`, not `SpanAttributes.foo`.

**Aggregate:** use `aggregates`+`groupby`. `$quantile` is the general/default percentile operator. Compute from raw spans; never average percentile samples. `Duration` is numeric already; for `attributes[...]` percentiles, `$regex`-gate numeric values first. Lookback / overall percentiles use `aggregate` with `$quantile` (not `window_aggregate`): `{"type":"aggregate","aggregates":[{"function":{"$quantile":[0.99,"Duration"]},"as":"p99"}],"groupby":{"SpanName":"span"}}`.

**window_aggregate:** rates/over-time only: `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"],"groupby":{"SpanName":"endpoint"}}`.

For calendar buckets, use explicit ISO bounds and time zone. P99 `Duration` output remains nanoseconds.

**Order:** filter first (before aggregate). Find → filter; analysis → aggregate/window_aggregate.

**Aggregate example:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`.

Full manual: resource `last9://reference/tracejson`
