Change events from the `last9_change_events` Prometheus metric (deployments, config changes, rollbacks, scaling, etc.) over a time range.

Response: `available_event_names` (valid filter values), `change_events` (timeseries with metric labels + timestamp/value pairs), `count`, `time_range`.

Each series has `metric` (labels: service_name or legacy service, deployment_environment, event_name, …) and `values` (original sample timestamps in Unix seconds, plus values). `count` counts series. Filter with the tool's own param names (`service_name`, `env`, `event_name`) — the server maps them onto the labels the events are stored with.

Workflow: first call without `event_name` to read `available_event_names`, then filter with the exact name from that list. Combine with service_name, env, and time filters as needed.

Parameters:
- `lookback_minutes`: (Optional) Minutes to look back. Default 60.
- `start_time_iso` / `end_time_iso`: (Optional) RFC3339/ISO8601 bounds. With only a start, end = start + lookback. With neither bound, end = now.
- `service_name`, `env`, `event_name`: (Optional) Filters. Use `event_name` only after discovering values via `available_event_names`.

Time: prefer `lookback_minutes` for relative windows; absolute ISO bounds override lookback. Legacy `YYYY-MM-DD HH:MM:SS` accepted for compatibility.

If unsure of `service_name` or `env`, call `did_you_mean` first.
