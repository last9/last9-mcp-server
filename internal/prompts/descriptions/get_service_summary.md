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
- highest rpm → throughput_rpm (ranking-identical to request_count; only sort_key_unit differs)
- busiest / omitted sort_by → request_count (volume, not errors)
Unknown sort_by values including errors, error_rate, and 5xx are rejected with the allowed list.

Critical rules:
- request_count is an interval total, not rpm and not a p95.
- throughput_rpm is request_count / window_minutes, not a p95. Prefer request_count for volume ranking; throughput_rpm does not change order within the same call.
- HTTP 4xx, HTTP 5xx, and gRPC counts are never added together. 429 is 4xx here (get_apm_service_deviations treats 429 with 5xx).
- grpc_error_count requires a present non-empty grpc_status_code that is not 0/OK. Series that omit the label stay 0 (HTTP-only spans).
- Dashboard links use a literal env only for exact `^name$`. Unanchored or regex env_scope (prod, .*, prod|staging) is not copied into the UI filter.
- A missing class is 0, not omitted. Zeros mean no matching class in this window.
- Empty rows stay empty: do not widen the interval, change status class, or invent placeholder names.
- Ranking is APM/server-span only. Log-only services are absent, not zero.
- If truncated is true, matched_count is the full ranked total before the limit cut — raise limit toward min(matched_count, 100) and recall.

Time:
- When both start_time_iso and end_time_iso are set, they define the interval and beat lookback.
- A single ISO bound fills the other with lookback_minutes (default 60). It does not mean "until now."
- Prefer lookback_minutes for relative windows.
- Response start_time/end_time/window_minutes describe the PromQL range actually summed (minutes rounded up). A 90s request becomes window_minutes=2.

env is a PromQL regex (default .*). Exact one-env match needs anchors (e.g. ^prod$). Unanchored prod also matches production.

Response envelope (besides rows):
- truncated, limit, row_count, matched_count — row_count is rows returned; matched_count is the pre-truncation total
- sort_by, sort_key_unit — active sort key and its unit (count or rpm)
- start_time, end_time, window_minutes — queried interval (see Time)
- env_scope — the env regex used for PromQL
- coverage — states that ranking is APM server-span only
- query_fingerprint — hash of the PromQL shapes (no label values); for provenance, not for ranking
- hint — present only when rows is empty

Parameters:
- lookback_minutes: (Optional) Minutes to look back from now. Default 60. Minimum 1.
- start_time_iso: (Optional) Start of the interval in RFC3339/ISO8601 (e.g. 2026-02-09T15:04:05Z).
- end_time_iso: (Optional) End of the interval in RFC3339/ISO8601 (e.g. 2026-02-09T16:04:05Z).
- env: (Optional) Environment Prom regex. Default .*.
- sort_by: (Optional) request_count (default), throughput_rpm, http_4xx_count, http_5xx_count, or grpc_error_count.
- limit: (Optional) Max ranked rows. Omit or 0 means 10. Other values below 1 are an error. Values above 100 clamp to 100.
