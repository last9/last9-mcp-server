package traces

const GetServiceGraphDescription = `
	Gets the upstream and downstream services for a given span name, along with the throughput for each service.
    Upstream services are the services that are called by the given span name, while downstream services are the services that call the given span.
    The throughput is the number of requests per minute for each service.

    span_name: (Required) The name of the span to get dependencies for.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges. Defaults to 60 minutes.
    start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes instead.
`
