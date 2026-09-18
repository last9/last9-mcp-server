`tracejson_query` is a JSON stage array. Each stage sets `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`. Prefer `get_service_traces` for exact trace_id/recent service traces.

**Filter shape:**
```json
[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]
```
`query` holds `$and`/`$or` of `{ "$eq"|"$neq"|"$contains"|"$regex"|"$gt"|…: [field,value] }`. Values are strings; always `$and`-wrap. Never SQL or filter_tags/tags.

**Pattern:** regex like `checkout.*` → `$regex`, not `$contains`.

**Existence:** `{"$neq":["attributes['key']",""]}`; never `$exists`/`$notnull`.

**Scope:** tenant name → `resources['last9.tenant']`; deployment env → `resources['deployment.environment']`.

**Time args:** `lookback_minutes` (default **60**); absolute RFC3339 uses `start_time_iso`+`end_time_iso`, never pipeline Timestamp filters.

**Fields:** TraceId, SpanId, ServiceName, SpanName, SpanKind, StatusCode, Duration, Timestamp, ParentSpanId. Enums need OTel prefixes (`SPAN_KIND_SERVER`, `STATUS_CODE_ERROR`). **Duration is nanoseconds** (1000ms=`1000000000`). Attributes use `attributes['key']`/`resources['key']`, never `SpanAttributes.foo`.

**Aggregate:** use `aggregates`+`groupby`. `$quantile` is the general/default percentile operator: `{"function":{"$quantile":[0.99,"Duration"]},"as":"p99"}`. Compute from raw spans; never average percentile samples. `Duration` is numeric already; for `attributes[...]` percentiles, `$regex`-gate numeric values first.

**window_aggregate:** `{"type":"window_aggregate","function":{"$quantile":[0.99,"Duration"]},"as":"p99","window":["24","hours"],"groupby":{"SpanName":"endpoint"}}`.

For calendar buckets, use explicit ISO bounds and time zone. P99 `Duration` output remains nanoseconds.

**Order:** filter first (match-all TraceId/SpanId before aggregate). Show/find → filter only; analysis → aggregate/window_aggregate.

Full manual: resource `last9://reference/tracejson`
