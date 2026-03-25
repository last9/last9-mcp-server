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

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ServiceLookbackMinutesDefault = 60
	TraceIDLookbackMinutesDefault = 4320
	LimitDefault                  = 10
)

// GetServiceTracesDescription provides the description for the service traces tool
const GetServiceTracesDescription = `Retrieve traces from Last9 by trace ID or service name.

This tool allows you to get specific traces either by providing a trace ID for a single trace,
or by providing a service name to get all traces for that service within a time range.
Prefer this tool over ` + "`get_traces`" + ` whenever you have an exact ` + "`trace_id`" + `.

Parameters:
- trace_id: (Optional) Specific trace ID to retrieve. Cannot be used with service_name.
- service_name: (Optional) Name of service to get traces for. Cannot be used with trace_id.
- lookback_minutes: (Optional) Number of minutes to look back from now. Default: 4320 minutes for trace_id lookups, 60 minutes for service_name lookups
- start_time_iso: (Optional) Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z)
- end_time_iso: (Optional) End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z)
- limit: (Optional) Maximum number of traces to return. Default: 10
- env: (Optional) Environment to filter by. Use "get_service_environments" tool to get available environments.

Time format rules:
- Prefer lookback_minutes for relative windows.
- Use start_time_iso/end_time_iso for absolute windows.
- Legacy format YYYY-MM-DD HH:MM:SS is accepted only for compatibility.
- If both lookback_minutes and absolute times are provided, absolute times take precedence.

Examples:
1. trace_id="abc123def456" - retrieves the specific trace
2. service_name="payment-service" + lookback_minutes=30 - gets all payment service traces from last 30 minutes

If a trace_id lookup returns no data, ask the user for a specific time window and retry with
start_time_iso/end_time_iso or a larger explicit lookback_minutes.

Returns trace data including trace IDs, spans, duration, timestamps, and status information.`

