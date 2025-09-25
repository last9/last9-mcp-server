import { Config, ServiceSummary, PromInstantResponse } from '../types';
import { getTimeRange, makePromInstantQuery } from '../utils';

export const GET_SERVICE_SUMMARY_DESCRIPTION = `
Get service summary over a given time range.
Includes service name, environment, throughput, error rate, and response time.
All values are p95 quantiles over the time range.
Response times are in milliseconds. Throughput and error rates are in requests per minute (rpm).
Each service includes:
- service name
- environment
- throughput in requests per minute (rpm)
- error rate in requests per minute (rpm)
- p95 response time in milliseconds
Parameters:
- start_time: (Required) Start time of the time range in ISO format.
- end_time: (Required) End time of the time range in ISO format.
- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
`;

export const GET_SERVICE_ENVIRONMENTS_DESCRIPTION = `
Return the environments available for the services. This tool returns an array of environments.
All other tools that retrieve information about services
like get_service_performance_details, get_service_dependency_graph, get_service_operations_summary,
get_service_sumary etc. require a mandatory "env" parameter. This must be one of the
environments returned by this tool. If the returned array is empty, use an empty string ""
as the value for the "env" parameter for other tools.
Parameters:
- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

Returns an array of environments.
`;

export const GET_SERVICE_PERFORMANCE_DETAILS_DESCRIPTION = `
Get service performance metrics over a given time range.
Returns the following information
	- service name
	- environment
	- throughput in rpm
	- error rate in rpm for 4xx and 5xx errors
	- error percentage
	- p50, p90, p95 and avg response times in seconds
	- apdex score
	- availability in percentage
	- top 10 web operations by response time
	- top 10 operations by error rate
	- top 10 errors or exceptions by count for the service
The details for the operations in the "top 10 web operations by response time" and "top 10 operations by error rate" can be fetched using the "get_service_operation_details" tool.
This tool can be used to get all perforamnce and debugging details for a service over a time range.
It can also be used to get a summary for performance bottlenecks and errors / exceptions in a service.
Parameters:
- start_time: (Required) Start time of the time range in ISO format.
- end_time: (Required) End time of the time range in ISO format.
- env: (Required) Environment to filter by. Use "get_service_environments" tool to get available environments.
`;

