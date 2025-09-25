import { McpAgent } from "agents/mcp";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { Config } from './types';
import { setupConfig, populateAPICfg, makePromInstantQuery, makePromRangeQuery, makePromLabelValuesQuery } from './utils';

// Define our Last9 MCP agent with tools
export class Last9MCP extends McpAgent {
	server = new McpServer({
		name: "Last9 MCP Server",
		version: "0.1.13",
	}, {
		capabilities: {
			tools: {},
			prompts: {}
		}
	});

	private config: Config | null = null;

	async init() {
		// Initialize configuration from environment
		this.config = setupConfig(this.env as any);
		if (this.config) {
			await populateAPICfg(this.config);
		}

		// Get exceptions tool
		this.server.tool(
			"get_exceptions",
			{
				limit: z.number().min(1).max(100).optional().default(20),
				lookback_minutes: z.number().min(1).max(1440).optional().default(5),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				span_name: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetExceptions(args);
			}
		);

		// Get service summary tool
		this.server.tool(
			"get_service_summary",
			{
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceSummary(args);
			}
		);

		// Get service environments tool
		this.server.tool(
			"get_service_environments",
			{
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceEnvironments(args);
			}
		);

		// Get service performance details tool
		this.server.tool(
			"get_service_performance_details",
			{
				service_name: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServicePerformanceDetails(args);
			}
		);

		// Get service operations summary tool
		this.server.tool(
			"get_service_operations_summary",
			{
				service_name: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceOperationsSummary(args);
			}
		);

		// Get service dependency graph tool
		this.server.tool(
			"get_service_dependency_graph",
			{
				service_name: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceDependencyGraph(args);
			}
		);

		// Prometheus range query tool
		this.server.tool(
			"prometheus_range_query",
			{
				query: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handlePrometheusRangeQuery(args);
			}
		);

		// Prometheus instant query tool
		this.server.tool(
			"prometheus_instant_query",
			{
				query: z.string(),
				time_iso: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handlePrometheusInstantQuery(args);
			}
		);

		// Prometheus label values tool
		this.server.tool(
			"prometheus_label_values",
			{
				match_query: z.string(),
				label: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handlePrometheusLabelValues(args);
			}
		);

		// Prometheus labels tool
		this.server.tool(
			"prometheus_labels",
			{
				match_query: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handlePrometheusLabels(args);
			}
		);

		// Get logs tool
		this.server.tool(
			"get_logs",
			{
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				lookback_minutes: z.number().min(1).max(1440).optional().default(5),
				limit: z.number().min(1).max(100).optional().default(20),
				logjson_query: z.array(z.record(z.any())).optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetLogs(args);
			}
		);

		// Get service logs tool
		this.server.tool(
			"get_service_logs",
			{
				service_name: z.string(),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				lookback_minutes: z.number().min(1).max(1440).optional().default(5),
				limit: z.number().min(1).max(100).optional().default(20),
				severity_filters: z.array(z.string()).optional(),
				body_filters: z.array(z.string()).optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceLogs(args);
			}
		);

		// Get service traces tool
		this.server.tool(
			"get_service_traces",
			{
				service_name: z.string(),
				lookback_minutes: z.number().min(1).max(1440).optional().default(5),
				start_time_iso: z.string().optional(),
				end_time_iso: z.string().optional(),
				limit: z.number().min(1).max(100).optional().default(10),
				order: z.string().optional().default("Duration"),
				direction: z.enum(["forward", "backward"]).optional().default("backward"),
				span_kind: z.array(z.string()).optional(),
				span_name: z.string().optional(),
				status_code: z.array(z.string()).optional(),
				env: z.string().optional(),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetServiceTraces(args);
			}
		);

		// Get drop rules tool
		this.server.tool(
			"get_drop_rules",
			{},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetDropRules(args);
			}
		);

		// Add drop rule tool
		this.server.tool(
			"add_drop_rule",
			{
				name: z.string(),
				filters: z.array(z.object({
					key: z.string(),
					value: z.string(),
					operator: z.enum(["equals", "not_equals"]),
					conjunction: z.enum(["and"]),
				})),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleAddDropRule(args);
			}
		);

