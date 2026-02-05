package traces

const GetExceptionsDescription = `
	Get server side exceptions over the given time range.
    Includes the exception type, message, stack trace, service name, trace ID and span attributes.

    limit: (Optional) The maximum number of exceptions to return. Defaults to 20.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Default: 60 minutes.
    start_time_iso: (Optional) The start time to get the data from. Leave empty to use lookback_minutes instead.
    end_time_iso: (Optional) The end time to get the data from. Leave empty to default to current time.
    service_name: (Optional) Filter exceptions by service name (e.g. api-service).
    span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
    deployment_environment: (Optional) Filter exceptions by deployment environment from resource attributes (e.g. production, staging).
`

const GetServiceGraphDescription = `
	Gets the upstream and downstream services for a given span name, along with the throughput for each service.
    Upstream services are the services that are called by the given span name, while downstream services are the services that call the given span.
    The throughput is the number of requests per minute for each service.
    
    span_name: (Required) The name of the span to get dependencies for.
    lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges. Defaults to 60 minutes.
    start_time_iso: (Optional) The start time to get the data from. Leave empty to use lookback_minutes instead.
`
