Enumerate the alertable signals on a covered Last9 Discover chart: for each signal you get its key, name, unit, and query metadata, which is the input you need before creating a chart-derived alert rule. This tool is read-only — it never mutates anything, so use it freely during discovery. Lifecycle contrast: alert rules later created from these signals via create-from-chart fire immediately once created, unlike create_alert's disabled-first flow that requires promoting with patch_alert.

Covered surfaces today are exactly two: discover-service and discover-exceptions. Any other surface value is a coverage miss.

Parameters:
- surface (Required): Surface identifier the chart lives on; one of discover-service or discover-exceptions.
- chart_key (Required): Chart key within the surface, for example apdex, error_rate, response_time, or exception_count.
- service_name (Required): Service/entity name the chart is scoped to.
- env: Environment when the chart distinguishes one, for example prod; omit when it does not.
- attributes: Extra identity dimensions some charts need (such as operation, exception type, or dependency target); use exactly the attribute key names describe reports — do not guess key names.

Credentials must clear the alert-intelligence route's POST gate; viewer-role tokens are rejected upstream with a permissions error.

On success the response lists the chart's signals with per-signal units as returned by describe. If the chart is not covered, the tool says so honestly with the upstream reason plus guidance: verify the surface and chart_key against the service's Discover page in the Last9 dashboard, and route genuinely uncataloged charts to dashboard/API-based alerting instead of inventing keys.