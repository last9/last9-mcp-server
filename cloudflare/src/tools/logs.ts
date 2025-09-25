import { Config, LogEntry, DropRule } from '../types';
import { getTimeRange, fetchPhysicalIndex } from '../utils';

export const GET_LOGS_DESCRIPTION = `Get logs using JSON pipeline queries for advanced filtering, parsing, aggregation, and processing.

This tool requires the logjson_query parameter which contains a JSON pipeline query. Use the logjson_query_builder prompt to generate these queries from natural language descriptions.

Parameters:
- logjson_query: (Required) JSON pipeline query array for advanced log filtering and processing. Use logjson_query_builder prompt to generate from natural language.
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 5 minutes.
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

The logjson_query supports:
- Filter operations: Filter logs based on conditions
- Parse operations: Parse log content (json, regexp, logfmt)
- Aggregate operations: Perform aggregations (sum, avg, count, etc.)
- Window aggregate operations: Time-windowed aggregations
- Transform operations: Transform/extract fields
- Select operations: Select specific fields and apply limits

Response contains the results of the JSON pipeline query execution.`;

export const GET_SERVICE_LOGS_DESCRIPTION = `
Get raw log entries for a specific service over a time range. This tool retrieves actual log entries including log messages, timestamps, severity levels, and other metadata. Useful for debugging issues, monitoring service behavior, and analyzing specific log patterns.

Filtering behavior:
- severity_filters: Array of severity patterns (e.g., ["error", "warn"]) - uses OR logic (matches any pattern)
- body_filters: Array of message content patterns (e.g., ["timeout", "failed"]) - uses OR logic (matches any pattern)
- Multiple filter types are combined with AND logic (service AND severity AND body)

Examples:
1. service_name="api" + severity_filters=["error"] + body_filters=["timeout"]
   → finds error logs containing "timeout" for the "api" service
2. service_name="web" + body_filters=["timeout", "failed", "error 500"]
   → finds logs containing "timeout" OR "failed" OR "error 500" for the "web" service
3. service_name="db" + severity_filters=["error", "critical"] + body_filters=["connection", "deadlock"]
   → finds error/critical logs containing "connection" OR "deadlock" for the "db" service

Parameters:
- service_name (string, required): Name of the service to get logs for.
- lookback_minutes (integer, optional): Number of minutes to look back from now. Default: 5 minutes.
- limit (integer, optional): Maximum number of log entries to return. Default: 20.
- env (string, optional): Environment to filter by. Use "get_service_environments" tool to get available environments.
- severity_filters (array, optional): Array of severity patterns to filter logs.
- body_filters (array, optional): Array of message content patterns to filter logs.
- start_time_iso (string, optional): Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes.
- end_time_iso (string, optional): End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.
`;

export const GET_DROP_RULES_DESCRIPTION = `
Retrieve and display the configured drop rules for log management in Last9.
Drop rules are filtering mechanisms that determine which logs are excluded from being processed and stored.
`;

export const ADD_DROP_RULE_DESCRIPTION = `
Add Drop Rule filtering capabilities, it supports filtering on metadata about the logs,
not the actual log content itself.

Not Supported
- Key:
	- filtering on message content in the values array is not supported
	- Message (attributes["message"])
	- Body (attributes["body"])
	- Individual keys like key1, key2, etc.
	- Regular expression patterns
	- Actual log content in values object

- Operators:
	- No partial matching
	- No contains, startswith, or endswith operators
	- No numeric comparisons (greater than, less than)

- Conjunctions:
	- No or logic between filters

Supported
- Key:
	- Log attributes (attributes["key_name"])
	- Resource attributes (resource.attributes["key_name"])

- Operators:
	- equals
	- not_equals

- Logical Conjunctions:
	- and

Key Requirements
- All attribute keys must use proper escaping with double quotes
- Resource attributes must be prefixed with resource.attributes
- Log attributes must be prefixed with attributes
- Each filter requires a conjunction (and) to combine with other filters

The system only supports filtering on metadata about the logs, not the actual log content itself.
`;

