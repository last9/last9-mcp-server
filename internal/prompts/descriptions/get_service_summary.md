Rank APM server-span services over one time window. Each row is one (service, env) with interval request_count, throughput_rpm, http_4xx_count, http_5xx_count, and grpc_error_count. The server sorts, limits, and stamps rank. Cite `rows` only; never invent service names.

Use this tool for fleet ranking by volume, rpm, HTTP 4xx, HTTP 5xx, or gRPC errors. It is the only fleet ranking tool for those questions.

When not to use:
- Exceptions → get_exceptions
- Log status codes or log-only services → get_logs
- Unlabeled percentages or current-vs-baseline change → get_apm_service_deviations
- One service's RED charts → get_service_performance_details

Language → sort_by (pass the named key; unlabeled "highest errors" is not the default):
- top / highest HTTP 5xx → http_5xx_count
- top / highest HTTP 4xx → http_4xx_count
- gRPC errors → grpc_error_count
- highest rpm → throughput_rpm
- busiest / omitted sort_by → request_count (volume, not errors)
Unknown sort_by values including errors, error_rate, and 5xx are rejected with the allowed list.

Critical rules:
- request_count is an interval total, not rpm and not a p95.
- throughput_rpm is request_count / window minutes, not a p95.
- HTTP 4xx, HTTP 5xx, and gRPC counts are never added together. 429 is 4xx here (get_apm_service_deviations treats 429 with 5xx).
- A missing class is 0, not omitted. Zeros mean no matching class in this window.
- Empty rows stay empty: do not widen the interval, change status class, or invent placeholder names.
- Ranking is APM/server-span only. Log-only services are absent, not zero.
- If truncated is true, raise limit (max 100) and recall.

Time:
- When both start_time_iso and end_time_iso are set, they define the interval and beat lookback.
- A single ISO bound fills the other with lookback_minutes (default 60). It does not mean "until now."
- Prefer lookback_minutes for relative windows.

env is a PromQL regex (default .*). Exact one-env match needs anchors (e.g. ^prod$). Unanchored prod also matches production.

Parameters:
- lookback_minutes: (Optional) Minutes to look back from now. Default 60. Minimum 1.
- start_time_iso: (Optional) Start of the interval in RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
- end_time_iso: (Optional) End of the interval in RFC3339/ISO8601 (e.g. 2026-02-09T16:04:05Z).
- env: (Optional) Environment Prom regex. Default .*.
- sort_by: (Optional) request_count (default), throughput_rpm, http_4xx_count, http_5xx_count, or grpc_error_count.
- limit: (Optional) Max ranked rows. Omit or 0 means 10. Other values below 1 are an error. Values above 100 clamp to 100.
