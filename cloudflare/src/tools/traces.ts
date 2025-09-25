import { Config, TraceEntry, ExceptionEntry } from '../types';
import { getTimeRange } from '../utils';

export const GET_EXCEPTIONS_DESCRIPTION = `
Get server side exceptions over the given time range.
Includes the exception type, message, stack trace, service name, trace ID and span attributes.

limit: (Optional) The maximum number of exceptions to return. Defaults to 20.
lookback_minutes: (Recommended) Number of minutes to look back from now. Use this for relative time ranges.
start_time_iso: (Optional) The start time to get the data from. Leave empty to use lookback_minutes instead.
end_time_iso: (Optional) The end time to get the data from. Leave empty to default to current time.
span_name: (Optional) The name of the span to get the data for. This is often the API endpoint name or controller name.
`;

export const GET_SERVICE_TRACES_DESCRIPTION = `
Query traces for a specific service with filtering options for span kinds, status codes, and other trace attributes.

This tool retrieves distributed tracing data for debugging performance issues, understanding request flows,
and analyzing service interactions. It supports various filtering and sorting options to help narrow down
specific traces of interest.

Filtering options:
- span_kind: Filter by span types (server, client, internal, consumer, producer)
- span_name: Filter by specific span names
- status_code: Filter by trace status (ok, error, unset)
- Time range: Use lookback_minutes or explicit start/end times

Examples:
1. service_name="api" + span_kind=["server"] + status_code=["error"]
   → finds failed server-side traces for the "api" service
2. service_name="payment" + span_name="process_payment" + lookback_minutes=30
   → finds payment processing traces from the last 30 minutes

Parameters:
- service_name (string, required): Name of the service to get traces for
- lookback_minutes (integer, optional): Number of minutes to look back from now. Default: 60 minutes
- limit (integer, optional): Maximum number of traces to return. Default: 10
- env (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.
- span_kind (array, optional): Array of span kinds to filter by
- span_name (string, optional): Filter by specific span name
- status_code (array, optional): Array of status codes to filter by
- order (string, optional): Field to order traces by. Default: "Duration"
- direction (string, optional): Sort direction. Default: "backward"
`;

export async function handleGetExceptions(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const { startTime, endTime } = getTimeRange(params, 60);
  const limit = params.limit || 20;
  const spanName = params.span_name || '';

  try {
    // Use base URL + telemetry path like Go implementation
    let url = `${config.baseURL}/telemetry/api/v1/exceptions`;

    // Add query parameters like Go implementation
    const params_obj = new URLSearchParams();
    params_obj.set('start', Math.floor(startTime.getTime() / 1000).toString());
    params_obj.set('end', Math.floor(endTime.getTime() / 1000).toString());
    params_obj.set('limit', limit.toString());

    if (spanName) {
      params_obj.set('span_name', spanName);
    }

    url += '?' + params_obj.toString();

    // Use Basic auth like Go implementation
    let authToken = config.authToken || '';
    if (!authToken.startsWith('Basic ')) {
      authToken = 'Basic ' + authToken;
    }

    const response = await fetch(url, {
      method: 'GET',
      headers: {
        'Authorization': authToken,
      },
    });

    if (!response.ok) {
      throw new Error(`Base URL API request failed: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();
    const exceptions = data.exceptions || [];

    if (exceptions.length === 0) {
      return {
        content: [{
          type: 'text',
          text: 'No exceptions found for the specified time range and filters.'
        }]
      };
    }

    const formattedExceptions: ExceptionEntry[] = exceptions.map((exc: any) => ({
      timestamp: exc.timestamp,
      message: exc.message,
      type: exc.exception_type || exc.type,
      stackTrace: exc.stack_trace,
      service: exc.service_name,
      traceId: exc.trace_id,
      spanId: exc.span_id,
      ...exc
    }));

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(formattedExceptions, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get exceptions: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handleGetServiceTraces(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const serviceName = params.service_name;
  if (!serviceName) {
    throw new Error('service_name is required');
  }

  const { startTime, endTime } = getTimeRange(params, 60);
  const limit = params.limit || 10;
  const env = params.env || '';
  const spanKind = params.span_kind || [];
  const spanName = params.span_name || '';
  const statusCode = params.status_code || [];
  const order = params.order || 'Duration';
  const direction = params.direction || 'backward';

  try {
    // Build URL with query parameters like Go implementation
    const region = config.baseURL.includes('aps1') ? 'ap-south-1' :
                   config.baseURL.includes('apse1') ? 'ap-southeast-1' : 'us-east-1';

    const baseUrl = `${config.apiBaseURL}/cat/api/traces/v2/query_range/json`;
    const urlParams = new URLSearchParams({
      region,
      start: Math.floor(startTime.getTime() / 1000).toString(),
      end: Math.floor(endTime.getTime() / 1000).toString(),
      limit: limit.toString(),
      order,
      direction,
    });
    const url = `${baseUrl}?${urlParams}`;

    // Build filters like Go implementation
    const filters = [];

    // Service name filter (matches Go implementation exactly)
    filters.push({ "$eq": ["ServiceName", serviceName] });

    // Span kind filters
    if (spanKind && spanKind.length > 0) {
      const spanKindFilters = spanKind.map(kind => ({ "$eq": ["SpanKind", kind] }));
      filters.push({ "$or": spanKindFilters });
    }

    // Span name filter
    if (spanName && spanName.trim() !== '') {
      filters.push({ "$eq": ["SpanName", spanName] });
    }

    // Status code filters
    if (statusCode && statusCode.length > 0) {
      const statusFilters = statusCode.map(status => ({ "$eq": ["StatusCode", status] }));
      filters.push({ "$or": statusFilters });
    }

    // Request body with pipeline structure like Go implementation
    const requestBody = {
      pipeline: [{
        query: { "$and": filters },
        type: "filter"
      }]
    };

    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Accept': 'application/json',
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${config.accessToken}`,
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      throw new Error(`Failed to get service traces: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();
    const traces = data.traces || [];

    if (traces.length === 0) {
      return {
        content: [{
          type: 'text',
          text: `No traces found for service "${serviceName}" with the specified filters.`
        }]
      };
    }

    const formattedTraces: TraceEntry[] = traces.map((trace: any) => ({
      traceId: trace.trace_id,
      spanId: trace.span_id,
      spanName: trace.span_name,
      duration: trace.duration,
      timestamp: trace.timestamp,
      status: trace.status || trace.status_code,
      serviceName: trace.service_name,
      spanKind: trace.span_kind,
      ...trace
    }));

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(formattedTraces, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get service traces: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}