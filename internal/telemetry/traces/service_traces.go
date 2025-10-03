package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	LookbackMinutesDefault = 60
	LimitDefault           = 10
	OrderDefault           = "Duration"
	DirectionDefault       = "backward"
)

// GetServiceTracesDescription provides the description for the service traces tool
const GetServiceTracesDescription = `Query traces for a specific service with filtering options for span kinds, status codes, and other trace attributes.

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
- service_name: (Required) Name of the service to get traces for
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 60 minutes
- limit: (Optional) Maximum number of traces to return. Default: 10
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.
- span_kind: (Optional) Array of span kinds to filter by
- span_name: (Optional) Filter by specific span name
- status_code: (Optional) Array of status codes to filter by
- order: (Optional) Field to order traces by. Default: "Duration"
- direction: (Optional) Sort direction. Default: "backward"

Returns a list of trace data including trace IDs, spans, duration, timestamps, and status information.`

// TraceQueryRequest represents the request structure for trace queries
type TraceQueryRequest struct {
	Pipeline []QueryStep `json:"pipeline"`
}

// QueryStep represents a single step in the trace query
type QueryStep struct {
	Query map[string]interface{} `json:"query"`
	Type  string                 `json:"type"`
}

// TraceQueryParams holds all the parameters for a trace query
type TraceQueryParams struct {
	ServiceName     string
	LookbackMinutes int
	Region          string
	Limit           int
	Order           string
	Direction       string
	SpanKinds       []string
	SpanName        string
	StatusCodes     []string
}

type TraceData struct {
	TraceID     string `json:"trace_id"`
	SpanID      string `json:"span_id"`
	SpanKind    string `json:"span_kind"`
	SpanName    string `json:"span_name"`
	ServiceName string `json:"service_name"`
	Duration    int64  `json:"duration"`
	Timestamp   int64  `json:"timestamp"`
	TraceState  string `json:"trace_state"`
	StatusCode  string `json:"status_code"`
}

type TraceQueryResponse struct {
	Data    []TraceData `json:"data"`
	Success bool        `json:"success"`
	Message string      `json:"message"`
}

// Span kind constants
const (
	SpanKindServer   = "SPAN_KIND_SERVER"
	SpanKindClient   = "SPAN_KIND_CLIENT"
	SpanKindInternal = "SPAN_KIND_INTERNAL"
	SpanKindConsumer = "SPAN_KIND_CONSUMER"
	SpanKindProducer = "SPAN_KIND_PRODUCER"
)

// Status code constants
const (
	StatusCodeUnset = "STATUS_CODE_UNSET"
	StatusCodeError = "STATUS_CODE_ERROR"
	StatusCodeOK    = "STATUS_CODE_OK"
)

// spanKindMapping maps user-friendly terms to span kind constants
var spanKindMapping = map[string]string{
	"server":   SpanKindServer,
	"client":   SpanKindClient,
	"internal": SpanKindInternal,
	"consumer": SpanKindConsumer,
	"producer": SpanKindProducer,
	// Also support the full constants directly
	"span_kind_server":   SpanKindServer,
	"span_kind_client":   SpanKindClient,
	"span_kind_internal": SpanKindInternal,
	"span_kind_consumer": SpanKindConsumer,
	"span_kind_producer": SpanKindProducer,
}

// statusCodeMapping maps user-friendly terms to status code constants
var statusCodeMapping = map[string]string{
	"unset":   StatusCodeUnset,
	"error":   StatusCodeError,
	"ok":      StatusCodeOK,
	"success": StatusCodeOK,
	// Also support the full constants directly
	"status_code_unset": StatusCodeUnset,
	"status_code_error": StatusCodeError,
	"status_code_ok":    StatusCodeOK,
}

