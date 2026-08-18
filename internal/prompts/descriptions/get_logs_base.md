`logjson_query` is a JSON **array of stages** — NOT SQL, NOT a query string. Each stage sets `"type"`: `filter`|`parse`|`aggregate`|`window_aggregate`. No `"stage"`/`"conditions"`.

**Order rule:** filter first → parse (before using extracted attrs) → aggregate or window_aggregate only for counts/trends. Never use SpanKind/StatusCode/Duration/SpanName as log filters.

**Filter:** `{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}` — operators: `$eq|$neq|$ieq|$contains|$containsWords|$gt|$gte|$lt|$lte|$regex`. Always `$and`-wrap. Values are strings.

**Parse:** `{"type":"parse","parser":"json","field":"Body","labels":{"key":"key"}}` — `parser` is `json`/`logfmt`/`regexp` (never `"format"`). Parse Body first; then filter on `attributes['key']`.

**Aggregate:** `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"_count"}],"groupby":{"ServiceName":"service"}}` — `aggregates` (plural), `function` as object.

**window_aggregate** for per-minute/trend counts — use `function`+`as`+`window` (NOT the aggregate stage, NOT `TimeBucket`):
`[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}},{"type":"window_aggregate","function":{"$count":[]},"as":"errors","window":["1","minutes"]}]`

**Severity-less logs:** empty `SeverityText` → parse Body `level`, gate `$ieq` on `ERROR` before counting.

**Existence / attrs:** exists → `{"$neq":["field",""]}` (never `$exists`). Never bare field names — dotted (`http.status_code`) OR single-token (`community_member_id`) — wrap as `attributes['key']`/`resources['key']`. Only top-level `ServiceName`/`Body`/`SeverityText`/`Timestamp` may be bare.

**Scope:** tenant → `resources['last9.tenant']`; env → `resources['deployment.environment']`. User `service.name` → `ServiceName`; `k8s.*` → `resources['k8s.…']`.

**Free-text IDs** (EPL_…) → `{"$contains":["Body","…"]}` — not `ServiceName`.

**HTTP 5xx:** known service → `get_service_logs` (`http_status_class`/`http_status_code`). Ad-hoc: `$eq` on discovered status field — never `SeverityText`.

**Time:** `lookback_minutes` (default **5**). **Absolute ISO bounds** → `start_time_iso`+`end_time_iso` on the tool call — never `Timestamp`/`$gte`/`$lte` in the pipeline.

**l9_sanity:** high `ratio` or "filter likely too broad" → next call `get_logs` with `aggregate`+`$count` and an ERROR/`SeverityText` gate. `matched_count: 0`: check `sample_bodies`, don't narrow.

Full manual: `last9://reference/logjson`