		// Get alert config tool
		this.server.tool(
			"get_alert_config",
			{},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetAlertConfig(args);
			}
		);

		// Get alerts tool
		this.server.tool(
			"get_alerts",
			{
				timestamp: z.number().optional(),
				window: z.number().min(60).max(86400).optional().default(900),
			},
			async (args) => {
				if (!this.config) throw new Error("Configuration not initialized");
				return await this.handleGetAlerts(args);
			}
		);

		// Setup prompts
		this.setupPrompts();
	}

	// Define available prompts
	private PROMPTS = {
		"logjson_query_builder": {
			name: "logjson_query_builder",
			description: "Convert natural language log queries into structured JSON pipeline queries for log analysis",
			arguments: [
				{
					name: "natural_language_query",
					description: "Natural language description of the log query to construct",
					required: true
				},
				{
					name: "query_context",
					description: "Additional context about the query (service, time range, specific fields to focus on)",
					required: false
				}
			]
		}
	};

	// Setup prompts using Cloudflare MCP Agent pattern
	setupPrompts() {
		// Use this.server.prompt() method instead of setRequestHandler
		this.server.prompt(
			"logjson_query_builder",
			{
				natural_language_query: z.string().describe("Natural language description of the log query to construct"),
				query_context: z.string().optional().describe("Additional context about the query (service, time range, specific fields to focus on)")
			},
			"Convert natural language log queries into structured JSON pipeline queries for log analysis",
			async ({ natural_language_query, query_context }) => {
				const naturalQuery = natural_language_query || "";
				const queryContext = query_context || "";

				const promptContent = `You are a specialized log query construction assistant for Last9 observability platform. Your role is to translate natural language queries into structured JSON pipeline queries that can be executed by log analysis tools.

## Your Task:
Convert the following natural language query into a valid JSON pipeline format:
**Query:** ${naturalQuery}
**Context:** ${queryContext}

## JSON Pipeline Format:
You must return a JSON array containing operation objects. Available operations:

### 1. Filter Operations:
- **filter**: Filter logs based on conditions
- **parse**: Parse log content (json, regexp, logfmt)
- **aggregate**: Perform aggregations (sum, avg, count, etc.)
- **window_aggregate**: Time-windowed aggregations
- **transform**: Transform/extract fields
- **select**: Select specific fields and apply limits (default limit: 20)

### 2. Field References:
- **Body**: Log message content
- **service**: Service name
- **severity**: Log level (DEBUG, INFO, WARN, ERROR, FATAL)
- **attributes['field_name']**: Log/span attributes
- **resource_attributes['field_name']**: Resource attributes (prefixed with resource_)

### 3. Common OpenTelemetry Fields:
- **HTTP**: attributes['http.method'], attributes['http.status_code'], attributes['http.route']
- **Database**: attributes['db.system'], attributes['db.statement'], attributes['db.operation']
- **Messaging**: attributes['messaging.system'], attributes['messaging.destination']
- **RPC**: attributes['rpc.system'], attributes['rpc.method'], attributes['rpc.grpc.status_code']
- **Kubernetes**: resource_attributes['k8s.pod.name'], resource_attributes['k8s.namespace.name']
- **Cloud**: resource_attributes['cloud.provider'], resource_attributes['cloud.region']

### 4. Filter Operators:
- **$eq**: Equals
- **$neq**: Not equals
- **$gt**: Greater than
- **$lt**: Less than
- **$gte**: Greater than or equal
- **$lte**: Less than or equal
- **$contains**: Contains text
- **$notcontains**: Doesn't contain text
- **$regex**: Regex match
- **$notnull**: Field exists
- **$and**: Multiple conditions (AND)
- **$or**: Multiple conditions (OR)

### 5. Aggregation Functions:
- **$sum**: Sum values
- **$avg**: Average values
- **$count**: Count records
- **$min**: Minimum value
- **$max**: Maximum value
- **$quantile**: Percentile calculation
- **$rate**: Rate calculation

### 6. Common Patterns:
- **5xx errors**: {"$and": [{"$gte": ["attributes['http.status_code']", 500]}, {"$lt": ["attributes['http.status_code']", 600]}]}
- **4xx errors**: {"$and": [{"$gte": ["attributes['http.status_code']", 400]}, {"$lt": ["attributes['http.status_code']", 500]}]}
- **Slow requests**: {"$gt": ["attributes['duration']", threshold_ms]}
- **Database errors**: {"$and": [{"$notnull": ["attributes['db.statement']"]}, {"$contains": ["Body", "error"]}]}
- **Authentication failures**: {"$or": [{"$eq": ["attributes['http.status_code']", 401]}, {"$contains": ["Body", "authentication failed"]}]}

### 7. Time Windows:
- **5 minutes**: ["5", "minutes"]
- **1 hour**: ["1", "hours"]
- **1 day**: ["24", "hours"]

### 8. Grouping:
- **By service**: {"resource_attributes['service.name']": "service"}
- **By endpoint**: {"attributes['http.route']": "endpoint"}
- **By host**: {"resource_attributes['host.name']": "host"}
- **By namespace**: {"resource_attributes['k8s.namespace.name']": "namespace"}

### 9. Select Operations:
- **Limit results**: {"type": "select", "limit": 20}
- **Custom limit**: {"type": "select", "limit": 50}
- **No limit**: Omit select operation (returns all results)

## Instructions:
1. Analyze the natural language query carefully
2. Identify the required operations (filter, parse, aggregate, etc.)
3. Use appropriate field references and operators
4. Return ONLY a valid JSON array - no explanations
5. Ensure proper JSON syntax and structure
6. Chain operations logically: filter → parse → aggregate → select
7. Add a select operation with limit: 20 for result limiting (unless a different limit is specified)

## Example Output Format:
[{
  "type": "filter",
  "query": {
    "$contains": ["Body", "error"]
  }
}, {
  "type": "aggregate",
  "function": {"$count": []},
  "as": "error_count",
  "groupby": {"service": "service"}
}, {
  "type": "select",
  "limit": 20
}]

Return only the JSON array for the given query.`;

				return [
					{
						role: "system",
						content: {
							type: "text",
							text: promptContent,
						},
					},
				];
			}
		);
	}

	// Tool implementation methods
	private getTimeRange(params: any, defaultLookbackMinutes: number = 60): { startTime: Date; endTime: Date } {
		let endTime = new Date();
		let lookbackMinutes = defaultLookbackMinutes;

		if (typeof params.lookback_minutes === 'number') {
			lookbackMinutes = params.lookback_minutes;
			if (lookbackMinutes < 1) {
				throw new Error('lookback_minutes must be at least 1');
			}
			if (lookbackMinutes > 1440) {
				throw new Error('lookback_minutes cannot exceed 1440 (24 hours)');
			}
		}

		let startTime = new Date(endTime.getTime() - lookbackMinutes * 60000);

		// Override with explicit timestamps if provided
		if (typeof params.start_time_iso === 'string' && params.start_time_iso !== '') {
			const parsed = new Date(params.start_time_iso.replace(' ', 'T') + 'Z');
			if (isNaN(parsed.getTime())) {
				throw new Error('Invalid start_time_iso format');
			}
			startTime = parsed;
		}

		if (typeof params.end_time_iso === 'string' && params.end_time_iso !== '') {
			const parsed = new Date(params.end_time_iso.replace(' ', 'T') + 'Z');
			if (isNaN(parsed.getTime())) {
				throw new Error('Invalid end_time_iso format');
			}
			endTime = parsed;
		}

		return { startTime, endTime };
	}

	private async makeAPIRequest(endpoint: string, method: string = 'GET', body?: any) {
		if (!this.config) throw new Error("Configuration not initialized");

		const url = `${this.config.apiBaseURL}${endpoint}`;
		const headers: Record<string, string> = {
			'X-LAST9-API-TOKEN': `Bearer ${this.config.accessToken}`,
			'Content-Type': 'application/json',
		};

		const response = await fetch(url, {
			method,
			headers,
			body: body ? JSON.stringify(body) : undefined,
		});

		if (!response.ok) {
			throw new Error(`API request failed: ${response.status} ${response.statusText}`);
		}

		return response.json();
	}

	private async makeBaseURLRequest(endpoint: string, method: string = 'GET', body?: any) {
		if (!this.config) throw new Error("Configuration not initialized");

		const url = `${this.config.baseURL}${endpoint}`;
		const headers: Record<string, string> = {
			'Authorization': `Basic ${this.config.authToken.replace('Basic ', '')}`,
			'Content-Type': 'application/json',
		};

		const response = await fetch(url, {
			method,
			headers,
			body: body ? JSON.stringify(body) : undefined,
		});

		if (!response.ok) {
			throw new Error(`Base URL API request failed: ${response.status} ${response.statusText}`);
		}

		return response.json();
	}

	private async makePromQuery(query: string, type: 'instant' | 'range', params: any = {}) {
		if (!this.config) throw new Error("Configuration not initialized");

		const baseUrl = `${this.config.prometheusReadURL}/api/v1`;
		let endpoint = type === 'instant' ? '/query' : '/query_range';

		const urlParams = new URLSearchParams({ query });

		if (type === 'instant') {
			if (params.time) urlParams.append('time', params.time.toString());
		} else {
			if (params.start) urlParams.append('start', params.start.toString());
			if (params.end) urlParams.append('end', params.end.toString());
			if (params.step) urlParams.append('step', params.step);
		}

		const response = await fetch(`${baseUrl}${endpoint}?${urlParams}`, {
			headers: {
				'X-LAST9-API-TOKEN': `Bearer ${this.config.accessToken}`,
				'Content-Type': 'application/json',
			},
		});

		if (!response.ok) {
			throw new Error(`Prometheus query failed: ${response.status} ${response.statusText}`);
		}

		return response.json();
	}

	// Tool handler implementations
	private async handleGetExceptions(params: any) {
		const { startTime, endTime } = this.getTimeRange(params, 60);
		const limit = params.limit || 20;
		const spanName = params.span_name || '';

		// Build query parameters like Go implementation
		const queryParams = new URLSearchParams();
		queryParams.set('start', Math.floor(startTime.getTime() / 1000).toString());
		queryParams.set('end', Math.floor(endTime.getTime() / 1000).toString());
		queryParams.set('limit', limit.toString());

		if (spanName) {
			queryParams.set('span_name', spanName);
		}

		const url = `${this.config!.baseURL}/telemetry/api/v1/exceptions?${queryParams.toString()}`;

		// Use GET method with Basic auth like Go implementation
		let authToken = this.config!.authToken;
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
			throw new Error(`Failed to get exceptions: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();

		return {
			content: [{ type: "text", text: JSON.stringify(data.exceptions || [], null, 2) }],
		};
	}

	private async handleGetServiceSummary(params: any) {
		const { startTime, endTime } = this.getTimeRange(params, 60);
		const env = params.env || '.*';

		const startTimeParam = Math.floor(startTime.getTime() / 1000);
		const endTimeParam = Math.floor(endTime.getTime() / 1000);
		const duration = Math.floor((endTimeParam - startTimeParam) / 60);

		// Build PromQL query for throughput
		const throughputQuery = `quantile_over_time(0.95, sum by (service_name)(trace_endpoint_count{env=~'${env}', span_kind='SPAN_KIND_SERVER'}[${duration}m]))`;

		// Use Last9's API endpoint like Go implementation
		const response = await makePromInstantQuery(throughputQuery, endTimeParam, this.config!);

		if (!response.ok) {
			throw new Error(`Failed to get service summary: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();
		const results = data.data?.result || [];

		if (results.length === 0) {
			return {
				content: [{ type: "text", text: "No services found for the given parameters" }],
			};
		}

		const services = results.map((result: any) => ({
			serviceName: result.metric.service_name,
			env,
			throughput: parseFloat(result.value[1]),
			errorRate: 0,
			responseTime: 0,
		}));

		return {
			content: [{ type: "text", text: JSON.stringify(services, null, 2) }],
		};
	}

	private async handleGetServiceEnvironments(params: any) {
		const { startTime, endTime } = this.getTimeRange(params, 60);
		const startTimeParam = Math.floor(startTime.getTime() / 1000);
		const endTimeParam = Math.floor(endTime.getTime() / 1000);

		// Use Last9's /prom_label_values API endpoint exactly like Go implementation
		// Go: utils.MakePromLabelValuesAPIQuery(client, "env", "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}", startTimeParam, endTimeParam, cfg)
		const requestBody = {
			label: "env",
			timestamp: startTimeParam,
			window: endTimeParam - startTimeParam,
			read_url: this.config!.prometheusReadURL,
			username: this.config!.prometheusUsername,
			password: this.config!.prometheusPassword,
			matches: ["domain_attributes_count{span_kind='SPAN_KIND_SERVER'}"]
		};

		const url = `${this.config!.apiBaseURL}/prom_label_values`;
		const response = await fetch(url, {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
				'X-LAST9-API-TOKEN': `Bearer ${this.config!.accessToken}`,
			},
			body: JSON.stringify(requestBody),
		});

		if (!response.ok) {
			throw new Error(`Failed to get service environments: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();
		const environments = data.data || [];

		return {
			content: [{ type: "text", text: JSON.stringify(environments, null, 2) }],
		};
	}

	private async handleGetServicePerformanceDetails(params: any) {
		const serviceName = params.service_name;
		if (!serviceName) {
			throw new Error('service_name is required');
		}

		// Parse time parameters exactly like Go implementation
		let startTimeParam: number, endTimeParam: number;

		// Handle end_time
		if (params.end_time && typeof params.end_time === 'string') {
			const t = new Date(params.end_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid end_time format, must be ISO8601');
			}
			endTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			endTimeParam = Math.floor(Date.now() / 1000);
		}

		// Handle start_time
		if (params.start_time && typeof params.start_time === 'string') {
			const t = new Date(params.start_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid start_time format, must be ISO8601');
			}
			startTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			startTimeParam = endTimeParam - 3600; // Default 1 hour back
		}

		// Handle environment exactly like Go
		const env = (params.env && typeof params.env === 'string' && params.env !== '') ? params.env : '.*';

		const timeRange = `${Math.floor((endTimeParam - startTimeParam) / 60)}m`;

		const details: any = {
			service_name: serviceName,
			env: env,
		};

		try {
			// Get Apdex Score over time range as a vector
			const apdexQuery = `sum(trace_service_apdex_score{service_name='${serviceName}', env=~'${env}'})`;
			const apdexResponse = await makePromRangeQuery(apdexQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (apdexResponse.ok) {
				const apdexData = await apdexResponse.json();
				details.apdex_score = apdexData.data?.result || [];
			}

			// Get Response Times - keep vector output
			const rtQuery = `sum by (quantile) (trace_service_response_time{service_name='${serviceName}', env='${env}'}[${timeRange}])`;
			const rtResponse = await makePromRangeQuery(rtQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (rtResponse.ok) {
				const rtData = await rtResponse.json();
				details.response_times = rtData.data?.result || [];
			}

			// Get Availability over time range as a vector
			const availQuery = `(1 - (sum(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[${timeRange}])) or 0) / (sum(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER'}[${timeRange}])) + 0.0000001)) * 100 default -999`;
			const availResponse = await makePromRangeQuery(availQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (availResponse.ok) {
				const availData = await availResponse.json();
				details.availability = availData.data?.result || [];
			}

			// Get Throughput by status code - keep vector output
			const throughputQuery = `sum by (http_status_code)(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER'}[${timeRange}])) * 60 default 0`;
			const throughputResponse = await makePromRangeQuery(throughputQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (throughputResponse.ok) {
				const throughputData = await throughputResponse.json();
				details.throughput = throughputData.data?.result || [];
			}

			// Get Error Rate by status code - keep vector output
			const errorRateQuery = `sum by (service_name, http_status_code)(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[${timeRange}])) * 60 default 0`;
			const errorRateResponse = await makePromRangeQuery(errorRateQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (errorRateResponse.ok) {
				const errorRateData = await errorRateResponse.json();
				details.error_rate = errorRateData.data?.result || [];
			}

			// Calculate Error Percentage over time range as a vector
			const errorPercentQuery = `(sum(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER', http_status_code=~'4.*|5.*'}[${timeRange}])) / sum(rate(trace_endpoint_count{service_name='${serviceName}', env='${env}', span_kind='SPAN_KIND_SERVER'}[${timeRange}])) * 100) default 0`;
			const errorPercentResponse = await makePromRangeQuery(errorPercentQuery, startTimeParam, endTimeParam, '60s', this.config!);
			if (errorPercentResponse.ok) {
				const errorPercentData = await errorPercentResponse.json();
				details.error_percentage = errorPercentData.data?.result || [];
			}

			// Get Top 10 Operations by Response Time - keep vector output
			const topRTQuery = `topk(10, quantile_over_time(0.95, sum by (span_name, messaging_system, rpc_system, span_kind,net_peer_name,process_runtime_name,db_system)(trace_endpoint_duration{service_name='${serviceName}', span_kind!='SPAN_KIND_INTERNAL', env='${env}', quantile='p95'}[${timeRange}])))`;
			const topRTResponse = await makePromInstantQuery(topRTQuery, endTimeParam, this.config!);
			if (topRTResponse.ok) {
				const topRTData = await topRTResponse.json();
				const topOperationsByResponseTime: any[] = [];
				if (topRTData.data?.result) {
					for (const r of topRTData.data.result) {
						const key = `${r.metric?.span_name || ''}-${r.metric?.span_kind || ''}-${r.metric?.net_peer_name || ''}-${r.metric?.db_system || ''}-${r.metric?.rpc_system || ''}-${r.metric?.messaging_system || ''}-${r.metric?.process_runtime_name || ''}`;
						if (r.value && r.value[1]) {
							const val = parseFloat(r.value[1]);
							const op: any = {};
							op[key] = val;
							topOperationsByResponseTime.push(op);
						}
					}
				}
				details.top_operations = { by_response_time: topOperationsByResponseTime };
			}

			// Get Top 10 Operations by Error Rate - keep vector output
			const topErrQuery = `sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_client_count{service_name="${serviceName}", env='${env}', exception_type!=''}[${timeRange}])) or sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, exception_type)(sum_over_time(trace_endpoint_count{service_name="${serviceName}", env='${env}', exception_type!=''}[${timeRange}])) or sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_client_count{service_name="${serviceName}", env='${env}', http_status_code=~"^[45].*"}[${timeRange}])) or sum by (span_name, span_kind, net_peer_name, db_system, rpc_system, messaging_system, process_runtime_name, http_status_code)(sum_over_time(trace_endpoint_count{service_name="${serviceName}", env='${env}', http_status_code=~"^[45].*"}[${timeRange}]))`;
			const topErrResponse = await makePromInstantQuery(topErrQuery, endTimeParam, this.config!);
			if (topErrResponse.ok) {
				const topErrData = await topErrResponse.json();
				const topOperationsByErrorRate: any[] = [];
				if (topErrData.data?.result) {
					for (const r of topErrData.data.result) {
						const key = `${r.metric?.span_name || ''}-${r.metric?.span_kind || ''}-${r.metric?.net_peer_name || ''}-${r.metric?.db_system || ''}-${r.metric?.rpc_system || ''}-${r.metric?.messaging_system || ''}-${r.metric?.process_runtime_name || ''}`;
						if (r.value && r.value[1]) {
							const val = parseInt(r.value[1], 10);
							const op: any = {};
							op[key] = val;
							topOperationsByErrorRate.push(op);
						}
					}
				}
				if (!details.top_operations) details.top_operations = {};
				details.top_operations.by_error_rate = topOperationsByErrorRate;
			}

			// Get Top 10 Errors/Exceptions by Count
			const topErrorsQuery = `topk(10, sum by (exception_type, http_status_code)(sum_over_time(trace_client_count{service_name="${serviceName}", env='${env}'}[${timeRange}])) or sum by (exception_type, http_status_code)(sum_over_time(trace_endpoint_count{service_name="${serviceName}", env='${env}'}[${timeRange}])))`;
			const topErrorsResponse = await makePromInstantQuery(topErrorsQuery, endTimeParam, this.config!);
			if (topErrorsResponse.ok) {
				const topErrorsData = await topErrorsResponse.json();
				const topErrors: any[] = [];
				if (topErrorsData.data?.result) {
					for (const r of topErrorsData.data.result) {
						let key = '';
						if (r.metric?.exception_type && r.metric.exception_type !== '') {
							key = r.metric.exception_type;
						} else if (r.metric?.http_status_code && r.metric.http_status_code !== '') {
							key = r.metric.http_status_code;
						} else {
							continue; // skip if neither is present
						}
						if (r.value && r.value[1]) {
							const val = parseInt(r.value[1], 10);
							const op: any = {};
							op[key] = val;
							topErrors.push(op);
						}
					}
				}
				details.top_errors = topErrors;
			}

		} catch (error) {
			throw new Error(`Failed to get service performance details: ${error instanceof Error ? error.message : 'Unknown error'}`);
		}

		return {
			content: [{ type: "text", text: JSON.stringify(details, null, 2) }],
		};
	}

	private async handleGetServiceOperationsSummary(params: any) {
		const serviceName = params.service_name;
		if (!serviceName) {
			throw new Error('service_name is required');
		}

		// Parse time parameters exactly like Go implementation
		let startTimeParam: number, endTimeParam: number;

		// Handle end_time
		if (params.end_time && typeof params.end_time === 'string') {
			const t = new Date(params.end_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid end_time format, must be ISO8601');
			}
			endTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			endTimeParam = Math.floor(Date.now() / 1000);
		}

		// Handle start_time
		if (params.start_time && typeof params.start_time === 'string') {
			const t = new Date(params.start_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid start_time format, must be ISO8601');
			}
			startTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			startTimeParam = endTimeParam - 3600; // default to last hour
		}

		const env = (params.env && typeof params.env === 'string' && params.env !== '') ? params.env : '.*';
		const timeRange = `${Math.floor((endTimeParam - startTimeParam) / 60)}m`;
		const timeDivider = Math.floor((endTimeParam - startTimeParam) / 60);

		const operationsSummary: any[] = [];

		try {
			// 1. Prepare the Prometheus query for throughput of endpoint operations
			const throughputQuery = `sum by (span_name, span_kind)(sum_over_time(trace_endpoint_count{service_name='${serviceName}', span_kind='SPAN_KIND_SERVER', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const throughputResponse = await makePromInstantQuery(throughputQuery, endTimeParam, this.config!);
			let throughputData: any[] = [];
			if (throughputResponse.ok) {
				const response = await throughputResponse.json();
				throughputData = response.data?.result || [];
			}

			// 2. Prepare the Prometheus query for response times of endpoint operations
			const respTimeQuery = `quantile_over_time(0.95, sum by (quantile, span_name, span_kind) (trace_endpoint_duration{service_name='${serviceName}', span_kind='SPAN_KIND_SERVER', env='${env}'}[${timeRange}]))`;
			const respTimeResponse = await makePromInstantQuery(respTimeQuery, endTimeParam, this.config!);
			let respTimeData: any[] = [];
			if (respTimeResponse.ok) {
				const response = await respTimeResponse.json();
				respTimeData = response.data?.result || [];
			}

			// 3. Prepare the Prometheus query for error rate of endpoint operations
			const errorRateQuery = `100 * (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='${serviceName}', span_kind='SPAN_KIND_SERVER', env=~'${env}', http_status_code=~'4.*|5.*'}[${timeRange}])) / ${timeDivider}) / (sum by (span_name, span_kind) (sum_over_time(trace_endpoint_count{service_name='${serviceName}', span_kind='SPAN_KIND_SERVER', env=~'${env}'}[${timeRange}])) / ${timeDivider})`;
			const errorRateResponse = await makePromInstantQuery(errorRateQuery, endTimeParam, this.config!);
			let errorRateData: any[] = [];
			if (errorRateResponse.ok) {
				const response = await errorRateResponse.json();
				errorRateData = response.data?.result || [];
			}

			// Process HTTP endpoint operations
			for (const r of throughputData) {
				const operation: any = {
					name: r.metric?.span_name,
					service_name: serviceName,
					env: env,
					throughput: 0,
					error_rate: 0,
					response_time: { p95: 0, p90: 0, p50: 0, avg: 0 },
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					operation.throughput = parseFloat(r.value[1]);
				}

				// Find matching response time data
				for (const rt of respTimeData) {
					if (rt.metric?.span_name === operation.name) {
						const quantile = rt.metric?.quantile;
						if (quantile && rt.value && rt.value[1]) {
							operation.response_time[quantile] = parseFloat(rt.value[1]);
						}
					}
				}

				// Find matching error rate data
				for (const er of errorRateData) {
					if (er.metric?.span_name === operation.name) {
						if (er.value && er.value[1]) {
							operation.error_rate = parseFloat(er.value[1]);
						}
					}
				}

				// Calculate error percentage
				if (operation.throughput > 0) {
					operation.error_percent = (operation.error_rate / operation.throughput) * 100;
				}

				operationsSummary.push(operation);
			}

			// 4. Database operations
			const dbThroughputQuery = `sum by (span_name, db_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='${serviceName}', span_kind='SPAN_KIND_CLIENT', db_system!='', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const dbThroughputResponse = await makePromInstantQuery(dbThroughputQuery, endTimeParam, this.config!);
			let dbThroughputData: any[] = [];
			if (dbThroughputResponse.ok) {
				const response = await dbThroughputResponse.json();
				dbThroughputData = response.data?.result || [];
			}

			const dbRespTimeQuery = `quantile_over_time(0.95, sum by (quantile, span_name, db_system, net_peer_name, rpc_system, span_kind) (trace_client_duration{service_name='${serviceName}', span_kind='SPAN_KIND_CLIENT', db_system!='', env='${env}'}[${timeRange}]))`;
			const dbRespTimeResponse = await makePromInstantQuery(dbRespTimeQuery, endTimeParam, this.config!);
			let dbRespTimeData: any[] = [];
			if (dbRespTimeResponse.ok) {
				const response = await dbRespTimeResponse.json();
				dbRespTimeData = response.data?.result || [];
			}

			// Process database operations
			for (const r of dbThroughputData) {
				const operation: any = {
					name: r.metric?.span_name,
					service_name: serviceName,
					env: env,
					db_system: r.metric?.db_system,
					net_peer_name: r.metric?.net_peer_name,
					throughput: 0,
					error_rate: 0,
					response_time: { p95: 0, p90: 0, p50: 0, avg: 0 },
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					operation.throughput = parseFloat(r.value[1]);
				}

				// Find matching response time data
				for (const rt of dbRespTimeData) {
					if (rt.metric?.span_name === operation.name &&
						rt.metric?.db_system === operation.db_system &&
						rt.metric?.net_peer_name === operation.net_peer_name) {
						const quantile = rt.metric?.quantile;
						if (quantile && rt.value && rt.value[1]) {
							operation.response_time[quantile] = parseFloat(rt.value[1]);
						}
					}
				}

				operationsSummary.push(operation);
			}

			// 5. HTTP Client operations
			const httpThroughputQuery = `sum by (span_name, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='${serviceName}', span_kind='SPAN_KIND_CLIENT', rpc_system!='', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const httpThroughputResponse = await makePromInstantQuery(httpThroughputQuery, endTimeParam, this.config!);
			let httpThroughputData: any[] = [];
			if (httpThroughputResponse.ok) {
				const response = await httpThroughputResponse.json();
				httpThroughputData = response.data?.result || [];
			}

			// Process HTTP client operations
			for (const r of httpThroughputData) {
				const operation: any = {
					name: r.metric?.span_name,
					service_name: serviceName,
					env: env,
					net_peer_name: r.metric?.net_peer_name,
					rpc_system: r.metric?.rpc_system,
					throughput: 0,
					error_rate: 0,
					response_time: { p95: 0, p90: 0, p50: 0, avg: 0 },
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					operation.throughput = parseFloat(r.value[1]);
				}

				operationsSummary.push(operation);
			}

			// 6. Messaging operations
			const messagingThroughputQuery = `sum by (span_name, messaging_system, net_peer_name, rpc_system, span_kind)(sum_over_time(trace_client_count{service_name='${serviceName}', messaging_system!='', span_kind='SPAN_KIND_PRODUCER', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const messagingThroughputResponse = await makePromInstantQuery(messagingThroughputQuery, endTimeParam, this.config!);
			let messagingThroughputData: any[] = [];
			if (messagingThroughputResponse.ok) {
				const response = await messagingThroughputResponse.json();
				messagingThroughputData = response.data?.result || [];
			}

			// Process messaging operations
			for (const r of messagingThroughputData) {
				const operation: any = {
					name: r.metric?.span_name,
					service_name: serviceName,
					env: env,
					messaging_system: r.metric?.messaging_system,
					net_peer_name: r.metric?.net_peer_name,
					rpc_system: r.metric?.rpc_system,
					throughput: 0,
					error_rate: 0,
					response_time: { p95: 0, p90: 0, p50: 0, avg: 0 },
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					operation.throughput = parseFloat(r.value[1]);
				}

				operationsSummary.push(operation);
			}

		} catch (error) {
			throw new Error(`Failed to get service operations summary: ${error instanceof Error ? error.message : 'Unknown error'}`);
		}

		// Prepare the final response structure exactly like Go
		const details = {
			service_name: serviceName,
			env: env,
			operations: operationsSummary,
		};

		return {
			content: [{ type: "text", text: JSON.stringify(details, null, 2) }],
		};
	}

	private async handleGetServiceDependencyGraph(params: any) {
		const serviceName = params.service_name;
		if (!serviceName) {
			throw new Error('service_name is required');
		}

		// Parse time parameters exactly like Go implementation
		let startTimeParam: number, endTimeParam: number;

		// Handle end_time
		if (params.end_time && typeof params.end_time === 'string') {
			const t = new Date(params.end_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid end_time format, must be ISO8601');
			}
			endTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			endTimeParam = Math.floor(Date.now() / 1000);
		}

		// Handle start_time
		if (params.start_time && typeof params.start_time === 'string') {
			const t = new Date(params.start_time);
			if (isNaN(t.getTime())) {
				throw new Error('invalid start_time format, must be ISO8601');
			}
			startTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			startTimeParam = endTimeParam - 3600; // default to last hour
		}

		const env = (params.env && typeof params.env === 'string' && params.env !== '') ? params.env : '.*';
		const timeRange = `${Math.floor((endTimeParam - startTimeParam) / 60)}m`;
		const timeDivider = Math.floor((endTimeParam - startTimeParam) / 60);

		const incoming: Record<string, any> = {};
		const outgoing: Record<string, any> = {};
		const databases: Record<string, any> = {};
		const messagingSystems: Record<string, any> = {};

		try {
			// Incoming requests (HTTP server operations)
			// 1. Throughput
			const incomingThroughputQuery = `sum by (client)(sum_over_time(trace_call_graph_count{server='${serviceName}', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const incomingThroughputResponse = await makePromInstantQuery(incomingThroughputQuery, endTimeParam, this.config!);
			let incomingThroughputData: any[] = [];
			if (incomingThroughputResponse.ok) {
				const response = await incomingThroughputResponse.json();
				incomingThroughputData = response.data?.result || [];
			}

			// 2. Response times
			const incomingRespTimeQuery = `quantile_over_time(0.95, sum by (client, quantile) (trace_call_graph_duration{server='${serviceName}', env=~'${env}'}[${timeRange}]))`;
			const incomingRespTimeResponse = await makePromInstantQuery(incomingRespTimeQuery, endTimeParam, this.config!);
			let incomingRespTimeData: any[] = [];
			if (incomingRespTimeResponse.ok) {
				const response = await incomingRespTimeResponse.json();
				incomingRespTimeData = response.data?.result || [];
			}

			// 3. Error rate
			const incomingErrorRateQuery = `sum by (client)(sum_over_time(trace_call_graph_count{server='${serviceName}', env=~'${env}', client_status=~'4.*|5.*'}[${timeRange}])) / ${timeDivider}`;
			const incomingErrorRateResponse = await makePromInstantQuery(incomingErrorRateQuery, endTimeParam, this.config!);
			let incomingErrorRateData: any[] = [];
			if (incomingErrorRateResponse.ok) {
				const response = await incomingErrorRateResponse.json();
				incomingErrorRateData = response.data?.result || [];
			}

			// Process incoming data
			for (const r of incomingThroughputData) {
				const client = r.metric?.client || 'unknown';
				const metrics: any = {
					throughput: 0,
					response_time_p95: 0,
					response_time_p90: 0,
					response_time_p50: 0,
					response_time_avg: 0,
					error_rate: 0,
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					metrics.throughput = parseFloat(r.value[1]);
				}
				incoming[client] = metrics;
			}

			for (const r of incomingRespTimeData) {
				const client = r.metric?.client || 'unknown';
				const quantile = r.metric?.quantile;
				if (incoming[client] && quantile && r.value && r.value[1]) {
					const val = parseFloat(r.value[1]);
					switch (quantile) {
						case 'p95':
							incoming[client].response_time_p95 = val;
							break;
						case 'p90':
							incoming[client].response_time_p90 = val;
							break;
						case 'p50':
							incoming[client].response_time_p50 = val;
							break;
						case 'avg':
							incoming[client].response_time_avg = val;
							break;
					}
				}
			}

			for (const r of incomingErrorRateData) {
				const client = r.metric?.client || 'unknown';
				if (incoming[client] && r.value && r.value[1]) {
					incoming[client].error_rate = parseFloat(r.value[1]);
				}
			}

			// Calculate error percentages for incoming
			for (const client in incoming) {
				const metrics = incoming[client];
				if (metrics.throughput > 0) {
					metrics.error_percent = (metrics.error_rate / metrics.throughput) * 100;
				}
			}

			// Outgoing requests (HTTP client operations)
			// 1. Throughput
			const outgoingThroughputQuery = `sum by (server)(sum_over_time(trace_call_graph_count{client='${serviceName}', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const outgoingThroughputResponse = await makePromInstantQuery(outgoingThroughputQuery, endTimeParam, this.config!);
			let outgoingThroughputData: any[] = [];
			if (outgoingThroughputResponse.ok) {
				const response = await outgoingThroughputResponse.json();
				outgoingThroughputData = response.data?.result || [];
			}

			// Process outgoing data (simplified for space - similar pattern as incoming)
			for (const r of outgoingThroughputData) {
				const server = r.metric?.server || 'unknown';
				const metrics: any = {
					throughput: 0,
					response_time_p95: 0,
					response_time_p90: 0,
					response_time_p50: 0,
					response_time_avg: 0,
					error_rate: 0,
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					metrics.throughput = parseFloat(r.value[1]);
				}
				outgoing[server] = metrics;
			}

			// Database operations
			// 1. Throughput
			const dbThroughputQuery = `sum by (net_peer_name, db_system)(sum_over_time(trace_client_count{service_name='${serviceName}', db_system!='', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const dbThroughputResponse = await makePromInstantQuery(dbThroughputQuery, endTimeParam, this.config!);
			let dbThroughputData: any[] = [];
			if (dbThroughputResponse.ok) {
				const response = await dbThroughputResponse.json();
				dbThroughputData = response.data?.result || [];
			}

			// Process database data
			for (const r of dbThroughputData) {
				const netPeerName = r.metric?.net_peer_name || 'unknown';
				const dbSystem = r.metric?.db_system || 'unknown';
				const key = `${netPeerName}-${dbSystem}`;
				const metrics: any = {
					throughput: 0,
					response_time_p95: 0,
					response_time_p90: 0,
					response_time_p50: 0,
					response_time_avg: 0,
					error_rate: 0,
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					metrics.throughput = parseFloat(r.value[1]);
				}
				databases[key] = metrics;
			}

			// Messaging systems
			// 1. Throughput
			const messagingThroughputQuery = `sum by (net_peer_name, messaging_system)(sum_over_time(trace_client_count{service_name='${serviceName}', messaging_system!='', env=~'${env}'}[${timeRange}])) / ${timeDivider}`;
			const messagingThroughputResponse = await makePromInstantQuery(messagingThroughputQuery, endTimeParam, this.config!);
			let messagingThroughputData: any[] = [];
			if (messagingThroughputResponse.ok) {
				const response = await messagingThroughputResponse.json();
				messagingThroughputData = response.data?.result || [];
			}

			// Process messaging data
			for (const r of messagingThroughputData) {
				const netPeerName = r.metric?.net_peer_name || 'unknown';
				const messagingSystem = r.metric?.messaging_system || 'unknown';
				const key = `${netPeerName}-${messagingSystem}`;
				const metrics: any = {
					throughput: 0,
					response_time_p95: 0,
					response_time_p90: 0,
					response_time_p50: 0,
					response_time_avg: 0,
					error_rate: 0,
					error_percent: 0,
				};

				if (r.value && r.value[1]) {
					metrics.throughput = parseFloat(r.value[1]);
				}
				messagingSystems[key] = metrics;
			}

		} catch (error) {
			throw new Error(`Failed to get service dependency graph: ${error instanceof Error ? error.message : 'Unknown error'}`);
		}

		// Prepare the final response structure exactly like Go
		const details = {
			service_name: serviceName,
			env: env,
			incoming: incoming,
			outgoing: outgoing,
			messaging_systems: messagingSystems,
			databases: databases,
		};

		return {
			content: [{ type: "text", text: JSON.stringify(details, null, 2) }],
		};
	}

	private async handlePrometheusRangeQuery(params: any) {
		const query = params.query;
		if (!query) {
			throw new Error('query is required');
		}

		// Parse time parameters exactly like Go implementation
		let startTimeParam: number, endTimeParam: number;

		// Handle end_time
		if (params.end_time_iso && typeof params.end_time_iso === 'string') {
			const t = new Date(params.end_time_iso);
			if (isNaN(t.getTime())) {
				throw new Error('invalid end_time_iso format, must be ISO8601');
			}
			endTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			endTimeParam = Math.floor(Date.now() / 1000);
		}

		// Handle start_time
		if (params.start_time_iso && typeof params.start_time_iso === 'string') {
			const t = new Date(params.start_time_iso);
			if (isNaN(t.getTime())) {
				throw new Error('invalid start_time_iso format, must be ISO8601');
			}
			startTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			startTimeParam = endTimeParam - 3600; // default to last hour
		}

		// Calculate step like Go implementation
		const duration = endTimeParam - startTimeParam;
		const step = Math.max(60, Math.floor(duration / 1000)) + 's';

		// Use makePromRangeQuery like other functions to call Last9's API endpoint
		const response = await makePromRangeQuery(query, startTimeParam, endTimeParam, step, this.config!);

		if (!response.ok) {
			throw new Error(`Failed to execute Prometheus range query: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();

		return {
			content: [{ type: "text", text: JSON.stringify(data.data?.result || [], null, 2) }],
		};
	}

	private async handlePrometheusInstantQuery(params: any) {
		const query = params.query;
		if (!query) {
			throw new Error('query is required');
		}

		let queryTime: number;

		// Handle time_iso exactly like Go implementation
		if (params.time_iso && typeof params.time_iso === 'string' && params.time_iso !== '') {
			const t = new Date(params.time_iso);
			if (isNaN(t.getTime())) {
				throw new Error('invalid time_iso format, must be ISO8601');
			}
			queryTime = Math.floor(t.getTime() / 1000);
		} else {
			queryTime = Math.floor(Date.now() / 1000);
		}

		// Use makePromInstantQuery like other functions to call Last9's API endpoint
		const response = await makePromInstantQuery(query, queryTime, this.config!);

		if (!response.ok) {
			throw new Error(`Failed to execute Prometheus instant query: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();

		return {
			content: [{ type: "text", text: JSON.stringify(data.data?.result || [], null, 2) }],
		};
	}

	private async handlePrometheusLabelValues(params: any) {
		const matchQuery = params.match_query;
		const label = params.label;

		if (!matchQuery) {
			throw new Error('match_query is required');
		}
		if (!label) {
			throw new Error('label is required');
		}

		// Parse time parameters exactly like Go implementation
		let startTimeParam: number, endTimeParam: number;

		// Handle end_time
		if (params.end_time_iso && typeof params.end_time_iso === 'string') {
			const t = new Date(params.end_time_iso);
			if (isNaN(t.getTime())) {
				throw new Error('invalid end_time_iso format, must be ISO8601');
			}
			endTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			endTimeParam = Math.floor(Date.now() / 1000);
		}

		// Handle start_time
		if (params.start_time_iso && typeof params.start_time_iso === 'string') {
			const t = new Date(params.start_time_iso);
			if (isNaN(t.getTime())) {
				throw new Error('invalid start_time_iso format, must be ISO8601');
			}
			startTimeParam = Math.floor(t.getTime() / 1000);
		} else {
			startTimeParam = endTimeParam - 3600; // default to last hour
		}

		// Use makePromLabelValuesQuery like Go implementation to call Last9's API endpoint
		const response = await makePromLabelValuesQuery(label, matchQuery, startTimeParam, endTimeParam, this.config!);

		if (!response.ok) {
			throw new Error(`Failed to get label values: ${response.status} ${response.statusText}`);
		}

		const data = await response.json();
		return {
			content: [{ type: "text", text: JSON.stringify(data.data || [], null, 2) }],
		};
	}

	private async handlePrometheusLabels(params: any) {
		if (!this.config) throw new Error("Configuration not initialized");

		// Use the shared handler from prometheus.ts
		const { handlePrometheusLabels } = await import('./tools/prometheus');
		return await handlePrometheusLabels(params, this.config);
	}

	private async handleGetLogs(params: any) {
		if (!this.config) throw new Error("Configuration not initialized");

		// Use the shared handler from logs.ts which requires logjson_query
		const { handleGetLogs } = await import('./tools/logs');
		return await handleGetLogs(params, this.config);
	}

	private async handleGetServiceLogs(params: any) {
		if (!this.config) throw new Error("Configuration not initialized");

		// Use the shared handler from logs.ts which now has physical index support
		const { handleGetServiceLogs } = await import('./tools/logs');
		return await handleGetServiceLogs(params, this.config);
	}

	private async handleGetServiceTraces(params: any) {
		const serviceName = params.service_name;
		const { startTime, endTime } = this.getTimeRange(params, 60);
		const limit = params.limit || 10;
		const env = params.env || '';
		const spanKind = params.span_kind || [];
		const spanName = params.span_name || '';
		const statusCode = params.status_code || [];
		const order = params.order || 'Duration';
		const direction = params.direction || 'backward';

		const data = await this.makeAPIRequest('/v1/traces', 'POST', {
			service_name: serviceName,
			start_time: startTime.toISOString(),
			end_time: endTime.toISOString(),
			limit,
			env,
			span_kind: spanKind,
			span_name: spanName,
			status_code: statusCode,
			order,
			direction,
		});

		return {
			content: [{ type: "text", text: JSON.stringify(data.traces || [], null, 2) }],
		};
	}

	private async handleGetDropRules(params: any) {
		const data = await this.makeAPIRequest('/v1/drop_rules');

		return {
			content: [{ type: "text", text: JSON.stringify(data, null, 2) }],
		};
	}

	private async handleAddDropRule(params: any) {
		const name = params.name;
		const filters = params.filters;

		const data = await this.makeAPIRequest('/v1/drop_rules', 'POST', {
			name,
			filters,
		});

		return {
			content: [{ type: "text", text: `Drop rule "${name}" created successfully.\n${JSON.stringify(data, null, 2)}` }],
		};
	}

	private async handleGetAlertConfig(params: any) {
		const data = await this.makeAPIRequest('/v1/alert_rules');

		return {
			content: [{ type: "text", text: JSON.stringify(data.alert_rules || data.rules || [], null, 2) }],
		};
	}

	private async handleGetAlerts(params: any) {
		const timestamp = params.timestamp || Math.floor(Date.now() / 1000);
		const window = params.window || 900;

		const data = await this.makeAPIRequest('/v1/alerts', 'POST', {
			timestamp,
			window,
		});

		return {
			content: [{ type: "text", text: JSON.stringify(data.alerts || data.data || [], null, 2) }],
		};
	}
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext) {
		const url = new URL(request.url);

		if (url.pathname === "/sse" || url.pathname === "/sse/message") {
			return Last9MCP.serveSSE("/sse").fetch(request, env, ctx);
		}

		if (url.pathname === "/mcp") {
			return Last9MCP.serve("/mcp").fetch(request, env, ctx);
		}

		// Health check endpoint
		if (url.pathname === "/health") {
			if (request.method === "OPTIONS") {
				return new Response(null, {
					headers: {
						"Access-Control-Allow-Origin": "*",
						"Access-Control-Allow-Methods": "GET, OPTIONS",
						"Access-Control-Allow-Headers": "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control, X-File-Name",
						"Access-Control-Allow-Credentials": "false",
						"Access-Control-Max-Age": "86400"
					}
				});
			}

			return new Response(JSON.stringify({
				status: "healthy",
				server: "Last9 MCP Server",
				version: "0.1.13"
			}), {
				headers: {
					"Content-Type": "application/json",
					"Access-Control-Allow-Origin": "*"
				}
			});
		}

		// API information endpoint
		if (url.pathname === "/api") {
			if (request.method === "OPTIONS") {
				return new Response(null, {
					headers: {
						"Access-Control-Allow-Origin": "*",
						"Access-Control-Allow-Methods": "GET, OPTIONS",
						"Access-Control-Allow-Headers": "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control, X-File-Name",
						"Access-Control-Allow-Credentials": "false",
						"Access-Control-Max-Age": "86400"
					}
				});
			}

			if (request.method !== "GET") {
				return new Response(JSON.stringify({
					error: "Method not allowed. Use GET."
				}), {
					status: 405,
					headers: {
						"Content-Type": "application/json",
						"Access-Control-Allow-Origin": "*"
					}
				});
			}

			// Get tool names by creating a temporary instance
			const toolNames = [
				"get_exceptions", "get_service_summary", "get_service_environments",
				"get_service_performance_details", "get_service_operations_summary",
				"get_service_dependency_graph", "prometheus_range_query", "prometheus_instant_query",
				"prometheus_label_values", "prometheus_labels", "get_logs", "get_service_logs",
				"get_service_traces", "get_drop_rules", "add_drop_rule", "get_alert_config", "get_alerts"
			];

			return new Response(JSON.stringify({
				server: "Last9 MCP Server",
				version: "0.1.13",
				protocol: "MCP",
				endpoints: {
					sse: "/sse",
					mcp: "/mcp",
					health: "/health",
					api: "/api",
					chat: "/chat"
				},
				tools: {
					count: toolNames.length,
					names: toolNames
				},
				description: "Last9 MCP Server - AI agent tool server for Last9 observability platform"
			}), {
				headers: {
					"Content-Type": "application/json",
					"Access-Control-Allow-Origin": "*"
				}
			});
		}

		// Chat interface endpoint
		if (url.pathname === "/chat") {
			if (request.method === "OPTIONS") {
				return new Response(null, {
					headers: {
						"Access-Control-Allow-Origin": "*",
						"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
						"Access-Control-Allow-Headers": "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control, X-File-Name",
						"Access-Control-Allow-Credentials": "false",
						"Access-Control-Max-Age": "86400"
					}
				});
			}

			if (request.method === "GET") {
				return new Response(JSON.stringify({
					endpoint: "/chat",
					description: "Chat interface for MCP interactions",
					methods: ["GET", "POST", "OPTIONS"],
					usage: {
						GET: "Returns this information",
						POST: "Send chat messages and receive MCP responses"
					},
					server: "Last9 MCP Server",
					version: "0.1.13"
				}), {
					headers: {
						"Content-Type": "application/json",
						"Access-Control-Allow-Origin": "*"
					}
				});
			}

			if (request.method === "POST") {
				try {
					const body = await request.json();
					const chatResponse = {
						response: "Chat functionality is available. Use MCP protocol via /mcp endpoint for tool interactions.",
						message_received: body.message || "",
						session_id: body.session_id || "",
						available_tools: 17,
						suggestion: "Use the /api endpoint to see available tools, then interact via /mcp endpoint"
					};

					return new Response(JSON.stringify(chatResponse), {
						headers: {
							"Content-Type": "application/json",
							"Access-Control-Allow-Origin": "*"
						}
					});
				} catch (error) {
					return new Response(JSON.stringify({
						error: "Invalid JSON request"
					}), {
						status: 400,
						headers: {
							"Content-Type": "application/json",
							"Access-Control-Allow-Origin": "*"
						}
					});
				}
			}

			return new Response(JSON.stringify({
				error: "Method not allowed. Use GET, POST, or OPTIONS."
			}), {
				status: 405,
				headers: {
					"Content-Type": "application/json",
					"Access-Control-Allow-Origin": "*"
				}
			});
		}

		return new Response(`
			<html>
				<head><title>Last9 MCP Server</title></head>
				<body>
					<h1>Last9 MCP Server</h1>
					<p>This is a remote MCP server for Last9 observability tools.</p>
					<p>Available endpoints:</p>
					<ul>
						<li><code>/sse</code> - Server-Sent Events endpoint for MCP connections</li>
						<li><code>/mcp</code> - HTTP MCP endpoint</li>
						<li><a href="/health"><code>/health</code></a> - Health check endpoint</li>
						<li><a href="/api"><code>/api</code></a> - API information and tool listing</li>
					</ul>
					<p>Connect from:</p>
					<ul>
						<li><a href="https://playground.ai.cloudflare.com/">Cloudflare AI Playground</a></li>
						<li>Claude Desktop (using mcp-remote proxy)</li>
						<li>Other MCP clients</li>
					</ul>
				</body>
			</html>
		`, {
			headers: { 'Content-Type': 'text/html' }
		});
	},
};