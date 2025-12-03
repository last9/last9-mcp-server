package traces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetServiceGraphArgs defines the input structure for getting service graph
type GetServiceGraphArgs struct {
	ServiceName     string  `json:"service_name" jsonschema:"Name of the service to get dependencies for (required)"`
	Env             string  `json:"env" jsonschema:"Environment to filter by (e.g., production, staging, gamma)"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 30, range: 1-1440)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
}

// ServiceGraphResponse contains the service graph data
type ServiceGraphResponse struct {
	ServiceName string                    `json:"service_name"`
	Environment string                    `json:"environment"`
	Incoming    ServiceGraphTraffic       `json:"incoming"`
	Outgoing    ServiceGraphTraffic       `json:"outgoing"`
	Internal    ServiceGraphInternalCalls `json:"internal"`
}

type ServiceGraphTraffic struct {
	Throughput   []PromQLResult `json:"throughput"`    // Calls per minute by client/server
	ResponseTime []PromQLResult `json:"response_time"` // P95 response time in ms
	Errors       []PromQLResult `json:"errors"`        // Error count (4xx/5xx)
}

type ServiceGraphInternalCalls struct {
	Throughput   []PromQLResult `json:"throughput"`    // Calls to databases, RPC, messaging
	ResponseTime []PromQLResult `json:"response_time"` // P95 response time for internal calls
	Errors       []PromQLResult `json:"errors"`        // Internal call errors
}

type PromQLResult struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
}

// NewGetServiceGraphHandler creates a handler for getting service dependencies using Prometheus queries
func NewGetServiceGraphHandler(cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceGraphArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetServiceGraphArgs) (*mcp.CallToolResult, any, error) {
		if args.ServiceName == "" {
			return nil, nil, errors.New("service_name is required and cannot be empty")
		}

		// Default lookback to 30 minutes (1800 seconds)
		lookbackMinutes := 30
		if args.LookbackMinutes != 0 {
			lookbackMinutes = int(args.LookbackMinutes)
		}

		// Prepare arguments map for GetTimeRange function
		arguments := make(map[string]interface{})
		if args.StartTimeISO != "" {
			arguments["start_time_iso"] = args.StartTimeISO
		}

		// Get time for instant query (end time)
		_, endTime, err := utils.GetTimeRange(arguments, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build environment filter
		envFilter := ""
		if args.Env != "" {
			envFilter = fmt.Sprintf(", env=~'%s'", args.Env)
		}

		// Time window in seconds (convert lookback minutes to seconds)
		timeWindow := lookbackMinutes * 60

		// Execute all queries
		response := ServiceGraphResponse{
			ServiceName: args.ServiceName,
			Environment: args.Env,
		}

		// 1. Incoming throughput: quantile_over_time(0.95, (sum by (client) (trace_call_graph_count{server="X", env=~'Y'}))[1800s])
		incomingThroughputQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (client) (trace_call_graph_count{server="%s"%s}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, incomingThroughputQuery, endTime); err == nil {
			response.Incoming.Throughput = parsePromQLResponse(result)
		}

		// 2. Outgoing throughput: quantile_over_time(0.95, (sum by (server) (trace_call_graph_count{client="X", env=~'Y'}))[1800s])
		outgoingThroughputQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (server) (trace_call_graph_count{client="%s"%s}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, outgoingThroughputQuery, endTime); err == nil {
			response.Outgoing.Throughput = parsePromQLResponse(result)
		}

		// 3. Incoming response time: quantile_over_time(0.95, sum by (client) (trace_call_graph_duration{server="X", env=~'Y', quantile="p95"})[1800s])
		incomingResponseTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (client) (trace_call_graph_duration{server="%s"%s, quantile="p95"})[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, incomingResponseTimeQuery, endTime); err == nil {
			response.Incoming.ResponseTime = parsePromQLResponse(result)
		}

		// 4. Outgoing response time: quantile_over_time(0.95, sum by (server) (trace_call_graph_duration{client="X", env=~'Y', quantile="p95"})[1800s])
		outgoingResponseTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (server) (trace_call_graph_duration{client="%s"%s, quantile="p95"})[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, outgoingResponseTimeQuery, endTime); err == nil {
			response.Outgoing.ResponseTime = parsePromQLResponse(result)
		}

		// 5. Incoming errors: quantile_over_time(0.95, (sum by (client) (trace_call_graph_count{server="X", env=~'Y', client_status=~"4.*|5.*"}))[1800s])
		incomingErrorsQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (client) (trace_call_graph_count{server="%s"%s, client_status=~"4.*|5.*"}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, incomingErrorsQuery, endTime); err == nil {
			response.Incoming.Errors = parsePromQLResponse(result)
		}

		// 6. Outgoing errors: quantile_over_time(0.95, (sum by (server) (trace_call_graph_count{client="X", env=~'Y', server_status=~"4.*|5.*"}))[1800s])
		outgoingErrorsQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (server) (trace_call_graph_count{client="%s"%s, server_status=~"4.*|5.*"}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, outgoingErrorsQuery, endTime); err == nil {
			response.Outgoing.Errors = parsePromQLResponse(result)
		}

		// 7. Internal response time: quantile_over_time(0.95, sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (trace_internal_call_graph_duration{client="X", env=~'Y', quantile="p95"})[1800s])
		internalResponseTimeQuery := fmt.Sprintf(
			`quantile_over_time(0.95, sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (trace_internal_call_graph_duration{client="%s"%s, quantile="p95"})[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, internalResponseTimeQuery, endTime); err == nil {
			response.Internal.ResponseTime = parsePromQLResponse(result)
		}

		// 8. Internal throughput: quantile_over_time(0.95, (sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (trace_internal_call_graph_count{client="X", env=~'Y'}))[1800s])
		internalThroughputQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (server_host, server_db_system, server_rpc_system, server_messaging_system, server_rpc_service) (trace_internal_call_graph_count{client="%s"%s}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, internalThroughputQuery, endTime); err == nil {
			response.Internal.Throughput = parsePromQLResponse(result)
		}

		// 9. Internal errors: quantile_over_time(0.95, (sum by (server) (trace_internal_call_graph_count{client="X", env=~'Y', client_status="STATUS_CODE_ERROR"}))[1800s])
		internalErrorsQuery := fmt.Sprintf(
			`quantile_over_time(0.95, (sum by (server) (trace_internal_call_graph_count{client="%s"%s, client_status="STATUS_CODE_ERROR"}))[%ds])`,
			args.ServiceName, envFilter, timeWindow,
		)
		if result, err := executePromInstantQuery(ctx, cfg, internalErrorsQuery, endTime); err == nil {
			response.Internal.Errors = parsePromQLResponse(result)
		}

		jsonData, err := json.Marshal(response)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(jsonData),
				},
			},
		}, nil, nil
	}
}

