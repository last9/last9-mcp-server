Get top query patterns for a specific database, aggregated by operation.

Shows the most active and slowest query patterns hitting a database, grouped by span_name
(which typically contains the SQL operation or query fingerprint). For each pattern, returns
throughput (calls/min), average latency, p95 latency, and error rate.

This is useful for identifying:
- Hot queries (high throughput) that dominate database load
- Slow query patterns (high p95 latency) that need optimization
- Failing queries (high error rate) that indicate bugs or schema issues

Parameters:
- db_system: (Required) Database system (e.g. "postgresql", "mysql", "mongodb", "redis").
- host: (Optional) Database host to filter by (net_peer_name).
- env: (Optional) Deployment environment filter.
- lookback_minutes: (Optional) Time window in minutes (default: 60).
- start_time_iso: (Optional) Start time in RFC3339 format.
- end_time_iso: (Optional) End time in RFC3339 format.
- sort_by: (Optional) Sort by "throughput" (default), "latency", or "errors".
- If unsure of the db_system or host spelling, call "did_you_mean" first.