// GetServiceTracesArgs defines the input structure for getting service traces
type GetServiceTracesArgs struct {
	ServiceName       string   `json:"service_name" jsonschema:"Name of the service to get traces for (required)"`
	LookbackMinutes   float64  `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from current time (default: 60, range: 1-10080)"`
	StartTimeISO      string   `json:"start_time_iso,omitempty" jsonschema:"Start time in ISO8601 format (e.g. 2024-06-01T12:00:00Z)"`
	EndTimeISO        string   `json:"end_time_iso,omitempty" jsonschema:"End time in ISO8601 format (e.g. 2024-06-01T13:00:00Z)"`
	Limit             float64  `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (default: 10, range: 1-1000)"`
	Order             string   `json:"order,omitempty" jsonschema:"Field to order traces by (default: Duration, options: Duration, Timestamp)"`
	Direction         string   `json:"direction,omitempty" jsonschema:"Sort direction (default: backward, options: forward, backward)"`
	SpanKind          []string `json:"span_kind,omitempty" jsonschema:"Filter by span kinds (e.g. [\"server\", \"client\"])"`
	SpanName          string   `json:"span_name,omitempty" jsonschema:"Filter by span name (e.g. user_service)"`
	StatusCode        []string `json:"status_code,omitempty" jsonschema:"Filter by status codes (e.g. [\"ok\", \"error\"])"`
}

// parseTraceQueryParams extracts and validates parameters from input struct
func parseTraceQueryParams(args GetServiceTracesArgs, cfg models.Config) (*TraceQueryParams, error) {
	// Required parameter
	if args.ServiceName == "" {
		return nil, errors.New("service_name is required and cannot be empty")
	}

	// Parse parameters with defaults
	queryParams := &TraceQueryParams{
		ServiceName:     args.ServiceName,
		LookbackMinutes: LookbackMinutesDefault,
		Region:          utils.GetDefaultRegion(cfg.BaseURL),
		Limit:           LimitDefault,
		Order:           OrderDefault,
		Direction:       DirectionDefault,
	}

	// Override defaults with provided values
	if args.LookbackMinutes != 0 {
		queryParams.LookbackMinutes = int(args.LookbackMinutes)
	}
	if args.Limit != 0 {
		queryParams.Limit = int(args.Limit)
	}
	if args.Order != "" {
		queryParams.Order = args.Order
	}
	if args.Direction != "" {
		queryParams.Direction = args.Direction
	}
	if args.SpanName != "" {
		queryParams.SpanName = args.SpanName
	}

	// Parse array parameters with mapping
	queryParams.SpanKinds = mapValues(args.SpanKind, spanKindMapping)
	queryParams.StatusCodes = mapValues(args.StatusCode, statusCodeMapping)

	return queryParams, nil
}


// mapValues converts user-friendly terms to constants using the provided mapping
func mapValues(userValues []string, mapping map[string]string) []string {
	var mapped []string
	for _, value := range userValues {
		if constant, exists := mapping[strings.ToLower(value)]; exists {
			mapped = append(mapped, constant)
		} else {
			// If no mapping found, use the original value (might already be a constant)
			mapped = append(mapped, value)
		}
	}
	return mapped
}

// buildTraceFilters creates the filter conditions for the trace query
func buildTraceFilters(params *TraceQueryParams) []map[string]interface{} {
	filters := []map[string]interface{}{
		{"$eq": []interface{}{"ServiceName", params.ServiceName}},
	}

	// Add span kind filters
	if len(params.SpanKinds) > 0 {
		filters = append(filters, utils.BuildOrFilter("SpanKind", params.SpanKinds))
	}

	// Add span name filter
	if params.SpanName != "" {
		filters = append(filters, map[string]interface{}{
			"$eq": []interface{}{"SpanName", params.SpanName},
		})
	}

	// Add status code filters
	if len(params.StatusCodes) > 0 {
		filters = append(filters, utils.BuildOrFilter("StatusCode", params.StatusCodes))
	}

	return filters
}

