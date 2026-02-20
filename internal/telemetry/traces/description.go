package traces

const GetExceptionsDescription = `
	Get server side exceptions over the given time range.
    Includes the exception type, message, stack trace, service name, trace ID and span attributes.

    limit: (Optional) The maximum number of exceptions to return. Defaults to 20.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges. Default: 60 minutes.
    start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes instead.
    end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time.
    service_name: (Optional) Filter exceptions by service name (e.g. api-service).
    span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
    deployment_environment: (Optional) Filter exceptions by deployment environment from resource attributes (e.g. production, staging).

    Time format rules:
    - Prefer lookback_minutes for relative windows.
    - Use start_time_iso/end_time_iso for absolute windows.
    - Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
`

const GetServiceGraphDescription = `
	Gets the upstream and downstream services for a given span name, along with the throughput for each service.
    Upstream services are the services that are called by the given span name, while downstream services are the services that call the given span.
    The throughput is the number of requests per minute for each service.

    span_name: (Required) The name of the span to get dependencies for.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges. Defaults to 60 minutes.
    start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to use lookback_minutes instead.
`
