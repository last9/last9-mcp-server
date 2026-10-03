Fetch raw log lines for one service (`service_name`, optional `severity_filters`, `body_filters`, `http_status_class`/`http_status_code`, `attribute_filters`, `env`, `index`, `limit`).

**Profile first:** Call `get_service_profile` for this service before using this tool. Use `signal_shape` and `telemetry` for routing — see `last9://reference/investigation`. If results contradict the profile, fall back to discovery tools (profile may be stale; 15min TTL).

**Use this tool when:** filtering one known service by severity, HTTP status, named attributes, or plain-text `body_filters`—no pipeline needed.

**HTTP status:** Pass `http_status_class` (`5xx`) or `http_status_code` (`500`). This tool discovers the status field for the service/env/window. If discovery finds none or more than one, pass `http_status_field` (e.g. `attributes['http.status_code']`). Severity is not an HTTP-error proxy (5xx often INFO)—do not use `severity_filters` for status.

**Named attributes:** `attribute_filters` is `[{field, value}]` equality. `field` uses logjson syntax (`attributes['user_id']`). Invalid syntax is rejected; unknown org fields are allowed. Discover names with `get_log_attributes` / `get_log_attributes_for_pipeline` if unsure.

**Prefer `get_logs` when:** you need parse/aggregate/`window_aggregate`, or an ad-hoc pipeline.

**Time:** Prefer `lookback_minutes` for relative windows; `start_time_iso`+`end_time_iso` (RFC3339) for absolute. Pass `index` only when the user names one (`physical_index:<name>` / `rehydration_index:<block>`).

Full service-logs reference: `last9://reference/service_logs`. Logjson: `last9://reference/logjson`