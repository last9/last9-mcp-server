`logjson_query`: JSON stages (`filter`|`parse`|`aggregate`|`window_aggregate`), **NOT SQL**/`stage`/`conditions`.

**Profile:** service→`get_service_profile`; `signal_shape`/`telemetry`. Stale→discovery; 400→`get_log_attributes_for_pipeline`, retry.

**Order:** scope→parse→filter→group/aggregate.

**Filter:** `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`. Predicates: one standalone, many in `$and`. Ops: `$and`/`$or`/`$not`, `$eq`/`$neq` (is not X; never `$not`+`$eq`)/`$gt`/`$gte`/`$lt`/`$lte`, `$regex`. Body words: `$containsWords` args `[field,word]` (e.g. `["Body","timeout"]`); ALL→`$and`; ANY→`$or`; never `$icontainsWords`.

**Parse:** JSON `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}`; also logfmt/regexp. Parse Body-derived fields before filtering, grouping, or aggregation. `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"count"}]}`; optional `groupby`. `$quantile` is the general/default percentile operator.

**window_aggregate:** type+function+as+window (not `aggregates`). Count: `{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]}`. P99: `{"type":"window_aggregate","function":{"$quantile":[0.99,"attributes['latency_ms']"]},"as":"p99","window":["24","hours"],"groupby":{"attributes['route']":"route"}}`.

**Percentiles:** Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone. Parse, then use the canonical anchored numeric `$regex` shown: `^[0-9]+(?:\\.[0-9]+)?$`. It excludes non-matching values from percentile calculations; disclose that exclusion in the answer. Only report returned P99 values; empty result→say unavailable and stop (no count/sample substitute). Never template/merge/recombine aggregated percentile rows. Use a discovered normalized route. Raw URI: aggregate exact values; never normalize/merge later. If `envelope.truncated`, preserve rows and disclose the row limit. Units: report source; never convert.

**Attrs:** exists→neq(field,"") (not `$exists`). Dotted (`community_member_id`)→`attributes['key']`/`resources['key']`; bare: ServiceName/Body/SeverityText/Timestamp.

**Scope:** no `service_name` arg; filter ServiceName in query. tenant/env→`resources['last9.tenant']`/`resources['deployment.environment']`; k8s.*→`resources['k8s.…']`; service.name→`ServiceName`.

**Free-text:** no service→search `Body` with `$contains` (even service-like); else `ServiceName`.

**HTTP counts:** `get_logs`; status from Body, not SeverityText. `get_service_logs` for lines.

**Time:** `lookback_minutes` default **5**; absolute→`start_time_iso`+`end_time_iso` (not Timestamp).

**l9_sanity:** high→add filter, recount; zero→inspect samples.

**Envelope:** `truncated`, `trimmed_items`, `partial`: disclose; follow `hints`.

Manual: `last9://reference/logjson`

## When to use
Counts, aggregates, percentiles or ad-hoc filters over logs, across services or with no known service.

## When not to use
- Raw lines for one known service: `get_service_logs`
- Ranked exceptions: `get_exceptions`
- APM fleet error ranking: `get_service_summary`

## Output shape
Log rows or aggregate rows, with `l9_result` / `l9_sanity` metadata on partial or suspicious counts.
