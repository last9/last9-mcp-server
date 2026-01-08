package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTracesDescription provides the description for the traces query tool
const GetTracesDescription = `Query distributed traces across all services using trace JSON pipeline queries.

This tool provides comprehensive access to trace data for debugging performance issues, understanding request flows,
and analyzing distributed system behavior. It accepts raw JSON pipeline queries for maximum flexibility.

The tool uses a pipeline-based query system similar to the logs API, allowing complex filtering and aggregation
operations on trace data.

Parameters:
- tracejson_query: (Required) JSON pipeline query for traces. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS)
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS)
- lookback_minutes: (Optional) Number of minutes to look back from current time (default: 5)
- limit: (Optional) Maximum number of traces to return (default: 20, range: 1-100)

Returns comprehensive trace data including trace IDs, spans, durations, timestamps, and metadata.

Example tracejson_query structures:
- Simple filter: [{"type": "filter", "query": {"$eq": ["ServiceName", "api"]}}]
- Multiple conditions: [{"type": "filter", "query": {"$and": [{"$eq": ["ServiceName", "api"]}, {"$eq": ["StatusCode", "STATUS_CODE_ERROR"]}]}}]
- Trace ID lookup: [{"type": "filter", "query": {"$eq": ["TraceId", "abc123"]}}]

Additional guidance:

# Trace Query Construction Prompt

## System Prompt

These are instructions for constructing natural language trace analytics queries into structured JSON trace pipeline queries that will be executed by the get_traces tool for trace analysis.

**Your Purpose:**
- You are a trace analytics assistant that can execute trace queries using the get_traces tool
- When users ask about traces, you should immediately use the get_traces tool with appropriate JSON query parameters
- Focus on accurate JSON structure and proper field references for trace data
- NEVER return raw JSON to users - always execute the query and analyze the results

**CRITICAL: DO NOT ADD AGGREGATION UNLESS EXPLICITLY REQUESTED**
- If the user asks to "show", "find", "get", "display" traces → Use ONLY filter operations
- If the user asks "how many", "count", "average", "sum" → Then add aggregation
- Most trace queries are simple filtering - do NOT assume aggregation is needed

**CRITICAL: AGGREGATION MUST ALWAYS BE PRECEDED BY FILTER**
- The first stage in any pipeline MUST be a filter operation
- If no specific filter is needed for aggregation, create a match-all filter using correct trace_id or span_id filters based on the server-fetched trace attributes list or standard fields
- Use filter to match all traces with non-empty trace_id or all spans before aggregating
- NEVER start a pipeline with aggregate or window_aggregate operations directly

**Process Flow:**
1. User provides natural language query about traces
2. You translate it to JSON pipeline format internally
3. You immediately call the get_traces tool with the JSON query and **ALWAYS USE lookback_minutes: 5 AS DEFAULT** unless the user specifies otherwise
4. You analyze the results and provide insights to the user

**CRITICAL DEFAULT TIME RULE:**
- **ALWAYS use lookback_minutes: 5 when no time range is specified**
- **NEVER use 60 minutes unless explicitly requested**
- **Default means 5 minutes, not 60 minutes**

The JSON pipeline format supports filtering, parsing, aggregation on trace data.

## JSON Query Format Specification

### Available Operations:

### Operation Selection Framework:

**When to use each operation type:**

- **filter**: When looking for specific traces, spans, or conditions
  - "Show me traces for service X"
  - "Find spans containing 'timeout'"
  - "Get error traces"

- **parse**: When trace content needs to be structured
  - "Parse JSON trace data and extract field Y"
  - "Extract duration from trace spans"

- **aggregate**: When you need counts, sums, averages, or grouping
  - "How many errors occurred?"
  - "Average response time by service"
  - "Count requests per endpoint"

- **window_aggregate**: When you need time-based metrics
  - "Error rate over 5-minute windows"
  - "Requests per minute"

**Default approach: Start with filtering, add other operations only when the query explicitly requests analysis, counting, or calculations.**

1. **filter** - Filter traces based on conditions (**USE THIS FOR MOST QUERIES**)
2. **parse** - Parse trace content (json, regexp, logfmt)
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

### Standard Trace Fields:

- **TraceId**: Trace identifier (primary filtering field, equivalent to Body in logs)
- **SpanId**: Span identifier (primary filtering field, equivalent to SeverityText in logs)
- **ServiceName**: Service name. Always prefer this over similar looking attributes in attributes or resource_attributes given below
- **SpanName**: Name of the span
- **SpanKind**: Span kind (CLIENT, SERVER, PRODUCER, CONSUMER, INTERNAL)
- **StatusCode**: Span status code (OK, ERROR, TIMEOUT)
- **StatusMessage**: Status message
- **Timestamp**: Trace timestamp
- **Duration**: Span duration
- **attributes['field_name']**: Span attributes (OpenTelemetry semantic conventions)
- **resource_attributes['field_name']**: Resource attributes (prefixed with resource_)

### Custom Fields for user's environment:
The server fetches available trace attribute labels internally and validates queries against them. If the fetched list is empty, validation falls back to standard fields only. In the query, apply this rule to get the attribute from the field name - if the field matches the pattern with resource_fieldname the attribute is resource_attributes['fieldname']. Otherwise it is attribute['fieldname'].
Any attribute used in the query should either be a standard attribute or returned by the server-fetched attributes list.

To find the appropriate field name, try partial matches or matching fields which have similar meaning from the above list.

**IMPORTANT**:  For filtering, if a field is not available in the list above, fall back to a regex based filter / parser instead of using conditions on attributes

## Query Analysis Patterns:

### Simple Retrieval (No Aggregation Needed):
- "Show me...", "Find...", "Get...", "Display..." → Use **filter** only
- "Recent traces", "Latest spans", "Failed requests" → Use **filter** only

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
1. Does the query ask for specific traces/spans? → **filter** ONLY (DO NOT ADD AGGREGATE)
2. Does it ask "how many", "count", "total"? → **filter** + **aggregate**
3. Does it ask for rates "per minute/hour"? → **window_aggregate**
4. Does it ask to "group by" something? → Add **groupby** to aggregate

### ❌ WRONG Examples (DO NOT DO THIS):
- "Show me error traces" → DON'T ADD: {"type": "aggregate"}
- "Find failed spans" → DON'T ADD: {"type": "aggregate"}
- "Get timeout traces" → DON'T ADD: {"type": "aggregate"}

### ✅ CORRECT Examples:
- "Show me error traces" → ONLY: [{"type": "filter", "query": {"$contains": ["StatusMessage", "error"]}}]
- "How many error traces?" → ADD: [{"type": "filter"}, {"type": "aggregate"}]

## Translation Examples (Ordered by Complexity):

### Example 1: Simple Trace Search (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Show me traces containing trace ID abc123"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$contains": ["TraceId", "abc123"]}
    ]
  }
}]
~~~

### Example 2: Service Error Traces (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Find error traces from auth service"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["ServiceName", "auth"]},
      {"$eq": ["StatusCode", "ERROR"]}
    ]
  }
}]
~~~

### Example 3: Span Duration Filter (FILTER ONLY - NO AGGREGATION)
**Natural Language:** "Get slow spans taking more than 1000ms"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$gt": ["Duration", "1000"]}
    ]
  }
}]
~~~

### Example 4: Aggregation - Average Duration
**Natural Language:** "What is the average span duration grouped by service?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$neq": ["Duration", ""]}
    ]
  }
}, {
  "type": "aggregate",
  "aggregates": [
    {
      "function": {"$avg": ["Duration"]},
      "as": "avg_duration"
    }
  ],
  "groupby": {"ServiceName": "service"}
}]
~~~

### Example 5: Count Error Traces
**Natural Language:** "How many error traces occurred by service?"
**JSON:**
~~~json
[{
  "type": "filter",
  "query": {
    "$and": [
      {"$eq": ["StatusCode", "ERROR"]}
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

## Translation Rules:

1. **Always return valid JSON array** containing operation objects
2. **Use proper field references**: TraceId, SpanId, ServiceName, attributes['field'], etc.
3. **Chain operations logically**: filter → parse → aggregate
4. **For time-based queries**, use window_aggregate with appropriate time units.
5. **For existence checks**, use $neq operator
6. **For text searches**, use $contains operator
7. **CRITICAL: When user query has no explicit logical operators (and/or), always wrap filter conditions in $and array, even for single conditions**
8. **Group multiple conditions** with $and or $or as appropriate when explicitly specified
9. **Use an attribute only if it exists in the standard or custom fields**. Otherwise fallback to a regex filter with field name and value eg, ".*fieldname.*[:=].*value.*"

## Default Parameters:

**CRITICAL TIME LOOKBACK RULES:**
- **DEFAULT IS ALWAYS 5 MINUTES when no time is specified**
- When the user says "recent" or doesn't specify a time range → **USE 5 MINUTES**
- For "last hour" or similar → use 60 minutes
- For specific timeframes → use the specified duration

**MANDATORY time window parsing:**
- NO TIME SPECIFIED → **5 minutes (NOT 60!)**
- "recent", "latest", "current" → **5 minutes**

## Execution Instructions:

When a user asks about traces:
1. **CRITICAL: When no time is specified, MUST use lookback_minutes: 5 (NOT 60!)**
2. **CRITICAL: When using window_aggregate without explicit time range, set lookback_minutes equal to window duration**
3. **Never return raw JSON** to the user
4. **Use type specified in the JSON query** (filter, parse, aggregate, window_aggregate), don't use anything else.
5. **If the user query is ambiguous**, ask for clarification instead of guessing
6. **Use filter or aggregation** only on labels returned by the server-fetched trace attributes list (or standard fields)
7. **Always analyze the results** and provide insights

**CRITICAL: Always execute queries with tools - never show raw JSON to users**

`

