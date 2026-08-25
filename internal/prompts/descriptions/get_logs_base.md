`logjson_query`: JSON stage array, **NOT SQL**. `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`.

**Order:** scope→parse→filter→aggregate. SpanKind/StatusCode/Duration/SpanName are trace-only.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}`. `$and`-wrap; string values. Body words: ALL → `$and` of one `$containsWords` per word; ANY → `$or`; never `$icontainsWords`.

**Parse:** `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; `json`/`logfmt`/`regexp`, not `"format"`. Parsed keys use `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99"}],"groupby":{"attributes['route']":"route"}}`. `$quantile` is the general/default percentile operator.

**window_aggregate:** `function`+`as`+`window`, not `aggregates`/`TimeBucket`: `{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone. Parse, then use the canonical anchored numeric `$regex` shown: `^[0-9]+(?:\\.[0-9]+)?$`. Never template/merge/recombine aggregated percentile rows. Use a discovered normalized route. Raw URI: aggregate exact values only; never normalize/merge variants afterward. If `l9_result.partial=true`, preserve rows and disclose partial coverage. Report source units; never infer/convert.

**Severity-less:** empty `SeverityText` → parse Body `level`, gate `$ieq` on `ERROR`.

**Attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Dotted/single-token (`community_member_id`) fields use `attributes['key']`/`resources['key']`; only ServiceName/Body/SeverityText/Timestamp may be bare.

**Scope:** tenant → `resources['last9.tenant']`; env → `resources['deployment.environment']`. User `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs:** `$contains` Body, not ServiceName.

**HTTP 5xx:** known service → `get_service_logs`; otherwise `$eq` the discovered status field, never SeverityText.

**Time:** `lookback_minutes` default **5**. ISO bounds: tool args `start_time_iso`+`end_time_iso`; no pipeline Timestamp filters.

**l9_sanity:** high ratio/broad filter → re-count with ERROR. Zero → inspect sample_bodies before narrowing.

Full manual: `last9://reference/logjson`
