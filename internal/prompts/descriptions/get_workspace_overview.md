Recommended first call: one cheap orientation snapshot of the workspace. Use it to learn which environments exist and which signals (logs, traces, metrics) were seen in the last hour before choosing a query tool, and to get the current time. Takes no parameters.

Returns JSON:
- `current_time`: same shape as `get_current_time` with no range (`now` as `{iso, epoch_seconds}`).
- `lookback_minutes`: window the probes read (60).
- `environments`: sorted `env` values seen on APM service metrics; `[]` when none.
- `telemetry`: `{logs, traces, metrics}` booleans for the window in the default scope. `false` means not observed in this window, not that the signal is missing: still query it for the user's requested time range, index or datasource.
- `service_count`: number of services on APM service metrics.
- `alert_group_count`: number of alert groups (as `get_alert_groups` counts them).
- `firing_alert_count`: alert instances in state `firing` that `get_alerts` lists over its default 15-minute window; pending and resolved instances are not counted.
- `errors`: `[{field, error}]` for each sub-call that failed or timed out. That field is `null`, which means unknown, not zero or false; the rest of the snapshot is still valid.

Sub-calls run concurrently, each bounded by a short timeout.

## Pairs well with
- `get_service_summary` (per-service detail)
- `get_alerts` (firing alert details)