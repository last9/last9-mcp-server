Recommended first call: one cheap orientation snapshot of the workspace. Use it to learn which environments exist and which signals (logs, traces, metrics) are present before choosing a query tool, and to get the current time. Takes no parameters.

Returns JSON:
- `current_time`: same shape as `get_current_time` with no range (`now` as `{iso, epoch_seconds}`).
- `lookback_minutes`: window the probes read (60).
- `environments`: sorted `env` values seen on APM service metrics; `[]` when none.
- `telemetry`: `{logs, traces, metrics}` booleans; `false` means nothing was seen in the window, so skip that signal's tools.
- `service_count`: number of services on APM service metrics.
- `alert_group_count`: number of alert groups (as `get_alert_groups` counts them).
- `firing_alert_count`: alert instances currently firing (15-minute window, as `get_alerts`).
- `errors`: `[{field, error}]` for each sub-call that failed or timed out. That field is `null`, which means unknown, not zero or false; the rest of the snapshot is still valid.

Sub-calls run concurrently, each bounded by a short timeout. For per-service detail follow up with `get_service_summary`; for firing alert details use `get_alerts`.