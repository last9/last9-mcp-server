package traces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetTracesDescription provides the description for the get traces tool
const GetTracesDescription = `Retrieve traces from Last9 by trace ID or service name.

This tool allows you to get specific traces either by providing a trace ID for a single trace,
or by providing a service name to get all traces for that service within a time range.

Parameters:
- trace_id: (Optional) Specific trace ID to retrieve. Cannot be used with service_name.
- service_name: (Optional) Name of service to get traces for. Cannot be used with trace_id.
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes
- start_time_iso: (Optional) Start time in ISO format (YYYY-MM-DD HH:MM:SS)
- end_time_iso: (Optional) End time in ISO format (YYYY-MM-DD HH:MM:SS)
- limit: (Optional) Maximum number of traces to return. Default: 10
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.

Examples:
1. trace_id="abc123def456" - retrieves the specific trace
2. service_name="payment-service" + lookback_minutes=30 - gets all payment service traces from last 30 minutes

Returns trace data including trace IDs, spans, duration, timestamps, and status information.`

// GetTracesArgs defines the input structure for getting traces
type GetTracesArgs struct {
	TraceID         string  `json:"trace_id,omitempty" jsonschema:"Specific trace ID to retrieve"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Name of service to get traces for"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 60, range: 1-1440)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to now - lookback_minutes."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in ISO format (YYYY-MM-DD HH:MM:SS). Leave empty to default to current time."`
	Limit           float64 `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (default: 10, range: 1-100)"`
	Env             string  `json:"env,omitempty" jsonschema:"Environment to filter by. Empty string if environment is unknown."`
}

// GetTracesQueryParams holds the parsed and validated parameters
type GetTracesQueryParams struct {
	TraceID         string
	ServiceName     string
	LookbackMinutes int
	Region          string
	Limit           int
	Env             string
}

// validateGetTracesArgs validates the input arguments
func validateGetTracesArgs(args GetTracesArgs) error {
	// Exactly one of trace_id or service_name must be provided
	if args.TraceID == "" && args.ServiceName == "" {
		return errors.New("either trace_id or service_name must be provided")
	}

	if args.TraceID != "" && args.ServiceName != "" {
		return errors.New("cannot specify both trace_id and service_name - use only one")
	}

	// Validate limits
	if args.LookbackMinutes > 0 && (args.LookbackMinutes < 1 || args.LookbackMinutes > 1440) {
		return errors.New("lookback_minutes must be between 1 and 1440 (24 hours)")
	}

	if args.Limit > 0 && (args.Limit < 1 || args.Limit > 100) {
		return errors.New("limit must be between 1 and 100")
	}

	return nil
}

// parseGetTracesParams extracts and validates parameters from input struct
func parseGetTracesParams(args GetTracesArgs, cfg models.Config) (*GetTracesQueryParams, error) {
	// Validate arguments
	if err := validateGetTracesArgs(args); err != nil {
		return nil, err
	}

	// Parse parameters with defaults
	queryParams := &GetTracesQueryParams{
		TraceID:         args.TraceID,
		ServiceName:     args.ServiceName,
		LookbackMinutes: LookbackMinutesDefault,
		Region:          utils.GetDefaultRegion(cfg.BaseURL),
		Limit:           LimitDefault,
		Env:             args.Env,
	}

	// Override defaults with provided values
	if args.LookbackMinutes != 0 {
		queryParams.LookbackMinutes = int(args.LookbackMinutes)
	}
	if args.Limit != 0 {
		queryParams.Limit = int(args.Limit)
	}

	return queryParams, nil
}

// buildGetTracesFilters creates the filter conditions for the trace query
func buildGetTracesFilters(params *GetTracesQueryParams) []map[string]interface{} {
	var filters []map[string]interface{}

	// Filter by trace ID if provided
	if params.TraceID != "" {
		filters = append(filters, map[string]interface{}{
			"$eq": []interface{}{"TraceId", params.TraceID},
		})
	}

	// Filter by service name if provided
	if params.ServiceName != "" {
		filters = append(filters, map[string]interface{}{
			"$eq": []interface{}{"ServiceName", params.ServiceName},
		})
	}

	// Add environment filter if provided
	if params.Env != "" {
		filters = append(filters, map[string]interface{}{
			"$eq": []interface{}{"resource.attributes.deployment.environment", params.Env},
		})
	}

	return filters
}

// buildGetTracesRequestURL constructs the API endpoint URL with query parameters
func buildGetTracesRequestURL(cfg models.Config, params *GetTracesQueryParams, startTime, endTime int64) (*url.URL, error) {
	u, err := url.Parse(cfg.APIBaseURL + "/cat/api/traces/v2/query_range/json")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", params.Region)
	q.Set("start", strconv.FormatInt(startTime, 10))
	q.Set("end", strconv.FormatInt(endTime, 10))
	q.Set("limit", strconv.Itoa(params.Limit))
	q.Set("order", "Duration")
	q.Set("direction", "backward")
	u.RawQuery = q.Encode()

	return u, nil
}

// GetTracesHandler creates a handler for getting traces by ID or service name
func GetTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetTracesArgs) (*mcp.CallToolResult, any, error) {
		// Parse and validate parameters
		queryParams, err := parseGetTracesParams(args, cfg)
		if err != nil {
			return nil, nil, err
		}

		// Prepare arguments map for GetTimeRange function
		arguments := make(map[string]interface{})
		if args.StartTimeISO != "" {
			arguments["start_time_iso"] = args.StartTimeISO
		}
		if args.EndTimeISO != "" {
			arguments["end_time_iso"] = args.EndTimeISO
		}

		// Get time range
		startTime, endTime, err := utils.GetTimeRange(arguments, queryParams.LookbackMinutes)
		if err != nil {
			return nil, nil, err
		}

		// Build request URL
		requestURL, err := buildGetTracesRequestURL(cfg, queryParams, startTime.Unix(), endTime.Unix())
		if err != nil {
			return nil, nil, err
		}

		// Build filters
		filters := buildGetTracesFilters(queryParams)

		// Create HTTP request using existing pattern
		httpReq, err := createTraceRequest(ctx, requestURL, filters, cfg)
		if err != nil {
			return nil, nil, err
		}

		// Execute request
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("API request failed with status %d: %s",
				resp.StatusCode, string(body))
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// Transform raw response to structured TraceQueryResponse (reusing existing function)
		traceResponse := transformToTraceQueryResponse(result)

		// Add context about the query type
		if queryParams.TraceID != "" {
			traceResponse.Message = fmt.Sprintf("Retrieved trace data for trace ID: %s", queryParams.TraceID)
		} else {
			traceResponse.Message = fmt.Sprintf("Retrieved %d traces for service: %s", len(traceResponse.Data), queryParams.ServiceName)
		}

		jsonData, err := json.Marshal(traceResponse)
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
