`logjson_query`: JSON stages (`filter`|`parse`|`aggregate`|`window_aggregate`), **NOT SQL**/`stage`/`conditions`.

**Profile:** service→`get_service_profile`; `signal_shape`/`telemetry`. Stale→discovery; 400→`get_log_attributes_for_pipeline`, retry.

**Order:** scope→parse→filter→group/aggregate.

**Filter:** `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`. Always wrap predicates in `$and` (even one), except OR/NOT. Ops: `$and`/`$or`/`$not`, `$eq`/`$neq`/`$gt`/`$gte`/`$lt`/`$lte`, `$containsWords`, `$regex`. Body words: ALL → `$and` of one `$containsWords` per word; ANY → `$or`; never `$icontainsWords`.

**Parse:** JSON `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; also logfmt/regexp. Parse Body-derived fields before filtering, grouping, or aggregation. `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`; optional `groupby`. `$quantile` is the general/default percentile operator.

**window_aggregate:** type+function+as+window (not `aggregates`). Count: `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]}`. P99: `{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone. Parse, then use the canonical anchored numeric `$regex` shown: `^[0-9]+(?:\\.[0-9]+)?$`. It excludes non-matching values from percentile calculations; disclose that exclusion in the answer. Report a P99 only if a successful aggregate returns it; otherwise say unavailable. Never template/merge/recombine aggregated percentile rows. Use a discovered normalized route. Raw URI: aggregate exact values only; never normalize/merge variants afterward. If `l9_result.partial=true`, preserve rows and disclose partial coverage. Report source units; never infer/convert.

**Attrs:** exists→neq(field,"") (not `$exists`). Dotted (`community_member_id`)→`attributes['key']`/`resources['key']`; bare: ServiceName/Body/SeverityText/Timestamp.

**Scope:** tenant/env→`resources['last9.tenant']`/`resources['deployment.environment']`; k8s.*→`resources['k8s.…']`; service (incl. service.name)→`ServiceName`.

**Free-text IDs:** named→`ServiceName`; otherwise `$contains` ID/token in Body (incl. `_api`/`_service`).

**HTTP 5xx:** known→`get_service_logs`; else discover status→`$eq` (not SeverityText).

**Time:** `lookback_minutes` default **5**; absolute→`start_time_iso`+`end_time_iso` (not Timestamp).

**l9_sanity:** high/broad→ERROR re-count; zero→inspect samples.

Manual: `last9://reference/logjson`
