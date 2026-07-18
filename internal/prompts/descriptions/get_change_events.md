Change events from the `last9_change_events` Prometheus metric (deployments, config changes, rollbacks, scaling, etc.) over a time range.

Response: `available_event_names` (valid filter values), `change_events` (timeseries with metric labels + timestamp/value pairs), `count`, `time_range`.

Each event has `metric` (labels: service_name, env, event_type, message, …) and `values` (timestamp/value pairs).

Workflow: first call without `event_name` to read `available_event_names`, then filter with the exact name from that list. Combine with service_name, env, and time filters as needed.

Parameters:
- `lookback_minutes`: (Optional) Minutes to look back. Default 60.
- `start_time_iso` / `end_time_iso`: (Optional) RFC3339/ISO8601 bounds. Default start = now − lookback; end = now.
- `service_name`, `env`, `event_name`: (Optional) Filters. Use `event_name` only after discovering values via `available_event_names`.

Time: prefer `lookback_minutes` for relative windows; absolute ISO bounds override lookback. Legacy `YYYY-MM-DD HH:MM:SS` accepted for compatibility.

If unsure of `service_name` or `env`, call `did_you_mean` first.
