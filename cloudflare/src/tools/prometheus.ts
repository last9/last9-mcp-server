import { Config, PromInstantResponse, PromRangeResponse } from '../types';
import { getTimeRange, makePromInstantQuery, makePromRangeQuery, makePromLabelsQuery } from '../utils';

export const PROMQL_RANGE_QUERY_DESCRIPTION = `
Perform a Prometheus range query to get metrics data over a specified time range.
This tool can be used to query Prometheus for metrics data over a specified time range.
It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
are common. To get possible values of a label, the prometheus_label_values tool can be used.
It returns a structured response with the following fields:
- metric: A map of metric labels and their values.
- value: A list of lists. Each item in the list has timestamp as the first element
	and the value as the second.
Example:
[ {
	"metric": {
		"__name__": "http_request_duration_seconds",
		"method": "GET",
		"status": "200"
	},
	"value": [
		[1700000000, "0.123"],
		[1700000060, "0.456"],
		...
	]
}]
The response will contain the metrics data for the specified query.
Parameters:
- query: (Required) The Prometheus query to execute.
- start_time: (Required) Start time of the time range in ISO format.
- end_time: (Required) End time of the time range in ISO format.
`;

export const PROMQL_INSTANT_QUERY_DESCRIPTION = `
Perform a Prometheus instant query to get metrics data.
Typically, the query should have rollup functions like sum_over_time, avg_over_time, quantile_over_time, etc
over a time window. For example: avg_over_time(trace_endpoint_count{env="prod"}[1h])
This tool can be used to query Prometheus for metrics data at a specific point in time.
It is recommended to initially check the the available labels on the promql metric using the prometheus_labels tool
for filtering by a specific environment. Labels like "env", "environment" or "development_environment"
are common. To get possible values of a label, the prometheus_label_values tool can be used.
It returns a structured response with the following fields:
- metric: A map of metric labels and their values.
- value: A list of lists. Each item in the list has timestamp as the first element
	and the value as the second.
Response Example:
[ {
	"metric": {
		"__name__": "http_request_duration_seconds",
		"method": "GET",
		"status": "200"
	},
	"value": [1700000000, "0.123"]
}]
The response will contain the metrics data for the specified query.
Parameters:
- query: (Required) The Prometheus query to execute.
- time: (Required) The point in time to query in ISO format.
`;

export const PROMQL_LABEL_VALUES_DESCRIPTION = `
Return the label values for a particular label and promql filter query.
This works similar to the prometheus /label_values call
It returns an array of values for the label.
Parameters:
- match_query: (Required) A valid promql filter query
- label: (Required) Name of the label to return values for
- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

match_query should be a well formed, valid promql query
It is enouraged to not use default
values of start_time and end_time and use values that are appropriate for the
use case
`;

export const PROMQL_LABELS_DESCRIPTION = `
Return the labels for a given  promql match query.
This works similar to the prometheus /labels call
It returns an array of labels.
Parameters:
- match_query: (Required) A valid promql filter query
- start_time: (Optional) Start time of the time range in ISO format. Defaults to end_time - 1 hour
- end_time: (Optional) End time of the time range in ISO format. Defaults to current time

match_query should be a well formed, valid promql query
It is enouraged to not use default
values of start_time and end_time and use values that are appropriate for the
use case
`;

export async function handlePrometheusRangeQuery(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const query = params.query;
  if (!query) {
    throw new Error('query is required');
  }

  const { startTime, endTime } = getTimeRange(params, 60);
  const start = Math.floor(startTime.getTime() / 1000);
  const end = Math.floor(endTime.getTime() / 1000);

  // Calculate appropriate step (resolution) - typically 1 minute intervals
  const duration = end - start;
  const step = Math.max(60, Math.floor(duration / 1000)) + 's'; // At least 60s steps

  try {
    const response = await makePromRangeQuery(query, start, end, step, config);

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Prometheus range query failed: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const data = await response.json();
    const results: PromRangeResponse[] = data.data?.result || [];

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(results, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to execute Prometheus range query: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handlePrometheusInstantQuery(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const query = params.query;
  if (!query) {
    throw new Error('query is required');
  }

  // Use provided time or current time
  let queryTime: number;
  if (params.time_iso && typeof params.time_iso === 'string' && params.time_iso !== '') {
    const parsed = new Date(params.time_iso.replace(' ', 'T') + 'Z');
    if (isNaN(parsed.getTime())) {
      throw new Error('Invalid time_iso format');
    }
    queryTime = Math.floor(parsed.getTime() / 1000);
  } else {
    queryTime = Math.floor(Date.now() / 1000);
  }

  try {
    const response = await makePromInstantQuery(query, queryTime, config);

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Prometheus instant query failed: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const data = await response.json();
    const results: PromInstantResponse[] = data.data?.result || [];

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(results, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to execute Prometheus instant query: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handlePrometheusLabelValues(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const matchQuery = params.match_query;
  const label = params.label;

  if (!matchQuery) {
    throw new Error('match_query is required');
  }

  if (!label) {
    throw new Error('label is required');
  }

  const { startTime, endTime } = getTimeRange(params, 60);
  const start = Math.floor(startTime.getTime() / 1000);
  const end = Math.floor(endTime.getTime() / 1000);

  try {
    const url = `${config.prometheusReadURL}/api/v1/label/${encodeURIComponent(label)}/values`;
    const urlParams = new URLSearchParams({
      'match[]': matchQuery,
      start: start.toString(),
      end: end.toString(),
    });

    const response = await fetch(`${url}?${urlParams}`, {
      headers: {
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Failed to get label values: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const data = await response.json();
    const values = data.data || [];

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(values, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get Prometheus label values: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handlePrometheusLabels(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  const matchQuery = params.match_query;

  if (!matchQuery) {
    throw new Error('match_query is required');
  }

  const { startTime, endTime } = getTimeRange(params, 60);
  const start = Math.floor(startTime.getTime() / 1000);
  const end = Math.floor(endTime.getTime() / 1000);

  try {
    const response = await makePromLabelsQuery(matchQuery, start, end, config);

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Failed to get labels: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const data = await response.json();
    const labels = data.data || [];

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(labels, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get Prometheus labels: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}