## Tool: get_exceptions

Get server side exceptions aggregated over the given time range.
Returns exception type, service name, span name, occurrence count, first_seen, and last_seen timestamps.

IMPORTANT: `trace_id` is always null in this response. The data comes from aggregated metrics, not raw spans.

Investigation flow — follow this exactly:
1. Call `get_exceptions` to identify which service/exception_type is problematic.
2. Call `get_service_traces` with:
   - `service_name` = exception.service_name
   - `start_time_iso` = exception.first_seen
   - `end_time_iso` = exception.last_seen
   - `env` = exception.deployment_environment (if present)
   - If you somehow have a `trace_id`, use `get_service_traces` with `trace_id` instead of `service_name`. Never use `get_traces` for trace_id lookups.
3. STOP. Report findings to the user. Do NOT call `get_traces`, `get_service_logs`,
   or `get_logs` after this — those calls are unnecessary and will time out.

Parameters:
- `limit` (optional): Maximum number of exceptions to return. Defaults to 20.
- `lookback_minutes` (recommended): Minutes to look back from now. Default: 60.
- `start_time_iso` (optional): Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z).
- `end_time_iso` (optional): End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z).
- `service_name` (optional): Filter by service name (e.g. api-service).
- `span_name` (optional): Filter by span name (e.g. user_service).
- `deployment_environment` (optional): Filter by environment (e.g. production, staging).

Response fields per exception:
- `service_name`: Name of the service that threw the exception.
- `exception_type`: Type of the exception (e.g. NullPointerException).
- `span_name`: Name of the span where the exception occurred.
- `count`: Number of times this exception occurred in the time window.
- `first_seen`: RFC3339 timestamp of the earliest occurrence in the window.
- `last_seen`: RFC3339 timestamp of the most recent occurrence.
- `trace_id`: Always null (not available from aggregated metrics).

---

## Tool: get_service_traces

Use this tool to retrieve traces from Last9 either by exact `trace_id` or by `service_name`.

Parameters:
- `trace_id` (optional): Specific trace ID to retrieve. Cannot be used with `service_name`.
- `service_name` (optional): Name of the service to query. Cannot be used with `trace_id`.
- `lookback_minutes` (optional): Minutes to look back from now. Default is 4320 for `trace_id` lookups and 60 for `service_name` lookups.
- `start_time_iso` (optional): Start time in RFC3339 / ISO8601 format, for example `2026-02-09T15:04:05Z`.
- `end_time_iso` (optional): End time in RFC3339 / ISO8601 format, for example `2026-02-09T16:04:05Z`.
- `limit` (optional): Maximum number of traces to return. Default is 10.
- `env` (optional): Environment filter.

Rules:
- Exactly one of `trace_id` or `service_name` must be provided.
- Use `start_time_iso` and `end_time_iso` from the exception's `first_seen`/`last_seen` for targeted lookups.
- After getting results from `get_service_traces`, stop and report. Do NOT follow up with `get_traces` or `get_service_logs`.

---

## Tool: get_traces

Query distributed traces using a JSON pipeline for complex filtering, aggregation, and analytics.

Use `get_traces` ONLY when:
- You need to search across all services with complex filter conditions
- You need aggregations (count by service, p99 latency, etc.)
- You need to filter on specific span attributes or resource attributes

Do NOT use `get_traces` when:
- You already know the `service_name` — use `get_service_traces` instead
- You already know the `trace_id` — use `get_service_traces` instead
- You are navigating from `get_exceptions` results — use `get_service_traces` instead

Parameters:
- `tracejson_query` (required): JSON pipeline array. First stage must always be a `filter`.
- `lookback_minutes` (optional): Default 5.
- `start_time_iso` / `end_time_iso` (optional): Absolute time bounds.
- `limit` (optional): Default 5000.

Example:
```json
[{"type": "filter", "query": {"$and": [{"$eq": ["ServiceName", "api"]}, {"$eq": ["StatusCode", "STATUS_CODE_ERROR"]}]}}]
```
