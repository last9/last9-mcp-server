Query logs with `logjson_query` — JSON **array of stages**. Each stage `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`. No `"stage"`/`"conditions"`.

**Profile first (service-scoped):** When the query filters a known service (`ServiceName` / `service.name`), call `get_service_profile` → use `signal_shape`/`telemetry`; `last9://reference/investigation`.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}` — `$eq|$neq|$contains|$gt|$gte|$lt|$lte|$regex` on `[field, value]` strings. Always `$and`-wrap.

**Parse / aggregate:** parse Body (`json`/`logfmt`/`regexp`), then filter `attributes['…']`. **Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"_count"}],"groupby":{"ServiceName":"service"}}` — `function` uses `{"$count":[]}`, `{"$max":["field"]}`, or `{"$avg":["field"]}`. **window_aggregate** for trends/per-minute counts — `aggregates`+`window_minutes`, not `TimeBucket`.

**Severity-less logs:** empty `SeverityText` → parse Body `level`, gate `$eq`/`$ieq` on `ERROR` before counting.

**Existence / attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Structured fields → `attributes['key']` — not Body `$contains`.

**Scope:** tenant/env → `resources['last9.tenant']`/`resources['deployment.environment']`; `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs** (EPL_…) → `{"$contains":["Body","…"]}` — not `ServiceName`.

**HTTP 5xx:** filter status field with literal code (e.g. `$eq` on `attributes['status_code']`,`"500"`) — never `SeverityText`/`ERROR`; avoid regex-only when user names a code.

**Time:** `lookback_minutes` (default **5**). Absolute bounds → `start_time_iso`+`end_time_iso` on the tool — never `Timestamp`/`$gte`/`$lte` in pipeline.

**l9_sanity:** high `ratio` or "filter likely too broad" → `get_logs` aggregate `$count` + ERROR gate — not discovery-only.

Full manual: `last9://reference/logjson`