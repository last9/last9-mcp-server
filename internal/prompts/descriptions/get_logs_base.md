`logjson_query`: JSON stage array, **NOT SQL**. Types: `filter`|`parse`|`aggregate`|`window_aggregate`; no `"stage"`/`"conditions"`.

**Order:** scope→parse→filter→aggregate.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}`. Ops: `$and`/`$or`/`$not`; `$eq`/`$neq`; `$containsWords`; `$regex`. Body words: ALL → `$and` of one `$containsWords` per word; ANY → `$or`; never `$icontainsWords`.

**Parse:** `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; also `logfmt`/`regexp`, not `"format"`. Outputs use `attributes['key']`.

**Aggregate:** `aggregates` entries use `function`+`as`; optional `groupby`. `$quantile` is the general/default percentile operator.

**window_aggregate:** `function`+`as`+`window`, not `aggregates`/`TimeBucket`. Count: `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]}`. P99: `{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone. Parse, then use the canonical anchored numeric `$regex` shown: `^[0-9]+(?:\\.[0-9]+)?$`. It excludes non-matching values from percentile calculations; disclose that exclusion in the answer. Never template/merge/recombine aggregated percentile rows. Use a discovered normalized route. Raw URI: aggregate exact values only; never normalize/merge variants afterward. If `l9_result.partial=true`, preserve rows and disclose partial coverage. Report source units; never infer/convert.

**Severity-less:** empty `SeverityText` → parse Body `level`; `$ieq` `ERROR`.

**Attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Dotted/single-token (`community_member_id`) use `attributes['key']`/`resources['key']`; only ServiceName/Body/SeverityText/Timestamp may be bare.

**Scope:** tenant → `resources['last9.tenant']`; env → `resources['deployment.environment']`; `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs:** `$contains` Body, never ServiceName.

**HTTP 5xx:** known→`get_service_logs`; else `$eq` discovered status, never SeverityText.

**Time:** `lookback_minutes` default **5**. ISO args: `start_time_iso`+`end_time_iso`, not Timestamp filters.

**l9_sanity:** high ratio/broad filter→ERROR re-count; zero→inspect samples.

Full manual: `last9://reference/logjson`
