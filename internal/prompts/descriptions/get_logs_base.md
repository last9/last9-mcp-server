Query logs with `logjson_query` — a JSON **array of stages**. Each stage MUST set `"type"` to `filter`|`parse`|`aggregate`|`window_aggregate`. Do **not** use `"stage"` or `"conditions"`.

**Filter shape:**
```json
[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]
```
`query` holds `$and`/`$or` of `{ "$eq"|"$neq"|"$contains"|"$exists"|"$gt"|"$gte"|"$lt"|"$lte": [field, value] }`. Values are strings. Always wrap in `$and` (use `$or` for service/canary variants). Never invent SQL.

**Parse / aggregate:** `{"type":"parse","parser":"json","field":"Body","labels":{}}` (or `logfmt`/`regexp`). Aggregate: `{"type":"aggregate","aggregates":[{"function":{"$count":[]},"as":"_count"}],"groupby":{"ServiceName":"service"}}`. Prefer **json** parse for access logs; afterward filter/groupby with `attributes['uri']` / `attributes['status_code']` (use `attributes['...']`, not bare names).

**HTTP access logs (critical):** Severity is NOT an HTTP-error proxy (5xx often INFO). Filter the status field — e.g. `$gte`/`$lt` on `attributes['status_code']` or `attributes['http.status_code']` for 500–599 — never `SeverityText`/`ERROR` for HTTP 5xx. Paths/URIs are usually `attributes['uri']` after a Body json parse. Discover exact keys with `get_log_attributes*`.

**Time (tool args):** Prefer `lookback_minutes` (default **5**, not 60). Absolute → `start_time_iso`+`end_time_iso`. Never put the window as a pipeline `Timestamp` filter.

**Fields:** Without discovery only `ServiceName`, `Body`, `Timestamp`, `SeverityText`. Free-text IDs → Body `$contains`. Org attrs → discovery `filter_field` (`attributes['key']` / `resources['key']`).

**Order:** scope filter first → parse before Body-derived filters/groupby → aggregate only for count/sum/avg/trend.

Full manual: resource `last9://reference/logjson`