// buildRequestURL constructs the API endpoint URL with query parameters
func buildRequestURL(cfg models.Config, params *TraceQueryParams, startTime, endTime int64) (*url.URL, error) {
	u, err := url.Parse(cfg.APIBaseURL + "/cat/api/traces/v2/query_range/json")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", params.Region)
	q.Set("start", strconv.FormatInt(startTime, 10))
	q.Set("end", strconv.FormatInt(endTime, 10))
	q.Set("limit", strconv.Itoa(params.Limit))
	q.Set("order", params.Order)
	q.Set("direction", params.Direction)
	u.RawQuery = q.Encode()

	return u, nil
}

// createTraceRequest builds the HTTP request with proper headers and payload
func createTraceRequest(ctx context.Context, requestURL *url.URL, filters []map[string]interface{}, cfg models.Config) (*http.Request, error) {
	// Create query request payload
	queryRequest := TraceQueryRequest{
		Pipeline: []QueryStep{{
			Query: map[string]interface{}{"$and": filters},
			Type:  "filter",
		}},
	}

	payloadBytes, err := json.Marshal(queryRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL.String(), bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// Handle Bearer token authentication
	accessToken := cfg.AccessToken
	if !strings.HasPrefix(accessToken, "Bearer ") {
		accessToken = "Bearer " + accessToken
	}
	req.Header.Set("Authorization", accessToken)
	req.Header.Set("X-LAST9-API-TOKEN", accessToken)

	return req, nil
}

// Helper functions for safe type conversion
func getStringValue(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getInt64Value(m map[string]interface{}, key string) int64 {
	switch val := m[key].(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	default:
		return 0
	}
}

func parseTimestamp(timestamp string) int64 {
	if timestamp == "" {
		return 0
	}

	// Parse RFC3339 timestamp format (e.g., "2025-08-27T05:50:02.47609145Z")
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		// If parsing fails, try without nanoseconds
		t, err = time.Parse(time.RFC3339, timestamp)
		if err != nil {
			return 0
		}
	}

	return t.Unix()
}

// transformToTraceQueryResponse converts raw API response to structured TraceQueryResponse
func transformToTraceQueryResponse(rawResult map[string]interface{}) TraceQueryResponse {
	response := TraceQueryResponse{
		Success: true,
		Message: "Traces retrieved successfully",
		Data:    []TraceData{},
	}

	// Check if the response has the expected structure
	data, ok := rawResult["data"].(map[string]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid response structure: missing data field"
		return response
	}

	result, ok := data["result"].([]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid response structure: missing result array"
		return response
	}

	// Transform each trace item
	for _, item := range result {
		traceItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		traceData := TraceData{
			TraceID:     getStringValue(traceItem, "TraceId"),
			SpanID:      getStringValue(traceItem, "SpanId"),
			SpanKind:    getStringValue(traceItem, "SpanKind"),
			SpanName:    getStringValue(traceItem, "SpanName"),
			ServiceName: getStringValue(traceItem, "ServiceName"),
			Duration:    getInt64Value(traceItem, "Duration"),
			Timestamp:   parseTimestamp(getStringValue(traceItem, "Timestamp")),
			TraceState:  getStringValue(traceItem, "TraceState"),
			StatusCode:  getStringValue(traceItem, "StatusCode"),
		}

		response.Data = append(response.Data, traceData)
	}

	return response
}

// GetServiceTracesHandler creates a handler for querying service traces
func GetServiceTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetServiceTracesArgs) (*mcp.CallToolResult, any, error) {
		// Parse and validate parameters
		queryParams, err := parseTraceQueryParams(args, cfg)
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
		requestURL, err := buildRequestURL(cfg, queryParams, startTime.Unix(), endTime.Unix())
		if err != nil {
			return nil, nil, err
		}

		// Build filters
		filters := buildTraceFilters(queryParams)

		// Create HTTP request
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

		// Transform raw response to structured TraceQueryResponse
		traceResponse := transformToTraceQueryResponse(result)

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
