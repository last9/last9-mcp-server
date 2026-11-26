Recommended first call when a request names no service or tool: one cheap orientation snapshot of the workspace, and the current time. Takes no parameters.

## When to use
At the start of an open-ended investigation, to learn which environments exist and which signals (logs, traces, metrics) were seen in the last hour before choosing a query tool.

## When not to use
- The request names a service, signal or tool: call its tool directly
- Resolving a time range like "last 2h": `get_current_time`
- Per-service traffic and error rates: `get_service_summary`
- Which alerts are firing, and their labels: `get_alerts`

## Pairs well with
- `get_service_summary` (per-service detail)
- `get_alerts` (firing alert details)

## Output shape
JSON:
- `current_time`: same shape as `get_current_time` with no range (`now` as `{iso, epoch_seconds}`).
- `lookback_minutes`: window the probes read (60).
- `environments`: sorted `env` values seen on APM service metrics; `[]` when none.
- `telemetry`: `{logs, traces, metrics}` booleans for the window in the default scope. `false` means not observed in this window, not that the signal is missing: still query it for the user's requested time range, index or datasource.
- `service_count`: distinct services `get_service_summary` ranks over the window, across every env, counted from sampled traffic in the hour (its `matched_count` counts service and env pairs, so it can be higher).
- `alert_group_count`: number of groups `get_alert_groups` lists with no filters, read from the group lookup alone, so it stays known when alert rules cannot be read.
- `firing_alert_count`: alert instances in state `firing` that `get_alerts` lists over its default 15-minute window; pending and resolved instances are not counted.
- `errors`: `[{field, error}]` for each sub-call that failed or timed out. That field is `null`, which means unknown, not zero or false; the rest of the snapshot is still valid.

## Known traps
Sub-calls run concurrently, each bounded by a short timeout, so a slow backend yields a `null` field rather than a slow call. Counts cover everything the API token can read, whatever toolsets the server serves.