export async function handleGetLogs(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  console.log('handleGetLogs called with params:', JSON.stringify(params, null, 2));
  // Check if logjson_query is provided
  const logjsonQuery = params.logjson_query;
  if (!logjsonQuery) {
    throw new Error('logjson_query parameter is required. Use the logjson_query_builder prompt to generate JSON pipeline queries from natural language');
  }

  // Handle logjson_query directly
  return handleLogJSONQuery(logjsonQuery, params, config);
}

export async function handleGetServiceLogs(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  console.log('handleGetServiceLogs called with params:', JSON.stringify(params, null, 2));
  const serviceName = params.service_name;
  if (!serviceName) {
    throw new Error('service_name is required');
  }

  const { startTime, endTime } = getTimeRange(params, 5);
  const limit = params.limit || 20;
  const env = params.env || '';
  const severityFilters = params.severity_filters || [];
  const bodyFilters = params.body_filters || [];

  // Fetch physical index before making logs queries for performance optimization
  let physicalIndex = '';
  try {
    physicalIndex = await fetchPhysicalIndex(serviceName, env, config);
  } catch (error) {
    // Continue without optimization if physical index fetch fails
  }

  try {
    // Use the logs/api/v2/query_range/json endpoint like the Go implementation
    const region = config.baseURL.includes('aps1') ? 'ap-south-1' :
                   config.baseURL.includes('apse1') ? 'ap-southeast-1' : 'us-east-1';

    let url = `${config.apiBaseURL}/logs/api/v2/query_range/json?direction=backward&start=${Math.floor(startTime.getTime()/1000)}&end=${Math.floor(endTime.getTime()/1000)}&region=${region}`;

    // Add physical index parameters if available
    if (physicalIndex && physicalIndex.trim() !== '') {
      url += `&index=${encodeURIComponent(physicalIndex)}`;
      if (physicalIndex.startsWith('physical_index:')) {
        url += '&index_type=physical';
      }
    }

    // Build the pipeline request body matching Go implementation structure
    const andConditions = [
      { "$eq": ["ServiceName", serviceName] }
    ];

    // Add severity filters with OR logic
    if (severityFilters.length > 0) {
      const orConditions = severityFilters.map(severity => ({
        "$regex": ["SeverityText", `(?i)${severity}`]
      }));
      andConditions.push({ "$or": orConditions });
    }

    // Add body filters with OR logic
    if (bodyFilters.length > 0) {
      const orConditions = bodyFilters.map(bodyPattern => ({
        "$regex": ["Body", `(?i)${bodyPattern}`]
      }));
      andConditions.push({ "$or": orConditions });
    }

    const requestBody = {
      pipeline: [
        {
          query: { "$and": andConditions },
          type: "filter"
        }
      ]
    };

    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      throw new Error(`Failed to get service logs: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();
    const logs = data.result || [];

    if (logs.length === 0) {
      return {
        content: [{
          type: 'text',
          text: `No logs found for service "${serviceName}" in the specified time range.`
        }]
      };
    }

    // Format logs for display
    const formattedLogs = logs.map((log: any) => {
      const stream = log.stream || {};
      const values = log.values || [];

      return values.map((value: [string, string]) => ({
        timestamp: new Date(parseInt(value[0]) * 1000).toISOString(),
        message: value[1],
        ...stream,
      }));
    }).flat();

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(formattedLogs, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get service logs: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

export async function handleGetDropRules(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  console.log('handleGetDropRules called with params:', JSON.stringify(params, null, 2));
  try {
    const url = `${config.apiBaseURL}/v1/drop_rules`;

    const response = await fetch(url, {
      method: 'GET',
      headers: {
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to get drop rules: ${response.status} ${response.statusText}`);
    }

    const data = await response.json();

    return {
      content: [{
        type: 'text',
        text: JSON.stringify(data, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to get drop rules: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

// handleLogJSONQuery processes logjson_query parameter and calls the log JSON query API directly
export async function handleLogJSONQuery(
  logjsonQuery: any,
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  console.log('handleLogJSONQuery called with logjsonQuery:', JSON.stringify(logjsonQuery, null, 2));
  console.log('handleLogJSONQuery called with params:', JSON.stringify(params, null, 2));
  // Determine time range from parameters
  const { startTime, endTime } = parseTimeRange(params);

  try {
    // Use the logs/api/v2/query_range/json endpoint like the Go implementation
    const region = config.baseURL.includes('aps1') ? 'ap-south-1' :
                   config.baseURL.includes('apse1') ? 'ap-southeast-1' : 'us-east-1';

    const url = `${config.apiBaseURL}/logs/api/v2/query_range/json?direction=backward&start=${Math.floor(startTime/1000)}&end=${Math.floor(endTime/1000)}&region=${region}`;

    const requestBody = {
      pipeline: logjsonQuery
    };

    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      throw new Error(`Failed to execute log JSON query: ${response.status} ${response.statusText}`);
    }

    const result = await response.json();

    // Return the result in MCP format
    return {
      content: [{
        type: 'text',
        text: JSON.stringify(result, null, 2)
      }]
    };

  } catch (error) {
    throw new Error(`Failed to execute log JSON query: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

// parseTimeRange extracts start and end times from parameters
function parseTimeRange(params: Record<string, any>): { startTime: number; endTime: number } {
  const now = new Date();

  // Default to last 5 minutes if no time parameters provided
  let startTime = now.getTime() - (5 * 60 * 1000); // 5 minutes ago in milliseconds
  let endTime = now.getTime();

  // Check for lookback_minutes
  if (typeof params.lookback_minutes === 'number') {
    startTime = now.getTime() - (params.lookback_minutes * 60 * 1000);
  }

  // Check for explicit start_time_iso
  if (typeof params.start_time_iso === 'string' && params.start_time_iso !== '') {
    const parsed = new Date(params.start_time_iso + 'Z');
    if (!isNaN(parsed.getTime())) {
      startTime = parsed.getTime();
    }
  }

  // Check for explicit end_time_iso
  if (typeof params.end_time_iso === 'string' && params.end_time_iso !== '') {
    const parsed = new Date(params.end_time_iso + 'Z');
    if (!isNaN(parsed.getTime())) {
      endTime = parsed.getTime();
    }
  }

  return { startTime, endTime };
}

export async function handleAddDropRule(
  params: Record<string, any>,
  config: Config
): Promise<{ content: any[] }> {
  console.log('handleAddDropRule called with params:', JSON.stringify(params, null, 2));
  const name = params.name;
  const filters = params.filters;

  if (!name) {
    throw new Error('name is required');
  }

  if (!filters || !Array.isArray(filters) || filters.length === 0) {
    throw new Error('filters array is required and must not be empty');
  }

  // Validate filters
  for (const filter of filters) {
    if (!filter.key || !filter.value || !filter.operator || !filter.conjunction) {
      throw new Error('Each filter must have key, value, operator, and conjunction properties');
    }

    if (!['equals', 'not_equals'].includes(filter.operator)) {
      throw new Error('operator must be either "equals" or "not_equals"');
    }

    if (filter.conjunction !== 'and') {
      throw new Error('conjunction must be "and"');
    }
  }

  try {
    const url = `${config.apiBaseURL}/v1/drop_rules`;
    const requestBody: DropRule = {
      name,
      filters,
    };

    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'X-LAST9-API-TOKEN': `Bearer ${config.accessToken}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(requestBody),
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Failed to add drop rule: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const result = await response.json();

    return {
      content: [{
        type: 'text',
        text: `Drop rule "${name}" created successfully.\n${JSON.stringify(result, null, 2)}`
      }]
    };

  } catch (error) {
    throw new Error(`Failed to add drop rule: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}