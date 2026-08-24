`logjson_query` is a JSON stage array — **NOT SQL** or a query string. Each stage sets `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`.

**Order:** scope filter → parse before extracted attrs → filter parsed values → aggregate/window_aggregate. SpanKind/StatusCode/Duration/SpanName are trace-only.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}`. Operators: `$eq|$neq|$ieq|$contains|$containsWords|$gt|$gte|$lt|$lte|$regex`. Always `$and`-wrap; values are strings.

**Parse:** `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; parser is `json`/`logfmt`/`regexp`, never `"format"`. Parsed keys use `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99"}],"groupby":{"attributes['route']":"route"}}`. `$quantile` is the general/default percentile operator.

**window_aggregate:** use `function`+`as`+`window`, not `aggregates`/`TimeBucket`: `{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** compute P99 from raw values; never average P99 samples/series. Parse first, then `$regex`-gate numeric fields before aggregation. For calendar buckets use explicit ISO bounds and time zone; report source units and prefer normalized routes over raw URLs.

**Severity-less logs:** empty `SeverityText` → parse Body `level`, gate `$ieq` on `ERROR` before counting.

**Existence / attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Dotted or single-token (`community_member_id`) fields use `attributes['key']`/`resources['key']`; only ServiceName/Body/SeverityText/Timestamp may be bare.

**Scope:** tenant → `resources['last9.tenant']`; env → `resources['deployment.environment']`. User `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs** → `$contains` on Body, not ServiceName.

**HTTP 5xx:** known service → `get_service_logs`; otherwise `$eq` the discovered status field, never SeverityText.

**Time:** `lookback_minutes` defaults to **5**. Absolute ISO bounds use tool args `start_time_iso`+`end_time_iso`, never pipeline Timestamp filters.

**l9_sanity:** high ratio/broad-filter note → re-count with an ERROR gate. Zero matches → inspect sample_bodies before narrowing.

Full manual: `last9://reference/logjson`
