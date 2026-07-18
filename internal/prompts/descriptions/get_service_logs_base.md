Fetch raw log lines for one service (`service_name`, optional `severity_filters`, `body_filters`, `env`, `index`, `limit`).

**Use this tool when:** filtering by log severity levels (`severity_filters`: error/warn/fatal) or plain-text message search (`body_filters`) for a single known service—no pipeline needed.

**Prefer `get_logs` instead when:** filtering HTTP/gRPC status, user IDs, latency, URIs, or any structured attribute. Discover fields with `get_log_attributes` / `get_log_attributes_for_pipeline`, then call `get_logs` with a `logjson_query`. Indexed attribute filters beat `body_filters`.

**HTTP errors:** Severity is not an HTTP-error proxy (5xx often INFO). Use `get_logs` + discovered status field—not `severity_filters`/`SeverityText`.

**Time:** Prefer `lookback_minutes` for relative windows; `start_time_iso`+`end_time_iso` (RFC3339) for absolute. Pass `index` only when the user names one (`physical_index:<name>` / `rehydration_index:<block>`).

Full service-logs reference: `last9://reference/service_logs`. Logjson: `last9://reference/logjson`