// GetTracesArgs represents the input arguments for the traces query tool
type GetTracesArgs struct {
	TracejsonQuery  []interface{} `json:"tracejson_query,omitempty"`
	StartTimeISO    string        `json:"start_time_iso,omitempty"`
	EndTimeISO      string        `json:"end_time_iso,omitempty"`
	LookbackMinutes int           `json:"lookback_minutes,omitempty"`
	Limit           int           `json:"limit,omitempty"`
}

// NewGetTracesHandler creates a handler for getting traces using tracejson_query parameter
func NewGetTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTracesArgs) (*mcp.CallToolResult, any, error) {
		// Check if tracejson_query is provided
		if len(args.TracejsonQuery) == 0 {
			return nil, nil, fmt.Errorf("tracejson_query parameter is required. Use the tracejson_query_builder prompt to generate JSON pipeline queries from natural language")
		}

		// Handle tracejson_query directly
		result, err := handleTraceJSONQuery(ctx, client, cfg, args.TracejsonQuery, args)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}

func handleTraceJSONQuery(ctx context.Context, client *http.Client, cfg models.Config, tracejsonQuery []interface{}, args GetTracesArgs) (*mcp.CallToolResult, error) {
	// Determine time range from parameters
	startTime, endTime, err := parseTimeRangeFromArgs(args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time range: %v", err)
	}

	attributes, err := fetchTraceAttributes(ctx, client, cfg, startTime/1000, endTime/1000, utils.GetDefaultRegion(cfg.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trace attributes for validation: %v", err)
	}
	if err := validateTraceJSONQuery(tracejsonQuery, attributes); err != nil {
		return nil, err
	}

	// Use util to execute the query
	resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, tracejsonQuery, startTime, endTime, args.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to call trace JSON query API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("traces API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Return the result in MCP format
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: formatJSON(result),
			},
		},
	}, nil
}

// parseTimeRangeFromArgs extracts start and end times from GetTracesArgs
func parseTimeRangeFromArgs(args GetTracesArgs) (int64, int64, error) {
	now := time.Now()

	// Default to last hour if no time parameters provided
	startTime := now.Add(-time.Hour).UnixMilli()
	endTime := now.UnixMilli()

	// Check for lookback_minutes
	if args.LookbackMinutes > 0 {
		startTime = now.Add(-time.Duration(args.LookbackMinutes) * time.Minute).UnixMilli()
	}

	// Check for explicit start_time_iso
	if args.StartTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", args.StartTimeISO); err == nil {
			startTime = parsed.UnixMilli()
		}
	}

	// Check for explicit end_time_iso
	if args.EndTimeISO != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", args.EndTimeISO); err == nil {
			endTime = parsed.UnixMilli()
		}
	}

	return startTime, endTime, nil
}

// formatJSON formats JSON for display
func formatJSON(data interface{}) string {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(bytes)
}
