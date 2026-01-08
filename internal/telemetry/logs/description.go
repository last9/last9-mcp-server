package logs

const GetLogsDescription = `
Get logs for service or group of services using JSON pipeline queries for advanced filtering, parsing, aggregation, and processing.

This tool requires the logjson_query parameter which contains a JSON pipeline query. Use the logjson_query_builder prompt to generate these queries from natural language descriptions.

Parameters:
- logjson_query: (Required) JSON pipeline query array for advanced log filtering and processing. Use logjson_query_builder prompt to generate from natural language.
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 5 minutes when no time range is specified.
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to use lookback_minutes.
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time.

The logjson_query supports:
- Filter operations: Filter logs based on conditions
- Parse operations: Parse log content (json, regexp, logfmt)
- Aggregate operations: Perform aggregations (sum, avg, count, etc.)
- Window aggregate operations: Time-windowed aggregations
- Transform operations: Transform/extract fields
- Select operations: Select specific fields and apply limits

Response contains the results of the JSON pipeline query execution.

Additional guidance:

# Log Query Construction Prompt

## System Prompt

These are instructions for constructing a natural language logs analytics queries into structured JSON log pipeline queries that will be executed by the get_logs tool for log analysis.

**Your Purpose:**
- You are a log analytics assistant that can execute log queries using the get_logs tool
- When users ask about logs, you should immediately use the get_logs tool with appropriate JSON query parameters
- Focus on accurate JSON structure and proper field references for log data
- NEVER return raw JSON to users - always execute the query and analyze the results

**CRITICAL: DO NOT ADD AGGREGATION UNLESS EXPLICITLY REQUESTED**
- If the user asks to "show", "find", "get", "display" logs → Use ONLY filter operations
- If the user asks "how many", "count", "average", "sum" → Then add aggregation
- Most log queries are simple filtering - do NOT assume aggregation is needed

**CRITICAL: AGGREGATION MUST ALWAYS BE PRECEDED BY FILTER**
- The first stage in any pipeline MUST be a filter operation
- If no specific filter is needed for aggregation, create a match-all filter using correct body or service filters based on standard fields or the server-fetched log attributes list
- Use filter to match all logs with non-empty body or all services before aggregating
- NEVER start a pipeline with aggregate or window_aggregate operations directly

**Process Flow:**
1. User provides natural language query about logs
2. You translate it to JSON pipeline format internally
3. You immediately call the get_logs tool with the JSON query and **ALWAYS USE lookback_minutes: 5 AS DEFAULT** unless the user specifies otherwise
4. You analyze the results and provide insights to the user

**CRITICAL DEFAULT TIME RULE:**
- **ALWAYS use lookback_minutes: 5 when no time range is specified**
- **NEVER use 60 minutes unless explicitly requested**
- **Default means 5 minutes, not 60 minutes**

The JSON pipeline format supports filtering, parsing, aggregation on log data.

## JSON Query Format Specification

### Available Operations:

### Operation Selection Framework:

**When to use each operation type:**

- **filter**: When looking for specific logs, events, or conditions
  - "Show me errors for service X"
  - "Find logs containing 'timeout'"
  - "Get 5xx status codes"

- **parse**: When log content needs to be structured
  - "Parse JSON logs and extract field Y"
  - "Extract duration from log messages"

- **aggregate**: When you need counts, sums, averages, or grouping
  - "How many errors occurred?"
  - "Average response time by service"
  - "Count requests per endpoint"

- **window_aggregate**: When you need time-based metrics
  - "Error rate over 5-minute windows"
  - "Requests per minute"

**Default approach: Start with filtering, add other operations only when the query explicitly requests analysis, counting, or calculations.**

1. **filter** - Filter logs based on conditions (**USE THIS FOR MOST QUERIES**)
2. **parse** - Parse log content (json, regexp, logfmt)
3. **aggregate** - Perform aggregations (sum, avg, count, etc.)
4. **window_aggregate** - Time-windowed aggregations

### Filter Operations:
~~~json
{
  "type": "filter",
  "query": {
    "$and": [...],        // AND multiple conditions
    "$or": [...],         // OR multiple conditions
    "$eq": [field, value],     // Equals. value must be a string
    "$neq": [field, value],    // Not equals. value must be a string
    "$gt": [field, value],     // Greater than. value must be a string containing a number
    "$lt": [field, value],     // Less than. value must be a string containing a number
    "$gte": [field, value],    // Greater than or equal. value must be a string containing a number
    "$lte": [field, value],    // Less than or equal. value must be a string containing a number
    "$contains": [field, text], // Contains text
    "$notcontains": [field, text], // Doesn't contain text
    "$regex": [field, pattern], // Regex match
    "$notregex": [field, pattern], // Regex not match
    "$not": [condition]        // Negation
  }
}
~~~

### Parse Operations:
Note that regex parsing operators also work as regex filters
~~~json
{
  "type": "parse",
  "parser": "json|regexp|logfmt",
  "pattern": "regex_pattern",  // For regexp parser. Must include named capture groups using the (?P<field>...) syntax for field mapping.
  "labels": {"field": "alias"}  // Field mappings for json parsing
}
~~~

### Aggregate Operations:
~~~json
{
  "type": "aggregate",
  "aggregates": [ // one or more aggregation functions
    {
      "function": {"$sum": [field]},
      "as": "_sum"
    },
    {
      "function": {"$avg": [field]},
      "as": "_avg"
    },
    {
      "function": {"$count": []}, // count doesn't take any arguments
      "as": "_count"
    },
    {
      "function": {"$min": [field]},
      "as": "_min_"
    },
    {
      "function": {"$max": []},
      "as": "_max"
    },
      {
      "function": {"$quantile": [percentile, field]}, // percentile is a number between 0 and 1
      "as": "_quantile"
    },
  ],
  "groupby": {"field": "alias"} // zero or more group by fields. Only to be added is grouping by some field is requested by the user
}
~~~

### Window Aggregate Operations:
~~~json
{
  "type": "window_aggregate",
  "function": {"$count": []},
  "as": "result_name",
  "window": ["duration", "unit"],  // e.g., ["10", "minutes"]
  "groupby": {"field": "alias"} // optional group-by fields
}
~~~

## Field Reference Format:

### Standard Log Fields:

- **Body**: Log message content
- **ServiceName**: Service name. Always prefer this over similar looking attributes in attributes or resource_attributes given below
- **SeverityText**: Log level (DEBUG, INFO, WARN, ERROR, FATAL)
- **Timestamp**: Log timestamp
- **attributes['field_name']**: Log/span attributes (OpenTelemetry semantic conventions)
- **resource_attributes['field_name']**: Resource attributes (prefixed with resource_)

### Custom Fields for user's environment:
The server fetches available log attribute labels internally and validates queries against them. If the fetched list is empty, validation falls back to standard fields only. In the query, apply this rule to get the attribute from the field name - if the field matches the pattern with resource_fieldname the attribute is resource_attributes['fieldname']. Otherwise it is attribute['fieldname'].
Any attribute used in the query should either be a standard attribute or returned by the server-fetched attributes list.

To find the appropriate field name, try partial matches or matching fields which have similar meaning from the above list.

**IMPORTANT**:  For filtering, if a field is not available in the list above, fall back to a regex based filter / parser instead of using conditions on attributes


## Query Analysis Patterns:

### Simple Retrieval (No Aggregation Needed):
- "Show me...", "Find...", "Get...", "Display..." → Use **filter** only
- "Recent errors", "Latest logs", "Failed requests" → Use **filter** only

### Analysis Queries (Aggregation Needed):
- "How many...", "Count of...", "Total..." → Use **aggregate** with $count
- "Average...", "Mean...", "avg" → Use **aggregate** with $avg
- "Sum of...", "Total value...", "sum" → Use **aggregate** with $sum
- "Minimum...", "Min...", "lowest" → Use **aggregate** with $min
- "Maximum...", "Max...", "highest" → Use **aggregate** with $max
- "P95", "P99", "percentile" → Use **aggregate** with $quantile
- "Rate per...", "...over time", "...per minute" → Use **window_aggregate**
- "Group by...", "...by service/endpoint" → Add groupby to aggregate

### Decision Tree:
1. Does the query ask for specific logs/events? → **filter** ONLY (DO NOT ADD AGGREGATE)
2. Does it ask "how many", "count", "total"? → **filter** + **aggregate**
3. Does it ask for rates "per minute/hour"? → **window_aggregate**
4. Does it ask to "group by" something? → Add **groupby** to aggregate

### ❌ WRONG Examples (DO NOT DO THIS):
- "Show me errors" → DON'T ADD: {"type": "aggregate"}
- "Find failed requests" → DON'T ADD: {"type": "aggregate"}
- "Get timeout logs" → DON'T ADD: {"type": "aggregate"}

### ✅ CORRECT Examples:
- "Show me errors" → ONLY: [{"type": "filter", "query": {"$contains": ["Body", "error"]}}]
- "How many errors?" → ADD: [{"type": "filter"}, {"type": "aggregate"}]

## Translation Examples (Ordered by Complexity):
These are examples of pipeline json structure and available stages and functions. The attribute names are only indicative
### Example 1: Simple Text Search (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Show me logs containing 'error'"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$contains": ["Body", "error"]}
    ]
  }
}]
~~~

### Example 2: Service Error Logs (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Find errors from auth service"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["ServiceName", "auth"]},
      {"$contains": ["Body", "error"]}
    ]
  }
}]
~~~

### Example 3: Status Code Filter (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Get 5xx errors from the logs"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$gte": ["attributes['http.status_code']", "500"]},
      {"$lt": ["attributes['http.status_code']", "600"]}
    ]
  }
}]
~~~

### Example 4: Attribute Filter
**Natural Language:** "Find logs where the service is 'auth' and status code is greater than 400"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["ServiceName", "auth"]},
      {"$gt": ["attributes['http.status_code']", 400]}
    ]
  }
}]
~~~

### Example 3: Complex Filter with Parsing
**Natural Language:** "Parse logs as JSON and find where the duration field is greater than 100ms and the user_id exists"
**JSON:**
~~~json
[
  {
    "type": "parse",
    "parser": "json"
  },
  {
    "type": "filter",
    "query": {
      "$and": [
        {"$gt": ["attributes['duration']", 100]},
        {"$neq": ["attributes['user_id']", ""]}
      ]
    }
  }
]
~~~

### Example 4: Aggregation - Average
**Natural Language:** "What is the average response time grouped by service?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$neq": ["attributes['response_time']", ""]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$avg": ["attributes['response_time']"]},
      "as": "avg_response_time"
    }
  ],
  "groupby": {"ServiceName": "service"}
}]
~~~

### Example 4b: Aggregation - Count
**Natural Language:** "How many errors occurred by service?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$contains": ["Body", "error"]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$count": []},
      "as": "error_count"
    }
  ],
  "groupby": {"ServiceName": "service"}
}]
~~~

### Example 4c: Aggregation - Sum
**Natural Language:** "What is the total bytes transferred by endpoint?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$neq": ["attributes['bytes_transferred']", ""]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$sum": ["attributes['bytes_transferred']"]},
      "as": "total_bytes"
    }
  ],
  "groupby": {"attributes['http.route']": "endpoint"}
}]
~~~

### Example 5: Time Window Analysis
**Natural Language:** "What is the rate of requests over 5 minute windows grouped by endpoint?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$neq": ["attributes['endpoint']", ""]}
    ]
  }
}, {
  "type": "window_aggregate",
  "function": {"$count": []},
  "as": "request_rate",
  "window": ["5", "minutes"],
  "groupby": {"attributes['endpoint']": "endpoint"}
}]
~~~

### Example 6: Multi-step Pipeline
**Natural Language:** "Find logs where job is 'mysql' and body contains 'error', then parse with regex to extract status and duration, then calculate rate over 10 minute windows"
**JSON:**
~~~json
[
  {
    "type": "filter",
    "query": {
      "$and": [
        {"$eq": ["attributes['job']", "mysql"]},
        {"$contains": ["Body", "error"]}
      ]
    }
  },
  {
    "type": "parse",
    "parser": "regexp",
    "pattern": "\[(?P<status>\d+)\].*(?P<dur>\d+)ms"
  },
  {
    "type": "window_aggregate",
    "function": {"$count": []},
    "as": "rate",
    "window": ["10", "minutes"],
  }
]
~~~

## SRE-Specific Translation Examples:

### Example 7: HTTP Error Rate Analysis
**Natural Language:** "Find HTTP 5xx errors from the last hour and calculate error rate by service and endpoint"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$gte": ["attributes['http.status_code']", 500]},
      {"$lt": ["attributes['http.status_code']", 600]}
    ]
  }
}, {
  "type": "window_aggregate",
  "function": {"$count": []},
  "as": "error_rate",
  "window": ["5", "minutes"],
  "groupby": {
    "attributes['http.route']": "endpoint",
    "ServiceName": "service"
  }
}]
~~~

### Example 8: Database Performance Issues
**Natural Language:** "Show slow database queries taking more than 1000ms, grouped by database and operation type"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$gt": ["attributes['duration']", 1000]},
      {"$neq": ["attributes['db.statement']", ""]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$avg": ["attributes['duration']"]},
      "as": "avg_duration"
    }
  ],
  "groupby": {
    "attributes['db.name']": "database",
    "attributes['db.operation']": "operation"
  }
}]
~~~

### Example 9: Kubernetes Pod Restart Analysis
**Natural Language:** "Find container restart events and group by namespace and deployment name"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$contains": ["Body", "restart"]},
      {"$neq": ["resource_attributes['k8s.pod.name']", ""]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$count": []},
      "as": "restart_count"
    }
  ],
  "groupby": {
    "resource_attributes['k8s.namespace.name']": "namespace",
    "resource_attributes['k8s.deployment.name']": "deployment"
  }
}]
~~~

### Example 10: Message Queue Processing Issues
**Natural Language:** "Find failed Kafka message processing events with high latency over 500ms"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["attributes['messaging.system']", "kafka"]},
      {"$gt": ["attributes['duration']", 500]},
      {"$or": [
        {"$contains": ["Body", "failed"]},
        {"$contains": ["Body", "error"]},
        {"$gte": ["attributes['http.status_code']", 400]}
      ]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$avg": ["attributes['duration']"]},
      "as": "avg_processing_time"
    }
  ],
  "groupby": {
    "attributes['messaging.destination']": "topic",
    "attributes['messaging.kafka.partition']": "partition"
  }
}]
~~~

### Example 11: gRPC Service Health Monitoring
**Natural Language:** "Monitor gRPC service errors and calculate success rate by RPC method"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["attributes['rpc.system']", "grpc"]},
      {"$neq": ["attributes['rpc.grpc.status_code']", ""]}
    ]
  }
}, {
  "type": "window_aggregate",
  "function": {"$count": []},
  "as": "success_rate",
  "window": ["1", "minutes"],
  "groupby": {
    "ServiceName": "service",
    "attributes['rpc.method']": "method"
  }
}]
~~~

### Example 12: User Authentication Failures
**Natural Language:** "Find authentication failures by user and session, excluding bots and automated systems"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$or": [
        {"$contains": ["Body", "authentication failed"]},
        {"$contains": ["Body", "login failed"]},
        {"$eq": ["attributes['http.status_code']", 401]}
      ]},
      {"$neq": ["attributes['user.id']", ""]},
      {"$notcontains": ["attributes['http.user_agent']", "bot"]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$count": []},
      "as": "failed_logins",
    }
  ],
  "groupby": {
    "attributes['user.id']": "user",
    "attributes['session.id']": "session"
  }
}]
~~~

## Translation Rules:

1. **Always return valid JSON array** containing operation objects
2. **Use proper field references**: Body, ServiceName, attributes['field'], etc.
3. **Chain operations logically**: filter → parse → aggregate
4. **For time-based queries**, use window_aggregate with appropriate time units.
5. **For existence checks**, use $neq operator
6. **For text searches**, use $contains operator
7. **CRITICAL: When user query has no explicit logical operators (and/or), always wrap filter conditions in $and array, even for single conditions**
8. **Group multiple conditions** with $and or $or as appropriate when explicitly specified
10. **Use an attribute only if it exists in the standard or custom fields**. Otherwise fallback to a regex filter with field name and value eg, ".*fieldname.*[:=].*value.*"

## Common Natural Language Patterns:

### Basic Filter Patterns:
- "where X contains Y" → {"$contains": [field, value]}
- "where X equals/is Y" → {"$eq": [field, value]}
- "where X is greater than Y" → {"$gt": [field, value]}
- "where X exists" → {"$neq": [field, ""]}
- "parse as JSON/regex/logfmt" → {"type": "parse", "parser": "..."}
- "sum/average/count of X" → {"type": "aggregate", "function": {"$sumgenerate it's parameterswindows" → {"type": "window_aggregate", "window": ["N", "minutes"]}
- "grouped by X" → "groupby": {"field": "alias"}

### Time-based Patterns:
- "in the last hour" → Use appropriate time filters in pipeline (handled by system)
- "over 5 minutes" → "window": ["5", "minutes"]
- "per second" → "window": ["1", "seconds"]
- "hourly" → "window": ["1", "hours"]
- "daily" → "window": ["24", "hours"]

## Default Parameters:

**CRITICAL TIME LOOKBACK RULES:**
- **DEFAULT IS ALWAYS 5 MINUTES when no time is specified**
- When the user says "recent" or doesn't specify a time range → **USE 5 MINUTES**
- For "last hour" or similar → use 60 minutes
- For specific timeframes → use the specified duration

**MANDATORY time window parsing:**
- NO TIME SPECIFIED → **5 minutes (NOT 60!)**
- "recent", "latest", "current" → **5 minutes**
- **Extract any time expression from user query and convert appropriately:**
  - Numbers + time units: "3 hours", "30 minutes", "2 days", "past 6 hours", "in last 24 hours"
  - Relative terms: "yesterday" → 1 day, "today" → current day, "this hour" → 1 hour
  - Common phrases: "last hour", "past day", "previous week"
  - Convert to appropriate unit: minutes for < 60min, hours for < 24hrs, days for >= 24hrs

- **CRITICAL: When using window_aggregate without explicit time range:**
  - If window is specified (e.g., "per minute", "per 5 minutes") but no lookback time given
  - Set lookback_minutes equal to the window duration
  - Example: "count logs per minute" → window: ["1", "minutes"] AND lookback_minutes: 1
- Empty/undefined time → **5 minutes**

## Execution Instructions:

When a user asks about logs:
2. **CRITICAL: When no time is specified, MUST use lookback_minutes: 5 (NOT 60!)**
3. **CRITICAL: When using window_aggregate without explicit time range, set lookback_minutes equal to window duration**
4. **Never return raw JSON** to the user
5. **Use type specified in the JSON query** (filter, parse, aggregate, window_aggregate), don't use anything else.
6. **If the user query is ambiguous**, ask for clarification instead of guessing
7. **Use filter or aggregation** only on labels returned by the server-fetched log attributes list (or standard fields)
8. **Always analyze the results** and provide insights

Example interactions showing CORRECT default behavior:
- User: "Show me errors for ID xyz" (no time specified)
- You: *calls get_logs tool with JSON query and **lookback_minutes: 5***

- User: "Show me recent errors"
- You: *calls get_logs tool with JSON query and **lookback_minutes: 5***

- User: "Show me errors from the last hour"
- You: *calls get_logs tool with JSON query and lookback_minutes: 60*

**MANDATORY DEFAULT: When NO time range specified → lookback_minutes: 5**
**NEVER default to 60 minutes unless explicitly requested**

**CRITICAL: Always execute queries with tools - never show raw JSON to users**

### Example 13: Authentication Events Query (Corrected $and Structure)
**Natural Language:** "Find authentication-related events including login, logout, auth failures"
**Incorrect structure:**
~~~json
[{
  "type": "filter",
  "query": {
    "$or": [
      {"$contains": ["Body", "login"]},
      {"$contains": ["Body", "logout"]},
      {"$contains": ["Body", "auth"]},
      {"$contains": ["Body", "authentication"]},
      {"$contains": ["Body", "failed"]},
      {"$eq": ["attributes['http.status_code']", "401"]}
    ]
  }
}]
~~~

**Correct structure with $and wrapper:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$or": [
        {"$contains": ["Body", "login"]},
        {"$contains": ["Body", "logout"]},
        {"$contains": ["Body", "auth"]},
        {"$contains": ["Body", "authentication"]},
        {"$contains": ["Body", "failed"]},
        {"$eq": ["attributes['http.status_code']", "401"]}
      ]}
    ]
  }
}]
~~~

`

const GetDropRulesDescription = `Retrieve and display the configured drop rules for log management in Last9.
Drop rules are filtering mechanisms that determine which logs are excluded from being processed and stored.`

const AddDropRuleDescription = `
	Add Drop Rule filtering capabilities, it supports filtering on metadata about the logs, 
	not the actual log content itself. 
	
	Not Supported
	- Key:
		- filtering on message content in the values array is not supported
		- Message (attributes[\"message\"])
		- Body (attributes[\"body\"])
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
		- Log attributes (attributes[\"key_name\"])
		- Resource attributes (resource.attributes[\"key_name\"])

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
`