// GetServiceTracesArgs defines the input structure for getting traces by service or ID
type GetServiceTracesArgs struct {
	TraceID         string  `json:"trace_id,omitempty" jsonschema:"Specific trace ID to retrieve"`
	ServiceName     string  `json:"service_name,omitempty" jsonschema:"Name of service to get traces for"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Number of minutes to look back from now (default: 4320 for trace_id, 60 for service_name, minimum: 1)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339/ISO8601 format (e.g. 2026-02-09T15:04:05Z). Leave empty to default to now - lookback_minutes."`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339/ISO8601 format (e.g. 2026-02-09T16:04:05Z). Leave empty to default to current time."`
	Limit           float64 `json:"limit,omitempty" jsonschema:"Maximum number of traces to return (optional, default: 10)"`
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

// TraceQueryRequest represents the request structure for trace queries
type TraceQueryRequest struct {
	Pipeline []QueryStep `json:"pipeline"`
}

// QueryStep represents a single step in the trace query
type QueryStep struct {
	Query map[string]interface{} `json:"query"`
	Type  string                 `json:"type"`
}

// TraceData represents structured trace data
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

// TraceQueryResponse represents the structured response
type TraceQueryResponse struct {
	Data    []TraceData `json:"data"`
	Success bool        `json:"success"`
	Message string      `json:"message"`
}

// TraceDetailsResponse represents the response returned by the dedicated trace details endpoint.
type TraceDetailsResponse struct {
	Traces []TraceDetailsSpan `json:"traces"`
}

// TraceDetailsSpan represents a single span from the dedicated trace details endpoint.
type TraceDetailsSpan struct {
	Timestamp          string            `json:"Timestamp"`
	TraceID            string            `json:"TraceId"`
	SpanID             string            `json:"SpanId"`
	TraceState         string            `json:"TraceState"`
	SpanName           string            `json:"SpanName"`
	SpanKind           string            `json:"SpanKind"`
	ServiceName        string            `json:"ServiceName"`
	ResourceAttributes map[string]string `json:"ResourceAttributes"`
	Duration           int64             `json:"Duration"`
	StatusCode         string            `json:"StatusCode"`
}

// validateGetServiceTracesArgs validates the input arguments
func validateGetServiceTracesArgs(args GetServiceTracesArgs) error {
	// Exactly one of trace_id or service_name must be provided
	if args.TraceID == "" && args.ServiceName == "" {
		return errors.New("either trace_id or service_name must be provided")
	}

	if args.TraceID != "" && args.ServiceName != "" {
		return errors.New("cannot specify both trace_id and service_name - use only one")
	}

	// Validate lookback only. Limit is optional and forwarded as provided.
	if args.LookbackMinutes != 0 && args.LookbackMinutes < 1 {
		return errors.New("lookback_minutes must be at least 1")
	}

	return nil
}

// parseGetServiceTraceParams extracts and validates parameters from input struct
func parseGetServiceTraceParams(args GetServiceTracesArgs, cfg models.Config) (*GetTracesQueryParams, error) {
	// Validate arguments
	if err := validateGetServiceTracesArgs(args); err != nil {
		return nil, err
	}

	// Parse parameters with defaults
	queryParams := &GetTracesQueryParams{
		TraceID:         args.TraceID,
		ServiceName:     args.ServiceName,
		LookbackMinutes: ServiceLookbackMinutesDefault,
		Region:          cfg.Region,
		Limit:           LimitDefault,
		Env:             args.Env,
	}

	if args.TraceID != "" {
		queryParams.LookbackMinutes = TraceIDLookbackMinutesDefault
	}

	// Override defaults with provided values
	if args.LookbackMinutes != 0 {
		queryParams.LookbackMinutes = int(args.LookbackMinutes)
	}
	if args.Limit >= 1 {
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
	u, err := url.Parse(cfg.APIBaseURL + constants.EndpointTracesQueryRange)
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

// buildTraceDetailsRequestURL constructs the dedicated trace details API URL for an exact trace lookup.
func buildTraceDetailsRequestURL(cfg models.Config, params *GetTracesQueryParams, startTime, endTime int64) (*url.URL, error) {
	u, err := url.Parse(cfg.APIBaseURL + fmt.Sprintf(constants.EndpointTraceDetails, url.PathEscape(params.TraceID)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("region", params.Region)
	q.Set("start", strconv.FormatInt(startTime, 10))
	q.Set("end", strconv.FormatInt(endTime, 10))
	q.Set("limit", strconv.Itoa(params.Limit))
	u.RawQuery = q.Encode()

	return u, nil
}

// GetServiceTracesHandler creates a handler for getting traces by service or ID
func GetServiceTracesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceTracesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetServiceTracesArgs) (*mcp.CallToolResult, any, error) {
		// Parse and validate parameters
		queryParams, err := parseGetServiceTraceParams(args, cfg)
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

		traceResponse, err := fetchServiceTraceResponse(ctx, client, cfg, queryParams, startTime.Unix(), endTime.Unix())
		if err != nil {
			return nil, nil, err
		}

		// Add context about the query type
		if queryParams.TraceID != "" {
			if len(traceResponse.Data) == 0 {
				traceResponse.Message = fmt.Sprintf(
					"No trace data found for trace ID: %s in the searched time window. Ask the user for a specific time window and retry with start_time_iso/end_time_iso or an explicit lookback_minutes.",
					queryParams.TraceID,
				)
			} else {
				traceResponse.Message = fmt.Sprintf("Retrieved trace data for trace ID: %s", queryParams.TraceID)
			}
		} else {
			traceResponse.Message = fmt.Sprintf("Retrieved %d traces for service: %s", len(traceResponse.Data), queryParams.ServiceName)
		}

		jsonData, err := json.Marshal(traceResponse)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Build deep link URL
		dlBuilder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
		// Build pipeline from filters
		pipeline := []map[string]interface{}{
			{
				"type":  "filter",
				"query": map[string]interface{}{"$and": buildGetTracesFilters(queryParams)},
			},
		}
		dashboardURL := dlBuilder.BuildTracesLink(startTime.UnixMilli(), endTime.UnixMilli(), pipeline, queryParams.TraceID, "")

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

func fetchServiceTraceResponse(ctx context.Context, client *http.Client, cfg models.Config, params *GetTracesQueryParams, startTime, endTime int64) (TraceQueryResponse, error) {
	if params.TraceID != "" {
		return fetchTraceDetailsResponse(ctx, client, cfg, params, startTime, endTime)
	}
	return fetchServiceQueryRangeResponse(ctx, client, cfg, params, startTime, endTime)
}

func fetchServiceQueryRangeResponse(ctx context.Context, client *http.Client, cfg models.Config, params *GetTracesQueryParams, startTime, endTime int64) (TraceQueryResponse, error) {
	requestURL, err := buildGetTracesRequestURL(cfg, params, startTime, endTime)
	if err != nil {
		return TraceQueryResponse{}, err
	}

	filters := buildGetTracesFilters(params)

	httpReq, err := createTraceRequest(ctx, requestURL, filters, cfg)
	if err != nil {
		return TraceQueryResponse{}, err
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return TraceQueryResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyStr := readTruncatedResponseBody(resp.Body)
		return TraceQueryResponse{}, fmt.Errorf(
			"API request failed with status %d (endpoint: %s%s). Response: %s",
			resp.StatusCode,
			cfg.APIBaseURL,
			constants.EndpointTracesQueryRange,
			bodyStr,
		)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return TraceQueryResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return transformToTraceQueryResponse(result), nil
}

func fetchTraceDetailsResponse(ctx context.Context, client *http.Client, cfg models.Config, params *GetTracesQueryParams, startTime, endTime int64) (TraceQueryResponse, error) {
	requestURL, err := buildTraceDetailsRequestURL(cfg, params, startTime, endTime)
	if err != nil {
		return TraceQueryResponse{}, err
	}

	httpReq, err := createTraceDetailsRequest(ctx, requestURL, cfg)
	if err != nil {
		return TraceQueryResponse{}, err
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return TraceQueryResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyStr := readTruncatedResponseBody(resp.Body)
		return TraceQueryResponse{}, fmt.Errorf(
			"API request failed with status %d (endpoint: %s%s). Response: %s",
			resp.StatusCode,
			cfg.APIBaseURL,
			fmt.Sprintf(constants.EndpointTraceDetails, params.TraceID),
			bodyStr,
		)
	}

	var result TraceDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return TraceQueryResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return transformTraceDetailsToTraceQueryResponse(result, params.Env), nil
}

// createTraceRequest builds an HTTP POST request for the trace API with authentication
func createTraceRequest(ctx context.Context, requestURL *url.URL, filters []map[string]interface{}, cfg models.Config) (*http.Request, error) {
	// Build the JSON query pipeline for the API
	queryRequest := TraceQueryRequest{
		Pipeline: []QueryStep{{
			Query: map[string]interface{}{"$and": filters},
			Type:  "filter",
		}},
	}

	// Marshal the request payload
	payloadBytes, err := json.Marshal(queryRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query payload: %w", err)
	}

	// Create the HTTP POST request
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL.String(), bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set required headers
	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	req.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)

	// Add authentication - ensure Bearer prefix
	accessToken := cfg.TokenManager.GetAccessToken(ctx)

	if !strings.HasPrefix(accessToken, constants.BearerPrefix) {
		accessToken = constants.BearerPrefix + accessToken
	}
	req.Header.Set(constants.HeaderXLast9APIToken, accessToken)

	return req, nil
}

// createTraceDetailsRequest builds an authenticated GET request for the dedicated trace details API.
func createTraceDetailsRequest(ctx context.Context, requestURL *url.URL, cfg models.Config) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)

	accessToken := cfg.TokenManager.GetAccessToken(ctx)
	if !strings.HasPrefix(accessToken, constants.BearerPrefix) {
		accessToken = constants.BearerPrefix + accessToken
	}
	req.Header.Set(constants.HeaderXLast9APIToken, accessToken)

	return req, nil
}

// transformToTraceQueryResponse converts the raw API response into structured trace data
func transformToTraceQueryResponse(rawResult map[string]interface{}) TraceQueryResponse {
	response := TraceQueryResponse{
		Success: true,
		Message: "Traces retrieved successfully",
		Data:    []TraceData{},
	}

	// Navigate through the API response structure
	data, ok := rawResult["data"].(map[string]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid API response: missing data field"
		return response
	}

	result, ok := data["result"].([]interface{})
	if !ok {
		response.Success = false
		response.Message = "Invalid API response: missing result array"
		return response
	}

	// Convert each trace item to structured format
	for _, item := range result {
		traceItem, ok := item.(map[string]interface{})
		if !ok {
			continue // Skip malformed items
		}

		traceData := TraceData{
			TraceID:     extractString(traceItem, "TraceId"),
			SpanID:      extractString(traceItem, "SpanId"),
			SpanKind:    extractString(traceItem, "SpanKind"),
			SpanName:    extractString(traceItem, "SpanName"),
			ServiceName: extractString(traceItem, "ServiceName"),
			Duration:    extractInt64(traceItem, "Duration"),
			Timestamp:   parseTimestampToUnix(extractString(traceItem, "Timestamp")),
			TraceState:  extractString(traceItem, "TraceState"),
			StatusCode:  extractString(traceItem, "StatusCode"),
		}

		response.Data = append(response.Data, traceData)
	}

	return response
}

// transformTraceDetailsToTraceQueryResponse flattens dedicated trace details spans into the tool's stable response shape.
func transformTraceDetailsToTraceQueryResponse(rawResult TraceDetailsResponse, env string) TraceQueryResponse {
	response := TraceQueryResponse{
		Success: true,
		Message: "Traces retrieved successfully",
		Data:    []TraceData{},
	}

	for _, span := range rawResult.Traces {
		if !traceDetailsMatchesEnv(span, env) {
			continue
		}

		response.Data = append(response.Data, TraceData{
			TraceID:     span.TraceID,
			SpanID:      span.SpanID,
			SpanKind:    span.SpanKind,
			SpanName:    span.SpanName,
			ServiceName: span.ServiceName,
			Duration:    span.Duration,
			Timestamp:   parseTimestampToUnix(span.Timestamp),
			TraceState:  span.TraceState,
			StatusCode:  span.StatusCode,
		})
	}

	return response
}

func traceDetailsMatchesEnv(span TraceDetailsSpan, env string) bool {
	if env == "" {
		return true
	}

	if span.ResourceAttributes == nil {
		return false
	}

	resourceEnv, ok := span.ResourceAttributes["deployment.environment"]
	if ok {
		return resourceEnv == env
	}

	resourceEnv, ok = span.ResourceAttributes["deployment_environment"]
	if ok {
		return resourceEnv == env
	}

	return false
}

func readTruncatedResponseBody(body io.Reader) string {
	respBody, _ := io.ReadAll(body)
	bodyStr := string(respBody)
	if len(bodyStr) > 100 {
		return bodyStr[:100] + "... (truncated)"
	}
	return bodyStr
}

// Helper function to safely extract string values from map
func extractString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

// Helper function to safely extract int64 values from map
func extractInt64(m map[string]interface{}, key string) int64 {
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

// Helper function to parse ISO timestamp strings to Unix timestamp
func parseTimestampToUnix(timestamp string) int64 {
	if timestamp == "" {
		return 0
	}

	// Try parsing with nanoseconds first (RFC3339Nano format)
	if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
		return t.Unix()
	}

	// Fallback to standard RFC3339 format
	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return t.Unix()
	}

	return 0
}
