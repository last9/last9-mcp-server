package traces

const GetExceptionsDescription = `
	Get server side exceptions over the given time range. 
    Includes the exception type, message, stack trace, service name, trace ID and span attributes.
    limit: The maximum number of exceptions to return.
    start_time_iso: The start time to get the data from. Defaults to 1 hour prior to end_time.
    end_time_iso: The end time to get the data from. Defaults to now.
    span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
`

const GetServiceGraphDescription = `
	Gets the upstream and downstream services for a given span name, along with the throughput for each service.
    Upstream services are the services that are called by the given span name, while downstream services are the services that call the given span.
    The throughput is the number of requests per minute for each service.
    
    lookback_minutes: (Optional) The number of minutes to look back for the data. Defaults to 60 minutes.   
    start_time_iso: (Optional) The start time to get the data from. Defaults to now.
    span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
`