export async function handleGetServiceSummary(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const { startTime, endTime } = getTimeRange(params, 60);
  const env = params.env || '.*';

  const startTimeParam = Math.floor(startTime.getTime() / 1000);
  const endTimeParam = Math.floor(endTime.getTime() / 1000);
  const duration = Math.floor((endTimeParam - startTimeParam) / 60);

  // Build PromQL query for throughput
  const throughputQuery = `quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{env=~'${env}', span_kind='SPAN_KIND_SERVER'}[${duration}m]))`;

  try {
    const response = await makePromInstantQuery(throughputQuery, endTimeParam, config);

    if (!response.ok) {
      throw new Error(`Failed to get service summary: ${response.status}`);
    }

    const data = await response.json();
    const results: PromInstantResponse[] = data.data?.result || [];

    if (results.length === 0) {
      return {
        content: [{
          type: 'text',
          text: 'No services found for the given parameters'
        }]
      };
    }

    const serviceMap: Record<string, ServiceSummary> = {};

    // Process throughput data
    for (const result of results) {
      const serviceName = result.metric.service_name;
      const value = parseFloat(result.value[1]);

      serviceMap[serviceName] = {
        serviceName,
        env,
        throughput: value,
        errorRate: 0,
        responseTime: 0,
      };
    }

    // Get response time data
    const responseTimeQuery = `quantile_over_time(0.95, sum by (service_name)(trace_service_response_time{quantile="p95", env=~'${env}'}[${duration}m]))`;
    const responseTimeResp = await makePromInstantQuery(responseTimeQuery, endTimeParam, config);

    if (responseTimeResp.ok) {
      const responseTimeData = await responseTimeResp.json();
      const responseTimeResults: PromInstantResponse[] = responseTimeData.data?.result || [];

      for (const result of responseTimeResults) {
        const serviceName = result.metric.service_name;
        const value = parseFloat(result.value[1]);

        if (serviceMap[serviceName]) {
          serviceMap[serviceName].responseTime = value;
        }
      }
    }

    // Get error rate data
    const errorRateQuery = `quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{env=~'${env}', span_kind='SPAN_KIND_SERVER', http_status_code=~'4..|5..'}[${duration}m]))`;
    const errorRateResp = await makePromInstantQuery(errorRateQuery, endTimeParam, config);

    if (errorRateResp.ok) {
      const errorRateData = await errorRateResp.json();
      const errorRateResults: PromInstantResponse[] = errorRateData.data?.result || [];

      for (const result of errorRateResults) {
        const serviceName = result.metric.service_name;
        const value = parseFloat(result.value[1]);

        if (serviceMap[serviceName]) {
          serviceMap[serviceName].errorRate = value;
        }
      }
    }

    const services = Object.values(serviceMap);

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(services, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get service summary: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handleGetServiceEnvironments(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const { startTime, endTime } = getTimeRange(params, 60);

  const startTimeParam = Math.floor(startTime.getTime() / 1000);
  const endTimeParam = Math.floor(endTime.getTime() / 1000);
  const duration = Math.floor((endTimeParam - startTimeParam) / 60);

  // Query to get all unique environments
  const envQuery = `group by (env)(trace_endpoint_count{span_kind='SPAN_KIND_SERVER'}[${duration}m])`;

  try {
    const response = await makePromInstantQuery(envQuery, endTimeParam, config);

    if (!response.ok) {
      throw new Error(`Failed to get service environments: ${response.status}`);
    }

    const data = await response.json();
    const results: PromInstantResponse[] = data.data?.result || [];

    const environments = results
      .map(result => result.metric.env)
      .filter(env => env && env !== '')
      .sort();

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(environments, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get service environments: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handleGetServicePerformanceDetails(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const serviceName = params.service_name;
  if (!serviceName) {
    throw new Error('service_name is required');
  }

  const { startTime, endTime } = getTimeRange(params, 60);
  const env = params.env || '.*';

  const startTimeParam = Math.floor(startTime.getTime() / 1000);
  const endTimeParam = Math.floor(endTime.getTime() / 1000);
  const duration = Math.floor((endTimeParam - startTimeParam) / 60);

  const performanceData: any = {
    service_name: serviceName,
    env,
    throughput: {},
    error_rate: {},
    error_percentage: {},
    response_times: {},
    apdex_score: {},
    availability: {},
    top_operations: {
      by_response_time: [],
      by_error_rate: []
    },
    top_errors: []
  };

  try {
    // Multiple queries for comprehensive performance data
    const queries = [
      // Throughput
      `sum by (http_status_code)(trace_endpoint_count{service_name='${serviceName}', env=~'${env}', span_kind='SPAN_KIND_SERVER'}[${duration}m])`,
      // Response times (multiple quantiles)
      `quantile_over_time(0.50, trace_service_response_time{service_name='${serviceName}', env=~'${env}'}[${duration}m])`,
      `quantile_over_time(0.90, trace_service_response_time{service_name='${serviceName}', env=~'${env}'}[${duration}m])`,
      `quantile_over_time(0.95, trace_service_response_time{service_name='${serviceName}', env=~'${env}'}[${duration}m])`,
      // Top operations by response time
      `topk(10, quantile_over_time(0.95, trace_endpoint_response_time{service_name='${serviceName}', env=~'${env}'}[${duration}m]))`,
    ];

    // Execute queries in parallel
    const responses = await Promise.all(
      queries.map(query => makePromInstantQuery(query, endTimeParam, config))
    );

    // Process responses
    for (let i = 0; i < responses.length; i++) {
      if (responses[i].ok) {
        const data = await responses[i].json();
        const results: PromInstantResponse[] = data.data?.result || [];

        switch (i) {
          case 0: // Throughput
            performanceData.throughput = results.map(r => ({
              metric: r.metric,
              values: [[endTimeParam, r.value[1]]]
            }));
            break;
          case 1: // P50 response time
            performanceData.response_times.p50 = results.map(r => ({
              metric: r.metric,
              values: [[endTimeParam, r.value[1]]]
            }));
            break;
          case 2: // P90 response time
            performanceData.response_times.p90 = results.map(r => ({
              metric: r.metric,
              values: [[endTimeParam, r.value[1]]]
            }));
            break;
          case 3: // P95 response time
            performanceData.response_times.p95 = results.map(r => ({
              metric: r.metric,
              values: [[endTimeParam, r.value[1]]]
            }));
            break;
          case 4: // Top operations by response time
            performanceData.top_operations.by_response_time = results.map(r => ({
              operation: r.metric.http_route || r.metric.span_name || 'unknown',
              response_time: parseFloat(r.value[1])
            }));
            break;
        }
      }
    }

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(performanceData, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get service performance details: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}