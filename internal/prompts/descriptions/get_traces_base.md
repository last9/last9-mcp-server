Query traces with `tracejson_query` — a JSON **array of stages**. Each stage MUST set `"type"` to `filter`|`parse`|`aggregate`|`window_aggregate`. Do **not** use `"stage"` or `"conditions"`. For an exact `trace_id`, prefer `get_service_traces`.

**Filter shape:**
```json
[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]
```
`query` holds `$and`/`$or` of `{ "$eq"|"$neq"|"$contains"|"$exists"|"$gt"|…: [field, value] }`. Values are strings. Always wrap in `$and`. Never invent SQL. No `filter_tags`/`tags` params—filter in the pipeline only.

**Time (tool args):** Prefer `lookback_minutes` (default **5**, not 60). Absolute → `start_time_iso`+`end_time_iso` (RFC3339). Never put the window as a `Timestamp` filter in the pipeline.

**Fields:** Top-level: `TraceId`, `SpanId`, `ServiceName`, `SpanName`, `SpanKind`, `StatusCode`, `Duration`, `Timestamp`, `ParentSpanId`. SpanKind/StatusCode need full OTel prefixes (`SPAN_KIND_SERVER`, `STATUS_CODE_ERROR`—not `SERVER`/`ERROR`). **`Duration` is nanoseconds** (1000ms = `1000000000`). Span/resource attrs → `get_trace_attributes` / `get_trace_attributes_for_pipeline`; use `attributes['key']` / `resources['key']` (never `SpanAttributes.foo`).

**Order:** filter first (match-all on `TraceId`/`SpanId` if needed before aggregate). Show/find → filter only; aggregate/window_aggregate only for counts/analysis.

Full manual: resource `last9://reference/tracejson`