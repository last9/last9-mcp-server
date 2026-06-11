Get change events from the last9_change_events prometheus metric over a given time range.
Returns change events that occurred in the specified time window.
Change events include deployments, configuration changes, and other system modifications.

The response includes:
- available_event_names: List of all available event types that can be used for filtering
- change_events: Array of timeseries data with metric labels and timestamp-value pairs
- count: Total number of change events returned
- time_range: Start and end time of the query window

Each change event includes:
- metric: Map of metric labels (service_name, env, event_type, message, etc.)
- values: Array of timestamp-value pairs representing the timeseries data

For optimal results, first call without event_name to get available_event_names, then use the exact event name from available_event_names for the event_name parameter. This approach is more reliable and eliminates ambiguity in event type detection.

Common event types (check available_event_names for actual values):
- deployment: deployment events, releases, builds, rollouts
- config_change: configuration changes, settings updates, parameter changes
- rollback: rollback events, reverts, undo operations
- scale_up/scale_down: scaling operations, capacity changes
- restart: service restarts, reboots, reloads
- upgrade/downgrade: version changes, updates
- maintenance: maintenance windows, scheduled downtime
- backup/restore: backup operations, recovery
- health_check: health checks, monitoring, status probes
- certificate: SSL/TLS operations, renewals, expirations
- database: database changes, migrations, schema updates

Best practices:
1. First call without event_name to get available_event_names
2. Use exact event name from available_event_names for the event_name parameter
3. Combine with other filters (service, environment, time) for precise results
4. Use available_event_names to discover what event types are available in the system

Parameters:
- start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Defaults to now - lookback_minutes.
- end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Defaults to now.
- lookback_minutes: (Optional) Number of minutes to look back from now. Defaults to 60 minutes.
- service: (Optional) Name of the service to filter change events for
- environment: (Optional) Environment to filter by
- event_name: (Optional) Name of the change event to filter by (use available_event_names to see valid values)

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
- If both lookback_minutes and absolute times are provided, absolute times take precedence.
- If unsure of the service or environment name, call "did_you_mean" first to find the correct spelling.