// executePromInstantQuery executes a Prometheus instant query and returns the result
func executePromInstantQuery(ctx context.Context, cfg models.Config, query string, endTime time.Time) (map[string]interface{}, error) {
	client := last9mcp.WithHTTPTracing(&http.Client{Timeout: 30 * time.Second})

	resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, endTime.Unix(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the data field from the response
	if data, ok := result["data"].(map[string]interface{}); ok {
		return data, nil
	}

	return result, nil
}

// parsePromQLResponse converts the Prometheus response to our PromQLResult format
func parsePromQLResponse(data interface{}) []PromQLResult {
	results := []PromQLResult{}

	// Handle the Prometheus response format: {"data": {"result": [...]}} or just {"result": [...]}
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return results
	}

	// Check if there's a "result" field directly
	var resultArray []interface{}
	if res, ok := dataMap["result"].([]interface{}); ok {
		resultArray = res
	}

	for _, item := range resultArray {
		if itemMap, ok := item.(map[string]interface{}); ok {
			result := PromQLResult{}

			// Extract metric labels
			if metricMap, ok := itemMap["metric"].(map[string]interface{}); ok {
				result.Metric = make(map[string]string)
				for k, v := range metricMap {
					if str, ok := v.(string); ok {
						result.Metric[k] = str
					}
				}
			}

			// Extract value [timestamp, value]
			if valueArray, ok := itemMap["value"].([]interface{}); ok {
				result.Value = valueArray
			}

			results = append(results, result)
		}
	}

	return results
}
