package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetExceptionsArgs defines the input structure for getting exceptions
type GetExceptionsArgs struct {
	Limit                 float64 `json:"limit,omitempty" jsonschema:"Maximum number of exceptions to return (default: 20, range: 1-1000)"`
	LookbackMinutes       float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-10080)"`
	StartTimeISO          string  `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO            string  `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	ServiceName           string  `json:"service_name,omitempty" jsonschema:"Filter exceptions by service name (e.g. api-service)"`
	SpanName              string  `json:"span_name,omitempty" jsonschema:"Filter exceptions by span name (e.g. user_service)"`
	DeploymentEnvironment string  `json:"deployment_environment,omitempty" jsonschema:"Filter exceptions by deployment environment from resource attributes (e.g. production, staging)"`
}

// NewGetExceptionsHandler creates a handler for getting exceptions
func NewGetExceptionsHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetExceptionsArgs) (*mcp.CallToolResult, any, error) {
		limit := 20
		if args.Limit != 0 {
			limit = int(args.Limit)
		}
		if limit > 100 {
			limit = 100 // Maximum limit for trace queries
		}

		lookbackMinutes := 60
		if args.LookbackMinutes != 0 {
			lookbackMinutes = int(args.LookbackMinutes)
		}

		// Prepare arguments map for GetTimeRange function
		arguments := make(map[string]interface{})
		if args.StartTimeISO != "" {
			arguments["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			arguments["end_time_iso"] = args.EndTimeISO
		}

		// Get time range using the common utility
		startTime, endTime, err := utils.GetTimeRange(arguments, lookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build trace JSON query pipeline with filters
		filters := make([]map[string]interface{}, 0)

		// Filter for traces with exceptions (exception.type exists and is not empty)
		filters = append(filters, map[string]interface{}{
			"$exists": []interface{}{"attributes['exception.type']"},
		})
		filters = append(filters, map[string]interface{}{
			"$ne": []interface{}{"attributes['exception.type']", ""},
		})

		// Filter by service name if provided
		if args.ServiceName != "" {
			filters = append(filters, map[string]interface{}{
				"$eq": []interface{}{"ServiceName", args.ServiceName},
			})
		}

		// Filter by span name if provided
		if args.SpanName != "" {
			filters = append(filters, map[string]interface{}{
				"$eq": []interface{}{"SpanName", args.SpanName},
			})
		}

		// Filter by deployment environment if provided
		if args.DeploymentEnvironment != "" {
			filters = append(filters, map[string]interface{}{
				"$eq": []interface{}{"resources['deployment.environment']", args.DeploymentEnvironment},
			})
		}

		// Build the pipeline query
		pipeline := []map[string]interface{}{
			{
				"type": "filter",
				"query": map[string]interface{}{
					"$and": filters,
				},
			},
		}

		// Convert start/end times to milliseconds
		startMs := startTime.UnixMilli()
		endMs := endTime.UnixMilli()

		// Use the MakeTracesJSONQueryAPI utility function
		resp, err := utils.MakeTracesJSONQueryAPI(ctx, client, cfg, pipeline, startMs, endMs, limit)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute trace query: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("exceptions API request failed with status %d: %s", resp.StatusCode, string(body))
		}

		// Parse the response
		var traceResponse struct {
			Result []map[string]interface{} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&traceResponse); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// Extract exception details from traces
		exceptions := make([]map[string]interface{}, 0, len(traceResponse.Result))
		for _, trace := range traceResponse.Result {
			// Extract relevant exception information
			exception := map[string]interface{}{
				"trace_id":     trace["TraceId"],
				"span_id":      trace["SpanId"],
				"service_name": trace["ServiceName"],
				"span_name":    trace["SpanName"],
				"timestamp":    trace["Timestamp"],
			}

			// Extract attributes if they exist
			if attrs, ok := trace["attributes"].(map[string]interface{}); ok {
				exception["exception_type"] = attrs["exception.type"]
				exception["exception_message"] = attrs["exception.message"]
				exception["exception_stacktrace"] = attrs["exception.stacktrace"]
				exception["exception_escaped"] = attrs["exception.escaped"]
			}

			// Extract resource attributes if they exist
			if resources, ok := trace["resources"].(map[string]interface{}); ok {
				exception["deployment_environment"] = resources["deployment.environment"]
				exception["service_namespace"] = resources["service.namespace"]
				exception["service_instance_id"] = resources["service.instance.id"]
			}

			// Extract span kind
			if spanKind, ok := trace["SpanKind"].(string); ok {
				exception["span_kind"] = spanKind
			}

			// Extract duration
			if duration, ok := trace["Duration"].(float64); ok {
				exception["duration_ms"] = duration / 1000000 // Convert nanoseconds to milliseconds
			}

			// Extract status
			if status, ok := trace["StatusCode"].(string); ok {
				exception["status_code"] = status
			}

			exceptions = append(exceptions, exception)
		}

		// Format response
		responseData := map[string]interface{}{
			"exceptions": exceptions,
			"count":      len(exceptions),
			"start_time": startTime.Format("2006-01-02T15:04:05Z"),
			"end_time":   endTime.Format("2006-01-02T15:04:05Z"),
		}

		jsonData, err := json.Marshal(responseData)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug)
		dashboardURL := dlBuilder.BuildExceptionsLink(startMs, endMs, args.ServiceName, "")

		return &mcp.CallToolResult{
			Meta: deeplink.ToMeta(dashboardURL),
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: string(jsonData),
				},
			},
		}, nil, nil
	}
}
