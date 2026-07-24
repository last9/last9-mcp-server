Query logs with `logjson_query` — JSON **array of stages**. Each stage `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`. No `"stage"`/`"conditions"`.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}` — `$eq|$neq|$contains|$gt|$gte|$lt|$lte|$regex` on `[field, value]` strings. Always `$and`-wrap.

**Parse / aggregate:** parse Body (`json`/`logfmt`/`regexp`), then filter `attributes['…']`. **Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"_count"}],"groupby":{"ServiceName":"service"}}` — `function` uses `{"$count":[]}`, `{"$max":["field"]}`, or `{"$avg":["field"]}`. **window_aggregate** for trends/per-minute counts — `aggregates`+`window_minutes`, not `TimeBucket`.

**Severity-less logs:** empty `SeverityText` → parse Body `level`, gate `$eq`/`$ieq` on `ERROR` before counting.

**Existence / attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Structured fields → `attributes['key']` — not Body `$contains`.

**Scope:** tenant → `resources['last9.tenant']`; env → `resources['deployment.environment']`. User `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs** (EPL_…) → `{"$contains":["Body","…"]}` — not `ServiceName`.

**HTTP 5xx:** filter status field — never `SeverityText`/`ERROR`.

**Time:** `lookback_minutes` (default **5**); absolute → `start_time_iso`+`end_time_iso` — never `Timestamp` in pipeline.

**l9_sanity** high ratio → re-call `get_logs` with ERROR gate.

Full manual: `last9://reference/logjson`