Query distributed traces across all services using trace JSON pipeline queries.

This tool provides comprehensive access to trace data for debugging performance issues, understanding request flows,
and analyzing distributed system behavior. It accepts raw JSON pipeline queries for maximum flexibility.

Use this tool for broad trace searches, analytics, and aggregations. For an exact trace ID lookup, prefer
the `get_service_traces` tool with `trace_id` because it avoids the slower chunked query path.

The tool uses a pipeline-based query system similar to the logs API, allowing complex filtering and aggregation
operations on trace data.

Parameters:
- tracejson_query: (Required) JSON pipeline query for traces. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language
- start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)
- end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)
- lookback_minutes: (Optional) Number of minutes to look back from current time (default: 60)
- limit: (Optional) Maximum number of traces to return (default: 5000)

Time format rules:
- Prefer lookback_minutes for relative windows (for example, last 5 or 60 minutes).
- Use start_time_iso/end_time_iso for absolute windows.
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
- If both lookback_minutes and absolute times are provided, absolute times take precedence.

Returns comprehensive trace data including trace IDs, spans, durations, timestamps, and metadata.

IMPORTANT: There is no "filter_tags", "tags", or "attributes" parameter. ALL filtering — including by span tags,
attributes, session IDs, trace metadata, or any key-value pair — must be expressed as a tracejson_query filter.
Do NOT invent parameter names; use tracejson_query exclusively for filtering.

Example tracejson_query structures:
- Simple filter: [{"type": "filter", "query": {"$eq": ["ServiceName", "api"]}}]
- Multiple conditions: [{"type": "filter", "query": {"$and": [{"$eq": ["ServiceName", "api"]}, {"$eq": ["StatusCode", "STATUS_CODE_ERROR"]}]}}]
- Filter by span tag/attribute: [{"type": "filter", "query": {"$eq": ["dd_session_id", "abc123"]}}]