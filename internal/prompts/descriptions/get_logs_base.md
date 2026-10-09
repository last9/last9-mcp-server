`logjson_query`: JSON stages (`filter`|`parse`|`aggregate`|`window_aggregate`), **NOT SQL**/`stage`/`conditions`.

**Profile:** service→`get_service_profile`; route `signal_shape`/`telemetry`; stale→discovery. Unknown op/schema 400 → call `get_log_attributes_for_pipeline` with intended stages, retry.

**Order:** scope→parse→filter→aggregate.

**Filter:** `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`. Always wrap predicates in `$and` (even one), except OR/NOT. Ops: `$and`/`$or`/`$not`, `$eq`/`$neq`/`$gt`/`$gte`/`$lt`/`$lte`, `$containsWords`, `$regex`. Numeric threshold (e.g. response_time > 500) → `$gt`. Body words: ALL → `$and` of one `$containsWords` per word; ANY → `$or`; never `$icontainsWords`.

**Parse:** JSON `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; also logfmt/regexp, not format. Output: `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`; optional `groupby`. `$quantile` is the general/default percentile operator.

**window_aggregate:** `function`+`as`+`window` (not `aggregates`). Count: `{"function":{"$count":[]},"as":"count","window":["5","minutes"]}`. P99: `{"function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone. Parse, then use the canonical anchored numeric `$regex` shown: `^[0-9]+(?:\\.[0-9]+)?$`. It excludes non-matching values from percentile calculations; disclose that exclusion in the answer. Never template/merge/recombine aggregated percentile rows. Use a discovered normalized route. Raw URI: aggregate exact values only; never normalize/merge variants afterward. If `l9_result.partial=true`, preserve rows and disclose partial coverage. Report source units; never infer/convert.

**Attrs:** exists → neq(field,"") (never `$exists`). Dotted/single (`community_member_id`) → `attributes['key']`/`resources['key']`; only ServiceName/Body/SeverityText/Timestamp bare.

**Scope:** tenant/env → `resources['last9.tenant']`/`resources['deployment.environment']`; k8s.* → `resources['k8s.…']`. Every named service (incl. service.name) → `ServiceName` filter, even when grouping.

**Free-text IDs:** Use `ServiceName` only when user names a service; otherwise `$contains` the ID/token in Body, even `_api`/`_service`.

**HTTP 5xx:** known→`get_service_logs`; else `$eq` discovered status, not SeverityText.

**Time:** `lookback_minutes` default **5**; ISO uses `start_time_iso`+`end_time_iso`, not Timestamp filters.

**l9_sanity:** high/broad→ERROR re-count; zero→inspect samples.

Full manual: `last9://reference/logjson`
